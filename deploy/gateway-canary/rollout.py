#!/usr/bin/env python3
"""Gracefully replace one canary proxy during a bounded fixture load."""
import argparse, json, subprocess, time
from pathlib import Path
a = argparse.ArgumentParser()
a.add_argument('--output', type=Path, required=True)
args = a.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
K = ['kubectl', '--context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging', '-n', 'registry-gateway-canary']

def pods():
    return json.loads(subprocess.check_output(K + ['get', 'pods', '-o', 'json']))['items']
ready = [p for p in pods() if p['metadata']['name'].startswith('registry-canary-registry-gateway-canary-') and all((x.get('ready') for x in p.get('status', {}).get('containerStatuses', [])))]
assert len(ready) == 2, len(ready)
with open(args.output / 'load.log', 'w') as log:
    p = subprocess.Popen(['python3', 'gateway-canary/measure.py', '--arm', 'ngf', '--rate', '500', '--seconds', '90', '--epoch', 'canary-rollout', '--output', str(args.output / 'load')], stdout=log, stderr=subprocess.STDOUT)
    time.sleep(30)
    assert p.poll() is None, 'load stopped before replacement'
    old = ready[0]['metadata']['name']
    started = time.time()
    subprocess.run(K + ['delete', 'pod', old, '--wait=false'], check=True)
    subprocess.run(K + ['rollout', 'status', 'deployment/registry-canary-registry-gateway-canary', '--timeout=180s'], check=True)
    report = {'deleted_pod': old, 'started_at': started, 'ready_at': time.time(), 'pods': [{'name': x['metadata']['name'], 'ready': all((c.get('ready') for c in x.get('status', {}).get('containerStatuses', [])))} for x in pods() if x['metadata']['name'].startswith('registry-canary-registry-gateway-canary-')]}
    report['measurement_exit'] = p.wait(timeout=200)
    (args.output / 'operation.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    assert report['measurement_exit'] == 0
