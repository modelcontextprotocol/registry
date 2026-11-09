#!/usr/bin/env python3
"""Bootstrap read-only source access, a dedicated repository-password Secret, and email alerts.

Run after the suspended restore CronJob/reader account have been deployed.
No production Kubernetes writes or repository changes. Uses existing gcloud auth.
"""
import argparse
import base64
import json
import math
import subprocess
import urllib.error
import urllib.parse
import urllib.request

PROJECT = 'mcp-registry-staging'
READER = 'registry-restore-reader@' + PROJECT + '.iam.gserviceaccount.com'
METRIC = 'registry_staging_restore_success'
MANAGED = 'registry-staging-restore'
HEARTBEATS = 'sum(sum_over_time({__name__="logging.googleapis.com/user/registry_staging_restore_watchdog",monitored_resource="k8s_container"}[1h]))'


def require_watchdog_heartbeat(response):
    rows = response.get('data', {}).get('result', [])
    if response.get('status') != 'success' or len(rows) != 1 or not math.isfinite(float(rows[0]['value'][1])) or float(rows[0]['value'][1]) < 1:
        raise RuntimeError('Watchdog heartbeat is not visible through PromQL yet; keep freshness disabled and retry after ingestion')



def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--enable-freshness', action='store_true', help='Enable only after first successful restore')
    args = parser.parse_args()
    token = subprocess.check_output(['gcloud', 'auth', 'print-access-token'], text=True, timeout=30).strip()

    def request(url, method='GET', body=None):
        req = urllib.request.Request(url, method=method,
            headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'},
            data=None if body is None else json.dumps(body).encode())
        with urllib.request.urlopen(req, timeout=45) as response:
            return json.load(response)

    def listing(url, key):
        result = []
        while True:
            data = request(url)
            result.extend(data.get(key, []))
            if not data.get('nextPageToken'):
                return result
            url = url.split('?')[0] + '?' + urllib.parse.urlencode({'pageToken': data['nextPageToken']})

    # Preserve every unrelated binding and use the IAM etag to reject concurrent changes.
    url = 'https://storage.googleapis.com/storage/v1/b/mcp-registry-prod-backups/iam'
    policy = request(url + '?optionsRequestedPolicyVersion=3')
    binding = {'role': 'roles/storage.objectViewer', 'members': ['serviceAccount:' + READER],
               'condition': {'title': 'staging-read-current-backups',
                             'expression': "resource.name == 'projects/_/buckets/mcp-registry-prod-backups' || resource.name.startsWith('projects/_/buckets/mcp-registry-prod-backups/objects/postgres-custom-v1/')"}}
    policy['version'] = 3
    matches = [b for b in policy.get('bindings', []) if b.get('condition', {}).get('title') == binding['condition']['title']]
    if matches and matches != [binding]:
        raise RuntimeError('Existing restore-reader binding differs; review before changing it')
    if not matches:
        policy.setdefault('bindings', []).append(binding)
        request(url, 'PUT', policy)

    # Separate staging-purpose password; never reuse the production write-capable HMAC key.
    k = ['kubectl', '--request-timeout=30s', '-n', 'default']
    prod = 'gke_mcp-registry-prod_us-central1-b_mcp-registry-prod'
    staging = 'gke_mcp-registry-staging_us-central1-b_mcp-registry-staging'
    password = json.loads(subprocess.check_output(k + ['--context', prod, 'get', 'secret',
                         'k8up-repo-password', '-o', 'json'], timeout=45))['data']['password']
    base64.b64decode(password, validate=True)
    secret = {'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': 'registry-staging-restore-password',
              'namespace': 'default', 'labels': {'environment': 'staging', 'app': MANAGED}},
              'type': 'Opaque', 'data': {'password': password}}
    subprocess.run(k + ['--context', staging, 'apply', '--server-side', '--field-manager=staging-restore-bootstrap', '-f', '-'],
                   input=json.dumps(secret).encode(), stdout=subprocess.DEVNULL, check=True, timeout=45)

    prod_base = 'https://monitoring.googleapis.com/v3/projects/mcp-registry-prod'
    policies = listing(prod_base + '/alertPolicies', 'alertPolicies')
    backup = next(p for p in policies if p.get('userLabels') ==
                  {'managed_by': 'registry-backup-monitoring', 'backup_alert': 'failure'})
    channels = [request('https://monitoring.googleapis.com/v3/' + n) for n in backup['notificationChannels']]
    channels = [c for c in channels if c['type'] == 'email' and c.get('enabled', True)]
    if len(channels) != 1:
        raise RuntimeError('Expected one enabled production backup email recipient')
    base = 'https://monitoring.googleapis.com/v3/projects/' + PROJECT
    existing = listing(base + '/notificationChannels', 'notificationChannels')
    channel = next((c for c in existing if c['type'] == 'email' and c.get('labels') == channels[0]['labels']), None)
    if channel is None:
        channel = request(base + '/notificationChannels', 'POST', {'type': 'email', 'labels': channels[0]['labels'],
                          'displayName': 'Registry staging restore alerts', 'enabled': True})
    if not channel.get('enabled', True):
        raise RuntimeError('Staging email channel is disabled')
    scope = 'resource.type="k8s_container" AND resource.labels.cluster_name="mcp-registry-staging" AND resource.labels.namespace_name="default" AND resource.labels.pod_name=~"^registry-staging-restore-"'
    logs = 'https://logging.googleapis.com/v2/projects/' + PROJECT + '/metrics'
    for name, marker in ((METRIC, 'REGISTRY_STAGING_RESTORE_SUCCESS'),
                         ('registry_staging_restore_watchdog', 'REGISTRY_STAGING_RESTORE_WATCHDOG_OK')):
        metric = {'name': name, 'description': 'Staging restore success or watchdog heartbeat',
                  'filter': scope + ' AND textPayload:"' + marker + '"',
                  'metricDescriptor': {'metricKind': 'DELTA', 'valueType': 'INT64', 'unit': '1'}}
        try:
            request(logs + '/' + name)
        except urllib.error.HTTPError as exc:
            if exc.code != 404:
                raise
            request(logs, 'POST', metric)
        else:
            # Updating an unchanged metric is unnecessary during alert bootstrap.
            current = request(logs + '/' + name)
            if current.get('filter') != metric['filter']:
                request(logs + '/' + name, 'PUT', metric)
    if args.enable_freshness:
        query_url = 'https://monitoring.googleapis.com/v1/projects/' + PROJECT + '/location/global/prometheus/api/v1/query?'
        require_watchdog_heartbeat(request(query_url + urllib.parse.urlencode({'query': HEARTBEATS})))
    doc = {'mimeType': 'text/markdown', 'content': 'Staging refresh failed or missed its weekly success. Staging may still be stopped. Inspect registry-staging-restore Jobs, init-container logs and registry-staging-restore-state ConfigMap in default. Never restore into production. See deploy/README.md. Logs: https://console.cloud.google.com/logs/query?project=mcp-registry-staging'}
    alerts = [
        {'displayName': 'Staging database restore failed', 'enabled': True, 'userLabels': {'managed_by': MANAGED, 'restore_alert': 'failure'},
         'conditions': [{'displayName': 'Restore or watchdog failure', 'conditionMatchedLog': {'filter': scope + ' AND textPayload:"REGISTRY_STAGING_RESTORE_FAILED"'}}],
         'alertStrategy': {'notificationRateLimit': {'period': '3600s'}, 'autoClose': '86400s'}},
        {'displayName': 'Staging restore watchdog missing for 1 hour', 'enabled': args.enable_freshness, 'userLabels': {'managed_by': MANAGED, 'restore_alert': 'freshness'},
         'conditions': [{'displayName': 'No successful restore watchdog check for 1 hour', 'conditionPrometheusQueryLanguage': {
             'query': '(' + HEARTBEATS + ' or vector(0)) < 1',
             'duration': '600s', 'evaluationInterval': '300s'}}], 'alertStrategy': {'autoClose': '86400s'}}]
    existing = listing(base + '/alertPolicies', 'alertPolicies')
    for alert in alerts:
        alert.update(combiner='OR', notificationChannels=[channel['name']], documentation=doc, severity='ERROR')
        if alert['userLabels']['restore_alert'] == 'freshness':
            alert['documentation'] = {'mimeType': 'text/markdown', 'content': 'No successful staging restore watchdog heartbeat was observed in the preceding hour, confirmed for ten minutes. This indicates a monitoring gap, not necessarily a failed restore or staging outage. Check the watchdog CronJob, its latest Job/logs and metric ingestion, then inspect staging app readiness and registry-staging-restore-state. Logs: https://console.cloud.google.com/logs/query?project=mcp-registry-staging'}
        previous = next((p for p in existing if p.get('userLabels') == alert['userLabels']), None)
        if previous:
            alert['name'] = previous['name']
            request('https://monitoring.googleapis.com/v3/' + previous['name'], 'PATCH', alert)
        else:
            request(base + '/alertPolicies', 'POST', alert)
    print('Configured source read-only access, dedicated staging password, email failure alert; freshness enabled:', args.enable_freshness)


if __name__ == '__main__':
    main()
