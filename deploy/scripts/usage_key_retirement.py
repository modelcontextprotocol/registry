"""Read-only checks before the normal saved-plan apply revokes the usage key."""
import hashlib
import json
import subprocess
from datetime import datetime, timezone

from production_apply import require
from production_gateway import read_json

JOBS = ('registry-usage-collector', 'registry-usage-history-backup')
SECRET = 'registry-usage-collector-gcp'

# The November 24 production verification wrote a day to an isolated database
# and checked a snapshot in the existing usage repository. Kubernetes assigns
# these UIDs. Recreating a Job or Pod cannot reproduce this evidence.
PRODUCTION_KEY = ('projects/mcp-registry-prod/serviceAccounts/'
                  'registry-usage-reader@mcp-registry-prod.iam.gserviceaccount.com/'
                  'keys/3efe060cd4192eabfc6bb124a9aea3bf259d4842')
PRODUCTION_PREFIX = 'urn:pulumi:gcpProd::mcp-registry-infra::'
KEY_RESOURCE = 'gcp:serviceaccount/key:Key::usage-collector-key'
RETIREMENT_RESOURCES = {
    KEY_RESOURCE,
    'kubernetes:core/v1:Secret::registry-usage-collector-gcp',
    'gcp:projects/iAMMember:IAMMember::usage-collector-log-viewer',
    'gcp:storage/bucketIAMMember:BucketIAMMember::usage-backup-objects',
    'gcp:storage/bucketIAMMember:BucketIAMMember::usage-backup-bucket',
}
PRODUCTION_PROOF = {
    'registry-usage-collector': {
        'schedule': '2026-11-24T01:15:00Z',
        'cron': 'f90dd92d-c563-4967-a15c-3c061b9c94cd',
        'template': '9921c4d383d3e10ea5461ec98884a2428cd9e5e6dd91b26368879987d170ddd9',
        'job': 'fcb11196-8470-4662-967a-3208dcc8f319',
        'pod': 'ddcb4c3c-60da-4806-a6e4-fcfeed4b5633',
    },
    'registry-usage-history-backup': {
        'schedule': '2026-11-24T03:30:00Z',
        'cron': '4097b1db-c8eb-4844-ae7d-babe5dee6f30',
        'template': '47e5ee9a4f61d9004091638a06bbabf1f4d1e73739cc01086cb267331100639b',
        'job': '2b029f07-c4da-4743-ba01-54ab21b60b6e',
        'pod': 'af1c26af-4bc3-4eef-9eae-a00690c2e7c0',
    },
}
PRODUCTION_PROOF_EXPIRES = datetime(2026, 11, 25, 1, 15, tzinfo=timezone.utc)


def has_condition(job, *types):
    return any(condition['type'] in types and condition['status'] == 'True'
               for condition in job.get('status', {}).get('conditions', []))


def owned_by(obj, owner):
    return any(ref.get('controller') and ref['uid'] == owner['metadata']['uid']
               for ref in obj['metadata'].get('ownerReferences', []))


def check_identity(spec, name):
    require(spec.get('serviceAccountName') == name and
            spec.get('nodeSelector', {}).get('iam.gke.io/gke-metadata-server-enabled') == 'true',
            f'{name}-requires-metadata-identity')
    encoded = json.dumps(spec)
    require(SECRET not in encoded and 'GOOGLE_APPLICATION_CREDENTIALS' not in encoded,
            f'{name}-uses-key')


