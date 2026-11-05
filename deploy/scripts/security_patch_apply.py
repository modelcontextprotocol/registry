#!/usr/bin/env python3
"""Apply the reviewed November 2026 component upgrade using a saved full plan.

The allowlist contains hashes of reviewed field transitions, never secret values.
No delete/replacement is allowed. Raw plans, state and backups remain private;
backups are retained in the existing private GCS bucket, not Actions artifacts.
"""
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

from production_apply import (Blocked, require, quiet, parse_preview_events, excluded_unchanged_rbac,
                              decode_plan, dependency_signature, reconcile_plan_dependencies)
from public_read_apply import preflight as volume_preflight

POLICY_PATH = Path(__file__).with_name('security_patch_202611.json')
PG_IMAGE = 'ghcr.io/cloudnative-pg/postgresql:17.5'
IMAGES = {
    ('cnpg-system', 'cloudnative-pg'): 'ghcr.io/cloudnative-pg/cloudnative-pg:1.30.1',
    ('ingress-nginx', 'ingress-nginx-controller'): 'registry.k8s.io/ingress-nginx/controller:v1.15.1@sha256:594ceea76b01c592858f803f9ff4d2cb40542cae2060410b2c95f75907d659e1',
    ('monitoring', 'grafana'): 'docker.io/grafana/grafana:12.4.14',
}
MISSING = {'__missing_field__': True}


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def differences(before, after, path=()):
    if before == after:
        return []
    if isinstance(before, dict) and isinstance(after, dict) and before != MISSING and after != MISSING:
        return [d for key in sorted(before.keys() | after.keys())
                for d in differences(before.get(key, MISSING), after.get(key, MISSING), path+(key,))]
    # Keep upstream CRD schemas and RBAC rules atomic; inspect their hashes against
    # the release assets. Walk pod arrays to preserve environment-specific settings.
    if isinstance(before, list) and isinstance(after, list) and len(before) == len(after) and path[-1:] not in [('versions',), ('rules',)]:
        return [d for i, (a, b) in enumerate(zip(before, after)) for d in differences(a, b, path+(i,))]
    return [(path, before, after)]


def fix_new_crd_reference(graph, full_graph, stack):
    """Constrain the chart's new resource reference to its fixed Kubernetes ID.

    Helm v3 serializes an unknown new CRD ID as an empty string during preview.
    Pulumi then treats that as an exact output constraint during apply. Only this
    reviewed, cluster-scoped CRD has a deterministic ID that we may fill in.
    """
    prefix = f'urn:pulumi:{stack}::mcp-registry-infra::'
    chart = prefix+'kubernetes:helm.sh/v3:Chart::cloudnative-pg'
    name = 'databaseroles.postgresql.cnpg.io'
    key = 'apiextensions.k8s.io/v1/CustomResourceDefinition::'+name
    urn = prefix+'kubernetes:helm.sh/v3:Chart$kubernetes:apiextensions.k8s.io/v1:CustomResourceDefinition::'+name
    resource = graph['resourcePlans'].get(urn)
    if not resource or resource.get('steps') != ['create']:
        return graph
    require(resource['goal']['type'] == 'kubernetes:apiextensions.k8s.io/v1:CustomResourceDefinition', 'new-crd-type')
    require(resource['goal']['inputDiff']['adds']['metadata']['name'] == name, 'new-crd-name')
    reference = graph['resourcePlans'][chart]['goal']['outputDiff']['updates']['resources'][key]
    full_reference = full_graph['resourcePlans'][chart]['goal']['outputDiff']['updates']['resources'][key]
    require(reference == full_reference and reference['urn'] == urn and reference['id'] == '', 'new-crd-reference')
    result = copy.deepcopy(graph)
    result['resourcePlans'][chart]['goal']['outputDiff']['updates']['resources'][key]['id'] = name
    return result


def validate(preview, environment, previous, policy):
    stack = {'prod': 'gcpProd', 'staging': 'gcpStaging'}[environment]
    prefix = f'urn:pulumi:{stack}::mcp-registry-infra::'
    require(preview.get('steps') and isinstance(preview.get('changeSummary'), dict), 'preview-complete')
    require(not preview.get('maybeCorrupt'), 'preview-state')
    require(not any(d.get('severity') == 'error' for d in preview.get('diagnostics', [])), 'preview-errors')
    changed = set()
    for step in preview['steps']:
        urn, op = step['urn'], step['op']
        if op in ('same', 'read', 'refresh'):
            continue
        require(urn.startswith(prefix), 'stack-identity')
        old, new = step.get('oldState'), step.get('newState')
        if urn == prefix+'pulumi:pulumi:Stack::mcp-registry-infra-'+stack:
            require(op == 'update' and old and new and old.get('inputs') == new.get('inputs'), 'stack-output-only')
            continue
        require(op in ('update', 'create') and new, 'no-delete-or-replacement')
        if excluded_unchanged_rbac(step, previous):
            continue
        suffix = urn[len(prefix):]
        rule = policy['resources'].get(suffix)
        require(rule is not None, 'outside-security-patch-scope')
        # TTL-cleaned Helm certificate jobs may be recreated only with exactly
        # the reviewed inputs. Their previous successful definition is the base.
        hook = rule.get('completedHook', False)
        if hook and op == 'create':
            old = previous.get(urn)
            require(old and old.get('outputs', {}).get('status', {}).get('succeeded') == 1, 'completed-hook-baseline')
        if old:
            require(all(old.get(k) == new.get(k) for k in ('provider', 'parent', 'protect', 'retainOnDelete')), 'resource-identity')
            allowed = {tuple(c['path']): c for c in rule.get('changes', [])}
            for path, before, after in differences(old['inputs'], new['inputs']):
                change = allowed.get(path)
                require(change and digest(before) in change['before'] and digest(after) == change['after'], 'unreviewed-field-transition')
        else:
            require(op == 'create' and digest(new['inputs']) == rule.get('create'), 'unreviewed-create')
        # The admission certificate already exists. Keep completed, TTL-expired
        # setup jobs out of the apply, including their reviewed image-only change.
        if not hook:
            changed.add(suffix)
    return sorted(changed)


