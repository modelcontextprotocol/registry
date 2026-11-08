#!/usr/bin/env python3
"""Prove native HTTP01 issuance/renewal without replacing public certificates."""
import argparse
import hashlib
import json
import subprocess
import time
from pathlib import Path

K = ['kubectl', '--context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging', '--request-timeout=30s']


def run(*args, **kw):
    return subprocess.run(K + list(args), capture_output=True, text=True, check=True, timeout=60, **kw)


def get(*args):
    return json.loads(run(*args, '-o', 'json').stdout)


def fingerprints():
    return {ns: hashlib.sha256(run('-n', ns, 'get', 'secret', name, '-o', 'jsonpath={.data.tls\\.crt}').stdout.encode()).hexdigest()
            for ns, name in [('default', 'mcp-registry-staging-tls'), ('monitoring', 'grafana-tls')]}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--cmctl', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    issuer = get('get', 'clusterissuer', 'letsencrypt-prod')
    solver = issuer['spec']['acme']['solvers']
    assert len(solver) == 1 and 'gatewayHTTPRoute' in solver[0]['http01']
    parent = solver[0]['http01']['gatewayHTTPRoute']['parentRefs'][0]
    assert parent == {'kind': 'Gateway', 'name': 'registry-edge', 'namespace': 'ingress-nginx', 'sectionName': 'http'}
    before = fingerprints()
    report = {'native_gateway_solver': True, 'proofs': []}
    name = 'gateway-native-proof-' + str(int(time.time()))
    for ns, host in [('default', 'staging.registry.modelcontextprotocol.io'), ('monitoring', 'grafana.staging.registry.modelcontextprotocol.io')]:
        created = []
        proof = {'namespace': ns, 'host': host, 'observed_http_routes': []}
        try:
            for kind, spec in [
                ('Issuer', {'acme': {'server': 'https://acme-staging-v02.api.letsencrypt.org/directory', 'privateKeySecretRef': {'name': name + '-account'}, 'solvers': solver}}),
                ('Certificate', {'secretName': name + '-tls', 'dnsNames': [host], 'issuerRef': {'name': name, 'kind': 'Issuer'}, 'privateKey': {'rotationPolicy': 'Always'}}),
            ]:
                run('create', '-f', '-', input=json.dumps({'apiVersion': 'cert-manager.io/v1', 'kind': kind, 'metadata': {'name': name, 'namespace': ns}, 'spec': spec}))
                created.append(kind)
            for revision in [1, 2]:
                if revision == 2:
                    subprocess.run([args.cmctl, 'renew', name, '-n', ns, '--context', K[1].split('=', 1)[1]], capture_output=True, check=True, timeout=30)
                deadline = time.monotonic() + 360
                while time.monotonic() < deadline:
                    for route in get('-n', ns, 'get', 'httproutes')['items']:
                        if not route['metadata']['name'].startswith('cm-acme-http-solver-'):
                            continue
                        if host not in route['spec'].get('hostnames', []):
                            continue
                        assert route['spec']['parentRefs'][0]['name'] == parent['name']
                        route_name = route['metadata']['name']
                        if route_name not in proof['observed_http_routes']:
                            proof['observed_http_routes'].append(route_name)
                    status = get('-n', ns, 'get', 'certificate', name).get('status', {})
                    conditions = {c['type']: c['status'] for c in status.get('conditions', [])}
                    if status.get('revision', 0) >= revision and conditions.get('Ready') == 'True' and conditions.get('Issuing') != 'True':
                        proof['revision_' + str(revision)] = {k: status[k] for k in ['revision', 'notBefore', 'notAfter', 'renewalTime']}
                        print(f'{ns}: native ACME revision {revision} ready', flush=True)
                        break
                    time.sleep(2)
                else:
                    raise RuntimeError('Native certificate issuance timed out in ' + ns)
            assert proof['observed_http_routes'], 'No native solver route observed'
        finally:
            for kind in reversed(created):
                run('-n', ns, 'delete', kind, name, '--ignore-not-found', '--wait=true')
            run('-n', ns, 'delete', 'secret', name + '-tls', name + '-account', '--ignore-not-found')
            report['proofs'].append(proof)
            report['public_certificates_unchanged'] = fingerprints() == before
            Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
    assert report['public_certificates_unchanged']
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
