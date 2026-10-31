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
KUBE_PROVIDER = PREFIX + "pulumi:providers:kubernetes::k8s-provider"
COMPLETED_HOOKS = {
    PREFIX + "kubernetes:helm.sh/v3:Chart$kubernetes:batch/v1:Job::ingress-nginx/ingress-nginx-admission-create",
    PREFIX + "kubernetes:helm.sh/v3:Chart$kubernetes:batch/v1:Job::ingress-nginx/ingress-nginx-admission-patch",
}


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


def excluded_completed_hook(step, previous):
    if step.get("urn") not in COMPLETED_HOOKS or step.get("op") != "create":
        return False
    before, after = previous.get(step["urn"]), step.get("newState")
    if not before or not after:
        return False
    status = before.get("outputs", {}).get("status", {})
    return (status.get("succeeded") == 1 and bool(status.get("completionTime"))
            and before["inputs"].get("spec", {}).get("ttlSecondsAfterFinished") == 0
            and before["inputs"] == after["inputs"]
            and before.get("provider") == after.get("provider")
            and before.get("parent") == after.get("parent"))



def excluded_unchanged_rbac(step, previous):
    if step.get("op") != "update":
        return False
    before, after = previous.get(step.get("urn")), step.get("newState")
    if not before or not after:
        return False
    if before.get("type") not in (
        "kubernetes:rbac.authorization.k8s.io/v1:Role",
        "kubernetes:rbac.authorization.k8s.io/v1:ClusterRole",
    ):
        return False
    # Provider reconciliation may report a change with identical desired inputs.
    # Exclude only exact existing definitions; never apply an RBAC operation.
    return (before["inputs"] == after["inputs"]
            and before.get("provider") == after.get("provider")
            and before.get("parent") == after.get("parent")
            and before.get("protect", False) == after.get("protect", False))


def validate_preview(preview, phase, completed_hooks=None):
    require(phase in ("off", "prepare", "enabled"), "scaling-phase")
    require(preview.get("steps") and isinstance(preview.get("changeSummary"), dict), "preview-incomplete")
    require(not preview.get("maybeCorrupt"), "preview-state")
    require(not any(d.get("severity") == "error" for d in preview.get("diagnostics", [])), "preview-diagnostics")
    labels, failures = [], []
    for step in preview["steps"]:
        if completed_hooks is not None and (excluded_completed_hook(step, completed_hooks) or excluded_unchanged_rbac(step, completed_hooks)):
            continue
        try:
            label = validate_step(step, phase)
            if label:
                labels.append(label)
        except Blocked as error:
            failures.append(str(error))
    require(not failures, ",".join(sorted(set(failures))))
    return sorted(set(labels))


