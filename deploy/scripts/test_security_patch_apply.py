import copy
import unittest

from production_apply import Blocked
from security_patch_apply import validate, digest, differences, MISSING


PREFIX = 'urn:pulumi:gcpStaging::mcp-registry-infra::'
RESOURCE = 'kubernetes:helm.sh/v3:Chart$kubernetes:apps/v1:Deployment::monitoring/grafana'


class SecurityPatchTests(unittest.TestCase):
    def setUp(self):
        self.old = {'inputs': {'spec': {'template': {'spec': {'containers': [{'name': 'grafana', 'image': 'old', 'env': [{'name': 'DATABASE_HOST', 'value': 'existing'}]}]}}}}, 'provider': 'existing'}
        self.new = copy.deepcopy(self.old)
        self.new['inputs']['spec']['template']['spec']['containers'][0]['image'] = 'patched'
        self.policy = {'resources': {RESOURCE: {'changes': [
            {'path': list(p), 'before': [digest(a)], 'after': digest(b)}
            for p, a, b in differences(self.old['inputs'], self.new['inputs'])]}}}
        self.step = {'urn': PREFIX+RESOURCE, 'op': 'update', 'oldState': self.old, 'newState': self.new}

    def check(self, step=None):
        return validate({'steps': [step or self.step], 'changeSummary': {'update': 1}}, 'staging', {}, self.policy)

    def test_reviewed_image_only(self):
        self.assertEqual(self.check(), [RESOURCE])

    def test_rejects_unreviewed_image(self):
        self.new['inputs']['spec']['template']['spec']['containers'][0]['image'] = 'attacker'
        with self.assertRaises(Blocked): self.check()

    def test_rejects_unrelated_environment_change(self):
        self.new['inputs']['spec']['template']['spec']['containers'][0]['env'][0]['value'] = 'different'
        with self.assertRaises(Blocked): self.check()

    def test_rejects_unknown_baseline(self):
        self.old['inputs']['spec']['template']['spec']['containers'][0]['image'] = 'unexpected'
        with self.assertRaises(Blocked): self.check()

    def test_rejects_provider_change(self):
        self.new['provider'] = 'other-cluster'
        with self.assertRaises(Blocked): self.check()

    def test_rejects_destructive_operations(self):
        for op in ['delete', 'replace', 'create-replacement', 'delete-replaced']:
            with self.subTest(op=op), self.assertRaises(Blocked):
                self.check(dict(self.step, op=op))

    def test_rejects_application_and_volume_changes(self):
        for resource in ['kubernetes:apps/v1:Deployment::mcp-registry', 'kubernetes:core/v1:PersistentVolumeClaim::registry-pg-1', 'gcp:container/nodePool:NodePool::workers']:
            with self.subTest(resource=resource), self.assertRaises(Blocked):
                self.check(dict(self.step, urn=PREFIX+resource))

    def test_exact_new_crd_only(self):
        self.policy['resources'][RESOURCE] = {'create': digest(self.new['inputs'])}
        self.check(dict(self.step, oldState=None, op='create'))
        self.new['inputs']['unexpected'] = 'field'
        with self.assertRaises(Blocked): self.check(dict(self.step, oldState=None, op='create'))

    def test_missing_not_null(self):
        self.assertNotEqual(digest(MISSING), digest(None))
        self.assertEqual(differences({}, {'field': None}), [(('field',), MISSING, None)])

    def test_partial_retry_allows_already_patched_fields(self):
        self.step['oldState'] = copy.deepcopy(self.new)
        self.check()

    def test_rejects_incomplete_preview(self):
        with self.assertRaises(Blocked): validate({'steps': [self.step]}, 'staging', {}, self.policy)


if __name__ == '__main__':
    unittest.main()
