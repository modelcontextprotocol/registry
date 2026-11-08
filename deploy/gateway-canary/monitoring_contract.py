#!/usr/bin/env python3
"""Exercise the actual Vector file -> CRI -> metrics pipeline, not a parser mock.

Run with --vector /path/to/vector (the deployed 0.61.0 version).
The corpus is also reusable by the isolated staging log-emitter check.
"""
import argparse
import json
import math
import socket
import subprocess
import tempfile
import time
import urllib.request
from pathlib import Path

ASSET = Path(__file__).resolve().parents[1] / 'pkg/k8s/gatewaycanary/assets/vector.json'


def record(**overrides):
    value = dict(kind='registry_access', uri='/v0.1/servers?test=monitoring',
                 host='fixture.gateway-canary.invalid', proxy_pod='monitoring-contract',
                 method='GET', status='200', cache='MISS', duration='0.5',
                 body_bytes='200', request_bytes='100', response_bytes='250',
                 upstream_time='0.125', connect='0.005', header='0.025')
    value.update(overrides)
    return json.dumps(value, separators=(',', ':'))


def corpus():
    return [record(), record(host='alias.gateway-canary.invalid', upstream_time='0.1, 0.2 : 0.3', connect='0.01, - : 0.02', header='-, 0.04 : 0.05'),
            record(upstream_time='-', connect='-', header='-', cache='HIT'),
            record(upstream_time='garbage', connect='-1', header='bad'),
            record(uri='/v0.1/servers?long=' + 'x' * 300000),
            record(request_bytes='0'),
            record(upstream_time='', connect='', header='', cache='HIT')]


def cri(text, flag='F', stream='stdout'):
    return f'2026-11-07T00:00:00.000000000Z {stream} {flag} {text}\n'


def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


def metrics(addresses):
    result = {}
    for address in addresses:
        with urllib.request.urlopen(address, timeout=2) as response:
            for line in response.read().decode().splitlines():
                if line.startswith(('registry_gateway_canary_', 'vector_component_')):
                    name = line.split('{', 1)[0].split(' ')[0]
                    result[name] = result.get(name, 0.0) + float(line.rsplit(' ', 1)[1])
    return result


def check(values, expected):
    for name, value in expected.items():
        actual = values.get('registry_gateway_canary_' + name, 0)
        assert math.isclose(actual, value, abs_tol=1e-8), (name, actual, value)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vector', required=True)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='gateway-monitoring-contract-') as temp:
        root = Path(temp)
        (root / 'state').mkdir()
        config = json.loads(ASSET.read_text())
        config['data_dir'] = str(root / 'state')
        config['sources']['access']['include'] = [str(root / '*.log')]
        addresses = []
        for sink in config['sinks'].values():
            sink['address'] = '127.0.0.1:' + str(port())
            addresses.append('http://' + sink['address'] + '/metrics')
        cfg = root / 'vector.json'
        cfg.write_text(json.dumps(config))
        with (root / 'stderr').open('w') as log:
            process = subprocess.Popen([args.vector, '--config', str(cfg), '--threads', '1'], stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 20
                while True:
                    try:
                        metrics(addresses)
                        break
                    except OSError:
                        assert time.monotonic() < deadline, (root / 'stderr').read_text()
                        time.sleep(0.1)
                rows = corpus()
                # Interleave two files and stdout/stderr; split even the kind marker.
                with (root / 'a.log').open('w') as a, (root / 'b.log').open('w') as b:
                    a.write(cri(rows[0][:12], 'P')); a.flush()
                    b.write(cri(rows[1])); b.flush()
                    a.write(cri('unrelated stderr message', stream='stderr'))
                    a.write(cri(rows[0][12:])); a.write(cri(rows[2])); a.write(cri(rows[3]))
                    for offset in range(0, len(rows[4]), 131072):
                        part = rows[4][offset:offset+131072]
                        a.write(cri(part, 'F' if offset+131072 >= len(rows[4]) else 'P'))
                    a.write(cri(rows[5])); a.write(cri(rows[6])); a.write(cri('{"kind":"registry_access",broken}'))
                    a.flush()
                # A never-finished record must expire visibly, not count as a request.
                (root / 'orphan.log').write_text(cri(rows[0][:30], 'P'))
                # A runaway fragment sequence must flush at the configured bound.
                (root / 'bounded.log').write_text(cri('{"kind":"registry_access",', 'P')
                                                  + cri('x' * 60000, 'P') * 3)
                expected = {'requests_total': 6, 'internal_requests_total': 1,
                            'request_duration_seconds_count': 6, 'response_bytes_count': 6,
                            'request_bytes_count': 6, 'response_body_bytes_count': 6,
                            'upstream_duration_seconds_count': 2, 'upstream_connect_seconds_count': 2, 'upstream_header_seconds_count': 2, 'upstream_duration_seconds_sum': 0.25,
                            'upstream_all_attempts_seconds_count': 3, 'upstream_all_attempts_seconds_sum': 0.85,
                            'connect_all_attempts_seconds_count': 3, 'connect_all_attempts_seconds_sum': 0.04,
                            'header_all_attempts_seconds_count': 3, 'header_all_attempts_seconds_sum': 0.14,
                            'multi_upstream_requests_total': 1, 'upstream_timing_errors_total': 1,
                            'parse_errors_total': 1, 'cri_errors_total': 2}
                deadline = time.monotonic() + 15
                while True:
                    values = metrics(addresses)
                    try:
                        check(values, expected)
                        # VRL aborts are explicitly rerouted to our parse-error counter;
                        # they need not increment Vector's component error counter.
                        assert values.get('vector_component_received_events_total', 0) > 0, 'Internal metrics missing'
                        break
                    except AssertionError:
                        if time.monotonic() >= deadline:
                            raise
                        time.sleep(0.25)
                result = {'passed': True, 'expected': expected,
                          'cases': ['CRI split marker', '300KB record', 'interleaved file/stream',
                                    'retry groups and missing timings', 'invalid timings',
                                    'cache hit with empty and dash timings', 'background refresh', 'malformed JSON',
                                    'partial timeout', 'partial fragment bound']}
                if args.output:
                    args.output.write_text(json.dumps(result, indent=2) + '\n')
                print(json.dumps(result))
            finally:
                process.terminate()
                process.wait(timeout=10)


if __name__ == '__main__':
    main()
