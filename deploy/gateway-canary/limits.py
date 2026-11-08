#!/usr/bin/env python3
"""Bounded staging source-IP and request-limit probes (960 health GETs per configured replica)."""
import argparse, json, concurrent.futures, collections, time, http.client
from check import Client, FIXTURE, STAGING
p = argparse.ArgumentParser()
p.add_argument('--ip', required=True)
p.add_argument('--fixture-ca', required=True)
p.add_argument('--output', required=True)
p.add_argument('--replicas', type=int, choices=[2,4], default=2, help='Configured proxy replicas; bounds the per-source limit test')
a = p.parse_args()
IP = a.ip
CA = a.fixture_ca
report = {}
c = http.client.HTTPConnection(IP, timeout=15)
c.request('GET', '/v0.1/health?probe=redirect', headers={'Host': STAGING})
r = c.getresponse()
r.read()
assert r.status == 308 and r.getheader('location') == 'https://' + STAGING + '/v0.1/health?probe=redirect'
c.close()
report['http_redirect_preserves_query'] = True
c = Client(IP, FIXTURE, CA)
r = c.request('/_fixture/echo', method='POST', body=b'x' * (1024 * 1024 + 1))
assert r['status'] == 413, r['status']
c.close()
report['body_limit_1MiB'] = True
c = Client(IP)
r = c.request('/v0.1/publish', method='POST', body=b'{}', headers={'Content-Type': 'application/json'})
assert r['status'] in [400, 401, 403, 422], r['status']
c.close()
report['invalid_publish_rejected'] = r['status']

def probe(worker):
    c = Client(IP)
    counts = collections.Counter()
    try:
        for i in range(120):
            r = c.request('/v0.1/health', {'X-Forwarded-For': f'198.51.{worker}.{i + 1}'})
            counts[r['status']] += 1
    finally:
        c.close()
    return counts
start = time.time()
with concurrent.futures.ThreadPoolExecutor(max_workers=8 * a.replicas) as p:
    counts = sum(p.map(probe, range(8 * a.replicas)), collections.Counter())
assert counts[429] > 0 and counts[200] > 0 and (sum(counts.values()) == 960 * a.replicas), counts
assert set(counts) <= set([200, 429]), counts
report['source_ip_limit_ignores_spoofed_xff'] = {'statuses': dict(counts), 'seconds': time.time() - start}
with open(a.output, 'w') as output:
    output.write(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
