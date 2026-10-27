#!/usr/bin/env python3
"""Local-only, counterbalanced statistics experiment; no cloud clients or credentials."""
import argparse
import hashlib
import json
import os
import pathlib
import signal
import subprocess
import time
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=pathlib.Path)
    parser.add_argument('--out', required=True, type=pathlib.Path)
    parser.add_argument('--pg-port', type=int, default=5432)
    parser.add_argument('--http-port', type=int, default=18080)
    parser.add_argument('--duration', default='20s')
    parser.add_argument('--rows', type=int, default=185001)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    base = ['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-h', '127.0.0.1', '-p', str(args.pg_port), '-U', 'mcpregistry', '-d', 'registry_bench', '-At']

    def sql(query):
        return subprocess.check_output(base + ['-c', query], text=True)

    original = int(sql("SELECT coalesce(attstattarget,-1) FROM pg_attribute WHERE attrelid='servers'::regclass AND attname='updated_at'"))
    original_count = int(sql('SELECT count(*) FROM servers'))
    if original_count < args.rows:
        raise SystemExit('Seed the isolated benchmark database first')
    metadata = {'fixture_rows': args.rows, 'actual_rows_before': original_count, 'go': subprocess.check_output(['go', 'version'], text=True).strip(), 'postgres': sql('SELECT version()').strip(), 'gomaxprocs': 2, 'duration': args.duration, 'order': [100, 1000, 1000, 100], 'table_sizes': sql("SELECT pg_relation_size('servers'),pg_total_relation_size('servers')").strip(), 'hardware': os.uname().machine}
    metadata['binary_sha256'] = hashlib.sha256(args.binary.read_bytes()).hexdigest()
    metadata['source_commit'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    metadata['source_status'] = subprocess.check_output(['git', 'status', '--short'], text=True).strip()
    metadata['source_sha256'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(pathlib.Path('tools/readbench').glob('*.go'))}
    metadata['fixture_sha256'] = hashlib.sha256(pathlib.Path('data/seed.json').read_bytes()).hexdigest()
    metadata['postgres_settings'] = json.loads(sql("SELECT json_object_agg(name,setting) FROM pg_settings WHERE name IN ('shared_buffers','max_connections','work_mem','effective_cache_size','random_page_cost','default_statistics_target','jit','plan_cache_mode','max_parallel_workers_per_gather','track_io_timing')"))
    metadata['statement_stats_reset'] = sql('SELECT stats_reset FROM pg_stat_statements_info').strip()
    (args.out / 'metadata.json').write_text(json.dumps(metadata, indent=2))
    observations = []
    try:
        for run, target in enumerate(metadata['order']):
            # Delete only this tool's synthetic publishes, never fixture or arbitrary data.
            sql("DELETE FROM servers WHERE server_name LIKE 'io.benchmark/publish-%'")
            sql(f'ALTER TABLE servers ALTER COLUMN updated_at SET STATISTICS {target}; ANALYZE servers(updated_at)')
            log = (args.out / f'app-{run}.log').open('w')
            proc = subprocess.Popen([str(args.binary.resolve()), '-mode', 'serve', '-listen', f'127.0.0.1:{args.http_port}', '-db', f'postgres://mcpregistry@127.0.0.1:{args.pg_port}/registry_bench'], stdout=log, stderr=log, env={**os.environ, 'GOMAXPROCS': '2'})
            try:
                url = f'http://127.0.0.1:{args.http_port}'
                for _ in range(100):
                    if proc.poll() is not None:
                        raise RuntimeError('Benchmark server failed; inspect its log')
                    try:
                        with urllib.request.urlopen(url+'/_readbench/stats', timeout=1):
                            break
                    except OSError:
                        time.sleep(.1)
                else:
                    raise RuntimeError('Benchmark server did not become ready')
                for scenario, rate in [('sparse', 600), ('mixed', 1800)]:
                    for attempt in range(3):
                        name = f'{run}-stats{target}-{scenario}' + (f'-retry{attempt}' if attempt else '')
                        common = [str(args.binary.resolve()), '-target', url, '-rows', str(args.rows), '-scenario', scenario]
                        subprocess.run(common+['-duration', '5s', '-rate', '100', '-publish-rate', '0', '-out', str(args.out/(name+'-warmup.json'))], check=True)
                        before = sql("SELECT coalesce(json_agg(t),'[]') FROM (SELECT queryid,calls,total_exec_time,shared_blks_hit,shared_blks_read,query FROM pg_stat_statements WHERE query LIKE '%FROM servers%')t")
                        (args.out/(name+'-db-before.json')).write_text(before)
                        subprocess.run(common+['-duration', args.duration, '-rate', str(rate), '-out', str(args.out/(name+'.json'))], check=True)
                        after = sql("SELECT coalesce(json_agg(t),'[]') FROM (SELECT queryid,calls,total_exec_time,shared_blks_hit,shared_blks_read,query FROM pg_stat_statements WHERE query LIKE '%FROM servers%')t")
                        (args.out/(name+'-db-after.json')).write_text(after)
                        result = json.loads((args.out/(name+'.json')).read_text())
                        if not result.get('valid_for_comparison', False):
                            print('Invalid timed run preserved:', name, result.get('invalid_reasons'), flush=True)
                            if attempt == 2:
                                raise RuntimeError('Three invalid timed trials; host clock must stabilize')
                            continue
                        observations.append({'run': run, 'target': target, 'scenario': scenario, 'evidence_prefix': name, 'result': result})
                        print(name, 'goodput', round(result['successful_reads_per_second_including_drain'], 1), 'p95', round(result['successful_latency_from_scheduled_time'].get('all_reads', {}).get('p95_ms', 0), 2), 'dropped', result['driver_dropped_reads'], 'publish', result['status'].get('publish:200', 0), flush=True)
                        break
            finally:
                proc.send_signal(signal.SIGTERM)
                try:
                    proc.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
                log.close()
    finally:
        sql(f'ALTER TABLE servers ALTER COLUMN updated_at SET STATISTICS {original}; ANALYZE servers(updated_at)')
        (args.out/'comparison.json').write_text(json.dumps(observations, indent=2))


if __name__ == '__main__':
    main()
