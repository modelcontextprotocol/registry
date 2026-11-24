import hashlib
import json
from copy import deepcopy
from datetime import timedelta
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

from production_apply import Blocked
from usage_key_retirement import (
    JOBS, KEY_RESOURCE, PRODUCTION_KEY, PRODUCTION_PREFIX, PRODUCTION_PROOF_EXPIRES,
    RETIREMENT_RESOURCES, SECRET, check_retirement, reviewed_production_plan, verify_runs,
)


def resources():
    items = []
    for name in JOBS:
        pod_spec = {'serviceAccountName': name, 'nodeSelector': {'iam.gke.io/gke-metadata-server-enabled': 'true'}}
        schedule = '2026-11-24T03:30:00Z'
        cron = {'kind': 'CronJob', 'status': {'lastScheduleTime': schedule}, 'metadata': {'name': name, 'uid': name},
                'spec': {'jobTemplate': {'spec': {'template': {'spec': deepcopy(pod_spec)}}}}}
        job = {'kind': 'Job', 'metadata': {'uid': name+'-job', 'annotations': {'batch.kubernetes.io/cronjob-scheduled-timestamp': schedule},
               'ownerReferences': [{'controller': True, 'uid': name}]}, 'spec': {'template': {'spec': deepcopy(pod_spec)}},
               'status': {'conditions': [{'type': 'Complete', 'status': 'True'}]}}
        pod = {'kind': 'Pod', 'metadata': {'ownerReferences': [{'controller': True, 'uid': name+'-job'}]},
               'spec': pod_spec, 'status': {'phase': 'Succeeded'}}
        items.extend([cron, job, pod])
    return items


class RetirementTests(unittest.TestCase):
    def test_completed_scheduled_runs(self):
        verify_runs(resources())

    def test_old_or_unproven_runs(self):
        for scenario in ('missing', 'manual', 'pending', 'old-job', 'old-pod', 'suspended', 'retry'):
            with self.subTest(scenario=scenario), self.assertRaises(Blocked):
                items = resources()
                cron, job, pod = items[-3:]
                if scenario == 'missing':
                    items.pop()
                if scenario == 'manual':
                    job['metadata']['annotations'] = {'cronjob.kubernetes.io/instantiate': 'manual'}
                if scenario == 'pending':
                    job['status'] = {}
                if scenario == 'old-job':
                    job['spec']['template']['spec'].pop('serviceAccountName')
                if scenario == 'old-pod':
                    pod['spec']['volumes'] = [{'secret': {'secretName': SECRET}}]
                if scenario == 'suspended':
                    cron['spec']['suspend'] = True
                if scenario == 'retry':
                    items.append({'kind': 'Job', 'metadata': {}, 'spec': {'template': {
                        'spec': {'volumes': [{'secret': {'secretName': SECRET}}]}}}})
                verify_runs(items)

    def test_reviewed_jobs_are_bound_to_their_pods_and_current_cron_templates(self):
        items = resources()
        proof = {}
        for cron, scheduled, _ in (items[:3], items[3:6]):
            name = cron['metadata']['name']
            job = deepcopy(scheduled)
            job['metadata'] = {'uid': name + '-manual'}
            pod = {'kind': 'Pod', 'metadata': {'uid': name + '-pod', 'ownerReferences': [
                {'controller': True, 'uid': job['metadata']['uid']}]},
                'status': {'phase': 'Succeeded'}, 'spec': deepcopy(job['spec']['template']['spec'])}
            items.extend((job, pod))
            scheduled['spec']['template']['spec']['volumes'] = [{'secret': {'secretName': SECRET}}]
            spec = cron['spec']['jobTemplate']['spec']['template']['spec']
            proof[name] = {
                'schedule': cron['status']['lastScheduleTime'], 'cron': name, 'job': name + '-manual',
                'pod': name + '-pod',
                'template': hashlib.sha256(json.dumps(spec, sort_keys=True, separators=(',', ':')).encode()).hexdigest(),
            }
        verify_runs(items, proof)
        with self.assertRaises(Blocked):
            verify_runs(items)
        for field in proof[JOBS[0]]:
            candidate = deepcopy(proof)
            candidate[JOBS[0]][field] = 'different'
            with self.subTest(field=field), self.assertRaises(Blocked):
                verify_runs(items, candidate)
        for target in ('failed-job', 'foreign-pod', 'active-legacy', 'pod-key'):
            altered = deepcopy(items)
            job, pod = altered[-2:]
            if target == 'failed-job':
                job['status'] = {}
            if target == 'foreign-pod':
                pod['metadata']['ownerReferences'] = []
            if target == 'active-legacy':
                altered.append({'kind': 'Pod', 'metadata': {}, 'status': {'phase': 'Running'},
                                'spec': {'volumes': [{'secret': {'secretName': SECRET}}]}})
            if target == 'pod-key':
                pod['spec']['env'] = [{'name': 'GOOGLE_APPLICATION_CREDENTIALS'}]
            with self.subTest(target=target), self.assertRaises(Blocked):
                verify_runs(altered, proof)

    def test_reviewed_plan_matches_only_five_deletions_and_the_refreshed_key_id_before_expiry(self):
        changes = [{'urn': PRODUCTION_PREFIX + resource, 'operation': 'delete'} for resource in RETIREMENT_RESOURCES]
        step = {'urn': PRODUCTION_PREFIX + KEY_RESOURCE, 'op': 'delete', 'old': {'id': PRODUCTION_KEY}}
        before = PRODUCTION_PROOF_EXPIRES - timedelta(seconds=1)
        with TemporaryDirectory() as directory:
            path = Path(directory) / 'preview'
            path.write_text(json.dumps({'resourcePreEvent': {'metadata': step}}))
            self.assertTrue(reviewed_production_plan(changes, 'gcpProd', 'prod', before, path))
            for resources_, stack, env, now in [
                (changes[:-1], 'gcpProd', 'prod', before),
                (changes + changes[:1], 'gcpProd', 'prod', before),
                (changes[1:] + [dict(changes[0], operation='update')], 'gcpProd', 'prod', before),
                (changes, 'gcpProd', 'prod', PRODUCTION_PROOF_EXPIRES),
                (changes, 'gcpStaging', 'staging', before),
            ]:
                self.assertFalse(reviewed_production_plan(resources_, stack, env, now, path))
            step['old']['id'] = 'another-key'
            path.write_text(json.dumps({'resourcePreEvent': {'metadata': step}}))
            self.assertFalse(reviewed_production_plan(changes, 'gcpProd', 'prod', before, path))

    @patch('usage_key_retirement.read_json')
    def test_only_key_deletion_enters_retirement_gate(self, read):
        step = {'urn': 'urn:pulumi:gcpProd::mcp-registry-infra::gcp:serviceaccount/key:Key::usage-collector-key',
                'operation': 'update'}
        check_retirement([step], 'gcpProd', 'prod', None, {})
        read.assert_not_called()
        step['operation'] = 'delete'
        read.return_value = {'mcp-registry:usageCollector': {'value': 'true'}}
        with self.assertRaises(Blocked):
            check_retirement([step], 'gcpProd', 'prod', None, {})
