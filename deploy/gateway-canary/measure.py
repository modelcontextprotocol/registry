#!/usr/bin/env python3
"""Bounded matched fixture load. K8s reads plus exec in the dedicated generator."""
import argparse, concurrent.futures, fcntl, json, os, re, subprocess, tempfile, threading, time, urllib.parse
from pathlib import Path
K = ['kubectl', '--context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging', '--request-timeout=30s']
NS = 'registry-gateway-canary'


def gateway_target():
    target = os.environ.get('REGISTRY_GATEWAY_TARGET', 'canary')
    if target == 'public':
        return 'ingress-nginx', 'registry-edge-registry-gateway-canary'
    if target == 'canary':
        return NS, 'registry-canary-registry-gateway-canary'
    raise ValueError('REGISTRY_GATEWAY_TARGET must be public or canary')


def get(*a):
    return json.loads(subprocess.check_output(K + list(a) + ['-o', 'json']))

def raw(path):
    return subprocess.check_output(K + ['get', '--raw', path], text=True)

def pods():
    return [p for ns in sorted({NS, gateway_target()[0]}) for p in get('-n', ns, 'get', 'pods')['items']]

def vector():
    out = {}
    for p in pods():
        if not p['metadata']['name'].startswith('vector-') or p['metadata'].get('deletionTimestamp'):
            continue
        for port in [9598, 9599]:
            text = raw('/api/v1/namespaces/' + NS + '/pods/' + p['metadata']['name'] + ':' + str(port) + '/proxy/metrics')
            for line in text.splitlines():
                if not line.startswith('registry_gateway_canary_'):
                    continue
                name = line.split('{', 1)[0].split(' ')[0]
                if 'traffic="fixture"' not in line and (not any((x in name for x in ['parse_errors', 'response_bytes_missing']))):
                    continue
                if name.endswith('_bucket'):
                    continue
                out[name] = out.get(name, 0) + float(line.rsplit(' ', 1)[1])
    return out

def sample(node):
    d = json.loads(raw('/api/v1/nodes/' + node + '/proxy/stats/summary'))
    return {'node': node, 'cpu': d['node']['cpu'], 'memory': d['node']['memory'], 'pods': [{'name': p['podRef']['name'], 'containers': [{k: c[k] for k in ['name', 'cpu', 'memory']} for c in p['containers']]} for p in d['pods'] if p['podRef']['namespace'] in {NS, gateway_target()[0]}]}

def stamp(t):
    from datetime import datetime
    return datetime.fromisoformat(t.replace('Z', '+00:00')).timestamp()

def origin():
    return json.loads(raw('/api/v1/namespaces/' + NS + '/services/fixture:80/proxy/_fixture/stats'))

def query(expr):
    q = urllib.parse.urlencode({'query': expr, 'time': str(time.time()), 'nocache': '1'})
    return json.loads(raw('/api/v1/namespaces/monitoring/services/victoria-metrics-victoria-metrics-single-server:8428/proxy/api/v1/query?' + q))['data']['result']

def current_client_query():
    # Counter totals are comparable only for currently running exporter instances;
    # deleted pod series remain in storage for historical rate queries.
    instances = [re.escape(p['status']['podIP'] + ':9598') for p in pods()
                 if p['metadata']['name'].startswith('vector-')
                 and not p['metadata'].get('deletionTimestamp') and p['status'].get('podIP')]
    return 'sum(registry_gateway_canary_requests_total{traffic="fixture",instance=~' + json.dumps('|'.join(instances)) + '})'

