#!/usr/bin/env python3
"""Exercise real CRI splitting and retry metrics on both existing staging nodes.

Creates one node-pinned log-emitter Pod at a time, with no Service or public route.
Uses fixture-labelled records and removes each emitter, including on failure.
"""
import argparse
import json
import re
import subprocess
import time
from pathlib import Path

from measure import K, NS, get, raw, query
from monitoring_contract import check, corpus

IMAGE = 'python:3.13-alpine@sha256:1e1f6334529d1fb26a2ef619d7cffb30a7c7d7cc1cc7338ebee283a58d1aeaca'


def totals():
    result = {}
    inventory = get('-n', NS, 'get', 'pods', '-l', 'app=gateway-canary-vector')['items']
    for pod in inventory:
        if pod['metadata'].get('deletionTimestamp'):
            continue
        for port in [9598, 9599]:
            text = raw(f"/api/v1/namespaces/{NS}/pods/{pod['metadata']['name']}:{port}/proxy/metrics")
            for line in text.splitlines():
                fixture_counter = any(line.startswith('registry_gateway_canary_' + name + '{traffic="fixture"')
                                      for name in ['upstream_timing_errors_total', 'multi_upstream_requests_total'])
                if 'proxy_pod="monitoring-contract"' in line or fixture_counter:
                    name = line.split('{', 1)[0]
                    result[name] = result.get(name, 0) + float(line.rsplit(' ', 1)[1])
    return result


def run(*args, **kwargs):
    return subprocess.run(K + list(args), check=True, capture_output=True, text=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    nodes = get('get', 'nodes')['items']
    node_names = sorted(n['metadata']['name'] for n in nodes)
    canary = [n['metadata']['name'] for n in nodes if n['metadata']['labels'].get('registry-role') == 'gateway-canary']
    assert len(canary) == 2, 'Requires exactly the existing two canary nodes'
    baseline = totals()
    started = time.time()
    for index, node in enumerate(canary):
        name = f'monitoring-contract-{index}'
        manifest = {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': name, 'namespace': NS}, 'spec': {
            'nodeName': node, 'restartPolicy': 'Never', 'automountServiceAccountToken': False, 'terminationGracePeriodSeconds': 1,
            'tolerations': [{'key': 'registry-role', 'operator': 'Equal', 'value': 'gateway-canary', 'effect': 'NoSchedule'}],
            'containers': [{'name': 'nginx', 'image': IMAGE, 'command': ['python3', '-u', '-c', 'import time; time.sleep(600)'],
                            'resources': {'requests': {'cpu': '10m', 'memory': '24Mi'}, 'limits': {'cpu': '100m', 'memory': '64Mi'}},
                            'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True,
                                                'capabilities': {'drop': ['ALL']}, 'seccompProfile': {'type': 'RuntimeDefault'}}}]}}
        # Do not overwrite or delete a Pod we did not create.
        run('create', '-f', '-', input=json.dumps(manifest))
        try:
            run('-n', NS, 'wait', '--for=condition=Ready', 'pod/' + name, '--timeout=120s')
            script = 'import sys; data=sys.stdin.read(); f=open("/proc/1/fd/1","w"); f.write(data); f.flush(); f.close()'
            run('-n', NS, 'exec', '-i', name, '--', 'python3', '-c', script, input='\n'.join(corpus()) + '\n')
            collectors = get('-n', NS, 'get', 'pods', '-l', 'app=gateway-canary-vector')['items']
            collector = next(p['metadata']['name'] for p in collectors
                             if p['spec']['nodeName'] == node and not p['metadata'].get('deletionTimestamp'))
            uid = get('-n', NS, 'get', 'pod', name)['metadata']['uid']
            log_path = f'/var/log/pods/{NS}_{name}_{uid}/nginx/0.log'
            time.sleep(2)
            framing = run('-n', NS, 'exec', collector, '--', 'sh', '-c',
                          'awk \'{print $3}\' "$1"', 'sh', log_path).stdout.splitlines()
            assert 'P' in framing, ('Expected actual CRI partial records for the long message', framing)
            deadline = time.monotonic() + 30
            while True:
                current = totals()
                delta = {k: current.get(k, 0) - baseline.get(k, 0) for k in set(current) | set(baseline)}
                expected = {'requests_total': 6 * (index + 1), 'internal_requests_total': index + 1,
                            'upstream_duration_seconds_count': 2 * (index + 1),
                            'upstream_connect_seconds_count': 2 * (index + 1),
                            'upstream_header_seconds_count': 2 * (index + 1),
                            'upstream_all_attempts_seconds_count': 3 * (index + 1),
                            'upstream_all_attempts_seconds_sum': 0.85 * (index + 1),
                            'connect_all_attempts_seconds_sum': 0.04 * (index + 1),
                            'header_all_attempts_seconds_sum': 0.14 * (index + 1),
                            'multi_upstream_requests_total': index + 1, 'upstream_timing_errors_total': index + 1}
                try:
                    check(delta, expected)
                    break
                except AssertionError:
                    if time.monotonic() >= deadline:
                        raise
                    time.sleep(1)
        finally:
            run('-n', NS, 'delete', 'pod', name, '--wait=true', '--timeout=60s')
    deadline = time.monotonic() + 60
    # Deleted exporters may retain their last up=1 sample during lookback.
    # Raw totals must select live Pod IPs explicitly, not historical series.
    collectors = get('-n', NS, 'get', 'pods', '-l', 'app=gateway-canary-vector')['items']
    instances = '|'.join(re.escape(p['status']['podIP'] + ':9598') for p in collectors
                         if not p['metadata'].get('deletionTimestamp'))
    while True:
        observed = query('sum(registry_gateway_canary_requests_total{proxy_pod="monitoring-contract",instance=~'
                         + json.dumps(instances) + '})')
        if observed and float(observed[0]['value'][1]) == current['registry_gateway_canary_requests_total']:
            break
        assert time.monotonic() < deadline, 'VictoriaMetrics did not reconcile with collectors'
        time.sleep(3)
    assert sorted(n['metadata']['name'] for n in get('get', 'nodes')['items']) == node_names, 'Node inventory changed'
    result = {'passed': True, 'started': started, 'finished': time.time(), 'nodes': node_names,
              'canary_nodes': canary, 'delta': delta, 'vm_requests': observed,
              'real_cri_partial_records': True, 'emitters_removed': True}
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