def verify_runs(items, reviewed_proof=None):
    jobs = [obj for obj in items if obj['kind'] == 'Job']
    pods = [obj for obj in items if obj['kind'] == 'Pod']
    for name in JOBS:
        cron = next((obj for obj in items if obj['kind'] == 'CronJob' and obj['metadata']['name'] == name), None)
        require(cron and not cron['spec'].get('suspend'), f'{name}-schedule-required')
        check_identity(cron['spec']['jobTemplate']['spec']['template']['spec'], name)
        # kubectl create job --from can also set the CronJob owner reference.
        schedule = cron.get('status', {}).get('lastScheduleTime')
        scheduled = [job for job in jobs if owned_by(job, cron) and
                     job['metadata'].get('annotations', {}).get('batch.kubernetes.io/cronjob-scheduled-timestamp') == schedule]
        require(schedule and len(scheduled) == 1, f'{name}-latest-scheduled-run-required')
        latest = scheduled[0]
        require(has_condition(latest, 'Complete'), f'{name}-latest-scheduled-run-incomplete')
        evidence = latest
        if reviewed_proof is not None:
            proof = reviewed_proof[name]
            spec = cron['spec']['jobTemplate']['spec']['template']['spec']
            digest = hashlib.sha256(json.dumps(spec, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
            require(cron['metadata']['uid'] == proof['cron'] and schedule == proof['schedule']
                    and digest == proof['template'], f'{name}-reviewed-template')
            evidence = next((j for j in jobs if j['metadata'].get('uid') == proof['job']), None)
            require(evidence and has_condition(evidence, 'Complete'), f'{name}-reviewed-job-required')
        check_identity(evidence['spec']['template']['spec'], name)
        successful = [pod for pod in pods if owned_by(pod, evidence) and pod['status']['phase'] == 'Succeeded']
        if reviewed_proof is not None:
            require(any(pod['metadata'].get('uid') == proof['pod'] for pod in successful), f'{name}-reviewed-pod-required')
        require(successful, f'{name}-successful-pod-required')
        for pod in successful:
            check_identity(pod['spec'], name)
    # Include manually created Jobs, even between Pod attempts.
    for job in jobs:
        terminal = has_condition(job, 'Complete', 'Failed')
        require(terminal or SECRET not in json.dumps(job['spec']['template']['spec']), 'unfinished-job-uses-usage-key')
    for pod in pods:
        require(pod['status']['phase'] in ('Failed', 'Succeeded') or SECRET not in json.dumps(pod['spec']),
                'unfinished-pod-uses-usage-key')


def reviewed_production_plan(resources, stack, environment, now, preview_path):
    if (stack, environment) != ('gcpProd', 'prod') or now >= PRODUCTION_PROOF_EXPIRES:
        return False
    expected = {(PRODUCTION_PREFIX + resource, 'delete') for resource in RETIREMENT_RESOURCES}
    if len(resources) != len(expected) or {(r['urn'], r['operation']) for r in resources} != expected:
        return False
    # Bind the approval to the provider ID in the refreshed, saved preview.
    with preview_path.open() as events:
        for line in events:
            if not line.strip():
                continue
            step = json.loads(line).get('resourcePreEvent', {}).get('metadata', {})
            if step.get('urn') == PRODUCTION_PREFIX + KEY_RESOURCE and step['op'] == 'delete':
                return (step.get('old') or {}).get('id') == PRODUCTION_KEY
    return False


def check_retirement(resources, stack, environment, directory, env, reviewed=False):
    key = f'urn:pulumi:{stack}::mcp-registry-infra::gcp:serviceaccount/key:Key::usage-collector-key'
    if not any(step['urn'] == key and step['operation'] == 'delete' for step in resources):
        return
    config = read_json(['pulumi', 'config', '--json', '--stack', stack], env=env)
    if config.get('mcp-registry:usageCollector', {}).get('value') != 'true':
        return  # The independent full-feature retirement does not keep the jobs.
    require(config.get('mcp-registry:usageIdentityPhase', {}).get('value') == 'keyless', 'usage-key-retirement-phase')
    project = 'mcp-registry-' + environment
    kube_env = dict(env, KUBECONFIG=str(directory / 'usage-kubeconfig'))
    subprocess.run(['gcloud', 'container', 'clusters', 'get-credentials', project, '--project', project,
                    '--zone', 'us-central1-b'], env=kube_env, capture_output=True, timeout=60, check=True)
    objects = read_json(['kubectl', '--context', f'gke_{project}_us-central1-b_{project}', '--namespace', 'monitoring',
                         'get', 'cronjobs,jobs,pods', '-o', 'json'], env=kube_env)
    verify_runs(objects['items'], PRODUCTION_PROOF if reviewed else None)
    if reviewed:
        print('Verified the reviewed November 24 production collector and backup Jobs.', flush=True)
