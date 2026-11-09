import datetime as dt
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('restore', ROOT / 'pkg/k8s/stagingrestore/run.py')
restore = importlib.util.module_from_spec(spec)
spec.loader.exec_module(restore)
NOW = dt.datetime(2026, 11, 10, tzinfo=dt.timezone.utc)


class StagingRestoreTests(unittest.TestCase):
    def snapshot(self):
        return {'id': 'a' * 64, 'hostname': 'registry-postgres', 'tags': ['postgres-custom'], 'time': NOW.isoformat()}

    def test_rejects_stale_future_or_wrong_backup(self):
        for changes in ({'time': (NOW - dt.timedelta(hours=37)).isoformat()},
                        {'time': (NOW + dt.timedelta(hours=1)).isoformat()},
                        {'hostname': 'unrelated'}, {'tags': []}, {'id': 'latest'}):
            with self.subTest(changes=changes), self.assertRaises(RuntimeError):
                restore.select_snapshot([{**self.snapshot(), **changes}], NOW)
        self.assertEqual(restore.select_snapshot([self.snapshot()], NOW), 'a' * 64)

    def test_wrong_environment_cannot_stop_app(self):
        with patch.dict(restore.os.environ, {'RESTORE_ENVIRONMENT': 'prod'}), patch.object(restore, 'api') as api:
            with self.assertRaises(RuntimeError):
                restore.stop()
            api.assert_not_called()

    def test_failed_job_and_lost_controller_detected(self):
        job = {'metadata': {'name': 'registry-staging-restore-1', 'creationTimestamp': NOW.isoformat()},
               'status': {'conditions': [{'type': 'Failed', 'status': 'True'}]}}
        self.assertIn('failed', restore.failed_reason([job], {}, NOW))
        self.assertIn('40 minutes', restore.failed_reason([], {'active_id': 'old', 'started': (NOW - dt.timedelta(minutes=41)).isoformat()}, NOW))
        job['status'] = {'conditions': [{'type': 'Complete', 'status': 'True'}]}
        self.assertIsNone(restore.failed_reason([job], {}, NOW))
        self.assertIn('8 days', restore.failed_reason([], {'last_success': (NOW - dt.timedelta(days=9)).isoformat()}, NOW))

    def test_active_restore_lock_prevents_scale(self):
        dep = {'spec': {'replicas': 1}, 'metadata': {'generation': 1}, 'status': {'observedGeneration': 1, 'updatedReplicas': 1, 'availableReplicas': 1}}
        with patch.object(restore, 'identity', return_value=dep), patch.object(restore, 'api', return_value={'data': {'active_id': 'other'}}) as api:
            with self.assertRaises(RuntimeError):
                restore.stop()
            self.assertEqual(api.call_count, 1)
            self.assertEqual(api.call_args.args, (restore.STATE,))

    def test_manifest_validates_before_stopping_and_never_restarts_after_failed_init(self):
        resources = json.loads((ROOT / 'pkg/k8s/stagingrestore/resources.json').read_text())['items']
        cron = next(x for x in resources if x['kind'] == 'CronJob' and x['metadata']['name'] == 'registry-staging-restore')
        job = cron['spec']['jobTemplate']['spec']
        pod = job['template']['spec']
        self.assertEqual(job['backoffLimit'], 0)
        self.assertEqual(cron['spec']['concurrencyPolicy'], 'Forbid')
        self.assertEqual([x['name'] for x in pod['initContainers']], ['list-backup', 'select', 'fetch-backup', 'verify-archive', 'stop', 'restore'])
        self.assertEqual([x['name'] for x in pod['containers']], ['start'])
        for container in pod['initContainers'][:3]:
            self.assertNotIn('envFrom', container)
        role = next(x for x in resources if x['kind'] == 'Role')
        self.assertFalse(any('secrets' in r['resources'] or 'pods/exec' in r['resources'] for r in role['rules']))


if __name__ == '__main__':
    unittest.main()
