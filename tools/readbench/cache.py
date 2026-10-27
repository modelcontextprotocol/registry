#!/usr/bin/env python3
"""Isolate cache coalescing with a six-second artificial miss, not a capacity forecast."""
import argparse
import json
import os
import pathlib
import signal
import subprocess
import time
import urllib.request


def wait(url, proc):
    for _ in range(100):
        if proc.poll() is not None:
            raise RuntimeError('Child process failed; inspect its log')
        try:
            with urllib.request.urlopen(url, timeout=1):
                return
        except OSError:
            time.sleep(.1)
    raise RuntimeError('Server did not start')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=pathlib.Path)
    parser.add_argument('--out', required=True, type=pathlib.Path)
    parser.add_argument('--pg-port', type=int, default=5432)
    args = parser.parse_args()
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out/'metadata.json').write_text(json.dumps({'nginx': subprocess.check_output(['nginx', '-v'], stderr=subprocess.STDOUT, text=True).strip(), 'order': [5, 10, 10, 5], 'artificial_delay_seconds': 6}, indent=2))
    observations = []
    for run, lock in enumerate([5, 10, 10, 5]):
        for attempt in range(3):
            root = args.out/(str(run) + (f'-retry{attempt}' if attempt else ''))
            root.mkdir()
            (root/'cache').mkdir()
            app_log = (root/'app.log').open('w')
            nginx_log = (root/'nginx.log').open('w')
            app = subprocess.Popen([str(args.binary.resolve()), '-mode', 'serve', '-listen', '127.0.0.1:18080', '-listing-delay', '6s', '-db', f'postgres://mcpregistry@127.0.0.1:{args.pg_port}/registry_bench'], env={**os.environ, 'GOMAXPROCS': '2'}, stdout=app_log, stderr=app_log)
            nginx = None
            try:
                wait('http://127.0.0.1:18080/_readbench/stats', app)
                config = f'''worker_processes 1;
    error_log "{root}/error.log";
    pid "{root}/nginx.pid";
    events {{ worker_connections 4096; }}
    http {{
     access_log "{root}/access.log";
     proxy_cache_path "{root}/cache" levels=1:2 keys_zone=bench:20m max_size=1g inactive=5m use_temp_path=off;
     upstream backend {{ server 127.0.0.1:18080; keepalive 64; }}
     server {{ listen 127.0.0.1:18082;
      location / {{ proxy_pass http://backend; proxy_http_version 1.1; proxy_set_header Connection ""; }}
      location = /v0.1/servers {{
       proxy_pass http://backend; proxy_http_version 1.1; proxy_set_header Connection "";
       proxy_buffering on; proxy_cache bench;
       proxy_cache_key "$scheme$request_method$host$request_uri";
       proxy_cache_valid 200 30s;
       proxy_cache_lock on; proxy_cache_lock_timeout {lock}s; proxy_cache_lock_age {lock}s;
       proxy_cache_background_update on;
       proxy_cache_use_stale updating error timeout http_500 http_502 http_503 http_504;
       add_header X-Registry-Cache $upstream_cache_status always;
      }}
     }}
    }}
    '''
                config_path = root/'nginx.conf'
                config_path.write_text(config)
                nginx = subprocess.Popen(['nginx', '-p', str(root)+'/', '-c', str(config_path), '-g', 'daemon off;'], stdout=nginx_log, stderr=nginx_log)
                wait('http://127.0.0.1:18082/_readbench/stats', nginx)
                result_path = root/'result.json'
                subprocess.run([str(args.binary.resolve()), '-target', 'http://127.0.0.1:18082', '-duration', '8s', '-timeout', '20s', '-rate', '100', '-workers', '1024', '-scenario', 'hot', '-publish-rate', '1', '-out', str(result_path)], check=True)
                result = json.loads(result_path.read_text())
                if not result.get('valid_for_comparison', False):
                    print('Invalid cache trial preserved:', root, result.get('invalid_reasons'), flush=True)
                    if attempt == 2:
                        raise RuntimeError('Three invalid cache trials; host clock must stabilize')
                    continue
                observations.append({'run': run, 'evidence_directory': root.name, 'lock_seconds': lock, 'artificial_origin_delay_seconds': 6, 'result': result})
                print('lock', lock, 'origins', result['origin_read_starts'], 'p95', result['successful_latency_from_scheduled_time']['all_reads']['p95_ms'], 'status', result['status'], flush=True)
                break
            finally:
                for proc in [nginx, app]:
                    if proc is not None:
                        proc.send_signal(signal.SIGTERM)
                        try:
                            proc.wait(timeout=15)
                        except subprocess.TimeoutExpired:
                            proc.kill()
                            proc.wait()
                app_log.close()
                nginx_log.close()
    (args.out/'comparison.json').write_text(json.dumps(observations, indent=2))


if __name__ == '__main__':
    main()