def validate_step(step, phase):
    op, urn = step["op"], step["urn"]
    if op in ("same", "refresh", "read"):
        return None
    require(not step.get("replaceReasons") and "replace" not in op, "resource-replacement")
    old, new = step.get("oldState"), step.get("newState")
    if urn == STACK:
        require(op == "update" and old and new, "stack-operation")
        return None
    if urn not in (APP, WORKERS, HPA):
        # Fixed categories only: never echo URNs, property values, or diagnostics.
        category = {
            "pulumi:providers:gcp": "cloud-provider",
            "pulumi:providers:kubernetes": "kubernetes-provider",
            "kubernetes:helm.sh/v3:Chart": "chart-component",
            "kubernetes:core/v1:Secret": "secret",
            "kubernetes:core/v1:ConfigMap": "configmap",
            "kubernetes:apps/v1:Deployment": "other-deployment",
            "kubernetes:apps/v1:StatefulSet": "stateful-workload",
            "kubernetes:postgresql.cnpg.io/v1:Cluster": "database",
            "gcp:container/cluster:Cluster": "cluster",
            "gcp:container/nodePool:NodePool": "other-node-pool",
            "gcp:projects/service:Service": "cloud-service",
            "gcp:projects/iAMMember:IAMMember": "cloud-iam",
            "gcp:storage/hmacKey:HmacKey": "storage-key",
            "gcp:storage/bucket:Bucket": "storage-bucket",
            "gcp:storage/bucketIAMMember:BucketIAMMember": "storage-iam",
            "kubernetes:yaml:ConfigFile": "yaml-file-component",
            "kubernetes:yaml:ConfigGroup": "yaml-group-component",
            "kubernetes:core/v1:Namespace": "namespace",
            "kubernetes:core/v1:Service": "service",
            "kubernetes:core/v1:ServiceAccount": "service-account",
            "kubernetes:batch/v1:Job": "job",
            "kubernetes:batch/v1:CronJob": "cronjob",
            "kubernetes:apps/v1:DaemonSet": "daemonset",
            "kubernetes:autoscaling/v2:HorizontalPodAutoscaler": "other-autoscaler",
            "kubernetes:networking.k8s.io/v1:Ingress": "ingress",
            "kubernetes:networking.k8s.io/v1:IngressClass": "ingress-class",
            "kubernetes:cert-manager.io/v1:ClusterIssuer": "certificate-issuer",
            "kubernetes:apiextensions.k8s.io/v1:CustomResourceDefinition": "custom-resource-definition",
            "kubernetes:rbac.authorization.k8s.io/v1:Role": "role",
            "kubernetes:rbac.authorization.k8s.io/v1:ClusterRole": "cluster-role",
            "kubernetes:rbac.authorization.k8s.io/v1:RoleBinding": "role-binding",
            "kubernetes:rbac.authorization.k8s.io/v1:ClusterRoleBinding": "cluster-role-binding",
            "kubernetes:admissionregistration.k8s.io/v1:ValidatingWebhookConfiguration": "validating-webhook",
            "kubernetes:admissionregistration.k8s.io/v1:MutatingWebhookConfiguration": "mutating-webhook",
        }.get((new or old or {}).get("type", step.get("type")), "other-resource")
        operation = op if op in ("create", "update", "delete", "discard", "import") else "other-operation"
        raise Blocked("outside-scope-" + category + "-" + operation)
    if old and new:
        require(old.get("provider") == new.get("provider") and old.get("parent") == new.get("parent"), "resource-provider-or-parent")
        require(old.get("protect", False) == new.get("protect", False), "resource-protection")
    if urn == APP:
        require(op == "update" and old and new, "app-operation")
        require(app_without_scaling_fields(old["inputs"], phase, False) == app_without_scaling_fields(new["inputs"], phase, True), "app-unrelated-input-change")
        return "application scaling settings"
    elif urn == WORKERS:
        require(op == "update" and old and new, "worker-operation")
        before, after = copy.deepcopy(old["inputs"]), copy.deepcopy(new["inputs"])
        require(before["autoscaling"].pop("maxNodeCount") in (3, 4), "worker-existing-maximum")
        require(after["autoscaling"].pop("maxNodeCount") == (3 if phase == "off" else 4), "worker-target-maximum")
        require(before == after, "worker-unrelated-input-change")
        return "worker autoscaling ceiling"
    else:
        if phase == "enabled":
            require(op in ("create", "update") and new, "hpa-operation")
            check_hpa(new["inputs"])
        else:
            require(op == "delete" and old, "hpa-removal-operation")
            check_hpa(old["inputs"])
        return "bounded application autoscaler"


def parse_preview_events(text):
    # The combined preview digest rewrites refresh output events as updates.
    # Pre-events retain the true operation: refresh is read-only; a later
    # update/create/delete pre-event describes an operation in the saved plan.
    steps, diagnostics, summary = [], [], None
    for line in text.splitlines():
        if not line.strip():
            continue
        event = json.loads(line)
        if "resourcePreEvent" in event:
            m = event["resourcePreEvent"]["metadata"]
            steps.append({"op": m["op"], "urn": m["urn"], "type": m.get("type"),
                          "oldState": m.get("old"), "newState": m.get("new"),
                          "replaceReasons": m.get("keys", [])})
        if "diagnosticEvent" in event:
            diagnostics.append(event["diagnosticEvent"])
        if "summaryEvent" in event:
            summary = event["summaryEvent"]
        require("resOpFailedEvent" not in event, "preview-resource-operation")
    require(summary is not None, "preview-missing-summary")
    return {"steps": steps, "diagnostics": diagnostics,
            "changeSummary": summary.get("resourceChanges"),
            "maybeCorrupt": summary.get("maybeCorrupt", False)}


def decode_plan(text):
    data = json.loads(text)
    if "version" in data:
        require(data["version"] == 1, "plan-version")
        data = data["plan"]
    require(isinstance(data.get("resourcePlans"), dict), "plan-schema")
    return data


def resource_kind_label(urn):
    if urn == PREFIX + "kubernetes:helm.sh/v3:Chart::k8up":
        return "backup-chart"
    for token, label in (("pulumi:pulumi:Stack", "stack"), ("kubernetes:networking.k8s.io/v1:Ingress", "ingress"), ("kubernetes:helm.sh/v3:Chart", "chart"), ("kubernetes:cert-manager.io/v1:ClusterIssuer", "certificate"), ("kubernetes:apps/v1:Deployment", "deployment"), ("kubernetes:core/v1:Service", "service")):
        if token in urn:
            return label
    return "other"


