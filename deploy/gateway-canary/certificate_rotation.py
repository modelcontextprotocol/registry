#!/usr/bin/env python3
"""Force renewal and verify Gateway mTLS credentials propagate, without exposing keys."""
import argparse
import base64
import hashlib
import json
import subprocess
import ssl
import time
from pathlib import Path
from measure import K, NS, get
from monitoring_staging import IMAGE


def run(*args):
    return subprocess.run(K + list(args), check=True, capture_output=True, text=True, timeout=60)


def fingerprint(namespace, name):
    public = run('-n', namespace, 'get', 'secret', name, '-o', 'jsonpath={.data.tls\\.crt}').stdout
    pem = base64.b64decode(public).decode().split('-----END CERTIFICATE-----')[0] + '-----END CERTIFICATE-----'
    return hashlib.sha256(ssl.PEM_cert_to_DER_cert(pem)).hexdigest()


def controller_processes(controller):
    selector = ','.join(k + '=' + v for k, v in controller['spec']['selector']['matchLabels'].items())
    pods = get('-n', NS, 'get', 'pods', '-l', selector)['items']
    assert len(pods) == controller['spec']['replicas'], 'Controller is rolling; wait before renewal'
    result = {}
    for pod in pods:
        assert not pod['metadata'].get('deletionTimestamp'), 'Controller is terminating'
        statuses = pod['status'].get('containerStatuses', [])
        assert statuses and all(c.get('ready') for c in statuses), 'Controller is not ready'
        result[pod['metadata']['uid']] = {
            c['name']: {'restart_count': c['restartCount'], 'container_id': c['containerID']}
            for c in statuses}
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--cmctl', required=True)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    controller = get('-n', NS, 'get', 'deployment', 'registry-gateway-canary')
    generation = controller['metadata']['generation']
    processes_before = controller_processes(controller)
    started = time.time()
    report = {'started': started, 'renewals': []}
    for name in ['gateway-server-tls', 'gateway-agent-tls']:
        cert = get('-n', NS, 'get', 'certificate', name)
        previous_revision = cert['status']['revision']
        before = fingerprint(NS, name)
        subprocess.run([args.cmctl, 'renew', name, '-n', NS, '--context', K[1].split('=', 1)[1]], capture_output=True, check=True, timeout=30)
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            cert = get('-n', NS, 'get', 'certificate', name)
            conditions = {c['type']: c['status'] for c in cert.get('status', {}).get('conditions', [])}
            if cert.get('status', {}).get('revision', 0) > previous_revision and conditions.get('Ready') == 'True' and conditions.get('Issuing') != 'True':
                break
            time.sleep(2)
        else:
            raise RuntimeError(name + ' renewal timeout')
        after = fingerprint(NS, name)
        assert before != after
        report['renewals'].append({'name': name, 'revision': cert['status']['revision'], 'certificate_changed': True})
        print(name + ' renewed', flush=True)
    # The controller duplicates the agent Secret into each Gateway namespace.
    deadline = time.monotonic() + 180
    deployment = get('-n', 'ingress-nginx', 'get', 'deployment', 'registry-edge-registry-gateway-canary')
    copied_secret = next(v['secret']['secretName'] for v in deployment['spec']['template']['spec']['volumes'] if v['name'] == 'nginx-agent-tls')
    assert copied_secret.endswith('-gateway-agent-tls')
    while fingerprint('ingress-nginx', copied_secret) != fingerprint(NS, 'gateway-agent-tls'):
        assert time.monotonic() < deadline, 'Agent Secret did not propagate'
        time.sleep(3)
    assert get('-n', NS, 'get', 'deployment', 'registry-gateway-canary')['metadata']['generation'] == generation
    # A fresh mTLS handshake verifies the running server loaded the renewed leaf
    # and accepts the renewed client certificate. Credentials stay mounted in-cluster.
    probe = 'gateway-certificate-rotation-proof'
    expected = fingerprint(NS, 'gateway-server-tls')
    service = get('-n', NS, 'get', 'service', 'registry-gateway-canary')
    port = next(p['port'] for p in service['spec']['ports'] if p['name'] == 'agent-grpc')
    script = f"""import ssl,socket,hashlib,json,time
c=ssl.create_default_context(cafile="/tls/ca.crt")
c.minimum_version=ssl.TLSVersion.TLSv1_3
c.load_cert_chain("/tls/tls.crt","/tls/tls.key")
c.set_alpn_protocols(["h2"])
for attempt in range(60):
 with c.wrap_socket(socket.create_connection(("registry-gateway-canary",{port}),timeout=15),server_hostname="registry-gateway-canary.registry-gateway-canary.svc") as s:
  s.sendall(bytes([80,82,73,32,42,32,72,84,84,80,47,50,46,48,13,10,13,10,83,77,13,10,13,10,0,0,0,4,0,0,0,0,0]))
  frame=b""
  while len(frame)<9:
   chunk=s.recv(9-len(frame))
   if not chunk: raise RuntimeError("EOF before HTTP/2 frame header")
   frame+=chunk
  assert frame[3]==4, "No authenticated HTTP/2 settings frame"
  result={{"authenticated_http2":True,"server_certificate_sha256":hashlib.sha256(s.getpeercert(binary_form=True)).hexdigest(),"protocol":s.version()}}
 if result["server_certificate_sha256"]=="{expected}":
  print(json.dumps(result));break
 time.sleep(3)
else: raise RuntimeError("Renewed server certificate did not reach the running controller")
"""
    pod = {'apiVersion':'v1','kind':'Pod','metadata':{'name':probe,'namespace':NS},'spec':{
        'restartPolicy':'Never','automountServiceAccountToken':False,
        'containers':[{'name':'probe','image':IMAGE,'command':['python3','-c',script],
                       'resources':{'requests':{'cpu':'10m','memory':'24Mi'},'limits':{'cpu':'100m','memory':'64Mi'}},
                       'volumeMounts':[{'name':'tls','mountPath':'/tls','readOnly':True}],
                       'securityContext':{'allowPrivilegeEscalation':False,'readOnlyRootFilesystem':True,'capabilities':{'drop':['ALL']}}}],
        'volumes':[{'name':'tls','secret':{'secretName':'gateway-agent-tls'}}]}}
    subprocess.run(K+['create','-f','-'],input=json.dumps(pod),capture_output=True,text=True,check=True,timeout=30)
    try:
        deadline = time.monotonic() + 240
        while True:
            phase = get('-n', NS, 'get', 'pod', probe)['status']['phase']
            if phase == 'Succeeded':
                break
            if phase == 'Failed':
                raise RuntimeError(run('-n', NS, 'logs', probe).stdout[-1500:])
            assert time.monotonic() < deadline, 'mTLS probe timeout'
            time.sleep(3)
        handshake=json.loads(run('-n',NS,'logs',probe).stdout)
        assert handshake['server_certificate_sha256']==fingerprint(NS,'gateway-server-tls')
        report['fresh_mtls_handshake']=handshake
    finally:
        run('-n',NS,'delete','pod',probe,'--wait=false')
    current = get('-n', NS, 'get', 'deployment', 'registry-gateway-canary')
    assert current['metadata']['generation'] == generation, 'Controller deployment changed during renewal'
    assert controller_processes(current) == processes_before, 'Controller Pod or container restarted during renewal'
    report['controller_processes_unchanged'] = True
    report['controller_deployment_unchanged'] = True
    report['agent_secret_propagated'] = True
    report['finished'] = time.time()
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
