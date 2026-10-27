#!/usr/bin/env python3
"""Check response equivalence and retain SQL plans for the statistics experiment."""
import argparse
import hashlib
import json
import os
import pathlib
import signal
import subprocess
import urllib.parse
import urllib.request

from cache import wait


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=pathlib.Path)
    parser.add_argument('--out', required=True, type=pathlib.Path)
    parser.add_argument('--pg-port', type=int, default=5432)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    base = ['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-h', '127.0.0.1', '-p', str(args.pg_port), '-U', 'mcpregistry', '-d', 'registry_bench', '-At']

    def sql(query):
        return subprocess.check_output(base + ['-c', query], text=True)

    original = int(sql("SELECT coalesce(attstattarget,-1) FROM pg_attribute WHERE attrelid='servers'::regclass AND attname='updated_at'"))
    cases = {
        'page': {},
        'deep_page': {'cursor': 'io.benchmark/server-060000:1.0.0'},
        'updated': {'updated_since': '2026-10-19T12:00:00Z'},
        'sparse': {'updated_since': '2026-10-26T11:00:00Z'},
        'search': {'search': 'server-0000'},
        'latest': {'version': 'latest'},
        'exact_version': {'version': '1.0.1'},
        'deleted': {'include_deleted': 'true'},
        'empty': {'cursor': 'zzzz:1.0.0'},
    }
    results = {}
    with (args.out/'app.log').open('w') as log:
        proc = subprocess.Popen([str(args.binary.resolve()), '-mode', 'serve', '-db', f'postgres://mcpregistry@127.0.0.1:{args.pg_port}/registry_bench'], stdout=log, stderr=log, env={**os.environ, 'GOMAXPROCS': '2'})
        try:
            wait('http://127.0.0.1:18080/_readbench/stats', proc)
            for target in [100, 1000]:
                sql(f'ALTER TABLE servers ALTER COLUMN updated_at SET STATISTICS {target}; ANALYZE servers(updated_at)')
                checks = {}
                for version in ['v0', 'v0.1']:
                    for name, filters in cases.items():
                        query = {'limit': '100', 'cursor': 'io.benchmark/server-000000:0', **filters}
                        hashes = []
                        previous = None
                        deleted = 0
                        for _ in range(3):
                            url = f'http://127.0.0.1:18080/{version}/servers?' + urllib.parse.urlencode(query)
                            with urllib.request.urlopen(url, timeout=30) as response:
                                body = json.load(response)
                            hashes.append(hashlib.sha256(json.dumps(body, sort_keys=True).encode()).hexdigest())
                            rows = body['servers']
                            assert body['metadata']['count'] == len(rows)
                            for row in rows:
                                server = row['server']
                                meta = row['_meta']['io.modelcontextprotocol.registry/official']
                                key = (server['name'], server['version'])
                                assert previous is None or key > previous, 'Unordered or duplicate pagination'
                                previous = key
                                deleted += meta['status'] == 'deleted'
                                if 'updated_since' not in query and query.get('include_deleted') != 'true':
                                    assert meta['status'] != 'deleted'
                                if query.get('version') == 'latest':
                                    assert meta['isLatest']
                                if query.get('version') == '1.0.1':
                                    assert server['version'] == '1.0.1'
                            cursor = body['metadata'].get('nextCursor')
                            if not cursor:
                                break
                            assert cursor == ':'.join(previous)
                            query['cursor'] = cursor
                        if name == 'deleted':
                            assert deleted > 0, 'Fixture must exercise deleted records'
                        if name == 'empty':
                            assert not rows
                        checks[version+'/'+name] = {'page_hashes': hashes, 'deleted_records': deleted}
                results[str(target)] = checks
                for name, stamp in [('hour', '2026-10-26 11:00:00+00'), ('day', '2026-10-25 12:00:00+00'), ('week', '2026-10-19 12:00:00+00')]:
                    plan = sql(f"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT server_name,version,status,status_changed_at,status_message,published_at,updated_at,is_latest,value FROM servers WHERE updated_at > '{stamp}' AND (server_name,version)>('io.benchmark/server-030000','1.0.0') ORDER BY server_name,version LIMIT 100")
                    (args.out/f'stats{target}-{name}-plan.json').write_text(plan)
                    for mode in ['force_custom_plan', 'force_generic_plan']:
                        prepared = sql(f"SET plan_cache_mode={mode}; PREPARE bench(timestamptz,text,text,integer) AS SELECT server_name,version,status,status_changed_at,status_message,published_at,updated_at,is_latest,value FROM servers WHERE updated_at > $1 AND (server_name,version)>($2,$3) ORDER BY server_name,version LIMIT $4; EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE bench('{stamp}','io.benchmark/server-030000','1.0.0',100)")
                        (args.out/f'stats{target}-{name}-{mode}.txt').write_text(prepared)
            assert results['100'] == results['1000'], 'Statistics changed API results'
            (args.out/'equivalence.json').write_text(json.dumps({'passed': True, 'cases': results}, indent=2))
            print('Response equivalence passed: 18 cases, up to three pages each, full-body hashes; SQL plans retained')
        finally:
            proc.send_signal(signal.SIGTERM)
            try:
                proc.wait(timeout=15)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
            sql(f'ALTER TABLE servers ALTER COLUMN updated_at SET STATISTICS {original}; ANALYZE servers(updated_at)')


if __name__ == '__main__':
    main()
