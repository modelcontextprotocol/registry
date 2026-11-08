#!/usr/bin/env python3
"""Prove log -> Vector -> VictoriaMetrics -> Grafana -> internal webhook delivery.

Staging only. Temporarily mounts a file-provisioned test rule/contact point in
Grafana, requiring three Grafana rolling restarts (install, delete, unmount).
Does not modify notification policies or send to external recipients. Guarded
JSON patches refuse to overwrite a concurrent deployment. Cleanup errors fail
loudly and retain the journal needed to finish cleanup.
"""
import argparse
import copy
import json
import subprocess
import time
from pathlib import Path

from measure import K, NS, get, raw
from monitoring_contract import record
from monitoring_staging import IMAGE, run, collection_nodes

NAME = 'gateway-monitoring-proof'
RULE = 'gateway-monitoring-proof'
CONTACT = 'gateway-monitoring-proof-hook'
HANDLER = '''import json
from http.server import BaseHTTPRequestHandler, HTTPServer
events=[]
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  data=json.dumps(events).encode(); self.send_response(200); self.end_headers(); self.wfile.write(data)
 def do_POST(self):
  size=int(self.headers.get('Content-Length','0'))
  if size>65536: self.send_error(413); return
  payload=json.loads(self.rfile.read(size))
  events.append({'status':payload.get('status'),'alerts':[{'status':a.get('status'),'alertname':a.get('labels',{}).get('alertname')} for a in payload.get('alerts',[])]})
  del events[:-32]
  self.send_response(200); self.end_headers()
 def log_message(self,*args): pass
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
'''


def sql(query):
    return subprocess.check_output(K + ['-n', 'monitoring', 'exec', 'grafana-pg-1', '-c', 'postgres',
                                      '--', 'psql', '-U', 'postgres', '-d', 'app', '-At', '-c', query], text=True).strip()


def provision(probe):
    metric = 'registry_gateway_canary_requests_total{traffic="fixture",proxy_pod="' + probe + '",status="'
    expr = '(sum(' + metric + '599"}) or vector(0)) - (sum(' + metric + '200"}) or vector(0))'
    rule = {'uid': RULE, 'title': 'Gateway monitoring delivery proof', 'condition': 'B',
            'for': '0s', 'noDataState': 'OK', 'execErrState': 'Error',
            'labels': {'scope': 'gateway-monitoring-proof'},
            'notification_settings': {'receiver': CONTACT, 'group_wait': '1s', 'group_interval': '10s', 'repeat_interval': '1h'},
            'data': [{'refId': 'A', 'datasourceUid': 'P4169E866C3094E38', 'relativeTimeRange': {'from': 60, 'to': 0},
                      'model': {'expr': expr, 'instant': True, 'range': False, 'refId': 'A', 'intervalMs': 1000, 'maxDataPoints': 43200}},
                     {'refId': 'B', 'datasourceUid': '__expr__', 'relativeTimeRange': {'from': 0, 'to': 0},
                      'model': {'refId': 'B', 'type': 'threshold', 'expression': 'A',
                                'conditions': [{'type': 'query', 'query': {'params': ['B']}, 'reducer': {'type': 'last', 'params': []},
                                                'operator': {'type': 'and'}, 'evaluator': {'type': 'gt', 'params': [0]}}]}}]}
    return {'apiVersion': 1, 'groups': [{'orgId': 1, 'name': NAME, 'folder': 'Gateway Canary', 'interval': '10s', 'rules': [rule]}],
            'contactPoints': [{'orgId': 1, 'name': CONTACT, 'receivers': [{'uid': CONTACT, 'type': 'webhook',
                             'disableResolveMessage': False, 'settings': {'url': f'http://{NAME}.{NS}.svc.cluster.local:8080/'}}]}]}


def cm(data):
    return {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': NAME, 'namespace': 'monitoring'},
            'data': {'proof.json': json.dumps(data)}}


def patch_template(before, after):
    patch = [{'op': 'test', 'path': '/spec/template', 'value': before},
             {'op': 'replace', 'path': '/spec/template', 'value': after}]
    run('-n', 'monitoring', 'patch', 'deployment', 'grafana', '--type=json', '-p', json.dumps(patch))
    run('-n', 'monitoring', 'rollout', 'status', 'deployment/grafana', '--timeout=180s')


def emit(probe, status):
    script = 'import sys; data=sys.stdin.read(); f=open("/proc/1/fd/1","w"); f.write(data); f.flush(); f.close()'
    run('-n', NS, 'exec', '-i', NAME, '--', 'python3', '-c', script,
        input=record(proxy_pod=probe, status=status, uri='/monitoring/probe') + '\n')


