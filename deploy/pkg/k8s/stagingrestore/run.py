"""Staging restore controller, run inside a dedicated Kubernetes Job."""
import datetime as dt
import json
import os
from pathlib import Path
import ssl
import sys
import time
import urllib.request

WORK = Path('/work')
BASE = 'https://kubernetes.default.svc'
STATE = '/api/v1/namespaces/default/configmaps/registry-staging-restore-state'
DEPLOY = '/apis/apps/v1/namespaces/default/deployments/mcp-registry'
TOKEN = Path('/var/run/secrets/kubernetes.io/serviceaccount')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def now():
    return dt.datetime.now(dt.timezone.utc)


def parse_time(value):
    return dt.datetime.fromisoformat(value.replace('Z', '+00:00'))


def api(path, method='GET', data=None):
    context = ssl.create_default_context(cafile=str(TOKEN / 'ca.crt'))
    headers = {'Authorization': 'Bearer ' + (TOKEN / 'token').read_text(),
               'Content-Type': 'application/json-patch+json'}
    req = urllib.request.Request(BASE + path, method=method, headers=headers,
                                 data=None if data is None else json.dumps(data).encode())
    with urllib.request.urlopen(req, context=context, timeout=20) as response:
        return json.load(response)


def state_update(state, values):
    return api(STATE, 'PATCH', [
        {'op': 'test', 'path': '/metadata/resourceVersion', 'value': state['metadata']['resourceVersion']},
        *[{'op': 'add', 'path': '/data/' + k, 'value': v} for k, v in values.items()]])


def select_snapshot(records, current):
    require(len(records) == 1, 'Expected exactly one latest snapshot')
    record = records[0]
    import re
    require(re.fullmatch('[0-9a-f]{64}', record['id']), 'Invalid snapshot ID')
    require(record.get('hostname') == 'registry-postgres' and
            'postgres-custom' in record.get('tags', []), 'Wrong backup identity')
    age = (current - parse_time(record['time'])).total_seconds()
    require(0 <= age <= 36 * 3600, 'Latest backup is missing, future-dated or older than 36 hours')
    return record['id']


def select():
    snapshot = select_snapshot(json.loads((WORK / 'snapshots.json').read_text()), now())
    (WORK / 'snapshot-id').write_text(snapshot)
    print('Selected production backup snapshot ' + snapshot, flush=True)


def identity():
    require(os.environ.get('RESTORE_ENVIRONMENT') == 'staging', 'Only staging is supported')
    pg = api('/apis/postgresql.cnpg.io/v1/namespaces/default/clusters/registry-pg')
    dep = api(DEPLOY)
    require(pg['metadata']['labels'].get('environment') == 'staging' and
            dep['metadata']['labels'].get('environment') == 'staging', 'Target is not staging')
    return dep


def scale(expected, desired):
    # The Scale API omits spec.replicas at zero. Test its resourceVersion and
    # use add (which also replaces) rather than testing a possibly absent field.
    current = api(DEPLOY + '/scale')
    require(current.get('spec', {}).get('replicas', 0) == expected, 'Staging replica count changed')
    api(DEPLOY + '/scale', 'PATCH', [
        {'op': 'test', 'path': '/metadata/resourceVersion', 'value': current['metadata']['resourceVersion']},
        {'op': 'add', 'path': '/spec/replicas', 'value': desired}])


def stop():
    dep = identity()
    require(dep['spec']['replicas'] > 0, 'Staging already stopped: inspect previous restore before retrying')
    require(dep['status'].get('observedGeneration') == dep['metadata']['generation'] and
            dep['status'].get('updatedReplicas') == dep['spec']['replicas'] and
            dep['status'].get('availableReplicas') == dep['spec']['replicas'], 'Staging rollout is not stable')
    state = api(STATE)
    require(not state['data'].get('active_id'), 'Another restore owns the maintenance lock')
    state_update(state, {'active_id': os.environ['POD_UID'], 'started': now().isoformat(),
                        'replicas': str(dep['spec']['replicas']), 'deployment_uid': dep['metadata']['uid'],
                        'snapshot': (WORK / 'snapshot-id').read_text(), 'phase': 'stopping'})
    scale(dep['spec']['replicas'], 0)
    for _ in range(60):
        pods = api('/api/v1/namespaces/default/pods?labelSelector=app%3Dmcp-registry')['items']
        if not pods:
            state_update(api(STATE), {'phase': 'restoring'})
            return
        time.sleep(3)
    raise RuntimeError('Staging application did not stop within 180 seconds')


def start():
    dep = identity()
    state = api(STATE)
    require(state['data'].get('active_id') == os.environ['POD_UID'], 'Maintenance lock ownership changed')
    require(dep['metadata']['uid'] == state['data']['deployment_uid'] and dep['spec']['replicas'] == 0,
            'Staging deployment changed during restore')
    replicas = int(state['data']['replicas'])
    require(1 <= replicas <= 4, 'Invalid saved replica count')
    scale(0, replicas)
    state_update(api(STATE), {'phase': 'validating-app'})
    for _ in range(90):
        dep = api(DEPLOY)
        if dep['status'].get('availableReplicas', 0) >= replicas:
            try:
                with urllib.request.urlopen('http://mcp-registry/v0.1/servers?limit=1', timeout=10) as r:
                    data = json.load(r)
                require(len(data.get('servers', [])) > 0, 'Listing returned no restored records')
                state_update(api(STATE), {'phase': 'succeeded', 'last_success': now().isoformat(), 'active_id': ''})
                print('REGISTRY_STAGING_RESTORE_SUCCESS snapshot=' + state['data']['snapshot'], flush=True)
                return
            except (OSError, ValueError, RuntimeError):
                pass
        time.sleep(5)
    raise RuntimeError('Staging did not become healthy after restore; inspect app logs')


def failed_reason(jobs, state, current):
    if state.get('active_id') and (current - parse_time(state['started'])).total_seconds() > 2400:
        return 'maintenance exceeded 40 minutes; staging may still be stopped'
    if state.get('last_success') and (current - parse_time(state['last_success'])).total_seconds() > 8 * 86400:
        return 'no successful staging refresh in 8 days'
    if not jobs:
        return None
    job = max(jobs, key=lambda j: j['metadata']['creationTimestamp'])
    if any(c['type'] == 'Failed' and c['status'] == 'True' for c in job.get('status', {}).get('conditions', [])):
        return 'job ' + job['metadata']['name'] + ' failed; inspect init-container logs'
    return None


def watch():
    jobs = api('/apis/batch/v1/namespaces/default/jobs?labelSelector=app%3Dregistry-staging-restore')['items']
    reason = failed_reason(jobs, api(STATE)['data'], now())
    if reason:
        raise RuntimeError(reason)
    print('REGISTRY_STAGING_RESTORE_WATCHDOG_OK', flush=True)


def main():
    phase = sys.argv[1]
    try:
        if phase == 'alert-test':
            raise RuntimeError('Notification delivery test; no database operation performed')
        {'select': select, 'stop': stop, 'start': start, 'watch': watch}[phase]()
    except Exception as exc:
        print('REGISTRY_STAGING_RESTORE_FAILED phase=' + phase + ' reason=' + str(exc), flush=True)
        raise SystemExit(1) from None


if __name__ == '__main__':
    main()
