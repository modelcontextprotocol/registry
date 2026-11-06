#!/usr/bin/env python3
"""Quiescent same-node collector restart: check recovery without historical replay."""
import argparse, json, subprocess, time
from pathlib import Path
import measure
a = argparse.ArgumentParser()
a.add_argument('--output', type=Path, required=True)
args = a.parse_args()
ps = measure.pods()
proxy_nodes = {p['spec'].get('nodeName') for p in ps if p['metadata']['name'].startswith('registry-canary-registry-gateway-canary-')}
old = next((p for p in ps if p['metadata']['name'].startswith('vector-') and p['spec'].get('nodeName') in proxy_nodes))
node = old['spec']['nodeName']
started = time.time()
subprocess.run(measure.K + ['-n', measure.NS, 'delete', 'pod', old['metadata']['name'], '--wait=false'], check=True)
new = None
while time.time() < started + 120:
    for p in measure.pods():
        if p['metadata']['name'].startswith('vector-') and p['spec'].get('nodeName') == node and (p['metadata']['name'] != old['metadata']['name']) and any((x.get('ready') for x in p.get('status', {}).get('containerStatuses', []))):
            new = p
    if new:
        break
    time.sleep(3)
assert new, 'replacement collector did not become ready'
time.sleep(5)
metrics = measure.raw('/api/v1/namespaces/' + measure.NS + '/pods/' + new['metadata']['name'] + ':9598/proxy/metrics')
count = sum((float(x.rsplit(' ', 1)[1]) for x in metrics.splitlines() if x.startswith('registry_gateway_canary_requests_total{') and 'traffic="fixture"' in x))
report = {'old': old['metadata']['name'], 'new': new['metadata']['name'], 'node': node, 'ready_seconds': time.time() - started, 'replayed_fixture_requests': count}
args.output.write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
assert count == 0, 'unexpected replay after checkpoint restore'
