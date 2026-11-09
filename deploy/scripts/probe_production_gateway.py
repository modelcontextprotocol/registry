#!/usr/bin/env python3
"""Low-volume public connectivity checks, before and after a production cutover.

Uses normal DNS and certificate verification. Does not exercise writes, OAuth,
rate-limit exhaustion, cache staleness, certificate renewal or peak capacity.
"""
import json
import subprocess
import tempfile
from pathlib import Path

from production_gateway import HOSTS


def probe(host, path, scheme, expected):
    with tempfile.TemporaryDirectory(prefix='registry-connectivity-') as directory:
        headers = Path(directory) / 'headers'
        result = subprocess.run([
            'curl', '--silent', '--show-error', '--max-time', '20',
            '--dump-header', str(headers), '--output', '/dev/null',
            '--write-out', '%{http_code} %{remote_ip} %{ssl_verify_result}',
            f'{scheme}://{host}{path}',
        ], capture_output=True, text=True, timeout=25)
        fields = result.stdout.split()
        passed = result.returncode == 0 and len(fields) == 3
        if passed:
            passed = int(fields[0]) in expected and fields[1] == '34.61.200.254'
            if scheme == 'https':
                passed = passed and fields[2] == '0'
            else:
                locations = [line.split(':', 1)[1].strip() for line in headers.read_text().splitlines()
                             if line.lower().startswith('location:')]
                passed = passed and locations == [f'https://{host}{path}']
        return {'host': host, 'path': path, 'scheme': scheme, 'passed': passed,
                'status': fields[0] if fields else None,
                'remote_ip': fields[1] if len(fields) > 1 else None}


def main():
    results = []
    for host in HOSTS:
        results.append(probe(host, '/v0.1/servers?limit=1', 'http', {301, 302, 307, 308}))
        for path in ('/v0/health', '/v0/servers?limit=1', '/v0.1/servers?limit=1'):
            results.append(probe(host, path, 'https', {200}))
    host = 'grafana.prod.registry.modelcontextprotocol.io'
    results.append(probe(host, '/login', 'http', {301, 302, 307, 308}))
    results.append(probe(host, '/api/health', 'https', {200}))
    results.append(probe(host, '/login', 'https', {200}))
    print(json.dumps(results, indent=2))
    return 0 if all(r['passed'] for r in results) else 1


if __name__ == '__main__':
    raise SystemExit(main())
