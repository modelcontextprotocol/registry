#!/usr/bin/env python3
"""Adopt the two existing staging experiment resources before the normal apply.

Run from deploy/ with the staging passphrase supplied through Pulumi's usual
environment variables. Never fetch or print plaintext stack secrets.
"""

import json
import os
from pathlib import Path
import subprocess
import tempfile


PREFIX = "urn:pulumi:gcpStaging::mcp-registry-infra::"
RESOURCES = (
    {
        "type": "gcp:container/nodePool:NodePool",
        "name": "registry-database-nodepool",
        "id": "projects/mcp-registry-staging/locations/us-central1-b/clusters/"
        "mcp-registry-staging/nodePools/mcp-registry-staging-database",
        "provider": "pulumi:providers:gcp::gcp-explicit",
        "protect": True,
    },
    {
        "type": "kubernetes:autoscaling/v2:HorizontalPodAutoscaler",
        "name": "mcp-registry-capacity",
        "id": "default/mcp-registry",
        "provider": "pulumi:providers:kubernetes::k8s-provider",
        "protect": False,
    },
)


def pending_imports(state):
    resources = state["deployment"]["resources"]
    by_urn = {r["urn"]: r for r in resources}
    if PREFIX + "pulumi:pulumi:Stack::mcp-registry-infra-gcpStaging" not in by_urn:
        raise ValueError("Expected the existing gcpStaging stack")
    pending = []
    for expected in RESOURCES:
        urn = PREFIX + expected["type"] + "::" + expected["name"]
        provider_urn = PREFIX + expected["provider"]
        existing = by_urn.get(urn)
        if existing:
            if existing.get("id") != expected["id"]:
                raise ValueError("Unexpected identity for " + expected["name"])
            if not existing.get("provider", "").startswith(provider_urn + "::"):
                raise ValueError("Unexpected provider for " + expected["name"])
            continue
        if any(r.get("id") == expected["id"] and r["type"] == expected["type"] for r in resources):
            raise ValueError("Resource already managed under another name: " + expected["name"])
        if provider_urn not in by_urn:
            raise ValueError("Missing existing provider for " + expected["name"])
        pending.append(expected)
    return pending


def main():
    env = dict(os.environ, PULUMI_BACKEND_URL="gs://mcp-registry-staging-pulumi-state")
    with tempfile.TemporaryDirectory(prefix="staging-capacity-state-") as directory:
        state_file = Path(directory) / "state.json"
        subprocess.run([
            "pulumi", "stack", "export", "--stack", "gcpStaging",
            "--file", str(state_file), "--non-interactive",
        ], env=env, check=True)
        pending = pending_imports(json.loads(state_file.read_text()))
        for r in pending:
            subprocess.run([
                "pulumi", "import", r["type"], r["name"], r["id"],
                "--provider", "existing=" + PREFIX + r["provider"],
                "--protect=" + str(r["protect"]).lower(),
                "--stack", "gcpStaging", "--generate-code=false",
                "--non-interactive", "--suppress-outputs", "--yes",
            ], env=env, check=True)
        print(f"Staging capacity adoption complete: {len(pending)} imports")


if __name__ == "__main__":
    main()
