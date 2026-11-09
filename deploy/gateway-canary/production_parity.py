#!/usr/bin/env python3
"""Bounded parity checks on staging fixtures; never sends a load test to prod.

Requires the Pulumi fixture flag and its public CA certificate. Creates two
temporary rate policies targeting only fixture routes and deletes them in finally.
No node, application or production mutation. Run after check.py's cache suite.
"""
import argparse
import collections
import concurrent.futures
import json
import http.client
import os
from pathlib import Path
import re
import socket
import subprocess
import time
import uuid

from check import Client, FIXTURE, tls_context

ALIAS = 'alias.gateway-canary.invalid'
CONTEXT = 'gke_mcp-registry-staging_us-central1-b_mcp-registry-staging'
KUBE = ['kubectl', '--context', CONTEXT, '--request-timeout=30s']


def http2_aliases(ip, ca):
    # Send both authorities over exactly one socket; curl is allowed to open a
    # second connection even when coalescing is supported by the server.
    import h2.config
    import h2.connection
    import h2.events
    context = tls_context(ca)
    context.set_alpn_protocols(['h2'])
    statuses = []
    with context.wrap_socket(socket.create_connection((ip, 443), timeout=20), server_hostname=FIXTURE) as sock:
        assert sock.selected_alpn_protocol() == 'h2'
        assert {FIXTURE, ALIAS} <= {v for k, v in sock.getpeercert()['subjectAltName'] if k == 'DNS'}
        conn = h2.connection.H2Connection(config=h2.config.H2Configuration(header_encoding='utf-8'))
        conn.initiate_connection(); sock.sendall(conn.data_to_send())
        for host in (FIXTURE, ALIAS, FIXTURE):
            stream = conn.get_next_available_stream_id()
            conn.send_headers(stream, [(':method', 'GET'), (':scheme', 'https'), (':authority', host),
                                       (':path', '/_fixture/echo')], end_stream=True)
            sock.sendall(conn.data_to_send())
            done = False
            while not done:
                data = sock.recv(65535); assert data
                for event in conn.receive_data(data):
                    if isinstance(event, h2.events.ResponseReceived):
                        assert event.stream_id == stream
                        statuses.append(dict(event.headers)[':status'])
                    if isinstance(event, h2.events.DataReceived):
                        conn.acknowledge_received_data(event.flow_controlled_length, event.stream_id)
                    if isinstance(event, h2.events.StreamEnded) and event.stream_id == stream:
                        done = True
                    assert not isinstance(event, (h2.events.StreamReset, h2.events.ConnectionTerminated)), event
                pending = conn.data_to_send()
                if pending: sock.sendall(pending)
    assert statuses == ['200', '200', '200'], statuses
    return {'tls_connections': 1, 'authorities': [FIXTURE, ALIAS, FIXTURE], 'statuses': statuses}


def kubectl(args, document=None):
    return subprocess.check_output(KUBE + args, input=json.dumps(document) if document else None,
                                   text=True, stderr=subprocess.PIPE, timeout=45)


def nodes():
    items = json.loads(kubectl(['get', 'nodes', '-o', 'json']))['items']
    assert len(items) == 2 and all(any(c['type'] == 'Ready' and c['status'] == 'True'
                                     for c in n['status']['conditions']) for n in items)
    return sorted(n['metadata']['uid'] for n in items)


def upstream_attempts(row):
    return len(re.findall(r'\d+(?:\.\d+)?', row['upstream_time']))


def check_write_non_replay(rows):
    posts = [r for r in rows if r['method'] == 'POST']
    assert posts, 'No POST evidence'
    assert all(upstream_attempts(r) == 1 for r in posts), 'A POST used multiple upstream attempts'


