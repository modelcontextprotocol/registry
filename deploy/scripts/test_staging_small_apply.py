import copy
import unittest
from staging_small_apply import validate, APP, INGRESS, DATABASE, WORKERS, DATABASE_POOL, Blocked
from test_staging_idle_apply import step, preview


class SmallCapacityScope(unittest.TestCase):
    def workload(self, urn):
        resources = {'requests': {'cpu': '3' if urn == DATABASE else '800m', 'memory': '512Mi'}, 'limits': {'memory': '4Gi'}}
        if urn == DATABASE:
            before = {'spec': {'resources': resources, 'storage': {'size': '50Gi'}, 'enablePDB': True}}
            after = copy.deepcopy(before)
            after['spec']['resources']['requests']['cpu'] = '250m'
        else:
            before = {'spec': {'replicas': 1, 'template': {'spec': {'containers': [{'name': 'mcp-registry' if urn == APP else 'controller', 'image': 'unchanged', 'resources': resources}]}}}}
            after = copy.deepcopy(before)
            after['spec']['template']['spec']['containers'][0]['resources']['requests']['cpu'] = '100m'
        return step(urn, before, after)

    def test_prepare_only_cpu(self):
        for urn in (APP, INGRESS, DATABASE):
            change = self.workload(urn)
            validate(preview(change), 'prepare')
            with self.assertRaises(Blocked):
                validate(preview(change), 'enabled')
            change['newState']['inputs']['spec']['unrelated'] = True
            with self.assertRaises(Blocked):
                validate(preview(change), 'prepare')

    def test_resize_only_machine_and_preserves_protection(self):
        for urn in (WORKERS, DATABASE_POOL):
            old = {'nodeConfig': {'machineType': 'e2-standard-4', 'labels': {'registry-role': 'database'}, 'diskSizeGb': 50}, 'nodeCount': 1}
            new = copy.deepcopy(old)
            new['nodeConfig']['machineType'] = 'e2-standard-2'
            change = step(urn, old, new)
            validate(preview(change), 'enabled')
            with self.assertRaises(Blocked):
                validate(preview(change), 'prepare')
            for field in ('protect', 'retainOnDelete', 'provider'):
                invalid = copy.deepcopy(change)
                invalid['newState'][field] = 'changed'
                with self.assertRaises(Blocked):
                    validate(preview(invalid), 'enabled')
            for op in ('delete', 'replace', 'create', 'delete-replaced'):
                invalid = copy.deepcopy(change)
                invalid['op'] = op
                with self.assertRaises(Blocked):
                    validate(preview(invalid), 'enabled')
            new['nodeConfig']['diskSizeGb'] = 20
            with self.assertRaises(Blocked):
                validate(preview(step(urn, old, new)), 'enabled')

    def test_never_changes_other_resources(self):
        for phase in ('prepare', 'enabled'):
            with self.assertRaises(Blocked):
                validate(preview(step('unrelated', {}, {})), phase)
            with self.assertRaises(Blocked):
                validate({'steps': []}, phase)
