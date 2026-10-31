#!/usr/bin/env python3
"""Private, saved-plan staging capacity transition; only fixed labels reach CI."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

from production_apply import (Blocked, require, quiet, parse_preview_events,
                              decode_plan, dependency_signature, excluded_unchanged_rbac, apply_error_messages)

PREFIX = 'urn:pulumi:gcpStaging::mcp-registry-infra::'
WORKERS = PREFIX + 'gcp:container/nodePool:NodePool::mcp-registry-staging-nodepool'
APP_HPA = PREFIX + 'kubernetes:autoscaling/v2:HorizontalPodAutoscaler::mcp-registry-capacity'
INGRESS_HPA = PREFIX + 'kubernetes:helm.sh/v3:Chart$kubernetes:autoscaling/v2:HorizontalPodAutoscaler::ingress-nginx/ingress-nginx-controller'
APP_PDB = PREFIX + 'kubernetes:policy/v1:PodDisruptionBudget::mcp-registry'
INGRESS_PDB = PREFIX + 'kubernetes:helm.sh/v3:Chart$kubernetes:policy/v1:PodDisruptionBudget::ingress-nginx/ingress-nginx-controller'
TARGETS = (WORKERS, APP_HPA, INGRESS_HPA, APP_PDB, INGRESS_PDB)
STACK = PREFIX + 'pulumi:pulumi:Stack::mcp-registry-infra-gcpStaging'
HOOKS = {PREFIX + 'kubernetes:helm.sh/v3:Chart$kubernetes:batch/v1:Job::ingress-nginx/ingress-nginx-admission-' + name for name in ('create', 'patch')}


def completed_hook(step, previous):
    if step['urn'] not in HOOKS or step['op'] != 'create':
        return False
    before, after = previous.get(step['urn']), step.get('newState')
    if not before or not after:
        return False
    status = before.get('outputs', {}).get('status', {})
    return (status.get('succeeded') == 1 and bool(status.get('completionTime'))
            and before['inputs'].get('spec', {}).get('ttlSecondsAfterFinished') == 0
            and all(before.get(k) == after.get(k) for k in ('inputs', 'provider', 'parent')))


def validate(preview, idle, previous=None):
    require(preview.get('steps') and isinstance(preview.get('changeSummary'), dict), 'preview-incomplete')
    require(not preview.get('maybeCorrupt'), 'preview-state')
    require(not any(x.get('severity') == 'error' for x in preview.get('diagnostics', [])), 'preview-errors')
    labels = set()
    for step in preview['steps']:
        urn, op = step['urn'], step['op']
        if op in ('same', 'read', 'refresh'):
            continue
        if previous is not None and (completed_hook(step, previous) or excluded_unchanged_rbac(step, previous)):
            continue
        old, new = step.get('oldState'), step.get('newState')
        if urn == INGRESS_PDB:
            require((idle and op == 'delete' and old and not new) or (not idle and op == 'create' and new and not old), 'ingress-pdb-operation')
            state = old if idle else new
            require(not state.get('protect') and not step.get('replaceReasons'), 'ingress-pdb-protection')
            inputs = state['inputs']
            require(inputs['metadata']['name'] == 'ingress-nginx-controller' and inputs['metadata']['namespace'] == 'ingress-nginx', 'ingress-pdb-identity')
            require(inputs['spec'] == {'minAvailable': 1, 'selector': {'matchLabels': {'app.kubernetes.io/component': 'controller', 'app.kubernetes.io/instance': 'ingress-nginx', 'app.kubernetes.io/name': 'ingress-nginx'}}}, 'ingress-pdb-spec')
            labels.add('singleton ingress disruption budget')
            continue
        require(op == 'update' and old and new and not step.get('replaceReasons'), 'non-update-operation')
        require(all(old.get(k) == new.get(k) for k in ('provider', 'parent', 'protect')), 'resource-identity')
        if urn == STACK:
            continue
        require(urn in TARGETS, 'outside-idle-scope')
        before, after = copy.deepcopy(old['inputs']), copy.deepcopy(new['inputs'])
        if urn == WORKERS:
            require(before['autoscaling'].pop('minNodeCount') in (1, 2), 'existing-worker-minimum')
            require(after['autoscaling'].pop('minNodeCount') == (1 if idle else 2), 'target-worker-minimum')
            require(after['autoscaling']['maxNodeCount'] == 3, 'worker-ceiling')
            labels.add('worker minimum')
        elif urn == APP_PDB:
            require(before['spec'].pop('minAvailable') in (0, 1), 'existing-app-disruption-budget')
            require(after['spec'].pop('minAvailable') == (0 if idle else 1), 'target-app-disruption-budget')
            labels.add('singleton application disruption budget')
        else:
            for inputs in (before, after):
                metadata = inputs.get('metadata', {})
                annotations = metadata.get('annotations', {})
                if 'pulumi.com/patchForce' in annotations:
                    require(annotations.pop('pulumi.com/patchForce') == 'true', 'hpa-ownership-annotation')
                if not annotations:
                    metadata.pop('annotations', None)
            require(before['spec'].pop('minReplicas') in (1, 3), 'existing-replica-minimum')
            require(after['spec'].pop('minReplicas') == (1 if idle else 3), 'target-replica-minimum')
            require(after['spec']['maxReplicas'] == (4 if urn == APP_HPA else 6), 'replica-ceiling')
            labels.add('application minimum' if urn == APP_HPA else 'ingress minimum')
        require(before == after, 'unrelated-input-change')
    return sorted(labels)


def reconcile(full, target):
    result = copy.deepcopy(target)
    changes = []
    identity = ('type', 'name', 'custom', 'parent', 'provider', 'protect', 'deleteBeforeReplace',
                'ignoreChanges', 'additionalSecretOutputs', 'aliases', 'structuredAliases', 'id', 'customTimeouts')
    for urn, entry in result['resourcePlans'].items():
        f, t = full['resourcePlans'].get(urn, {}).get('goal'), entry.get('goal')
        if not f or not t or dependency_signature(f) == dependency_signature(t):
            continue
        require(urn not in TARGETS and not t.get('type', '').startswith('pulumi:providers:'), 'target-dependency-change')
        require(all(f.get(k) == t.get(k) for k in identity), 'dependency-identity')
        for field in ('dependencies', 'propertyDependencies'):
            if field in f:
                t[field] = copy.deepcopy(f[field])
            else:
                t.pop(field, None)
        changes.append(urn)
    return result, changes


def main(mode):
    require(mode in ('preview', 'apply'), 'mode')
    os.umask(0o077)
    env = dict(os.environ, PULUMI_BACKEND_URL='gs://mcp-registry-staging-pulumi-state',
               PULUMI_CONFIG_PASSPHRASE_FILE=str(Path('passphrase.staging.txt').resolve()),
               PULUMI_SKIP_UPDATE_CHECK='true', PULUMI_ENABLE_STREAMING_JSON_PREVIEW='true')
    with tempfile.TemporaryDirectory(prefix='registry-private-staging-') as temp:
        directory = Path(temp)
        quiet(['go', 'build'], directory, 'build', env)
        cfg = json.loads(quiet(['pulumi', 'config', '--json', '--stack', 'gcpStaging', '--non-interactive'], directory, 'config', env).read_text())
        idle = cfg.get('mcp-registry:stagingIdleCapacity', {}).get('value') == 'true'
        exported = directory / 'before.json'
        quiet(['pulumi', 'stack', 'export', '--stack', 'gcpStaging', '--file', str(exported), '--non-interactive'], directory, 'state', env)
        previous = {r['urn']: r for r in json.loads(exported.read_text())['deployment']['resources']}
        require(all(u in previous for u in TARGETS if u != INGRESS_PDB), 'existing-staging-targets')
        base = ['--stack', 'gcpStaging', '--non-interactive']
        full_plan = directory / 'full-plan.json'
        try:
            full = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(full_plan)] + base, directory, 'full-preview', env)
        except Blocked:
            # Only fixed component labels reach CI; never emit provider diagnostics.
            errors = apply_error_messages((directory / 'full-preview.out').read_text(), (directory / 'full-preview.err').read_text())
            for label, token in {'ingress-controller': 'ingress-nginx-controller', 'metrics-server': 'victoria-metrics-server', 'metrics-agent': 'victoria-metrics-agent', 'grafana': 'grafana', 'application': 'default/mcp-registry'}.items():
                if token in errors:
                    print('Preview conflict component: ' + label, flush=True)
            raise
        validate(parse_preview_events(full.read_text()), idle, previous)
        print('Full refreshed staging scope validation passed.', flush=True)
        targets = [arg for urn in TARGETS if urn != INGRESS_PDB or urn in previous or not idle for arg in ('--target', urn)]
        refresh = ['--refresh=false'] if not idle and INGRESS_PDB not in previous else ['--refresh']
        plan = directory / 'plan.json'
        preview = quiet(['pulumi', 'preview', '--json', '--save-plan', str(plan)] + refresh + base + targets, directory, 'preview', env)
        labels = validate(parse_preview_events(preview.read_text()), idle)
        full_graph = decode_plan(full_plan.read_text())
        graph, changes = reconcile(full_graph, decode_plan(plan.read_text()))
        if changes:
            confirm_plan = directory / 'confirm.json'
            confirm = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(confirm_plan)] + base, directory, 'confirm-preview', env)
            validate(parse_preview_events(confirm.read_text()), idle, previous)
            confirmation = decode_plan(confirm_plan.read_text())
            require(all(dependency_signature(full_graph['resourcePlans'][u]['goal']) == dependency_signature(confirmation['resourcePlans'][u]['goal']) for u in changes), 'unstable-dependency-graph')
        document = json.loads(plan.read_text())
        if 'version' in document:
            document['plan'] = graph
        else:
            document = graph
        apply_plan = directory / 'validated-plan.json'
        apply_plan.write_text(json.dumps(document))
        print('Private preview validated: ' + (', '.join(labels) or 'no capacity changes'), flush=True)
        if mode == 'apply':
            quiet(['pulumi', 'up', '--json', '--yes', '--skip-preview', '--plan', str(apply_plan)] + refresh + base + targets, directory, 'apply', env)
            print('Staging capacity apply completed; live verification remains required.')


if __name__ == '__main__':
    try:
        main(sys.argv[1] if len(sys.argv) == 2 else '')
    except Blocked as e:
        print('Staging validation blocked: ' + str(e), file=sys.stderr)
        sys.exit(1)
    except (ValueError, KeyError, TypeError, IndexError, OSError, subprocess.SubprocessError):
        print('Staging validation blocked: private command or schema failure.', file=sys.stderr)
        sys.exit(1)
