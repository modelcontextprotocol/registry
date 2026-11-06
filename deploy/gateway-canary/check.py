#!/usr/bin/env python3
"""Read-only registry probes and fault tests against the isolated canary fixture."""
import argparse, concurrent.futures, gzip, http.client, json, socket, ssl, time
from pathlib import Path
STAGING = 'staging.registry.modelcontextprotocol.io'
FIXTURE = 'fixture.gateway-canary.invalid'

class Client:

    def __init__(self, ip, host=STAGING, fixture_ca=None):

        class Connection(http.client.HTTPSConnection):

            def connect(conn):
                context = ssl.create_default_context(cafile=fixture_ca if host == FIXTURE else None)
                conn.sock = context.wrap_socket(socket.create_connection((ip, 443), timeout=20), server_hostname=host)
        self.conn = Connection(host, timeout=20)
        self.host = host

    def request(self, path, headers=None, method='GET', body=None):
        self.conn.request(method, path, body=body, headers={'Host': self.host, 'Accept-Encoding': 'gzip', **(headers or {})})
        r = self.conn.getresponse()
        wire = r.read()
        h = {k.lower(): v for (k, v) in r.getheaders()}
        decoded = gzip.decompress(wire) if h.get('content-encoding') == 'gzip' and wire else wire
        return {'status': r.status, 'headers': h, 'body': decoded, 'wire_bytes': len(wire)}

    def close(self):
        self.conn.close()

def run(ip, fixture_ca):
    real = Client(ip)
    fixture = Client(ip, FIXTURE, fixture_ca)
    out = {}
    for p in ['/v0.1/health', '/v0.1/version', '/v0.1/servers?limit=1', '/v0/servers?limit=1']:
        r = real.request(p)
        assert r['status'] == 200, (p, r['status'])
        json.loads(r['body'])
        out[p] = 200
    nonce = str(time.time_ns())
    p = '/v0.1/servers?canary=' + nonce
    a = fixture.request(p)
    b = fixture.request(p)
    assert a['status'] == b['status'] == 200, (a['status'], b['status'])
    assert a['headers']['x-registry-cache'] == 'MISS' and b['headers']['x-registry-cache'] == 'HIT'
    assert a['body'] == b['body'] and b['headers']['content-encoding'] == 'gzip'
    assert a['headers']['x-origin-sequence'] == b['headers']['x-origin-sequence'] == '1'
    assert a['headers'].get('strict-transport-security') == 'max-age=31536000; includeSubDomains'
    out['cache_miss_hit_origin_offload'] = True
    for h in [{'Authorization': 'Bearer fixture-only'}, {'Cookie': 'fixture=1'}, {'Accept-Encoding': 'gzip;q=0'}, {'Accept-Encoding': 'identity'}]:
        r = fixture.request(p, h)
        assert r['headers'].get('content-encoding', 'identity') == 'identity' and r['body'] == a['body']
        assert 'Accept-Encoding' in r['headers'].get('vary', '')
    out['conditional_gzip_identity_and_vary'] = True
    r = fixture.request(p, method='HEAD')
    assert r['status'] == 200 and (not r['body'])
    out['head'] = True
    for suffix in ['&mode=private', '&mode=set-cookie', '&status=500', '&status=502', '&status=503', '&status=504']:
        rs = [fixture.request(p + suffix) for _ in range(2)]
        assert all((x['headers'].get('x-registry-cache') != 'HIT' for x in rs)), suffix
    out['private_cookie_errors_not_cached'] = True
    for suffix in ['&q=a%2Fb', '&q=a/b', '&q=a&q=b', '&q=b&q=a']:
        r = fixture.request(p + suffix)
        assert r['headers']['x-registry-cache'] == 'MISS', suffix
        assert fixture.request(p + suffix)['headers']['x-registry-cache'] == 'HIT'
    out['original_query_key_preserved'] = True
    echo = fixture.request('/_fixture/echo', {'X-Forwarded-For': '198.51.100.99', 'X-Forwarded-Proto': 'http', 'X-Request-ID': 'gateway-canary-test'})
    headers = {k.lower(): v for (k, v) in json.loads(echo['body'])['headers'].items()}
    assert headers['x-forwarded-for'] != '198.51.100.99' and headers['x-forwarded-proto'] == 'https'
    assert headers['x-request-id'] == 'gateway-canary-test'
    out['forwarded_headers_and_request_id'] = True
    out['observed_client_ip'] = headers['x-forwarded-for']
    fault_path = p + '&fault=stale'
    fault_good = fixture.request(fault_path)
    assert fault_good['status'] == 200
    lock_path = p + '&delay=0.2&lock=1'
    def locked_read(_):
        c = Client(ip, FIXTURE, fixture_ca)
        try:
            return c.request(lock_path)
        finally:
            c.close()
    with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
        locked = list(pool.map(locked_read, range(32)))
    assert all(r['status'] == 200 and r['body'] == a['body'] for r in locked)
    assert max(int(r['headers']['x-origin-sequence']) for r in locked) <= 4
    out['cache_lock_at_most_one_origin_read_per_configured_replica'] = True
    time.sleep(31)
    expired = fixture.request(p)
    assert expired['headers']['x-registry-cache'] in ['STALE', 'EXPIRED', 'UPDATING']
    for _ in range(15):
        refreshed = fixture.request(p)
        if int(refreshed['headers'].get('x-origin-sequence', '0')) > 1:
            break
        time.sleep(0.3)
    assert int(refreshed['headers']['x-origin-sequence']) > 1
    out['expiry_background_refresh'] = True
    stale = fixture.request(fault_path, {'X-Fixture-Status': '503'})
    assert stale['status'] == 200 and stale['headers']['x-registry-cache'] == 'STALE'
    time.sleep(1)
    stale = fixture.request(fault_path, {'X-Fixture-Status': '503'})
    assert stale['status'] == 200 and stale['body'] == fault_good['body']
    assert stale['headers']['x-origin-sequence'] == fault_good['headers']['x-origin-sequence']
    out['last_good_cache_survives_origin_503'] = True
    real.close()
    fixture.close()
    return out
if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('--ip', required=True)
    p.add_argument('--output', required=True)
    p.add_argument('--fixture-ca', required=True)
    a = p.parse_args()
    r = run(a.ip, a.fixture_ca)
    Path(a.output).write_text(json.dumps(r, indent=2) + '\n')
    print(json.dumps(r, indent=2))
