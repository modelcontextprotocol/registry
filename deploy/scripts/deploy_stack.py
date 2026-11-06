#!/usr/bin/env python3
"""Normal full-stack GitOps preview/apply, using the exact saved preview plan.

No migration-specific resource allowlist. Raw events and plans can contain private
state, so they stay in an owner-only temporary directory and are deleted on exit.
The log contains resource identities/operations, never inputs or secret values.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

from production_apply import Blocked, require, quiet, parse_preview_events, decode_plan
from staging_image import resolve, validate


def commands(stack, plan):
    # Preview refresh is read-only. Apply must refresh too, otherwise TTL-deleted
    # resources still in the checkpoint can turn planned creates into replacements.
    base = ['--stack', stack, '--non-interactive']
    return (['pulumi', 'preview', '--refresh', '--json', '--save-plan', str(plan)] + base,
            ['pulumi', 'up', '--refresh', '--json', '--yes', '--skip-preview', '--plan', str(plan)] + base)


def summary(events):
    result = parse_preview_events(events)
    require(not result.get('maybeCorrupt'), 'preview-state')
    require(isinstance(result.get('changeSummary'), dict), 'preview-summary')
    require(not any(d.get('severity') == 'error' for d in result.get('diagnostics', [])), 'preview-errors')
    return {'changes': result['changeSummary'], 'resources': [
        {'operation': step['op'], 'urn': step['urn']}
        for step in result['steps'] if step['op'] not in ('same', 'read', 'refresh')]}


def constrain_admission_job_ids(document, stack):
    """Correct Helm v3's empty preview IDs for the two fixed-name setup Jobs.

    The existing security rollout fixed the same serialization issue for a CRD.
    Fill exact identities here; never remove constraints or alter resource inputs.
    """
    decode_plan(json.dumps(document))  # Validate the supported envelope before mutation.
    graph = document["plan"] if "version" in document else document
    prefix = f'urn:pulumi:{stack}::mcp-registry-infra::'
    chart = prefix + 'kubernetes:helm.sh/v3:Chart::ingress-nginx'
    changed = 0
    for name in ('ingress-nginx-admission-create', 'ingress-nginx-admission-patch'):
        identity = 'ingress-nginx/' + name
        urn = prefix + 'kubernetes:helm.sh/v3:Chart$kubernetes:batch/v1:Job::' + identity
        resource = graph['resourcePlans'].get(urn)
        if not resource or resource.get('steps') != ['create']:
            continue
        goal = resource['goal']
        metadata = goal['inputDiff']['adds']['metadata']
        require(goal['type'] == 'kubernetes:batch/v1:Job' and goal['parent'] == chart, 'admission-job-parent')
        require(metadata['name'] == name and metadata['namespace'] == 'ingress-nginx'
                and not metadata.get('generateName'), 'admission-job-fixed-name')
        key = 'batch/v1/Job::' + identity
        reference = graph['resourcePlans'][chart]['goal']['outputDiff']['updates']['resources'][key]
        require(reference['urn'] == urn and reference['id'] in ('', identity), 'admission-job-reference')
        if reference['id'] == '':
            reference['id'] = identity
            changed += 1
    return changed


def main(environment, mode):
    require(environment in ('staging', 'prod') and mode in ('preview', 'apply'), 'arguments')
    if mode == 'apply' and os.environ.get('GITHUB_ACTIONS') == 'true':
        require(os.environ.get('GITHUB_REF') == 'refs/heads/main', 'apply-main-only')
    os.umask(0o077)
    stack = 'gcpStaging' if environment == 'staging' else 'gcpProd'
    env = dict(os.environ, PULUMI_BACKEND_URL=f'gs://mcp-registry-{environment}-pulumi-state',
               PULUMI_SKIP_UPDATE_CHECK='true', PULUMI_ENABLE_STREAMING_JSON_PREVIEW='true')
    env.setdefault('PULUMI_CONFIG_PASSPHRASE_FILE', str(Path(f'passphrase.{environment}.txt').resolve()))
    if environment == 'staging':
        env['REGISTRY_STAGING_IMAGE'] = validate(env['REGISTRY_STAGING_IMAGE']) if env.get('REGISTRY_STAGING_IMAGE') else resolve()
    with tempfile.TemporaryDirectory(prefix='registry-private-deployment-') as temp:
        directory = Path(temp)
        preview, apply = commands(stack, directory/'plan.json')
        result = quiet(preview, directory, 'preview', env)
        print(json.dumps(summary(result.read_text()), indent=2), flush=True)
        plan = directory/'plan.json'
        document = json.loads(plan.read_text())
        fixed = constrain_admission_job_ids(document, stack)
        if fixed:
            plan.write_text(json.dumps(document))
            print(f'Constrained {fixed} Helm admission-job references to their fixed Kubernetes IDs.', flush=True)
        if mode == 'apply':
            quiet(apply, directory, 'apply', env)
            print('Deployment completed using the saved full-stack plan.', flush=True)


if __name__ == '__main__':
    try:
        require(len(sys.argv) == 3, 'arguments')
        main(*sys.argv[1:])
    except Blocked as error:
        sys.exit('Deployment blocked: ' + str(error))
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError):
        sys.exit('Deployment failed; private command or schema error.')
