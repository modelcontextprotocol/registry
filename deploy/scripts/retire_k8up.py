#!/usr/bin/env python3
"""One-time K8up retirement inventory; remove completed historical Jobs left after CRD retirement.

Run before the Pulumi apply to archive historical API metadata and check that
CRD garbage collection cannot reach retained volumes. Pulumi removes the idle
operator/CRDs and their historical owned objects. Run again with --cleanup-historical
afterward, using the original inventory to identify any remaining Jobs/Pods. No storage API is called.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess


KINDS = ('archives', 'backups', 'checks', 'podconfigs', 'prebackuppods',
         'prunes', 'restores', 'schedules', 'snapshots')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def completed_job(obj):
    return not obj.get('status', {}).get('active') and any(
        c['type'] in ('Complete', 'Failed') and c['status'] == 'True'
        for c in obj.get('status', {}).get('conditions', []))


def validate(inventory):
    retired = []
    for kind in KINDS:
        for obj in inventory[kind]:
            require(not obj['metadata'].get('finalizers'), 'unexpected K8up finalizer')
            require(kind == 'snapshots' or (kind in ('archives', 'backups', 'checks', 'prunes', 'restores') and obj.get('status', {}).get('finished') is True), 'active or unfinished K8up object')
            retired.append(obj)
    uids = {o['metadata']['uid'] for o in retired}
    jobs = [j for j in inventory['jobs'] if
            any(o['uid'] in uids for o in j['metadata'].get('ownerReferences', [])) or
            (j['metadata']['namespace'], j['metadata']['name']) == ('default', 'k8up-cleanup')]
    require(all(completed_job(j) and not j['metadata'].get('finalizers') for j in jobs), 'unfinished historical Job')
    retired.extend(jobs)
    # Follow the owner graph through Pods too; CRD GC must not reach a PVC.
    uids.update(j['metadata']['uid'] for j in jobs)
    pods = [p for p in inventory['pods'] if any(o['uid'] in uids for o in p['metadata'].get('ownerReferences', []))]
    require(all(p['status']['phase'] in ('Succeeded', 'Failed') for p in pods), 'historical Pod still running')
    uids.update(p['metadata']['uid'] for p in pods)
    require(not any(o['uid'] in uids for p in inventory['pvcs'] for o in p['metadata'].get('ownerReferences', [])), 'retained PVC owned by retired object')
    return jobs, pods


def cleanup_targets(prior, current):
    old_jobs, _ = validate(prior)
    known = {(j['metadata']['namespace'], j['metadata']['name']): j['metadata']['uid'] for j in old_jobs}
    jobs = [j for j in current['jobs'] if (j['metadata']['namespace'], j['metadata']['name']) in known]
    for job in jobs:
        meta = job['metadata']
        require(meta['uid'] == known[(meta['namespace'], meta['name'])], 'historical Job was replaced')
        require(completed_job(job) and not meta.get('finalizers'), 'historical Job changed or has finalizers')
    pods = [p for p in current['pods'] if any(o['uid'] in known.values() for o in p['metadata'].get('ownerReferences', []))]
    require(all(p['status']['phase'] in ('Succeeded', 'Failed') and not p['metadata'].get('finalizers') for p in pods), 'historical Pod still active or has finalizers')
    return jobs, pods


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('environment', choices=('staging', 'prod'))
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cleanup-historical', action='store_true')
    parser.add_argument('--prior-inventory', type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    args.output.mkdir(parents=True, exist_ok=False)
    k = ['kubectl', '--context', f'gke_mcp-registry-{args.environment}_us-central1-b_mcp-registry-{args.environment}', '--request-timeout=30s']

    def get(kind):
        return json.loads(subprocess.check_output(k + ['get', kind, '-A', '-o', 'json']))['items']

    available = subprocess.check_output(k + ['api-resources', '--api-group=k8up.io', '-o', 'name'], text=True).splitlines()
    require(set(available) <= {x + '.k8up.io' for x in KINDS}, 'unknown K8up API kind')
    inventory = {kind: get(kind + '.k8up.io') if kind + '.k8up.io' in available else [] for kind in KINDS}
    inventory.update({kind: get(kind) for kind in ('jobs', 'pods', 'pvcs')})
    (args.output / 'inventory.json').write_text(json.dumps(inventory, indent=2) + '\n')
    if args.cleanup_historical:
        require(args.prior_inventory is not None, 'original inventory required')
        prior = json.loads(args.prior_inventory.read_text())
        jobs, pods = cleanup_targets(prior, inventory)
    else:
        jobs, pods = validate(inventory)
    print(json.dumps({'historicalCustomResources': sum(len(inventory[kind]) for kind in KINDS),
                      'historicalJobs': [j['metadata']['name'] for j in jobs], 'historicalPods': len(pods)}))
    if args.cleanup_historical:
        require(not available, 'apply Pulumi retirement before orphan cleanup')
        require(not any(d['metadata']['name'] == 'k8up' and d['metadata']['namespace'] == 'default' for d in get('deployments')), 'operator still present')
        # Orphan propagation prevents deleting any storage-dependent objects.
        # UID/resourceVersion preconditions reject replacement or changed Jobs.
        for obj in jobs + pods:
            meta = obj['metadata']
            resource = 'jobs' if obj['kind'] == 'Job' else 'pods'
            prefix = '/apis/batch/v1' if resource == 'jobs' else '/api/v1'
            body = {'apiVersion': 'v1', 'kind': 'DeleteOptions', 'propagationPolicy': 'Orphan',
                    'preconditions': {'uid': meta['uid'], 'resourceVersion': meta['resourceVersion']}}
            subprocess.run(k + ['delete', '--raw', f"{prefix}/namespaces/{meta['namespace']}/{resource}/{meta['name']}", '-f', '-'],
                           input=json.dumps(body), text=True, check=True, stdout=subprocess.DEVNULL)


if __name__ == '__main__':
    main()
