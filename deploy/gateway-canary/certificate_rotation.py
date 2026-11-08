#!/usr/bin/env python3
"""Force renewal and verify Gateway mTLS credentials propagate, without exposing keys."""
import argparse
import base64
import hashlib
import json
import subprocess
import time
from pathlib import Path
from measure import K, NS, get
from monitoring_staging import IMAGE


def run(*args):
    return subprocess.run(K + list(args), check=True, capture_output=True, text=True, timeout=60)


def fingerprint(namespace, name):
    public = run('-n', namespace, 'get', 'secret', name, '-o', 'jsonpath={.data.tls\\.crt}').stdout
    return hashlib.sha256(base64.b64decode(public)).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--cmctl', required=True)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    controller = get('-n', NS, 'get', 'deployment', 'registry-gateway-canary')
    generation = controller['metadata']['generation']
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
    while fingerprint('ingress-nginx', 'gateway-agent-tls') != fingerprint(NS, 'gateway-agent-tls'):
        assert time.monotonic() < deadline, 'Agent Secret did not propagate'
        time.sleep(3)
    assert get('-n', NS, 'get', 'deployment', 'registry-gateway-canary')['metadata']['generation'] == generation
    # A fresh mTLS handshake verifies the running server loaded the renewed leaf
    # and accepts the renewed client certificate. Credentials stay mounted in-cluster.
    probe = 'gateway-certificate-rotation-proof'
    script = ('import ssl,socket,hashlib,json; '
              'c=ssl.create_default_context(cafile="/tls/ca.crt"); '
              'c.minimum_version=ssl.TLSVersion.TLSv1_3; '
              'c.load_cert_chain("/tls/tls.crt","/tls/tls.key"); '
              's=c.wrap_socket(socket.create_connection(("registry-gateway-canary",8443),timeout=15),'
              'server_hostname="registry-gateway-canary.registry-gateway-canary.svc"); '
              'print(json.dumps({"server_certificate_sha256":hashlib.sha256(s.getpeercert(binary_form=True)).hexdigest(),"protocol":s.version()})); s.close()')
    pod = {'apiVersion':'v1','kind':'Pod','metadata':{'name':probe,'namespace':NS},'spec':{
        'restartPolicy':'Never','automountServiceAccountToken':False,
        'containers':[{'name':'probe','image':IMAGE,'command':['python3','-c',script],
                       'resources':{'requests':{'cpu':'10m','memory':'24Mi'},'limits':{'cpu':'100m','memory':'64Mi'}},
                       'volumeMounts':[{'name':'tls','mountPath':'/tls','readOnly':True}],
                       'securityContext':{'allowPrivilegeEscalation':False,'readOnlyRootFilesystem':True,'capabilities':{'drop':['ALL']}}}],
        'volumes':[{'name':'tls','secret':{'secretName':'gateway-agent-tls'}}]}}
    subprocess.run(K+['create','-f','-'],input=json.dumps(pod),capture_output=True,text=True,check=True,timeout=30)
    try:
        subprocess.run(K+['-n',NS,'wait','--for=jsonpath={.status.phase}=Succeeded','pod/'+probe,'--timeout=120s'],capture_output=True,check=True,timeout=130)
        handshake=json.loads(run('-n',NS,'logs',probe).stdout)
        assert handshake['server_certificate_sha256']==fingerprint(NS,'gateway-server-tls')
        report['fresh_mtls_handshake']=handshake
    finally:
        run('-n',NS,'delete','pod',probe,'--wait=false')
    report['controller_deployment_unchanged'] = True
    report['agent_secret_propagated'] = True
    report['finished'] = time.time()
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
