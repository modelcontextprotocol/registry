#!/usr/bin/env python3
"""Create/update GCP backup alerts using the current gcloud identity.

This manages only its named log metric and labelled alert policies. It never
changes the database, backup repository, or Kubernetes workloads.
"""

import argparse
import json
import subprocess
import urllib.error
import urllib.request


METRIC = "registry_database_archive_backup_success"
MANAGED_BY = "registry-backup-monitoring"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True)
    parser.add_argument("--cluster", required=True)
    parser.add_argument("--email", required=True)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    scope = (
        'resource.type="k8s_container" '
        f'AND resource.labels.cluster_name={json.dumps(args.cluster)} '
        'AND resource.labels.namespace_name="default" '
    )
    metric = {
        "name": METRIC,
        "description": "Completed PostgreSQL archive backups and full repository checks; alerts on 25h without success.",
        "filter": scope + 'AND resource.labels.pod_name=~"^registry-database-backup-" '
        'AND textPayload:"REGISTRY_DATABASE_BACKUP_SUCCESS"',
        "metricDescriptor": {"metricKind": "DELTA", "valueType": "INT64", "unit": "1"},
    }
    documentation = {
        "mimeType": "text/markdown",
        "content": "Inspect registry-database-backup Jobs and pg-dump/restic container logs in default. Verify the postgres-custom-v1 "
        "repository with restic check. Do not reinitialize a repository or restore into production. "
        "See deploy/README.md, Database Backups, in modelcontextprotocol/registry.",
    }
    policies = [
        {
            "displayName": "Registry database backup failures",
            "userLabels": {"managed_by": MANAGED_BY, "backup_alert": "failure"},
            "conditions": [{
                "displayName": "Backup or integrity-check failure",
                "conditionMatchedLog": {"filter": scope
                    + 'AND resource.labels.pod_name=~"^registry-database-backup-" '
                    'AND textPayload=~"(?i)(REGISTRY_DATABASE_BACKUP_FAILED|ciphertext verification failed|pg_dump: error|pg_restore: error|fatal:|no such file|permission denied|checksum did NOT match)"'},
            }],
            "alertStrategy": {"notificationRateLimit": {"period": "3600s"}, "autoClose": "86400s"},
        },
        {
            "displayName": "Registry database backup missing for 25 hours",
            "userLabels": {"managed_by": MANAGED_BY, "backup_alert": "freshness"},
            "conditions": [{
                "displayName": "No successful logical backup in 25 hours",
                # sum handles explicit zero-valued log-metric samples; vector(0)
                # also catches a missing metric or a completely stopped scheduler.
                "conditionPrometheusQueryLanguage": {
                    "query": '(sum(sum_over_time({__name__="logging.googleapis.com/user/'
                    + METRIC + '",monitored_resource="k8s_container"}[25h])) or vector(0)) < 1',
                    "duration": "0s",
                    "evaluationInterval": "300s",
                },
            }],
            "alertStrategy": {"autoClose": "86400s"},
        },
    ]
    for policy in policies:
        policy.update(combiner="OR", enabled=True, documentation=documentation, severity="CRITICAL")
    if args.dry_run:
        print(json.dumps({"metric": metric, "email": args.email, "policies": policies}, indent=2))
        return

    token = subprocess.check_output(["gcloud", "auth", "print-access-token"], text=True).strip()

    def request(url, method="GET", body=None):
        req = urllib.request.Request(url, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=60) as response:
            return json.load(response)

    def list_all(url, key):
        values = []
        while True:
            response = request(url)
            values.extend(response.get(key, []))
            page = response.get("nextPageToken")
            if not page:
                return values
            # Only server-issued page tokens are appended to the same endpoint.
            import urllib.parse
            url = url.split("?")[0] + "?" + urllib.parse.urlencode({"pageToken": page})

    logging_url = f"https://logging.googleapis.com/v2/projects/{args.project}/metrics"
    try:
        request(logging_url + "/" + METRIC)
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        request(logging_url, "POST", metric)
    else:
        request(logging_url + "/" + METRIC, "PUT", metric)

    base = f"https://monitoring.googleapis.com/v3/projects/{args.project}"
    channels = list_all(base + "/notificationChannels", "notificationChannels")
    channel = next((c for c in channels if c.get("type") == "email"
                    and c.get("labels", {}).get("email_address") == args.email), None)
    if channel is None:
        channel = request(base + "/notificationChannels", "POST", {
            "type": "email", "displayName": "Registry backup alerts",
            "labels": {"email_address": args.email}, "enabled": True,
        })
    if not channel.get("enabled", True):
        raise RuntimeError("The matching notification channel is disabled; enable it before continuing")
    existing = list_all(base + "/alertPolicies", "alertPolicies")
    for policy in policies:
        policy["notificationChannels"] = [channel["name"]]
        old = next((p for p in existing if p.get("userLabels") == policy["userLabels"]), None)
        if old:
            policy["name"] = old["name"]
            result = request("https://monitoring.googleapis.com/v3/" + old["name"], "PATCH", policy)
        else:
            result = request(base + "/alertPolicies", "POST", policy)
        print(result["displayName"] + ": " + result["name"])
    print("Notification channel: " + channel["name"])


if __name__ == "__main__":
    main()
