import copy
import unittest

from retire_k8up import KINDS, cleanup_targets, validate


class RetirementChecks(unittest.TestCase):
    def fixture(self):
        data = {kind: [] for kind in KINDS}
        data['backups'] = [{'metadata': {'uid': 'backup'}, 'status': {'finished': True}}]
        data['jobs'] = [{'metadata': {'uid': 'job', 'namespace': 'default', 'name': 'old-backup',
                                      'ownerReferences': [{'uid': 'backup'}]},
                         'status': {'conditions': [{'type': 'Complete', 'status': 'True'}]}},
                        {'metadata': {'uid': 'current', 'namespace': 'default', 'name': 'registry-database-backup-123',
                                      'ownerReferences': [{'uid': 'cronjob'}]}, 'status': {'active': 1}}]
        data['pods'] = [{'metadata': {'uid': 'pod', 'ownerReferences': [{'uid': 'job'}]}, 'status': {'phase': 'Succeeded'}}]
        data['pvcs'] = [{'metadata': {'name': 'prod-dump-pvc'}}]
        return data

    def test_preserves_current_backup_jobs_and_unowned_volume(self):
        jobs, pods = validate(self.fixture())
        self.assertEqual([j['metadata']['name'] for j in jobs], ['old-backup'])
        self.assertEqual(len(pods), 1)

    def test_blocks_activity_finalizers_and_volume_cascades(self):
        original = self.fixture()
        for target, key, value in [('backups', 'status', {'finished': False}),
                                   ('jobs', 'status', {'active': 1}),
                                   ('pods', 'status', {'phase': 'Running'}),
                                   ('pods', 'metadata', {'uid': 'pod', 'ownerReferences': [{'uid': 'job'}], 'finalizers': ['external-cleanup']}),
                                   ('pvcs', 'metadata', {'ownerReferences': [{'uid': 'pod'}]}),
                                   ('backups', 'metadata', {'uid': 'backup', 'finalizers': ['external-cleanup']})]:
            with self.subTest(target=target, key=key):
                data = copy.deepcopy(original)
                data[target][0][key] = value
                with self.assertRaises(ValueError):
                    validate(data)

    def test_post_apply_cleanup_keeps_active_backups_and_rejects_replaced_jobs(self):
        prior = self.fixture()
        current = copy.deepcopy(prior)
        current['backups'] = []
        jobs, pods = cleanup_targets(prior, current)
        self.assertEqual(len(jobs), 1)
        self.assertEqual(len(pods), 1)
        current['jobs'][0]['metadata']['uid'] = 'replacement'
        with self.assertRaises(ValueError):
            cleanup_targets(prior, current)

    def test_post_apply_cleanup_finds_pods_after_job_gc(self):
        prior = self.fixture()
        current = copy.deepcopy(prior)
        current['jobs'] = []
        jobs, pods = cleanup_targets(prior, current)
        self.assertEqual(jobs, [])
        self.assertEqual(len(pods), 1)

    def test_post_apply_cleanup_finds_directly_owned_pods_after_orphaning(self):
        prior = self.fixture()
        prior['pods'][0]['metadata']['ownerReferences'] = [{'uid': 'backup'}]
        current = copy.deepcopy(prior)
        current['backups'] = []
        current['pods'][0]['metadata'].pop('ownerReferences')
        _, pods = cleanup_targets(prior, current)
        self.assertEqual([p['metadata']['uid'] for p in pods], ['pod'])
        current['pods'][0]['metadata']['uid'] = 'replacement'
        _, pods = cleanup_targets(prior, current)
        self.assertEqual(pods, [])


if __name__ == '__main__':
    unittest.main()
