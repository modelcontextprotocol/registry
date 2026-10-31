#!/usr/bin/env python3
"""Sanitized CI entry point for the bounded production scaling promotion.

Raw child output and plans exist only in an owner-only temporary directory,
outside the checkout, and are deleted on exit. Never upload these as artifacts.
The public result contains only fixed validation labels, never resource data.
"""

import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

from check_production_scaling import POOL_MAX, POOL_MIN


ANTI_AFFINITY = {"podAntiAffinity": {"requiredDuringSchedulingIgnoredDuringExecution": [{
    "topologyKey": "kubernetes.io/hostname",
    "labelSelector": {"matchLabels": {"app": "mcp-registry"}},
}]}}
HPA_SPEC = {
    "minReplicas": 2, "maxReplicas": 4,
    "scaleTargetRef": {"apiVersion": "apps/v1", "kind": "Deployment", "name": "mcp-registry"},
    "metrics": [{"type": "Resource", "resource": {"name": "cpu", "target": {"type": "Utilization", "averageUtilization": 70}}}],
    "behavior": {
        "scaleUp": {"stabilizationWindowSeconds": 0, "policies": [{"type": "Pods", "value": 2, "periodSeconds": 60}]},
        "scaleDown": {"stabilizationWindowSeconds": 600, "policies": [{"type": "Pods", "value": 1, "periodSeconds": 120}]},
    },
}
PREFIX = "urn:pulumi:gcpProd::mcp-registry-infra::"
APP = PREFIX + "kubernetes:apps/v1:Deployment::mcp-registry"
WORKERS = PREFIX + "gcp:container/nodePool:NodePool::mcp-registry-prod-nodepool"
HPA = PREFIX + "kubernetes:autoscaling/v2:HorizontalPodAutoscaler::mcp-registry-capacity"
STACK = PREFIX + "pulumi:pulumi:Stack::mcp-registry-infra-gcpProd"


class Blocked(Exception):
    """Only fixed, non-sensitive reason codes may be used as arguments."""


def require(condition, code):
    if not condition:
        raise Blocked(code)


def app_without_scaling_fields(inputs, phase, desired):
    value = copy.deepcopy(inputs)
    spec = value["spec"]
    replicas = spec["replicas"]
    require(replicas in (2, 3, 4), "app-replica-count")
    if desired and phase != "enabled":
        require(replicas == 2, "app-fixed-count")
    if phase != "enabled":
        spec.pop("replicas")
    ready = spec.pop("minReadySeconds", 0)
    require(ready in (0, 35), "app-readiness-window")
    pod = spec["template"]["spec"]
    grace = pod.pop("terminationGracePeriodSeconds", None)
    affinity = pod.pop("affinity", None)
    require(grace in (None, 30), "app-termination-window")
    require(affinity in (None, ANTI_AFFINITY), "app-existing-affinity")
    if desired:
        enabled = phase != "off"
        require(ready == (35 if enabled else 0), "app-target-readiness")
        require(affinity == (ANTI_AFFINITY if enabled else None), "app-target-affinity")
        require(grace == (30 if enabled else None), "app-target-termination")
    containers = pod["containers"]
    require(len(containers) == 1 and containers[0]["name"] == "mcp-registry", "app-container-shape")
    app = containers[0]
    cpu = app["resources"]["requests"].pop("cpu")
    require(cpu in ("800m", "1200m"), "app-existing-cpu")
    if desired:
        require(cpu == ("800m" if phase == "off" else "1200m"), "app-target-cpu")
    pool = [e for e in app["env"] if e["name"] in (POOL_MAX, POOL_MIN)]
    require(len({e["name"] for e in pool}) == len(pool), "app-duplicate-pool-env")
    require(not pool or {e["name"]: e.get("value") for e in pool} == {POOL_MAX: "30", POOL_MIN: "5"}, "app-existing-pool")
    if desired:
        require(bool(pool) == (phase != "off"), "app-target-pool")
    app["env"] = [e for e in app["env"] if e["name"] not in (POOL_MAX, POOL_MIN)]
    return value


def check_hpa(inputs):
    require(inputs["spec"] == HPA_SPEC, "hpa-spec")
    require(inputs["metadata"] == {"name": "mcp-registry", "namespace": "default"}, "hpa-metadata")
    require(set(inputs) <= {"apiVersion", "kind", "metadata", "spec"}, "hpa-extra-inputs")
    require(inputs.get("apiVersion") == "autoscaling/v2" and inputs.get("kind") == "HorizontalPodAutoscaler", "hpa-kind")


