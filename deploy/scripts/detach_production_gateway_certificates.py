#!/usr/bin/env python3
"""Review or detach production Certificate owners after the managed preparation.

Read-only by default. --apply removes only the verified old Ingress owner, with
UID/owner preconditions and public certificate-byte verification. No private key
is read. Run from deploy/ with the production Pulumi backend/passphrase selected.
"""
import argparse
import base64
import hashlib
import json
import os
import subprocess
from pathlib import Path
from production_gateway import CERTIFICATES, CONTEXT, check


def run(args, env):
    return subprocess.check_output(args, env=env, text=True, stderr=subprocess.PIPE, timeout=120)


def detach(apply, output):
    os.umask(0o077)
    env = dict(os.environ, PULUMI_BACKEND_URL='gs://mcp-registry-prod-pulumi-state')
    config = json.loads(run(['pulumi', 'config', '--json', '--stack', 'gcpProd'], env))
    if config.get('mcp-registry:gatewayProductionPhase', {}).get('value') != 'managed':
        raise ValueError('Apply the managed certificate preparation before detaching owners')
    check(config)
    state = json.loads(run(['pulumi', 'stack', 'export', '--stack', 'gcpProd', '--non-interactive'], env))
    resources = state['deployment']['resources']
    command = ['kubectl', '--context', CONTEXT, '--request-timeout=30s']
    read = lambda ns, kind, name: json.loads(run(command + ['-n', ns, 'get', kind, name, '-o', 'json'], env))
    prepared = []
    # Validate every Certificate before mutating either one.
    for ns, name, ingress, _ in CERTIFICATES:
        resource = next(r for r in resources if r['type'] == 'kubernetes:cert-manager.io/v1:Certificate' and r.get('id') == ns + '/' + name)
        if not resource.get('protect') or resource['inputs']['metadata'].get('ownerReferences') != []:
            raise ValueError('Production Certificate is not protected and managed in Pulumi')
        cert = read(ns, 'certificate', name)
        owners = cert['metadata'].get('ownerReferences', [])
        if owners:
            old = read(ns, 'ingress', ingress)
            annotations = old['metadata'].get('annotations', {})
            if any(k in annotations for k in ('cert-manager.io/issuer', 'cert-manager.io/cluster-issuer', 'kubernetes.io/tls-acme')):
                raise ValueError('Certificate ingress shim is still enabled')
            if len(owners) != 1 or owners[0].get('kind') != 'Ingress' or owners[0].get('name') != ingress or owners[0].get('uid') != old['metadata']['uid']:
                raise ValueError('Unexpected Certificate owner')
        cert_bytes = run(command + ['-n', ns, 'get', 'secret', name, '-o', r'jsonpath={.data.tls\.crt}'], env)
        prepared.append((ns, name, cert['metadata']['uid'], owners, cert_bytes))
    report = []
    for ns, name, uid, owners, before in prepared:
        if apply and owners:
            patch = [{'op': 'test', 'path': '/metadata/uid', 'value': uid}, {'op': 'test', 'path': '/metadata/ownerReferences', 'value': owners}, {'op': 'remove', 'path': '/metadata/ownerReferences'}]
            run(command + ['-n', ns, 'patch', 'certificate', name, '--type=json', '-p', json.dumps(patch)], env)
        after = read(ns, 'certificate', name)
        after_bytes = run(command + ['-n', ns, 'get', 'secret', name, '-o', r'jsonpath={.data.tls\.crt}'], env)
        if after['metadata']['uid'] != uid or before != after_bytes or (apply and after['metadata'].get('ownerReferences')):
            raise ValueError('Certificate identity changed during detachment')
        report.append({'namespace': ns, 'certificate': name, 'applied': apply, 'owner_detached': not after['metadata'].get('ownerReferences'), 'certificate_sha256': hashlib.sha256(base64.b64decode(before)).hexdigest()})
    output.write_text(json.dumps(report, indent=2) + '\n')
    print('Production certificate detachment ' + ('completed' if apply else 'reviewed; no changes made'))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        detach(args.apply, args.output)
    except (ValueError, KeyError, StopIteration, OSError, subprocess.SubprocessError):
        raise SystemExit('Certificate detachment blocked; verify managed preparation and live ownership privately.')
