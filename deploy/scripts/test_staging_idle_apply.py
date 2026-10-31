import copy
import unittest
from staging_idle_apply import validate, WORKERS, APP_HPA, INGRESS_HPA, APP_PDB, INGRESS_PDB, Blocked


def step(urn, before, after, op='update'):
    return {'urn': urn, 'op': op, 'oldState': {'inputs': before}, 'newState': {'inputs': after}}


def preview(change):
    return {'steps': [change], 'changeSummary': {'update': 1}}


class IdleScopeTests(unittest.TestCase):
    def test_only_minimum_changes_allowed(self):
        for urn, field, before, after in [(WORKERS, 'autoscaling', 2, 1), (APP_HPA, 'spec', 3, 1), (INGRESS_HPA, 'spec', 3, 1)]:
            key = 'minNodeCount' if urn == WORKERS else 'minReplicas'
            maxkey = 'maxNodeCount' if urn == WORKERS else 'maxReplicas'
            maximum = 3 if urn == WORKERS else (4 if urn == APP_HPA else 6)
            old = {field: {key: before, maxkey: maximum}, 'preserved': {'value': 'fixture'}}
            new = copy.deepcopy(old)
            new[field][key] = after
            self.assertTrue(validate(preview(step(urn, old, new)), True))
            self.assertTrue(validate(preview(step(urn, new, old)), False))
            new['preserved']['value'] = 'changed'
            with self.assertRaises(Blocked):
                validate(preview(step(urn, old, new)), True)

    def test_unrelated_or_destructive_operations_blocked(self):
        for urn, op in [('database', 'update'), (WORKERS, 'delete'), (WORKERS, 'replace'), (APP_HPA, 'create')]:
            with self.assertRaises(Blocked):
                validate(preview(step(urn, {}, {}, op)), True)

    def test_empty_and_failed_preview_blocked(self):
        for p in [{}, {'steps': [], 'changeSummary': {}}, dict(preview(step(WORKERS, {}, {})), maybeCorrupt=True)]:
            with self.assertRaises(Blocked):
                validate(p, True)

    def test_singleton_budget_transition_and_database_rejection(self):
        old = {'spec': {'minAvailable': 1, 'selector': {'matchLabels': {'app': 'mcp-registry'}}}}
        new = copy.deepcopy(old)
        new['spec']['minAvailable'] = 0
        validate(preview(step(APP_PDB, old, new)), True)
        validate(preview(step(APP_PDB, new, old)), False)
        with self.assertRaises(Blocked):
            validate(preview(step('registry-pg-primary', old, new)), True)
        ingress = {'metadata': {'name': 'ingress-nginx-controller', 'namespace': 'ingress-nginx'}, 'spec': {'minAvailable': 1, 'selector': {'matchLabels': {'app.kubernetes.io/component': 'controller', 'app.kubernetes.io/instance': 'ingress-nginx', 'app.kubernetes.io/name': 'ingress-nginx'}}}}
        change = {'urn': INGRESS_PDB, 'op': 'delete', 'oldState': {'inputs': ingress}, 'newState': None}
        validate(preview(change), True)
        change['oldState']['inputs']['spec']['minAvailable'] = 2
        with self.assertRaises(Blocked):
            validate(preview(change), True)

    def test_hpa_ownership_annotation_is_narrow(self):
        old = {'metadata': {}, 'spec': {'minReplicas': 3, 'maxReplicas': 4}}
        new = copy.deepcopy(old)
        new['spec']['minReplicas'] = 1
        new['metadata']['annotations'] = {'pulumi.com/patchForce': 'true'}
        validate(preview(step(APP_HPA, old, new)), True)
        new['metadata']['annotations']['unrelated'] = 'value'
        with self.assertRaises(Blocked):
            validate(preview(step(APP_HPA, old, new)), True)
