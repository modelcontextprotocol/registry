#!/usr/bin/env python3
"""Read-only transition guard. Never print Kubernetes objects or stack values."""

import json
import re
import subprocess
import sys


CONTEXT = "gke_mcp-registry-prod_us-central1-b_mcp-registry-prod"
POOL_MAX = "MCP_REGISTRY_DATABASE_MAX_CONNECTIONS"
POOL_MIN = "MCP_REGISTRY_DATABASE_MIN_CONNECTIONS"


def check_transition(phase, deployment, pods, hpas):
    if phase not in ("off", "prepare", "enabled"):
        raise ValueError("Unknown production scaling phase")
    spec, status = deployment["spec"], deployment.get("status", {})
    count = spec.get("replicas", 1)
    if not 2 <= count <= 4:
        raise ValueError("Unexpected live replica count")
    if status.get("observedGeneration", 0) < deployment["metadata"]["generation"]:
        raise ValueError("Wait for the current deployment to be observed")
    if any(status.get(k, 0) != count for k in ("replicas", "updatedReplicas", "readyReplicas", "availableReplicas")):
        raise ValueError("Wait for the current rollout to finish")
    targets = [h for h in hpas if h.get("spec", {}).get("scaleTargetRef", {}).get("name") == "mcp-registry"]
    if phase == "off" and (targets or count != 2):
        raise ValueError("Return to prepare and settle at two replicas before restoring legacy pools")
    live = [p for p in pods if p.get("status", {}).get("phase") not in ("Succeeded", "Failed")]
    if len(live) != count or any(p["metadata"].get("deletionTimestamp") for p in live):
        raise ValueError("Wait for old and terminating app pods to disappear")
    bounded_required = phase == "enabled" or count > 2 or bool(targets)
    templates = [spec["template"]["spec"]]
    for pod in live:
        if not any(c.get("type") == "Ready" and c.get("status") == "True" for c in pod.get("status", {}).get("conditions", [])):
            raise ValueError("All app pods must be Ready")
        templates.append(pod["spec"])
    for template in templates:
        containers = [c for c in template["containers"] if c["name"] == "mcp-registry"]
        if len(containers) != 1:
            raise ValueError("Unexpected app container")
        env = {e["name"]: e.get("value") for e in containers[0].get("env", [])}
        pool = (env.get(POOL_MAX, "60"), env.get(POOL_MIN, "10"))
        if pool not in (("60", "10"), ("30", "5")):
            raise ValueError("Unexpected live connection pool settings")
        version = re.fullmatch(r"ghcr.io/modelcontextprotocol/registry:(\d+)\.(\d+)\.(\d+)", containers[0].get("image", ""))
        if bounded_required and (not version or tuple(map(int, version.groups())) < (1, 8, 7)):
            raise ValueError("All live apps must run a release with configurable pools")
        if bounded_required and pool != ("30", "5"):
            raise ValueError("Apply prepare first; every live app must use pools 30/5 before enabling HPA")


def read_json(command):
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90, check=False)
    if result.returncode:
        raise ValueError("Read-only preflight command failed; inspect privately before retrying")
    return json.loads(result.stdout)


def main():
    config = read_json(["pulumi", "config", "--json", "--stack", "gcpProd"])
    phase = config.get("mcp-registry:productionAppScaling", {}).get("value", "off")
    # Obtain only this cluster's local credentials; kubectl always uses its explicit context.
    result = subprocess.run([
        "gcloud", "container", "clusters", "get-credentials", "mcp-registry-prod",
        "--project", "mcp-registry-prod", "--zone", "us-central1-b", "--quiet",
    ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=90, check=False)
    if result.returncode:
        raise ValueError("Production cluster access unavailable")
    command = ["kubectl", "--context", CONTEXT, "--request-timeout=30s", "-n", "default", "get"]
    deployment = read_json(command + ["deployment", "mcp-registry", "-o", "json"])
    pods = read_json(command + ["pods", "-l", "app=mcp-registry", "-o", "json"])["items"]
    hpas = read_json(command + ["hpa", "-o", "json"])["items"]
    check_transition(phase, deployment, pods, hpas)
    print("Production scaling transition preflight passed")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError):
        # Kubernetes and Pulumi diagnostics may contain operational details. Do not echo them in CI.
        print("Production scaling preflight blocked; inspect the transition privately.", file=sys.stderr)
        sys.exit(1)