def dependency_mismatch_labels(full_graph, target_graph):
    labels = set()
    for urn, target in target_graph["resourcePlans"].items():
        full_goal = full_graph["resourcePlans"].get(urn, {}).get("goal")
        target_goal = target.get("goal")
        if full_goal and target_goal and dependency_signature(full_goal) != dependency_signature(target_goal):
            labels.add({APP: "app", WORKERS: "worker", HPA: "hpa", KUBE_PROVIDER: "kubernetes-provider"}.get(urn, "outside-target"))
    return sorted(labels)


def reconcile_plan_dependencies(full_graph, target_graph):
    result = copy.deepcopy(target_graph)
    changed = []
    identity_fields = ("type", "name", "custom", "parent", "provider", "protect", "deleteBeforeReplace",
                       "ignoreChanges", "additionalSecretOutputs", "aliases", "structuredAliases", "id", "customTimeouts")
    for urn, resource in result["resourcePlans"].items():
        full = full_graph["resourcePlans"].get(urn, {}).get("goal")
        target = resource.get("goal")
        if not full or not target or dependency_signature(full) == dependency_signature(target):
            continue
        require(urn not in (APP, WORKERS, HPA) and not target.get("type", "").startswith("pulumi:providers:"), "target-dependency-mismatch")
        require(all(full.get(k) == target.get(k) for k in identity_fields), "dependency-goal-identity-mismatch")
        # Untargeted resources retain their old inputs in a targeted preview.
        # Use the full program's dependency metadata, while retaining EVERY
        # targeted-plan input, output, operation and identity constraint.
        for field in ("dependencies", "propertyDependencies"):
            if field in full:
                target[field] = copy.deepcopy(full[field])
            else:
                target.pop(field, None)
        changed.append(urn)
    return result, changed


def dependency_signature(goal):
    return (frozenset(goal.get("dependencies") or []),
            {k: frozenset(v) for k, v in (goal.get("propertyDependencies") or {}).items() if v})


def apply_error_messages(stdout, stderr):
    messages = []
    for line in stdout.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        diagnostic = event.get("diagnosticEvent", {})
        if diagnostic.get("severity") == "error":
            messages.append(diagnostic.get("urn", "") + " " + diagnostic.get("message", ""))
    # CLI startup failures may precede the JSON event stream. Never include
    # resource event data, which can contain unrelated words or private values.
    messages.extend(line for line in stderr.splitlines() if "error" in line.lower())
    return "\n".join(messages)


def apply_failure_category(text):
    text = text.lower()
    categories = {
        "plan-constraint": ("violates plan", "plan violation", "resource plan", "not allowed by the plan"),
        "field-ownership": ("fieldmanager", "field manager", "conflict"),
        "immutable-field": ("field is immutable", "may not change"),
        "access": ("forbidden", "permission denied", "unauthorized"),
        "missing-resource": ("not found", "does not exist"),
        "dependency": ("dependency", "dependents", "untargeted"),
        "timeout": ("timeout", "timed out", "deadline exceeded"),
        "provider": ("provider plugin", "plugin version"),
        "validation": ("invalid", "validation"),
        "concurrent-operation": ("already running", "another update", "locked"),
    }
    labels = [k for k, patterns in categories.items() if any(p in text for p in patterns)]
    for kind, token in {
        "application": "kubernetes:apps/v1:deployment::mcp-registry",
        "secret": "kubernetes:core/v1:secret",
        "provider": "pulumi:providers:",
        "job": "kubernetes:batch/v1:job",
        "worker": "gcp:container/nodepool:",
        "database": "kubernetes:postgresql.cnpg.io/v1:cluster",
        "ingress": "kubernetes:networking.k8s.io/v1:ingress",
        "chart": "kubernetes:helm.sh/v3:chart",
        "certificate": "kubernetes:cert-manager.io/v1:clusterissuer",
    }.items():
        if token in text:
            labels.append("resource-" + kind)
    for field in ("inputs", "outputs", "dependencies", "provider", "metadata", "affinity", "replicas", "resources", "env", "kubeconfig"):
        if field in text:
            labels.append("field-" + field)
    return ",".join(sorted(set(labels))) or "unclassified"


def quiet(command, directory, name, env, timeout=1800):
    stdout, stderr = directory / (name + ".out"), directory / (name + ".err")
    with stdout.open("wb") as out, stderr.open("wb") as err:
        result = subprocess.run(command, stdout=out, stderr=err, env=env, timeout=timeout, check=False)
    if result.returncode and name == "apply":
        raise Blocked("apply-failed-" + apply_failure_category(apply_error_messages(stdout.read_text(errors="replace"), stderr.read_text(errors="replace"))))
    require(result.returncode == 0, name + "-failed")
    return stdout