def preflight(environment, directory, env, label, patched=False):
    volumes = volume_preflight(environment, directory, env, label)
    context = f'gke_mcp-registry-{environment}_us-central1-b_mcp-registry-{environment}'
    clusters = json.loads(quiet(['kubectl', '--context', context, 'get', 'clusters.postgresql.cnpg.io', '-A', '-o', 'json'], directory, label+'-databases', env).read_text())['items']
    require({(c['metadata']['namespace'], c['metadata']['name']) for c in clusters} == {('default', 'registry-pg'), ('monitoring', 'grafana-pg')}, 'database-inventory')
    for cluster in clusters:
        require(cluster['spec']['imageName'] == PG_IMAGE and cluster['spec']['instances'] == 1 and cluster['status'].get('readyInstances') == 1, 'database-engine-and-readiness')
        if patched:
            namespace, name = cluster['metadata']['namespace'], cluster['metadata']['name']
            pod = json.loads(quiet(['kubectl', '--context', context, '-n', namespace, 'get', 'pod', name+'-1', '-o', 'json'], directory, label+'-'+name, env).read_text())
            require(any(c['image'] == IMAGES[('cnpg-system', 'cloudnative-pg')] for c in pod['spec']['initContainers']), 'database-instance-manager-version')
    for (namespace, name), image in IMAGES.items():
        deployment = json.loads(quiet(['kubectl', '--context', context, '-n', namespace, 'get', 'deployment', name, '-o', 'json'], directory, label+'-'+name, env).read_text())
        require(deployment['status'].get('readyReplicas', 0) >= deployment['spec'].get('replicas', 1), 'component-readiness')
        if patched:
            require(deployment['status'].get('updatedReplicas') == deployment['spec']['replicas'], 'component-rollout')
            require(image in [c['image'] for c in deployment['spec']['template']['spec']['containers']], 'component-version')
    return volumes


def backup(environment, directory, env):
    context = f'gke_mcp-registry-{environment}_us-central1-b_mcp-registry-{environment}'
    env = dict(env, KUBECTL_REMOTE_COMMAND_WEBSOCKETS='false')
    destination = f'gs://mcp-registry-{environment}-backups/security-upgrades/202611/{time.time_ns()}'
    for namespace, name in [('default', 'registry-pg'), ('monitoring', 'grafana-pg')]:
        command = ['kubectl', '--context', context, '-n', namespace, 'exec', name+'-1', '-c', 'postgres', '--']
        dump = quiet(command+['pg_dump', '-U', 'postgres', '-d', 'app', '-Fc'], directory, 'backup-'+name, env)
        require(dump.stat().st_size > 1000 and dump.read_bytes()[:5] == b'PGDMP', 'backup-format')
        # Read the entire archive, including table data: a readable archive index
        # alone does not detect truncated dumps. Keep all diagnostic output private.
        with dump.open('rb') as src, (directory/(name+'-toc')).open('wb') as out, (directory/(name+'-verify.err')).open('wb') as err:
            command = ['kubectl', '--context', context, '-n', namespace, 'exec', '-i', name+'-1', '-c', 'postgres', '--', 'pg_restore', '--file=/dev/null']
            result = subprocess.run(command, stdin=src, stdout=out, stderr=err, env=dict(env, KUBECTL_REMOTE_COMMAND_WEBSOCKETS='false'), timeout=180)
        require(result.returncode == 0, 'backup-archive-verification')
        quiet(['gcloud', 'storage', 'cp', str(dump), destination+'/'+name+'.dump', '--quiet'], directory, 'upload-'+name, env)
        readback = quiet(['gcloud', 'storage', 'cat', destination+'/'+name+'.dump'], directory, 'readback-'+name, env)
        require(hashlib.sha256(dump.read_bytes()).digest() == hashlib.sha256(readback.read_bytes()).digest(), 'backup-readback')
    print('Both database backups verified and retained at '+destination, flush=True)


