#!/usr/bin/env python3
"""Exercise the staging canary HPA from two to four replicas under fixture load."""
import argparse, json, subprocess, time
from pathlib import Path
a = argparse.ArgumentParser()
a.add_argument('--output', type=Path, required=True)
args = a.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
K = ['kubectl', '--context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging']
NS = 'registry-gateway-canary'
NAME = 'registry-canary-registry-gateway-canary'

def get(*args):
    return json.loads(subprocess.check_output(K + list(args) + ['-o', 'json']))
h = get('-n', NS, 'get', 'hpa', NAME)
assert h['spec']['maxReplicas'] == 4
assert h['status'].get('currentReplicas') == 2, 'wait for the cooldown to return to two replicas'
assert not any((p['metadata']['name'].startswith('registry-canary-baseline-') for p in get('-n', NS, 'get', 'pods')['items']))
log = open(args.output / 'load.log', 'w')
p = subprocess.Popen(['python3', 'gateway-canary/measure.py', '--arm', 'ngf', '--workers', '256', '--rate', '1000', '--seconds', '240', '--epoch', 'canary-autoscale', '--output', str(args.output / 'load')], stdout=log, stderr=subprocess.STDOUT)
rows = []
while p.poll() is None:
    h = get('-n', NS, 'get', 'hpa', NAME)
    d = get('-n', NS, 'get', 'deployment', NAME)
    nodes = get('get', 'nodes', '-l', 'cloud.google.com/gke-nodepool=registry-gateway-canary')['items']
    row = {'at': time.time(), 'hpa': h['status'], 'deployment': d['status'], 'nodes': len(nodes)}
    rows.append(row)
    print(json.dumps({'current': h['status'].get('currentReplicas'), 'desired': h['status'].get('desiredReplicas'), 'ready': d['status'].get('readyReplicas'), 'nodes': len(nodes)}), flush=True)
    time.sleep(10)
(args.output / 'operation.json').write_text(json.dumps({'exit': p.returncode, 'samples': rows}, indent=2) + '\n')
assert p.returncode == 0, p.returncode
assert max((r['deployment'].get('readyReplicas', 0) for r in rows)) >= 4, 'did not reach four ready replicas'