def origin_failover(ip, ca):
    namespace = 'registry-gateway-canary'
    deployment = json.loads(kubectl(['-n', namespace, 'get', 'deployment', 'fixture', '-o', 'json']))
    assert deployment['spec']['replicas'] == 1
    selector = ','.join(k + '=' + v for k, v in deployment['spec']['selector']['matchLabels'].items())
    marker = 'parity-failover-' + uuid.uuid4().hex
    try:
        kubectl(['-n', namespace, 'scale', 'deployment/fixture', '--replicas=2'])
        for _ in range(60):
            pods = json.loads(kubectl(['-n', namespace, 'get', 'pods', '-l', selector, '-o', 'json']))['items']
            ready = [p for p in pods if not p['metadata'].get('deletionTimestamp') and
                     any(c['type'] == 'Ready' and c['status'] == 'True' for c in p['status'].get('conditions', []))]
            if len(ready) == 2: break
            time.sleep(1)
        else:
            raise AssertionError('Two tiny fixture origins did not fit existing staging nodes')
        time.sleep(3)
        counts = {'GET': collections.Counter(), 'POST': collections.Counter()}
        c = Client(ip, FIXTURE, ca)
        try:
            for method in counts:
                for i in range(20):
                    r = c.request('/_fixture/echo', {'X-Fixture-Disconnect-Pod': ready[0]['metadata']['name'],
                                                   'X-Request-ID': f'{marker}-{method}-{i}'}, method=method, body=b'')
                    counts[method][r['status']] += 1
        finally:
            c.close()
        assert counts['GET'] == {200: 20}, counts
        assert counts['POST'][502] > 0 and set(counts['POST']) <= {200, 502}, counts
        proxies = json.loads(kubectl(['-n', 'ingress-nginx', 'get', 'pods', '-l',
                                      'gateway.networking.k8s.io/gateway-name=registry-edge', '-o', 'json']))['items']
        rows = []
        for proxy in proxies:
            logs = kubectl(['-n', 'ingress-nginx', 'logs', proxy['metadata']['name'], '-c', 'nginx', '--since=3m'])
            for line in logs.splitlines():
                try: row = json.loads(line)
                except ValueError: continue
                if row.get('request_id', '').startswith(marker): rows.append(row)
        assert len(rows) == 40
        assert any(r['method'] == 'GET' and upstream_attempts(r) == 2 for r in rows)
        assert all(upstream_attempts(r) <= 3 for r in rows)
        check_write_non_replay(rows)
        c = Client(ip, FIXTURE, ca)
        try:
            for i in range(6):
                path = f'/v0.1/servers?retry-cache={marker}-{i}'
                headers = {'X-Fixture-Disconnect-Pod': ready[0]['metadata']['name']}
                first = c.request(path, headers)
                second = c.request(path, headers)
                assert first['status'] == second['status'] == 200
                assert first['headers']['x-registry-cache'] == 'MISS' and second['headers']['x-registry-cache'] == 'HIT'
        finally:
            c.close()
        retry_cache_rows = []
        for proxy in proxies:
            logs = kubectl(['-n', 'ingress-nginx', 'logs', proxy['metadata']['name'], '-c', 'nginx', '--since=3m'])
            for line in logs.splitlines():
                try: row = json.loads(line)
                except ValueError: continue
                if 'retry-cache=' + marker in row.get('uri', ''): retry_cache_rows.append(row)
        assert len(retry_cache_rows) == 12
        assert any(r['cache'] == 'MISS' and upstream_attempts(r) == 2 for r in retry_cache_rows)
        return {'responses': {k: dict(v) for k, v in counts.items()}, 'get_failover_observed': True,
                'sent_post_not_replayed': True, 'attempts_at_most_three': True,
                'successful_retry_responses_cacheable': True}
    finally:
        kubectl(['-n', namespace, 'scale', 'deployment/fixture', '--replicas=1'])


