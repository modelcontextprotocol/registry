"""Failure-path regression checks; no cluster calls or credentials are used."""
import importlib
import fcntl
import os
import signal
import subprocess
import time
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch


class ValidationHelpersTest(unittest.TestCase):
    def test_write_non_replay_includes_successful_posts(self):
        from production_parity import check_write_non_replay
        success = {'method': 'POST', 'status': '200', 'upstream_time': '0.001'}
        failure = {'method': 'POST', 'status': '502', 'upstream_time': '0.002'}
        check_write_non_replay([success, failure])
        with self.assertRaises(AssertionError):
            check_write_non_replay([dict(success, upstream_time='0.001, 0.002'), failure])
        with self.assertRaises(AssertionError):
            check_write_non_replay([])

    def test_shared_probe_tls_context_requires_modern_verified_tls(self):
        import ssl
        from check import tls_context
        context = tls_context()
        self.assertGreaterEqual(context.minimum_version, ssl.TLSVersion.TLSv1_2)
        self.assertEqual(context.verify_mode, ssl.CERT_REQUIRED)
        self.assertTrue(context.check_hostname)

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

    def test_measurement_timeout_kills_descendants_and_releases_lock(self):
        module = importlib.import_module('resilience')
        with tempfile.TemporaryDirectory() as temp:
            lock = Path(temp) / 'lock'
            ready = Path(temp) / 'ready'
            # Both parent and child ignore TERM and retain the same flock FD.
            child = "import signal,time,pathlib,sys; signal.signal(signal.SIGTERM,signal.SIG_IGN); pathlib.Path(sys.argv[1]).touch(); time.sleep(60)"
            parent = "import fcntl,subprocess,sys,signal,time; f=open(sys.argv[1],'w'); fcntl.flock(f,fcntl.LOCK_EX); signal.signal(signal.SIGTERM,signal.SIG_IGN); subprocess.Popen([sys.executable,'-c',sys.argv[3],sys.argv[2]],pass_fds=[f.fileno()]); time.sleep(60)"
            process = subprocess.Popen([sys.executable, '-c', parent, str(lock), str(ready), child], start_new_session=True)
            try:
                deadline = time.monotonic() + 5
                while not ready.exists():
                    self.assertLess(time.monotonic(), deadline)
                    time.sleep(.01)
                code, timed_out = module.wait_measurement(process, timeout=.05, grace=.1)
                self.assertTrue(timed_out)
                self.assertEqual(code, -signal.SIGKILL)
                with lock.open('r+') as stream:
                    # The child's inherited FD would retain this lock if only
                    # the group leader had been killed/reaped.
                    fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            finally:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)

    def test_remote_cancellation_only_signals_this_epoch(self):
        module = importlib.import_module('resilience')
        processes = {101: b'/work/loadgen\0-epoch\0owned\0',
                     102: b'/work/loadgen\0-epoch\0another-run\0',
                     103: b'python3\0-c\0owned\0',
                     104: b'/work/loadgen\0-epoch\0'}
        signals = []
        class ProcPath:
            def __init__(self, value):
                self.value = str(value)
                self.name = self.value.rsplit('/', 1)[-1]
            def __truediv__(self, part):
                return ProcPath(self.value + '/' + str(part))
            def iterdir(self):
                return [ProcPath('/proc/' + str(pid)) for pid in processes]
            def read_bytes(self):
                pid = int(self.value.split('/')[2])
                if pid not in processes:
                    raise FileNotFoundError()
                return processes[pid]
        def signal_process(pid, sig):
            signals.append((pid, sig))
            if sig == signal.SIGKILL:
                del processes[pid]
        def execute(*args, **kwargs):
            script = args[args.index('-c') + 1]
            self.assertEqual(args[-1], 'owned')
            with patch.object(sys, 'argv', ['probe', args[-1]]), patch('pathlib.Path', ProcPath), patch('os.kill', signal_process), patch('time.sleep'):
                exec(compile(script, '<remote-cancellation>', 'exec'), {})
        with patch.object(module, 'run', side_effect=execute):
            module.stop_remote_load('owned')
        self.assertEqual(signals, [(101, signal.SIGTERM), (101, signal.SIGKILL)])
        self.assertEqual(set(processes), {102, 103, 104})

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
