#!/usr/bin/env python3
"""Exercise the staging canary HPA from two to four replicas under fixture load."""
import argparse, json, subprocess, time
from pathlib import Path
from measure import gateway_target
a = argparse.ArgumentParser()
a.add_argument('--output', type=Path, required=True)
args = a.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
K = ['kubectl', '--context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging']
NS, NAME = gateway_target()

def get(*args):
    return json.loads(subprocess.check_output(K + list(args) + ['-o', 'json']))
h = get('-n', NS, 'get', 'hpa', NAME)
node_names = sorted(n['metadata']['name'] for n in get('get', 'nodes')['items'])
assert len(node_names) == 4, 'Qualification requires the original four-node cluster'
assert h['spec']['maxReplicas'] == 4
assert h['status'].get('currentReplicas') == 2, 'wait for the cooldown to return to two replicas'
assert not any((p['metadata']['name'].startswith('registry-canary-baseline-') for p in get('-n', NS, 'get', 'pods')['items']))
with open(args.output / 'load.log', 'w') as log:
    p = subprocess.Popen(['python3', 'gateway-canary/measure.py', '--arm', 'ngf', '--workers', '256', '--rate', '1000', '--seconds', '240', '--epoch', 'canary-autoscale', '--output', str(args.output / 'load')], stdout=log, stderr=subprocess.STDOUT)
    rows = []
    while p.poll() is None:
        h = get('-n', NS, 'get', 'hpa', NAME)
        d = get('-n', NS, 'get', 'deployment', NAME)
        nodes = get('get', 'nodes', '-l', 'cloud.google.com/gke-nodepool=registry-gateway-canary')['items']
        cluster_nodes = sorted(n['metadata']['name'] for n in get('get', 'nodes')['items'])
        row = {'cluster_nodes': cluster_nodes, 'at': time.time(), 'hpa': h['status'], 'deployment': d['status'], 'nodes': len(nodes)}
        rows.append(row)
        print(json.dumps({'current': h['status'].get('currentReplicas'), 'desired': h['status'].get('desiredReplicas'), 'ready': d['status'].get('readyReplicas'), 'nodes': len(nodes)}), flush=True)
        time.sleep(10)
    (args.output / 'operation.json').write_text(json.dumps({'exit': p.returncode, 'samples': rows}, indent=2) + '\n')
    assert all(r['cluster_nodes'] == node_names for r in rows), 'Cluster nodes changed during scaling qualification'
    assert p.returncode == 0, p.returncode
    assert max((r['deployment'].get('readyReplicas', 0) for r in rows)) >= 4, 'did not reach four ready replicas'
