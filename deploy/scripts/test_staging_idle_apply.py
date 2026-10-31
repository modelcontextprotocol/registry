import copy
import unittest
from staging_idle_apply import validate, WORKERS, APP_HPA, INGRESS_HPA, Blocked


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
