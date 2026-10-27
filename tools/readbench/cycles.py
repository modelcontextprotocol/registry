#!/usr/bin/env python3
"""Compare baseline/candidate under repeated real-cursor walks. Local resources only."""
import argparse
import hashlib
import json
import os
import pathlib
import signal
import subprocess

from cache import wait


def stop(proc):
    proc.send_signal(signal.SIGTERM)
    try:
        proc.wait(timeout=15)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['baseline', 'candidate', 'out']:
        parser.add_argument('--'+name, required=True, type=pathlib.Path)
    parser.add_argument('--pg-port', type=int, default=55433)
    parser.add_argument('--crawlers', type=int, default=20)
    parser.add_argument('--rate', type=int, default=1200)
    parser.add_argument('--interval', default='20s')
    parser.add_argument('--recovery', default='30s')
    parser.add_argument('--short-percent', type=int, default=1)
    parser.add_argument('--search-rate', type=int, default=10)
    parser.add_argument('--publish-rate', type=int, default=1)
    parser.add_argument('--direct-search', action='store_true')
    parser.add_argument('--diverse-keys', action='store_true')
    parser.add_argument('--search-scenario', default='observed-search', choices=['observed-search','diverse-search'])
    parser.add_argument('--search-offering', default='0s')
    parser.add_argument('--search-workers', type=int, default=32)
    parser.add_argument('--baseline-sql', type=pathlib.Path)
    parser.add_argument('--candidate-sql', type=pathlib.Path)
    parser.add_argument('--pg-container')
    parser.add_argument('--docker-host')
    parser.add_argument('--order', nargs='+', choices=['baseline','candidate'], default=['baseline','candidate','candidate','baseline'])
    args = parser.parse_args()
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=True)
    base = ['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-h', '127.0.0.1', '-p', str(args.pg_port), '-U', 'mcpregistry', '-d', 'registry_bench', '-At']

    def sql(q):
        return subprocess.check_output(base+['-c', q], text=True)

    meta = {'order': args.order, 'gomaxprocs_per_app': 2, 'apps': 2, 'independent_cache_processes': 2, 'cache_ttl_seconds': 30, 'cache_lock_seconds': 5, 'crawlers': args.crawlers, 'rate_ceiling': args.rate, 'short_search_percent': args.short_percent, 'postgres': sql('SELECT version()').strip(), 'fixture_rows': int(sql('SELECT count(*) FROM servers')), 'nginx': subprocess.check_output(['nginx','-v'], stderr=subprocess.STDOUT, text=True).strip(), 'go': subprocess.check_output(['go','version'],text=True).strip(), 'binary_sha256': {n:hashlib.sha256(getattr(args,n).read_bytes()).hexdigest() for n in ['baseline','candidate']}}
    meta['settings'] = json.loads(sql("SELECT json_object_agg(name,setting) FROM pg_settings WHERE name IN ('shared_buffers','max_connections','default_statistics_target','plan_cache_mode','work_mem')"))
    meta['source_sha256'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(pathlib.Path('tools/readbench').glob('*')) if p.suffix in ['.go','.py','.sh']}
    (args.out/'metadata.json').write_text(json.dumps(meta,indent=2))
    meta['application_source_sha256'] = hashlib.sha256(pathlib.Path('internal/database/postgres.go').read_bytes()).hexdigest()
    meta['workload'] = {'direct_search':args.direct_search, 'diverse_keys':args.diverse_keys, 'search_scenario':args.search_scenario, 'search_offering':args.search_offering, 'search_workers':args.search_workers, 'search_rate':args.search_rate, 'publish_rate':args.publish_rate, 'interval':args.interval, 'recovery':args.recovery}
    docker = None
    if args.pg_container:
        if not args.docker_host or not args.docker_host.startswith('unix:///'):
            parser.error('container metrics require an explicit local Unix Docker socket')
        docker = ['docker','--host',args.docker_host]
        meta['database_container'] = json.loads(subprocess.check_output(docker+['inspect','--format','{{json .HostConfig}}',args.pg_container],text=True))
    (args.out/'metadata.json').write_text(json.dumps(meta,indent=2))

    def container_snapshot(path):
        if docker:
            stats = subprocess.check_output(docker+['exec',args.pg_container,'sh','-c','cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.current /sys/fs/cgroup/memory.events'],text=True)
            path.write_text(stats)

    results = []
    try:
        for run, variant in enumerate(meta['order']):
            for attempt in range(3):
                root = args.out/f'{run}-{variant}-{attempt}'
                root.mkdir()
                setup = getattr(args,variant+'_sql')
                if setup:
                    setup_sql = setup.read_text()
                    (root/'setup.sql').write_text(setup_sql)
                    sql(setup_sql)
                sql("DELETE FROM servers WHERE server_name LIKE 'io.benchmark/publish-%'")
                sql('ANALYZE servers')
                processes = []
                logs = []
                try:
                    for app_id in range(2):
                        log=(root/f'app-{app_id}.log').open('w'); logs.append(log)
                        port=18080+app_id
                        proc=subprocess.Popen([str(getattr(args,variant).resolve()),'-mode','serve','-listen',f'127.0.0.1:{port}','-db',f'postgres://mcpregistry@127.0.0.1:{args.pg_port}/registry_bench'],stdout=log,stderr=log,env={**os.environ,'GOMAXPROCS':'2'})
                        processes.append(proc);wait(f'http://127.0.0.1:{port}/_readbench/stats',proc)
                    for cache_id in range(2):
                        cache=root/f'nginx-{cache_id}';cache.mkdir();(cache/'cache').mkdir()
                        conf=f'''worker_processes 1;
error_log "{cache}/error.log";
pid "{cache}/nginx.pid";
events {{ worker_connections 4096; }}
http {{
 access_log off;
 proxy_cache_path "{cache}/cache" levels=1:2 keys_zone=bench:20m max_size=1g inactive=5m use_temp_path=off;
 upstream app {{ server 127.0.0.1:18080; server 127.0.0.1:18081; keepalive 64; }}
 server {{ listen 127.0.0.1:{18082+cache_id};
  location / {{ proxy_pass http://app; proxy_http_version 1.1; proxy_set_header Connection ""; }}
  location ~ ^/v0(?:\\.1)?/servers$ {{
   proxy_pass http://app; proxy_http_version 1.1; proxy_set_header Connection "";
   proxy_buffering on; proxy_cache bench;
   proxy_cache_key "$scheme$request_method$host$request_uri";
   proxy_cache_valid 200 30s; proxy_cache_lock on;
   proxy_cache_lock_timeout 5s; proxy_cache_lock_age 5s;
   proxy_cache_background_update on;
   proxy_cache_use_stale updating error timeout http_500 http_502 http_503 http_504;
   add_header X-Registry-Cache $upstream_cache_status always;
  }}
 }}
}}
'''
                        path=cache/'nginx.conf';path.write_text(conf)
                        log=(cache/'nginx.log').open('w');logs.append(log)
                        proc=subprocess.Popen(['nginx','-p',str(cache)+'/', '-c',str(path),'-g','daemon off;'],stdout=log,stderr=log);processes.append(proc);wait(f'http://127.0.0.1:{18082+cache_id}/_readbench/stats',proc)
                    # Warm application/DB only; edge caches remain cold for the first wave.
                    subprocess.run([str(args.candidate.resolve()),'-duration','3s','-rate','20','-scenario','observed-search','-publish-rate','0','-out',str(root/'warmup.json')],check=True)
                    counters="SELECT coalesce(json_agg(t),'[]') FROM (SELECT userid,queryid::text,toplevel,calls,total_exec_time,shared_blks_hit,shared_blks_read,query FROM pg_stat_statements WHERE query LIKE '%FROM servers%')t"
                    (root/'db-before.json').write_text(sql(counters))
                    container_snapshot(root/'cgroup-before.txt')
                    extra = ['-search-scenario',args.search_scenario,'-search-offering',args.search_offering,'-search-workers',str(args.search_workers)]
                    if args.direct_search: extra += ['-search-targets','http://127.0.0.1:18080,http://127.0.0.1:18081']
                    if args.diverse_keys: extra += ['-diverse-keys']
                    subprocess.run([str(args.candidate.resolve()),'-mode','cycles','-target','http://127.0.0.1:18082,http://127.0.0.1:18083','-metric-targets','http://127.0.0.1:18080,http://127.0.0.1:18081','-waves','3','-crawlers',str(args.crawlers),'-wave-interval',args.interval,'-recovery',args.recovery,'-rate',str(args.rate),'-search-rate',str(args.search_rate),'-short-percent',str(args.short_percent),'-publish-rate',str(args.publish_rate),'-out',str(root/'result.json')]+extra,check=True)
                    (root/'db-after.json').write_text(sql(counters))
                    container_snapshot(root/'cgroup-after.txt')
                    result=json.loads((root/'result.json').read_text())
                    if not result['valid_measurement']:
                        print('Invalid trial retained:', root.name, flush=True)
                        if attempt==2: raise RuntimeError('Host clock/authentication invalidated three attempts')
                        continue
                    results.append({'run':run,'variant':variant,'evidence_directory':root.name,'result':result})
                    print(variant,'walks',result['completed_crawls'],'failures',result['unexpected_failures'],'short422',result['short_search_422'],'latency',result['success_latency_ms'],flush=True)
                    break
                finally:
                    for proc in reversed(processes):stop(proc)
                    for log in logs:log.close()
    finally:
        (args.out/'comparison.json').write_text(json.dumps(results,indent=2))


if __name__=='__main__':main()
