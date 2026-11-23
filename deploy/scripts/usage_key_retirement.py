"""Read-only checks before the normal saved-plan apply revokes the usage key."""
import json
import subprocess

from production_apply import require
from production_gateway import read_json

JOBS = ('registry-usage-collector', 'registry-usage-history-backup')
SECRET = 'registry-usage-collector-gcp'


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


def verify_runs(items):
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
        check_identity(latest['spec']['template']['spec'], name)
        successful = [pod for pod in pods if owned_by(pod, latest) and pod['status']['phase'] == 'Succeeded']
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


def check_retirement(resources, stack, environment, directory, env):
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
    verify_runs(objects['items'])