def main(environment, mode):
    require(environment in ('prod', 'staging') and mode in ('preview', 'apply'), 'arguments')
    require(mode != 'apply' or os.environ.get('GITHUB_REF') == 'refs/heads/main', 'apply-requires-main')
    os.umask(0o077)
    stack = 'gcpProd' if environment == 'prod' else 'gcpStaging'
    env = dict(os.environ, PULUMI_BACKEND_URL=f'gs://mcp-registry-{environment}-pulumi-state',
               PULUMI_CONFIG_PASSPHRASE_FILE=os.environ.get('PULUMI_CONFIG_PASSPHRASE_FILE', str(Path(f'passphrase.{environment}.txt').resolve())),
               PULUMI_SKIP_UPDATE_CHECK='true', PULUMI_ENABLE_STREAMING_JSON_PREVIEW='true')
    policy = json.loads(POLICY_PATH.read_text())
    with tempfile.TemporaryDirectory(prefix='registry-private-security-') as temp:
        directory = Path(temp); base = ['--stack', stack, '--non-interactive']
        quiet(['go', 'build'], directory, 'build', env)
        cfg = json.loads(quiet(['pulumi', 'config', '--json']+base, directory, 'config', env).read_text())
        require(cfg.get('mcp-registry:securityPatch202611', {}).get('value') == 'true', 'explicit-security-profile')
        quiet(['gcloud', 'container', 'clusters', 'get-credentials', 'mcp-registry-'+environment, '--project=mcp-registry-'+environment, '--zone=us-central1-b', '--quiet'], directory, 'credentials', env)
        volumes = preflight(environment, directory, env, 'before')
        exported = directory/'before.json'
        quiet(['pulumi', 'stack', 'export', '--file', str(exported)]+base, directory, 'state', env)
        previous = {r['urn']: r for r in json.loads(exported.read_text())['deployment']['resources']}
        full_plan = directory/'full-plan.json'
        result = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(full_plan)]+base, directory, 'full-preview', env)
        changes = validate(parse_preview_events(result.read_text()), environment, previous, policy)
        print(f'Full security scope validated: {len(changes)} reviewed resource changes; no deletes or replacements.', flush=True)
        if not changes:
            print('Private security plan validated: no changes.', flush=True)
            return
        prefix = f'urn:pulumi:{stack}::mcp-registry-infra::'
        target_urns = [prefix+suffix for suffix in changes]
        targets = [arg for urn in target_urns for arg in ('--target', urn)]
        plan = directory/'target-plan.json'
        result = quiet(['pulumi', 'preview', '--refresh=false', '--json', '--save-plan', str(plan)]+base+targets, directory, 'preview', env)
        validate(parse_preview_events(result.read_text()), environment, previous, policy)
        full_graph, target_graph = decode_plan(full_plan.read_text()), decode_plan(plan.read_text())
        for urn in target_urns:
            require(dependency_signature(full_graph['resourcePlans'][urn]['goal']) == dependency_signature(target_graph['resourcePlans'][urn]['goal']), 'target-dependencies')
        graph, dependency_changes = reconcile_plan_dependencies(full_graph, target_graph)
        if dependency_changes:
            confirmation = directory/'confirmed-plan.json'
            result = quiet(['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(confirmation)]+base, directory, 'confirm-preview', env)
            validate(parse_preview_events(result.read_text()), environment, previous, policy)
            confirmed = decode_plan(confirmation.read_text())
            require(all(dependency_signature(full_graph['resourcePlans'][u]['goal']) == dependency_signature(confirmed['resourcePlans'][u]['goal']) for u in dependency_changes), 'stable-dependency-graph')
        graph = fix_new_crd_reference(graph, full_graph, stack)
        document = json.loads(plan.read_text())
        if 'version' in document:
            document['plan'] = graph
        else:
            document = graph
        plan = directory/'validated-plan.json'
        plan.write_text(json.dumps(document))
        print('Private targeted security plan validated; completed admission jobs excluded.', flush=True)
        if mode == 'apply' and changes:
            backup(environment, directory, env)
            require(preflight(environment, directory, env, 'preapply') == volumes, 'volume-identity-before-apply')
            quiet(['pulumi', 'up', '--refresh=false', '--json', '--yes', '--skip-preview', '--plan', str(plan)]+base+targets, directory, 'apply', env)
            # Operator readiness precedes asynchronous database instance-manager
            # rollouts. Wait for both upgraded databases, not just Helm resources.
            deadline = time.monotonic()+900
            while True:
                try:
                    require(preflight(environment, directory, env, 'after', patched=True) == volumes, 'volume-identity-after-apply')
                    break
                except Blocked:
                    if time.monotonic() >= deadline:
                        raise
                    time.sleep(15)
            print('Security upgrade completed; component versions, databases and volume identities verified.', flush=True)


if __name__ == '__main__':
    try:
        main(*sys.argv[1:])
    except Blocked as error:
        print('Security deployment blocked: '+str(error), file=sys.stderr); sys.exit(1)
    except (ValueError, KeyError, TypeError, IndexError, OSError, subprocess.TimeoutExpired):
        print('Security deployment blocked: private command or schema failure.', file=sys.stderr); sys.exit(1)
