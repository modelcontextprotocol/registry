#!/usr/bin/env python3
"""Saved-plan deployment of only public-read compression and log sampling.

Raw state, diagnostics and plans remain in private temporary files and are never
printed or uploaded. Capacity, application templates, databases and volumes are
outside this operation. Set either config flag false to roll that feature back.
"""
import copy
import json
import os
from pathlib import Path
import sys
import tempfile

from production_apply import (Blocked, require, quiet, parse_preview_events, decode_plan,
                              dependency_signature, reconcile_plan_dependencies,
                              excluded_unchanged_rbac, excluded_completed_hook)
import staging_idle_apply

POLICY = json.loads((Path(__file__).resolve().parents[1] / 'pkg/traffic/policy.json').read_text())


def identities(environment):
    require(environment in ('prod', 'staging'), 'environment')
    stack = 'gcpProd' if environment == 'prod' else 'gcpStaging'
    prefix = f'urn:pulumi:{stack}::mcp-registry-infra::'
    return stack, prefix + 'kubernetes:helm.sh/v3:Chart$kubernetes:core/v1:ConfigMap::ingress-nginx/ingress-nginx-controller', prefix + 'gcp:logging/projectExclusion:ProjectExclusion::registry-fast-public-read-sampling'


def validate(preview, environment, compression, sampling, previous=None):
    stack, cm, exclusion = identities(environment)
    require(preview.get('steps') and isinstance(preview.get('changeSummary'), dict), 'preview-complete')
    require(not preview.get('maybeCorrupt'), 'preview-state')
    require(not any(d.get('severity') == 'error' for d in preview.get('diagnostics', [])), 'preview-errors')
    labels = set()
    for step in preview['steps']:
        urn, op = step['urn'], step['op']
        if op in ('same', 'read', 'refresh'):
            continue
        if previous is not None:
            hook = excluded_completed_hook if environment == 'prod' else staging_idle_apply.completed_hook
            if hook(step, previous) or excluded_unchanged_rbac(step, previous):
                continue
        old, new = step.get('oldState'), step.get('newState')
        if urn == f'urn:pulumi:{stack}::mcp-registry-infra::pulumi:pulumi:Stack::mcp-registry-infra-{stack}':
            require(op == 'update' and old and new, 'stack-update-only')
            continue
        require(urn in (cm, exclusion), 'outside-public-read-scope')
        # Streaming event keys can list ordinary changed ConfigMap fields.
        # The operation itself must be update/create, never replace or delete.
        require(op in ('update', 'create') and new, 'no-delete-or-replacement')
        if old:
            require(all(old.get(k) == new.get(k) for k in ('provider', 'parent', 'protect', 'retainOnDelete')), 'identity-unchanged')
        if urn == cm:
            require(op == 'update' and old, 'existing-configmap-only')
            before, after = copy.deepcopy(old['inputs']), copy.deepcopy(new['inputs'])
            for field, snippet in [('http-snippet', POLICY['http_snippet']), ('location-snippet', POLICY['location_snippet'])]:
                b, a = before['data'][field], after['data'][field]
                require(b.count(snippet) <= 1 and a.count(snippet) <= 1, 'duplicate-snippet')
                require(a.endswith(snippet) if compression else snippet not in a, 'compression-target')
                before['data'][field] = b.removesuffix(snippet)
                after['data'][field] = a.removesuffix(snippet)
            require(before == after, 'unrelated-configmap-change')
            labels.add('public listing compression')
        else:
            expected = {'project': 'mcp-registry-'+environment, 'name': POLICY['exclusion_name'],
                        'description': POLICY['exclusion_description'], 'filter': POLICY['exclusion_filter'], 'disabled': not sampling}
            actual = dict(new['inputs'])
            # The Terraform bridge may annotate which optional inputs it defaulted.
            defaults = actual.pop('__defaults', [])
            require(isinstance(defaults, list) and set(defaults) <= set(expected), 'exclusion-default-metadata')
            require(set(actual) <= set(expected), 'exclusion-extra-input')
            for field in expected:
                require(actual.get(field) == expected[field], 'exclusion-field-'+field)
            if old:
                before = dict(old['inputs']); before.pop('__defaults', None); before['disabled'] = not sampling
                require(before == expected, 'unexpected-existing-log-policy')
            labels.add('successful listing log sampling')
    return sorted(labels)


def preflight(environment, directory, env, label):
    context = f'gke_mcp-registry-{environment}_us-central1-b_mcp-registry-{environment}'
    def read(kind):
        return json.loads(quiet(['kubectl', '--context', context, '--request-timeout=30s', 'get', kind, '-A', '-o', 'json'], directory, label+'-'+kind, env, timeout=90).read_text())['items']
    pvcs = read('pvc'); pods = read('pods')
    require(pvcs and all(p['status']['phase'] == 'Bound' for p in pvcs), 'bound-volumes')
    for namespace, prefix in [('ingress-nginx', 'ingress-nginx-controller-'), ('default', 'mcp-registry-'), ('default', 'registry-pg-'), ('monitoring', 'grafana-pg-')]:
        matches = [p for p in pods if p['metadata']['namespace'] == namespace and p['metadata']['name'].startswith(prefix) and p['status']['phase'] not in ('Succeeded', 'Failed')]
        require(matches and all(p['status']['phase'] == 'Running' and any(c['type'] == 'Ready' and c['status'] == 'True' for c in p['status'].get('conditions', [])) for p in matches), 'required-workloads-ready')
    return {(p['metadata']['namespace'], p['metadata']['name']): (p['metadata']['uid'], p['spec']['volumeName']) for p in pvcs}


