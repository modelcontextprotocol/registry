"""Failure-path regression checks; no cluster calls or credentials are used."""
import importlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch


class ValidationHelpersTest(unittest.TestCase):
    def test_preflight_failure_replaces_old_success(self):
        for name in ['native_acme', 'certificate_rotation', 'resilience']:
            with self.subTest(helper=name), tempfile.TemporaryDirectory() as temp:
                module = importlib.import_module(name)
                output = Path(temp) / ('operation.json' if name == 'resilience' else 'result.json')
                output.write_text('{"passed":true}')
                argv = [name, '--output', temp, '--scenario', 'proxy'] if name == 'resilience' else [name, '--output', str(output), '--cmctl', '/unused']
                with patch.object(sys, 'argv', argv), patch.object(module, 'get', side_effect=RuntimeError('preflight failed')), patch.dict('os.environ', {'REGISTRY_GATEWAY_TARGET': 'public'}):
                    with self.assertRaises(RuntimeError):
                        module.main()
                self.assertFalse(json.loads(output.read_text())['passed'])

    def test_failed_create_cannot_delete_another_runs_secrets(self):
        module = importlib.import_module('native_acme')
        issuer = {'spec': {'acme': {'solvers': [{'http01': {'gatewayHTTPRoute': {'parentRefs': [{'kind': 'Gateway', 'name': 'registry-edge', 'namespace': 'ingress-nginx', 'sectionName': 'http'}]}}}]}}}
        for fail_kind in ['Issuer', 'Certificate']:
            with self.subTest(fail_kind=fail_kind), tempfile.TemporaryDirectory() as temp:
                calls = []
                def run(*args, **kwargs):
                    calls.append(args)
                    if args[0] == 'create' and json.loads(kwargs['input'])['kind'] == fail_kind:
                        raise RuntimeError('create rejected')
                output = Path(temp) / 'result.json'
                with patch.object(sys, 'argv', ['native_acme', '--output', str(output), '--cmctl', '/unused']), patch.object(module, 'get', return_value=issuer), patch.object(module, 'fingerprints', return_value={'public': 'unchanged'}), patch.object(module, 'run', side_effect=run):
                    with self.assertRaises(RuntimeError):
                        module.main()
                deletions = [args for args in calls if 'delete' in args]
                if fail_kind == 'Issuer':
                    self.assertEqual(deletions, [])
                else:
                    self.assertEqual([args[3] for args in deletions], ['Issuer', 'secret'])
                    self.assertTrue(deletions[1][4].endswith('-account'))
                report = json.loads(output.read_text())
                self.assertFalse(report['passed'])
                self.assertFalse(report['proofs'][0]['completed'])


if __name__ == '__main__':
    unittest.main()
