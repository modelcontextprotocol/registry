import copy
import unittest

from production_apply import Blocked
from security_patch_apply import validate, digest, differences, MISSING, fix_new_crd_reference


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

    def test_completed_hook_is_validated_but_not_targeted(self):
        self.policy['resources'][RESOURCE]['completedHook'] = True
        self.old['outputs'] = {'status': {'succeeded': 1}}
        step = dict(self.step, oldState=None, op='create')
        preview = {'steps': [step], 'changeSummary': {'create': 1}}
        self.assertEqual(validate(preview, 'staging', {PREFIX+RESOURCE: self.old}, self.policy), [])
        self.new['inputs']['spec']['template']['spec']['containers'][0]['env'][0]['value'] = 'changed'
        with self.assertRaises(Blocked):
            validate(preview, 'staging', {PREFIX+RESOURCE: self.old}, self.policy)
        with self.assertRaises(Blocked):
            validate(preview, 'staging', {}, self.policy)

    def test_new_crd_reference_fills_only_deterministic_id(self):
        key = 'apiextensions.k8s.io/v1/CustomResourceDefinition::databaseroles.postgresql.cnpg.io'
        urn = PREFIX+'kubernetes:helm.sh/v3:Chart$kubernetes:apiextensions.k8s.io/v1:CustomResourceDefinition::databaseroles.postgresql.cnpg.io'
        chart = PREFIX+'kubernetes:helm.sh/v3:Chart::cloudnative-pg'
        graph = {'resourcePlans': {
            urn: {'steps': ['create'], 'goal': {
                'type': 'kubernetes:apiextensions.k8s.io/v1:CustomResourceDefinition',
                'inputDiff': {'adds': {'metadata': {'name': 'databaseroles.postgresql.cnpg.io'}, 'spec': {'reviewed': True}}}}},
            chart: {'goal': {'outputDiff': {'updates': {'resources': {key: {'urn': urn, 'id': ''}}}}}},
        }}
        expected = copy.deepcopy(graph)
        expected['resourcePlans'][chart]['goal']['outputDiff']['updates']['resources'][key]['id'] = 'databaseroles.postgresql.cnpg.io'
        self.assertEqual(fix_new_crd_reference(graph, graph, 'gcpStaging'), expected)
        self.assertEqual(graph['resourcePlans'][chart]['goal']['outputDiff']['updates']['resources'][key]['id'], '')
        different = copy.deepcopy(graph)
        different['resourcePlans'][chart]['goal']['outputDiff']['updates']['resources'][key]['urn'] = 'other'
        with self.assertRaises(Blocked): fix_new_crd_reference(graph, different, 'gcpStaging')
        graph['resourcePlans'][urn]['goal']['inputDiff']['adds']['metadata']['name'] = 'other'
        with self.assertRaises(Blocked): fix_new_crd_reference(graph, graph, 'gcpStaging')

    def test_rejects_incomplete_preview(self):
        with self.assertRaises(Blocked): validate({'steps': [self.step]}, 'staging', {}, self.policy)


if __name__ == '__main__':
    unittest.main()