def main(mode):
    require(mode in ("preview", "apply"), "execution-mode")
    os.umask(0o077)
    env = dict(os.environ, PULUMI_BACKEND_URL="gs://mcp-registry-prod-pulumi-state",
               PULUMI_CONFIG_PASSPHRASE_FILE=str(Path("passphrase.prod.txt").resolve()),
               PULUMI_SKIP_UPDATE_CHECK="true", PULUMI_ENABLE_STREAMING_JSON_PREVIEW="true")
    with tempfile.TemporaryDirectory(prefix="registry-private-pulumi-") as temp:
        directory = Path(temp)
        quiet(["go", "build"], directory, "build", env)
        print("Deployment program build passed.", flush=True)
        config = json.loads(quiet(["pulumi", "config", "--json", "--stack", "gcpProd", "--non-interactive"], directory, "config", env).read_text())
        phase = config.get("mcp-registry:productionAppScaling", {}).get("value", "off")
        quiet([sys.executable, "scripts/check_production_scaling.py"], directory, "preflight", env)
        print("Read-only transition preflight passed.", flush=True)
        exported = directory / "before.json"
        quiet(["pulumi", "stack", "export", "--stack", "gcpProd", "--file", str(exported), "--non-interactive"], directory, "state", env)
        previous = {r["urn"]: r for r in json.loads(exported.read_text())["deployment"]["resources"]}
        require(all(urn in previous for urn in (STACK, APP, WORKERS)), "existing-production-stack")
        full_plan = directory / "full-plan.json"
        full = quiet(["pulumi", "preview", "--refresh", "--json", "--save-plan", str(full_plan), "--stack", "gcpProd", "--non-interactive"], directory, "full-preview", env)
        validate_preview(parse_preview_events(full.read_text()), phase, completed_hooks=previous)
        print("Full change scope validation passed.", flush=True)
        # Never recreate TTL-expired setup jobs as a side effect of scaling.
        # The full preview above still blocks every other unrelated change.
        targets = ["--target", APP, "--target", WORKERS]
        if phase == "enabled" or HPA in previous:
            targets += ["--target", HPA]
        plan = directory / "plan.json"
        result = quiet(["pulumi", "preview", "--refresh", "--json", "--save-plan", str(plan), "--stack", "gcpProd", "--non-interactive"] + targets, directory, "preview", env)
        full_graph = decode_plan(full_plan.read_text())
        target_graph = decode_plan(plan.read_text())
        changed = dependency_mismatch_labels(full_graph, target_graph)
        labels = validate_preview(parse_preview_events(result.read_text()), phase)
        print("Full versus targeted dependency comparison: " + (",".join(changed) if changed else "identical"), flush=True)
        reconciled, dependency_changes = reconcile_plan_dependencies(full_graph, target_graph)
        if dependency_changes:
            confirmed_plan = directory / "confirmed-full-plan.json"
            confirmed = quiet(["pulumi", "preview", "--refresh", "--json", "--save-plan", str(confirmed_plan), "--stack", "gcpProd", "--non-interactive"], directory, "confirm-preview", env)
            validate_preview(parse_preview_events(confirmed.read_text()), phase, completed_hooks=previous)
            confirmation_graph = decode_plan(confirmed_plan.read_text())
            for urn in dependency_changes:
                require(dependency_signature(full_graph["resourcePlans"][urn]["goal"]) == dependency_signature(confirmation_graph["resourcePlans"][urn]["goal"]), "unstable-full-dependency-graph-" + resource_kind_label(urn))
            print("Untargeted dependency metadata confirmed; input and operation constraints preserved.", flush=True)
        plan_document = json.loads(plan.read_text())
        if "version" in plan_document:
            plan_document["plan"] = reconciled
        else:
            plan_document = reconciled
        apply_plan = directory / "validated-apply-plan.json"
        apply_plan.write_text(json.dumps(plan_document))
        print("Private preview validation passed: " + (", ".join(labels) if labels else "no infrastructure changes"), flush=True)
        if mode == "apply":
            # Recheck live pool/replica transitions immediately before applying the validated plan.
            quiet([sys.executable, "scripts/check_production_scaling.py"], directory, "preapply", env)
            quiet(["pulumi", "up", "--json", "--refresh", "--yes", "--skip-preview", "--plan", str(apply_plan), "--stack", "gcpProd", "--non-interactive"] + targets, directory, "apply", env)
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