def wait_receipt(status):
    deadline = time.monotonic() + 120
    while True:
        events = json.loads(raw(f'/api/v1/namespaces/{NS}/services/{NAME}:8080/proxy/'))
        if any(e['status'] == status for e in events):
            return events
        assert time.monotonic() < deadline, f'No {status} webhook receipt'
        time.sleep(3)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    nodes = get('get', 'nodes')['items']
    node_names = sorted(n['metadata']['name'] for n in nodes)
    canary = collection_nodes(nodes)
    assert sql(f"SELECT count(*) FROM alert_rule WHERE uid='{RULE}';") == '0', 'Proof rule already exists'
    contact_query = ("SELECT jsonb_path_exists(alertmanager_configuration::jsonb, "
                     "'$.alertmanager_config.receivers[*].grafana_managed_receiver_configs[*] ? (@.uid == \""
                     + CONTACT + "\")') FROM alert_configuration ORDER BY id DESC LIMIT 1;")
    assert sql(contact_query) == 'f', 'Proof contact already exists'
    # Preserve the default policy and every unrelated receiver byte-for-byte in the DB.
    route_before = sql("SELECT md5(alertmanager_configuration::jsonb->'alertmanager_config'->>'route') FROM alert_configuration ORDER BY id DESC LIMIT 1;")
    original = get('-n', 'monitoring', 'get', 'deployment', 'grafana')['spec']['template']
    active = copy.deepcopy(original)
    active['metadata'].setdefault('annotations', {})['registry.modelcontextprotocol.io/monitoring-proof'] = 'install'
    active['spec']['volumes'].append({'name': NAME, 'configMap': {'name': NAME, 'defaultMode': 420}})
    container = next(c for c in active['spec']['containers'] if c['name'] == 'grafana')
    container['volumeMounts'].append({'name': NAME, 'mountPath': '/etc/grafana/provisioning/alerting/proof.json',
                                    'subPath': 'proof.json', 'readOnly': True})
    probe = 'monitoring-proof-' + str(int(time.time()))
    journal = args.output.with_suffix('.journal.json')
    journal.write_text(json.dumps({'original_template': original, 'active_template': active, 'probe': probe}, indent=2))
    pod = {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': NAME, 'namespace': NS, 'labels': {'app': NAME}}, 'spec': {
        'nodeName': canary[0], 'restartPolicy': 'Never', 'automountServiceAccountToken': False,
        'tolerations': [{'key': 'registry-role', 'operator': 'Equal', 'value': 'gateway-canary', 'effect': 'NoSchedule'}],
        'containers': [{'name': 'nginx', 'image': IMAGE, 'command': ['python3', '-u', '-c', HANDLER],
                        'resources': {'requests': {'cpu': '10m', 'memory': '24Mi'}, 'limits': {'cpu': '100m', 'memory': '64Mi'}},
                        'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True,
                                            'capabilities': {'drop': ['ALL']}, 'seccompProfile': {'type': 'RuntimeDefault'}}}]}}
    service = {'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': NAME, 'namespace': NS},
               'spec': {'selector': {'app': NAME}, 'ports': [{'port': 8080, 'targetPort': 8080}]}}
    created = []
    installed = False
    try:
        for obj in [pod, service, cm(provision(probe))]:
            run('create', '-f', '-', input=json.dumps(obj))
            created.append(obj)
        run('-n', NS, 'wait', '--for=condition=Ready', 'pod/' + NAME, '--timeout=120s')
        # Save installed before waiting for readiness so a timeout still cleans up.
        patch = [{'op': 'test', 'path': '/spec/template', 'value': original}, {'op': 'replace', 'path': '/spec/template', 'value': active}]
        run('-n', 'monitoring', 'patch', 'deployment', 'grafana', '--type=json', '-p', json.dumps(patch))
        installed = True
        run('-n', 'monitoring', 'rollout', 'status', 'deployment/grafana', '--timeout=180s')
        assert sql(f"SELECT count(*) FROM alert_rule WHERE uid='{RULE}';") == '1'
        emit(probe, '599')
        firing = wait_receipt('firing')
        emit(probe, '200')
        resolved = wait_receipt('resolved')
        result = {'passed': True, 'probe': probe, 'firing': firing, 'resolved': resolved,
                  'external_notification_sent': False, 'nodes': node_names}
    finally:
        if installed:
            deletion = {'apiVersion': 1, 'deleteRules': [{'orgId': 1, 'uid': RULE}],
                        'deleteContactPoints': [{'orgId': 1, 'uid': CONTACT}]}
            run('-n', 'monitoring', 'patch', 'configmap', NAME, '--type=merge', '-p', json.dumps({'data': cm(deletion)['data']}))
            cleanup = copy.deepcopy(active)
            cleanup['metadata']['annotations']['registry.modelcontextprotocol.io/monitoring-proof'] = 'cleanup'
            patch_template(active, cleanup)
            assert sql(f"SELECT count(*) FROM alert_rule WHERE uid='{RULE}';") == '0', 'Proof rule cleanup failed'
            assert sql(contact_query) == 'f', 'Proof contact cleanup failed'
            patch_template(cleanup, original)
        for obj in reversed(created):
            run('-n', obj['metadata']['namespace'], 'delete', obj['kind'], NAME, '--wait=true', '--timeout=60s')
    assert sorted(n['metadata']['name'] for n in get('get', 'nodes')['items']) == node_names, 'Node inventory changed'
    assert route_before == sql("SELECT md5(alertmanager_configuration::jsonb->'alertmanager_config'->>'route') FROM alert_configuration ORDER BY id DESC LIMIT 1;")
    result['cleanup_complete'] = True
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