def main():
    # Concurrent runs share one generator Pod and metric counters, invalidating
    # each other's results. Fail before collecting or launching any traffic.
    lock_path = Path(tempfile.gettempdir()) / ('registry-staging-gateway-load-' + str(os.getuid()) + '.lock')
    load_lock = os.fdopen(os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600), 'r+')
    try:
        fcntl.flock(load_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        raise RuntimeError('Another local staging Gateway measurement is running') from None
    p = argparse.ArgumentParser()
    p.add_argument('--arm', choices=['baseline', 'ngf'], required=True)
    p.add_argument('--rate', type=int, required=True)
    p.add_argument('--workers', type=int, default=256)
    p.add_argument('--seconds', type=int, default=120)
    p.add_argument('--miss-percent', type=int, default=0)
    p.add_argument('--epoch', required=True)
    p.add_argument('--output', type=Path, required=True)
    a = p.parse_args()
    a.output.mkdir(parents=True, exist_ok=True)
    gateway_namespace, service = (NS, 'registry-canary-baseline-controller') if a.arm == 'baseline' else gateway_target()
    ip = get('-n', gateway_namespace, 'get', 'svc', service)['spec']['clusterIP']
    inventory = pods()
    nodes = sorted({x['spec']['nodeName'] for x in inventory if x.get('spec', {}).get('nodeName')})
    before = vector()
    o0 = origin()
    samples = []
    stop = threading.Event()

    def collect():
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(nodes)) as pool:
            while not stop.is_set():
                try:
                    samples.append({'at': time.time(), 'nodes': list(pool.map(sample, nodes))})
                except Exception as e:
                    samples.append({'at': time.time(), 'error': str(e)[:200]})
                stop.wait(5)
    thread = threading.Thread(target=collect)
    thread.start()
    start = time.time()
    cmd = K + ['-n', NS, 'exec', 'load-generator', '--', 'env', 'GOMAXPROCS=1', '/work/loadgen', '-target', 'https://' + ip + ':443', '-allow-non-rfc1918-target', '-ca', '/work/fixture.crt', '-workers', str(a.workers), '-rate', str(a.rate), '-seconds', str(a.seconds), '-miss-percent', str(a.miss_percent), '-epoch', a.epoch, '-out', '/work/result.json']
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=a.seconds + 60)
        assert r.returncode == 0, r.stderr[-2000:]
        client = json.loads(r.stdout)
        end = time.time()
        deadline = time.time() + 20
        while True:
            after = vector()
            delta = {k: after.get(k, 0) - before.get(k, 0) for k in set(after) | set(before)}
            expected = sum(client['statuses'].values()) if 'statuses' in client else sum(client['status'].values())
            count = delta.get('registry_gateway_canary_requests_total', 0)
            if a.arm == 'baseline' or count >= expected or time.time() > deadline:
                break
            time.sleep(1)
        drain = time.time() - end
        o1 = origin()
        time.sleep(16)
        vm = query(current_client_query())
        vm_deadline = time.time() + 20
        while a.arm == 'ngf' and (not vm or float(vm[0]['value'][1]) != after.get('registry_gateway_canary_requests_total', 0)) and (time.time() < vm_deadline):
            time.sleep(2)
            vm = query(current_client_query())
    finally:
        stop.set()
        thread.join(timeout=40)
    series = {}
    for row in samples:
        for n in row.get('nodes', []):
            entries = [('node/' + n['node'], n)] + [(p['name'] + '/' + c['name'], c) for p in n['pods'] for c in p['containers']]
            for (name, c) in entries:
                t = stamp(c['cpu']['time'])
                if start + 5 <= t <= end - 2:
                    series.setdefault(name, {})[t] = c
    resources = {}
    for (name, points) in series.items():
        pairs = sorted(points.items())
        if len(pairs) > 1:
            (first, last) = (pairs[0], pairs[-1])
            resources[name] = {'cpu_cores': (last[1]['cpu']['usageCoreNanoSeconds'] - first[1]['cpu']['usageCoreNanoSeconds']) / 1000000000.0 / (last[0] - first[0]), 'peak_working_set_bytes': max((x['memory'].get('workingSetBytes', 0) for x in points.values())), 'sample_seconds': last[0] - first[0]}
    report = {'generator_gomaxprocs': 1, 'gateway_namespace': gateway_namespace, 'gateway_service': service, 'arm': a.arm, 'rate': a.rate, 'seconds': a.seconds, 'miss_percent': a.miss_percent, 'started_at': start, 'finished_at': end, 'client': client, 'metric_delta': delta, 'collection_observed_drain_seconds': drain, 'vm_client_count': vm, 'origin_reads': sum(o1.values()) - sum(o0.values()), 'resources': resources, 'placement': [{'pod': x['metadata']['name'], 'node': x['spec'].get('nodeName')} for x in inventory]}
    (a.output / 'samples.json').write_text(json.dumps(samples))
    (a.output / 'result.json').write_text(json.dumps(report, indent=2))
    print(json.dumps(report, indent=2))
    assert client['p95_ms'] < 100, 'p95 latency exceeds the qualification gate'
    assert client['p99_ms'] < 250, 'p99 latency exceeds the qualification gate'
    assert client['generator_dropped'] == 0, 'generator could not sustain offered rate'
    assert not client['errors'], client['errors']
    assert client['statuses'] == {'200': client['offered']}, client['statuses']
    if a.arm == 'ngf':
        assert count == expected, (count, expected)
        assert vm and float(vm[0]['value'][1]) == after.get('registry_gateway_canary_requests_total', 0), 'VictoriaMetrics has not caught up with exporter'
        for name in ['request_duration_seconds_count', 'response_bytes_count', 'response_body_bytes_count', 'request_bytes_count']:
            assert delta.get('registry_gateway_canary_' + name) == expected, name
        assert delta.get('registry_gateway_canary_parse_errors_total', 0) == 0
        assert delta.get('registry_gateway_canary_response_bytes_missing_total', 0) == 0
if __name__ == '__main__':
    main()
