#!/usr/bin/env python3
"""Gracefully replace one canary proxy during a bounded fixture load."""
import argparse, json, subprocess, time
from pathlib import Path
from measure import gateway_target
a = argparse.ArgumentParser()
a.add_argument('--output', type=Path, required=True)
args = a.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
NS, NAME = gateway_target()
K = ['kubectl', '--context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging', '-n', NS]

deployment = json.loads(subprocess.check_output(K + ['get', 'deployment', NAME, '-o', 'json']))
selector = ','.join(k + '=' + v for k, v in deployment['spec']['selector']['matchLabels'].items())

def pods():
    return json.loads(subprocess.check_output(K + ['get', 'pods', '-l', selector, '-o', 'json']))['items']
ready = [p for p in pods() if not p['metadata'].get('deletionTimestamp') and p.get('status', {}).get('containerStatuses') and all((x.get('ready') for x in p.get('status', {}).get('containerStatuses', [])))]
assert len(ready) == deployment['spec']['replicas'] == deployment['status'].get('readyReplicas', 0) > 0, len(ready)
with open(args.output / 'load.log', 'w') as log:
    p = subprocess.Popen(['python3', 'gateway-canary/measure.py', '--arm', 'ngf', '--rate', '500', '--seconds', '180', '--epoch', 'canary-rollout', '--output', str(args.output / 'load')], stdout=log, stderr=subprocess.STDOUT)
    time.sleep(30)
    assert p.poll() is None, 'load stopped before replacement'
    old = ready[0]['metadata']['name']
    started = time.time()
    subprocess.run(K + ['delete', 'pod', old, '--wait=false'], check=True)
    subprocess.run(K + ['rollout', 'status', 'deployment/' + NAME, '--timeout=180s'], check=True)
    report = {'deleted_pod': old, 'started_at': started, 'ready_at': time.time(), 'pods': [{'name': x['metadata']['name'], 'ready': all((c.get('ready') for c in x.get('status', {}).get('containerStatuses', [])))} for x in pods() if x['metadata']['name'].startswith(NAME + '-')]}
    report['measurement_exit'] = p.wait(timeout=300)
    (args.output / 'operation.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))
    assert report['measurement_exit'] == 0
