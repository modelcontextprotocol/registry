#!/usr/bin/env python3
"""Compare complete response hashes for search, filters and real cursor pagination."""
import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import urllib.parse
import urllib.request
from cache import wait
from cycles import stop


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ('baseline', 'candidate', 'out'):
        parser.add_argument('--'+key, required=True, type=pathlib.Path)
    parser.add_argument('--pg-port', type=int, default=55434)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    terms = ['notion', 'net.todoist/mcp', 'NET.TODOIST/MCP', 'todoist/m', 'github/benchmark', 'io.github.github/', 'io.github.', 'benchmark', 'mcp', '/server', '000', 'example.benchmark-000008/serv', 'missing/foo', 'example/%', 'example/_', 'example/\\', '测试名/abc', 'foo/bar/baz']
    for n in range(40, 120):
        name = f'io.github.benchmark-{n:06d}/server' if n % 10 < 7 else f'com.example.benchmark-{n:06d}/server'
        terms += [name, name[3:].upper()]
    filters = [{}, {'version': 'latest'}, {'version': '1.0.1'}, {'include_deleted': 'true'}, {'updated_since': '2026-10-19T12:00:00Z'}]
    results = {}
    for variant in ('baseline', 'candidate'):
        checks = {}
        with (args.out/(variant+'.log')).open('w') as log:
            proc = subprocess.Popen([str(getattr(args, variant).resolve()), '-mode', 'serve', '-db', f'postgres://mcpregistry@127.0.0.1:{args.pg_port}/registry_bench'], stdout=log, stderr=log, env={**os.environ, 'GOMAXPROCS': '2'})
            try:
                wait('http://127.0.0.1:18080/_readbench/stats', proc)
                for api in ('v0', 'v0.1'):
                    for i, term in enumerate(terms):
                        query = {'search': term, 'limit': '2', **filters[i % len(filters)]}
                        hashes = []
                        previous = None
                        for _ in range(3):
                            url = f'http://127.0.0.1:18080/{api}/servers?' + urllib.parse.urlencode(query)
                            with urllib.request.urlopen(url, timeout=15) as response:
                                body = json.load(response)
                            hashes.append(hashlib.sha256(json.dumps(body,sort_keys=True).encode()).hexdigest())
                            for row in body['servers']:
                                key = (row['server']['name'], row['server']['version'])
                                assert previous is None or key > previous, 'Duplicate/unordered pagination'
                                previous = key
                            cursor = body['metadata'].get('nextCursor')
                            if not cursor:
                                break
                            query['cursor'] = cursor
                        checks[f'{api}/{i}'] = {'search':term, 'filters':filters[i % len(filters)], 'hashes':hashes}
            finally:
                stop(proc)
        results[variant] = checks
    passed = results['baseline'] == results['candidate']
    (args.out/'equivalence.json').write_text(json.dumps({'passed':passed, 'cases_per_variant':len(results['baseline']), 'responses_per_variant':sum(len(c['hashes']) for c in results['baseline'].values()), 'results':results},indent=2))
    assert passed, 'Search response mismatch; inspect retained evidence'
    print('Identical full response hashes:', len(results['baseline']), 'cases across both API versions')


if __name__ == '__main__':
    main()
