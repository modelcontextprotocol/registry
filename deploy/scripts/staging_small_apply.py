#!/usr/bin/env python3
"""Saved-plan staging rightsizing with exact resource and phase boundaries."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

import staging_idle_apply as idle
from production_apply import (Blocked, require, quiet, parse_preview_events,
                              decode_plan, dependency_signature, excluded_unchanged_rbac)

PREFIX = idle.PREFIX
WORKERS = idle.WORKERS
DATABASE_POOL = PREFIX + 'gcp:container/nodePool:NodePool::registry-database-nodepool'
APP = PREFIX + 'kubernetes:apps/v1:Deployment::mcp-registry'
INGRESS = PREFIX + 'kubernetes:helm.sh/v3:Chart$kubernetes:apps/v1:Deployment::ingress-nginx/ingress-nginx-controller'
DATABASE = PREFIX + 'kubernetes:postgresql.cnpg.io/v1:Cluster::registry-pg'
TARGETS = (APP, INGRESS, DATABASE, WORKERS, DATABASE_POOL)
CONTEXT = 'gke_mcp-registry-staging_us-central1-b_mcp-registry-staging'


def validate(preview, phase, previous=None):
    require(phase in ('prepare', 'enabled'), 'small-capacity-phase')
    require(preview.get('steps') and isinstance(preview.get('changeSummary'), dict), 'preview-incomplete')
    require(not preview.get('maybeCorrupt'), 'preview-state')
    require(not any(x.get('severity') == 'error' for x in preview.get('diagnostics', [])), 'preview-errors')
    labels = set()
    for step in preview['steps']:
        urn, op = step['urn'], step['op']
        if op in ('same', 'read', 'refresh'):
            continue
        if previous is not None and (idle.completed_hook(step, previous) or excluded_unchanged_rbac(step, previous)):
            continue
        old, new = step.get('oldState'), step.get('newState')
        require(op == 'update' and old and new and not step.get('replaceReasons'), 'update-only')
        require(all(old.get(k) == new.get(k) for k in ('provider', 'parent', 'protect', 'retainOnDelete')), 'resource-identity')
        if urn == idle.STACK:
            continue
        require(urn in TARGETS, 'outside-small-capacity-scope')
        before, after = copy.deepcopy(old['inputs']), copy.deepcopy(new['inputs'])
        if urn in (WORKERS, DATABASE_POOL):
            require(phase == 'enabled', 'prepare-must-not-resize-nodes')
            require(before['nodeConfig'].pop('machineType') in ('e2-standard-4', 'e2-standard-2'), 'existing-machine')
            require(after['nodeConfig'].pop('machineType') == 'e2-standard-2', 'target-machine')
            labels.add('staging node machine type')
        else:
            require(phase == 'prepare', 'prepare-workloads-before-resize')
            for value, desired in ((before, False), (after, True)):
                if urn == DATABASE:
                    resources = value['spec']['resources']
                    expected, allowed = '250m', ('3', '250m', '0.25')
                else:
                    containers = value['spec']['template']['spec']['containers']
                    expected_name = 'mcp-registry' if urn == APP else 'controller'
                    require(len(containers) == 1 and containers[0]['name'] == expected_name, 'container-identity')
                    resources = containers[0]['resources']
                    expected, allowed = '100m', ('800m', '0.8', '100m', '0.1')
                cpu = resources['requests'].pop('cpu')
                require(cpu in ((expected, '0.25' if urn == DATABASE else '0.1') if desired else allowed), 'cpu-reservation')
            labels.add('staging CPU reservation')
        require(before == after, 'unrelated-input-change')
    return sorted(labels)


def kube(args, directory, label, env):
    return json.loads(quiet(['kubectl', '--context', CONTEXT, '--request-timeout=30s'] + args,
                            directory, label, env, timeout=90).read_text())


def preflight(directory, env, phase):
    quiet(['gcloud', 'container', 'clusters', 'get-credentials', 'mcp-registry-staging',
           '--project=mcp-registry-staging', '--zone=us-central1-b', '--quiet'], directory, 'credentials', env)
    pvcs = kube(['get', 'pvc', '-A', '-o', 'json'], directory, 'pvcs', env)['items']
    require(pvcs and all(p['status']['phase'] == 'Bound' for p in pvcs), 'bound-volumes')
    pods = kube(['get', 'pods', '-A', '-o', 'json'], directory, 'pods', env)['items']
    live = [p for p in pods if p['status']['phase'] not in ('Succeeded', 'Failed')]
    require(live and all(p['status']['phase'] == 'Running' and not p['metadata'].get('deletionTimestamp')
                        and any(c['type'] == 'Ready' and c['status'] == 'True' for c in p['status'].get('conditions', []))
                        for p in live), 'all-staging-pods-ready')
    if phase == 'enabled':
        for prefix, cpu in [('mcp-registry-', '100m'), ('ingress-nginx-controller-', '100m'), ('registry-pg-', '250m')]:
            matches = [p for p in live if p['metadata']['name'].startswith(prefix)]
            require(matches, 'required-workload-present')
            require(all(p['spec']['containers'][0]['resources']['requests']['cpu'] == cpu for p in matches), 'prepared-live-reservations')
    return {(p['metadata']['namespace'], p['metadata']['name']): (p['metadata']['uid'], p['spec']['volumeName']) for p in pvcs}


def maintenance(directory, env, originals=None):
    # Only the two existing singleton staging databases. No volume or placement changes.
    if originals is None:
        originals = {}
        try:
            for namespace, name in [('default', 'registry-pg'), ('monitoring', 'grafana-pg')]:
                obj = kube(['-n', namespace, 'get', 'cluster.postgresql.cnpg.io', name, '-o', 'json'], directory, 'maintenance-before-'+name, env)
                require(obj['spec']['instances'] == 1 and obj['status'].get('readyInstances') == 1, 'healthy-singleton-database')
                original = obj['spec'].get('nodeMaintenanceWindow')
                require(not original or not original.get('inProgress'), 'existing-maintenance-window')
                originals[(namespace, name)] = original
                patch = {'spec': {'nodeMaintenanceWindow': {'inProgress': True, 'reusePVC': True}}}
                kube(['-n', namespace, 'patch', 'cluster.postgresql.cnpg.io', name, '--type=merge', '-p', json.dumps(patch), '-o', 'json'], directory, 'maintenance-start-'+name, env)
        except Exception:
            maintenance(directory, env, originals)
            raise
        return originals
    failures = []
    for (namespace, name), original in originals.items():
        try:
            patch = [{'op': 'remove', 'path': '/spec/nodeMaintenanceWindow'}] if original is None else [{'op': 'replace', 'path': '/spec/nodeMaintenanceWindow', 'value': original}]
            kube(['-n', namespace, 'patch', 'cluster.postgresql.cnpg.io', name, '--type=json', '-p', json.dumps(patch), '-o', 'json'], directory, 'maintenance-restore-'+name, env)
        except Exception:
            failures.append(name)
    require(not failures, 'maintenance-restoration-needs-attention')


def main(mode):
    require(mode in ('preview', 'apply'), 'mode')
    os.umask(0o077)
    env = dict(os.environ, PULUMI_BACKEND_URL='gs://mcp-registry-staging-pulumi-state',
               PULUMI_CONFIG_PASSPHRASE_FILE=str(Path('passphrase.staging.txt').resolve()),
               PULUMI_SKIP_UPDATE_CHECK='true', PULUMI_ENABLE_STREAMING_JSON_PREVIEW='true')
    with tempfile.TemporaryDirectory(prefix='registry-private-staging-small-') as temp:
        directory = Path(temp)
        quiet(['go', 'build'], directory, 'build', env)
        cfg = json.loads(quiet(['pulumi', 'config', '--json', '--stack', 'gcpStaging', '--non-interactive'], directory, 'config', env).read_text())
        phase = cfg.get('mcp-registry:stagingSmallCapacity', {}).get('value')
        require(phase in ('prepare', 'enabled'), 'phase')
        original_volumes = preflight(directory, env, phase)
        exported = directory / 'before.json'
        base = ['--stack', 'gcpStaging', '--non-interactive']
        quiet(['pulumi', 'stack', 'export', '--file', str(exported)] + base, directory, 'state', env)
        previous = {r['urn']: r for r in json.loads(exported.read_text())['deployment']['resources']}
        require(all(u in previous for u in TARGETS), 'existing-targets')
        full_plan = directory / 'full-plan.json'
        full = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(full_plan)] + base, directory, 'full-preview', env)
        validate(parse_preview_events(full.read_text()), phase, previous)
        print('Full staging rightsizing scope validated.', flush=True)
        targets = [arg for urn in (TARGETS[:3] if phase == 'prepare' else TARGETS[3:]) for arg in ('--target', urn)]
        plan = directory / 'plan.json'
        preview = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(plan)] + base + targets, directory, 'preview', env)
        labels = validate(parse_preview_events(preview.read_text()), phase)
        full_graph = decode_plan(full_plan.read_text())
        # Reuse the existing identity-preserving dependency reconciliation with these exact targets.
        idle.TARGETS = TARGETS
        graph, changes = idle.reconcile(full_graph, decode_plan(plan.read_text()))
        if changes:
            confirm_plan = directory / 'confirm.json'
            confirm = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(confirm_plan)] + base, directory, 'confirm-preview', env)
            validate(parse_preview_events(confirm.read_text()), phase, previous)
            confirmation = decode_plan(confirm_plan.read_text())
            require(all(dependency_signature(full_graph['resourcePlans'][u]['goal']) == dependency_signature(confirmation['resourcePlans'][u]['goal']) for u in changes), 'unstable-dependency-graph')
        document = json.loads(plan.read_text())
        if 'version' in document:
            document['plan'] = graph
        else:
            document = graph
        apply_plan = directory / 'validated-plan.json'
        apply_plan.write_text(json.dumps(document))
        print('Private staging plan validated: ' + (', '.join(labels) or 'no changes'), flush=True)
        if mode == 'apply':
            require(preflight(directory, env, phase) == original_volumes, 'volume-identity-before-apply')
            originals = maintenance(directory, env) if phase == 'enabled' and labels else None
            try:
                quiet(['pulumi', 'up', '--refresh', '--json', '--yes', '--skip-preview', '--plan', str(apply_plan)] + base + targets, directory, 'apply', env)
            finally:
                if originals is not None:
                    maintenance(directory, env, originals)
            pvcs = kube(['get', 'pvc', '-A', '-o', 'json'], directory, 'pvcs-after', env)['items']
            require({(p['metadata']['namespace'], p['metadata']['name']): (p['metadata']['uid'], p['spec']['volumeName']) for p in pvcs} == original_volumes, 'volume-identity-after-apply')
            print('Staging rightsizing apply completed; volume identities preserved; verify live health.')


if __name__ == '__main__':
    try:
        main(sys.argv[1] if len(sys.argv) == 2 else '')
    except Blocked as error:
        print('Staging rightsizing blocked: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except (ValueError, KeyError, TypeError, IndexError, OSError, subprocess.SubprocessError):
        print('Staging rightsizing blocked: private command or schema failure.', file=sys.stderr)
        sys.exit(1)