def run(ip, ca, output):
    os.umask(0o077)
    before_nodes = nodes()
    service = json.loads(kubectl(['-n', 'ingress-nginx', 'get', 'svc', 'ingress-nginx-controller', '-o', 'json']))
    assert service['status']['loadBalancer']['ingress'][0]['ip'] == ip
    report = {'node_uids_before': before_nodes, 'checks': {}}
    checks = report['checks']
    redirect_checks = []
    stage_host = 'staging.registry.modelcontextprotocol.io'
    for path in ['/', '/docs/', '/docs//', '/v0.1/servers/', '/docs/?x=1', '/docs?x=1/', '/docs/?a=%2F']:
        conn = http.client.HTTPConnection(ip, timeout=20)
        try:
            conn.request('GET', path, headers={'Host': stage_host})
            response = conn.getresponse(); response.read()
            expected_uri = path[:-1] if path.endswith('/') else path
            assert response.status == 308
            assert response.getheader('Location') == 'https://' + stage_host + expected_uri, (path, response.getheaders())
            redirect_checks.append(path)
        finally:
            conn.close()
    checks['production_raw_uri_redirect_semantics'] = redirect_checks
    c = Client(ip, FIXTURE, ca)
    nonce = str(time.time_ns())
    try:
        path = '/v0.1/servers?parity=' + nonce
        for host in (FIXTURE, ALIAS):
            first = c.request(path, {'Host': host})
            second = c.request(path, {'Host': host})
            assert first['status'] == second['status'] == 200
            assert first['headers']['x-registry-cache'] == 'MISS' and second['headers']['x-registry-cache'] == 'HIT'
        checks['two_sans_share_connection_but_not_cache_entries'] = True
        unknown = c.request('/_fixture/echo', {'Host': 'unrouted.gateway-canary.invalid'})
        assert unknown['status'] == 404
        checks['unlisted_host_not_routed'] = True
        r = c.request('/_fixture/echo', {'Proxy': 'http://untrusted.invalid', 'X-Forwarded-For': '198.51.100.99',
                                      'X-Forwarded-Host': 'untrusted.invalid', 'X-Forwarded-Scheme': 'http',
                                      'X-Scheme': 'http', 'X-Request-ID': 'production-parity'})
        body = json.loads(r['body']); headers = {k.lower(): v for k, v in body['headers'].items()}
        assert 'proxy' not in headers
        assert headers['x-forwarded-for'] != '198.51.100.99'
        assert headers['x-forwarded-scheme'] == headers['x-scheme'] == headers['x-forwarded-proto'] == 'https'
        assert headers['x-original-forwarded-for'] == '198.51.100.99'
        assert headers['x-original-forwarded-host'] == 'untrusted.invalid'
        assert headers['x-request-id'] == 'production-parity'
        checks['forwarded_headers_request_id_httpoxy'] = True
        ports = [json.loads(c.request('/_fixture/echo')['body'])['client_port'] for _ in range(8)]
        assert len(set(ports)) == 1
        checks['origin_connection_reused'] = True
    finally:
        c.close()

    checks['http2_cross_alias_connection_coalescing'] = http2_aliases(ip, ca)

    context = tls_context(ca)
    with context.wrap_socket(socket.create_connection((ip, 443), timeout=20), server_hostname=FIXTURE) as ws:
        ws.sendall((f'GET /_fixture/websocket HTTP/1.1\r\nHost: {ALIAS}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n'
                    'Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n').encode())
        response = b''
        while b'\r\n\r\n' not in response:
            data = ws.recv(4096)
            assert data
            response += data
        assert response.startswith(b'HTTP/1.1 101 ')
        assert b's3pPLMBiTxaQ9kYGzzhZRbK+xOo=' in response
        ws.sendall(b'\x81\x84\x01\x02\x03\x04' + bytes(b ^ (i + 1) for i, b in enumerate(b'ping')))
        echoed = b''
        while len(echoed) < 6:
            data = ws.recv(6 - len(echoed)); assert data; echoed += data
        assert echoed == b'\x81\x04ping'
    checks['websocket_upgrade_and_bidirectional_payload'] = True

    c = Client(ip, FIXTURE, ca)
    try:
        assert c.request('/_fixture/echo', method='POST', body=b'x' * (1024 * 1024 + 1))['status'] == 413
        checks['body_limit_1MiB'] = True
    finally:
        c.close()

    marker = uuid.uuid4().hex
    label = 'registry.modelcontextprotocol.io/parity-run'
    policies = json.loads((Path(__file__).parent.parent / 'pkg/k8s/gatewaycanary/assets/routes.json').read_text())
    try:
        for name, target in [('bulk', 'fixture-bulk'), ('registry', 'fixture-other')]:
            policy = next(o for o in policies if o['kind'] == 'RateLimitPolicy' and o['metadata']['name'] == name)
            policy['metadata'] = {'name': 'parity-' + name, 'namespace': 'ingress-nginx', 'labels': {label: marker}}
            policy['spec']['targetRefs'][0]['name'] = target
            kubectl(['create', '-f', '-'], policy)
        for _ in range(45):
            items = json.loads(kubectl(['-n', 'ingress-nginx', 'get', 'ratelimitpolicies', '-l', label + '=' + marker, '-o', 'json']))['items']
            if len(items) == 2 and all(any(c['type'] == 'Accepted' and c['status'] == 'True'
                                          for a in p.get('status', {}).get('ancestors', []) for c in a['conditions']) for p in items):
                break
            time.sleep(1)
        else:
            raise AssertionError('Fixture rate policies not accepted')
        time.sleep(2)

        def load(kind, count):
            def worker(n):
                conn = Client(ip, FIXTURE, ca); statuses = collections.Counter()
                try:
                    for i in range(n, count, 16):
                        host = FIXTURE if i % 2 else ALIAS
                        path = ('/v0/servers' if i % 4 < 2 else '/v0.1/servers') if kind == 'bulk' else '/_fixture/echo'
                        r = conn.request(path, {'Host': host, 'X-Forwarded-For': f'198.51.{i // 255}.{i % 255}'})
                        statuses[r['status']] += 1
                finally:
                    conn.close()
                return statuses
            start = time.monotonic()
            with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
                counts = sum(pool.map(worker, range(16)), collections.Counter())
            assert sum(counts.values()) == count and counts[200] > 0 and counts[429] > 0 and set(counts) == {200, 429}, counts
            return {'responses': dict(counts), 'seconds': round(time.monotonic() - start, 2)}

        checks['bulk_bucket_shared_across_aliases_and_versions_spoof_resistant'] = load('bulk', 2200)
        c = Client(ip, FIXTURE, ca)
        try:
            assert c.request('/_fixture/echo')['status'] == 200
            checks['bulk_and_general_buckets_independent'] = True
        finally:
            c.close()
        checks['general_bucket_shared_across_aliases_spoof_resistant'] = load('general', 850)
    finally:
        kubectl(['-n', 'ingress-nginx', 'delete', 'ratelimitpolicies', '-l', label + '=' + marker, '--wait=true'])
        report['temporary_rate_policies_removed'] = True
        report['node_uids_after'] = nodes()
        assert report['node_uids_after'] == before_nodes
        output.write_text(json.dumps(report, indent=2) + '\n')
    checks['origin_failover_and_write_non_replay'] = origin_failover(ip, ca)
    assert nodes() == before_nodes
    output.write_text(json.dumps(report, indent=2) + '\n')
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ip', required=True)
    parser.add_argument('--fixture-ca', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.write_text(json.dumps({'passed': False, 'reason': 'Validation has not completed'}) + '\n')
    try:
        result = run(args.ip, args.fixture_ca, args.output)
    except Exception as exc:
        result = json.loads(args.output.read_text())
        result.update(passed=False, reason=str(exc))
        args.output.write_text(json.dumps(result, indent=2) + '\n')
        raise
    result['passed'] = True
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))