def validate_preview(preview, phase):
    require(phase in ("off", "prepare", "enabled"), "scaling-phase")
    require(preview.get("steps") and isinstance(preview.get("changeSummary"), dict), "preview-incomplete")
    require(not preview.get("maybeCorrupt"), "preview-state")
    require(not any(d.get("severity") == "error" for d in preview.get("diagnostics", [])), "preview-diagnostics")
    labels = []
    for step in preview["steps"]:
        op, urn = step["op"], step["urn"]
        if op in ("same", "refresh", "read"):
            continue
        require(not step.get("replaceReasons") and "replace" not in op, "resource-replacement")
        old, new = step.get("oldState"), step.get("newState")
        if urn == STACK:
            require(op == "update" and old and new, "stack-operation")
            continue
        require(urn in (APP, WORKERS, HPA), "resource-outside-scaling-scope")
        if old and new:
            require(old.get("provider") == new.get("provider") and old.get("parent") == new.get("parent"), "resource-provider-or-parent")
            require(old.get("protect", False) == new.get("protect", False), "resource-protection")
        if urn == APP:
            require(op == "update" and old and new, "app-operation")
            require(app_without_scaling_fields(old["inputs"], phase, False) == app_without_scaling_fields(new["inputs"], phase, True), "app-unrelated-input-change")
            labels.append("application scaling settings")
        elif urn == WORKERS:
            require(op == "update" and old and new, "worker-operation")
            before, after = copy.deepcopy(old["inputs"]), copy.deepcopy(new["inputs"])
            require(before["autoscaling"].pop("maxNodeCount") in (3, 4), "worker-existing-maximum")
            require(after["autoscaling"].pop("maxNodeCount") == (3 if phase == "off" else 4), "worker-target-maximum")
            require(before == after, "worker-unrelated-input-change")
            labels.append("worker autoscaling ceiling")
        else:
            if phase == "enabled":
                require(op in ("create", "update") and new, "hpa-operation")
                check_hpa(new["inputs"])
            else:
                require(op == "delete" and old, "hpa-removal-operation")
                check_hpa(old["inputs"])
            labels.append("bounded application autoscaler")
    return sorted(set(labels))


def quiet(command, directory, name, env, timeout=1800):
    stdout, stderr = directory / (name + ".out"), directory / (name + ".err")
    with stdout.open("wb") as out, stderr.open("wb") as err:
        result = subprocess.run(command, stdout=out, stderr=err, env=env, timeout=timeout, check=False)
    require(result.returncode == 0, name + "-failed")
    return stdout


def main(mode):
    require(mode in ("preview", "apply"), "execution-mode")
    os.umask(0o077)
    env = dict(os.environ, PULUMI_BACKEND_URL="gs://mcp-registry-prod-pulumi-state",
               PULUMI_CONFIG_PASSPHRASE_FILE=str(Path("passphrase.prod.txt").resolve()),
               PULUMI_SKIP_UPDATE_CHECK="true")
    env.pop("PULUMI_ENABLE_STREAMING_JSON_PREVIEW", None)
    with tempfile.TemporaryDirectory(prefix="registry-private-pulumi-") as temp:
        directory = Path(temp)
        quiet(["go", "build"], directory, "build", env)
        config = json.loads(quiet(["pulumi", "config", "--json", "--stack", "gcpProd", "--non-interactive"], directory, "config", env).read_text())
        phase = config.get("mcp-registry:productionAppScaling", {}).get("value", "off")
        quiet([sys.executable, "scripts/check_production_scaling.py"], directory, "preflight", env)
        plan = directory / "plan.json"
        result = quiet(["pulumi", "preview", "--refresh", "--json", "--save-plan", str(plan), "--stack", "gcpProd", "--non-interactive"], directory, "preview", env)
        labels = validate_preview(json.loads(result.read_text()), phase)
        print("Private preview validation passed: " + (", ".join(labels) if labels else "no infrastructure changes"), flush=True)
        if mode == "apply":
            # Recheck live pool/replica transitions immediately before applying the validated plan.
            quiet([sys.executable, "scripts/check_production_scaling.py"], directory, "preapply", env)
            quiet(["pulumi", "up", "--refresh", "--yes", "--skip-preview", "--plan", str(plan), "--stack", "gcpProd", "--non-interactive"], directory, "apply", env)
            print("Production apply completed; live verification remains required.")


if __name__ == "__main__":
    try:
        main(sys.argv[1] if len(sys.argv) == 2 else "")
    except Blocked as error:
        print("Production validation blocked: " + str(error), file=sys.stderr)
        sys.exit(1)
    except (ValueError, KeyError, TypeError, IndexError, OSError, subprocess.SubprocessError):
        print("Production validation blocked: private command or schema failure.", file=sys.stderr)
        sys.exit(1)
