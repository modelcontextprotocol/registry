from copy import deepcopy
import unittest
from unittest.mock import patch

from production_apply import Blocked
from usage_key_retirement import JOBS, SECRET, check_retirement, verify_runs


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