def main(environment, mode):
    stack, cm, exclusion = identities(environment)
    require(mode in ('preview', 'apply'), 'mode')
    os.umask(0o077)
    env = dict(os.environ, PULUMI_BACKEND_URL=f'gs://mcp-registry-{environment}-pulumi-state',
               PULUMI_CONFIG_PASSPHRASE_FILE=str(Path(f'passphrase.{environment}.txt').resolve()),
               PULUMI_SKIP_UPDATE_CHECK='true', PULUMI_ENABLE_STREAMING_JSON_PREVIEW='true',
               PULUMI_K8S_ENABLE_CONFIGMAP_MUTABLE='true')
    with tempfile.TemporaryDirectory(prefix='registry-private-public-read-') as temp:
        directory = Path(temp); base = ['--stack', stack, '--non-interactive']
        quiet(['go', 'build'], directory, 'build', env)
        cfg = json.loads(quiet(['pulumi', 'config', '--json']+base, directory, 'config', env).read_text())
        flags = [cfg.get('mcp-registry:'+k, {}).get('value') for k in ('publicReadCompression', 'publicReadLogSampling')]
        require(all(f in ('true', 'false') for f in flags), 'explicit-feature-flags')
        compression, sampling = [f == 'true' for f in flags]
        quiet(['gcloud', 'container', 'clusters', 'get-credentials', 'mcp-registry-'+environment, '--project=mcp-registry-'+environment, '--zone=us-central1-b', '--quiet'], directory, 'credentials', env)
        volumes = preflight(environment, directory, env, 'before')
        exported = directory/'before.json'
        quiet(['pulumi', 'stack', 'export', '--file', str(exported)]+base, directory, 'state', env)
        previous = {r['urn']: r for r in json.loads(exported.read_text())['deployment']['resources']}
        require(cm in previous, 'existing-ingress-configmap')
        full_plan = directory/'full-plan.json'
        full = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(full_plan)]+base, directory, 'full-preview', env)
        validate(parse_preview_events(full.read_text()), environment, compression, sampling, previous)
        print('Full public-read change scope validated.', flush=True)
        targets = ['--target', cm, '--target', exclusion]
        plan = directory/'target-plan.json'
        result = quiet(['pulumi', 'preview', '--refresh=false', '--json', '--save-plan', str(plan)]+base+targets, directory, 'preview', env)
        labels = validate(parse_preview_events(result.read_text()), environment, compression, sampling)
        full_graph = decode_plan(full_plan.read_text()); target_graph = decode_plan(plan.read_text())
        for u in (cm, exclusion):
            require(dependency_signature(full_graph['resourcePlans'][u]['goal']) == dependency_signature(target_graph['resourcePlans'][u]['goal']), 'target-dependencies')
        graph, changes = reconcile_plan_dependencies(full_graph, target_graph)
        if changes:
            confirm_plan = directory/'confirm.json'
            confirm = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(confirm_plan)]+base, directory, 'confirm-preview', env)
            validate(parse_preview_events(confirm.read_text()), environment, compression, sampling, previous)
            confirmation = decode_plan(confirm_plan.read_text())
            require(all(dependency_signature(full_graph['resourcePlans'][u]['goal']) == dependency_signature(confirmation['resourcePlans'][u]['goal']) for u in changes), 'stable-dependency-graph')
        document = json.loads(plan.read_text())
        if 'version' in document: document['plan'] = graph
        else: document = graph
        validated = directory/'validated-plan.json'; validated.write_text(json.dumps(document))
        print('Private plan validated: '+(', '.join(labels) or 'no changes'), flush=True)
        if mode == 'apply':
            require(preflight(environment, directory, env, 'preapply') == volumes, 'volume-identity-before-apply')
            quiet(['pulumi', 'up', '--refresh=false', '--json', '--yes', '--skip-preview', '--plan', str(validated)]+base+targets, directory, 'apply', env)
            require(preflight(environment, directory, env, 'after') == volumes, 'volume-identity-after-apply')
            print('Public-read apply completed; volume identities preserved. Verify responses and telemetry.')


if __name__ == '__main__':
    try:
        main(*sys.argv[1:])
    except Blocked as error:
        print('Public-read deployment blocked: '+str(error), file=sys.stderr); sys.exit(1)
    except (ValueError, KeyError, TypeError, IndexError, OSError):
        print('Public-read deployment blocked: private command or schema failure.', file=sys.stderr); sys.exit(1)
