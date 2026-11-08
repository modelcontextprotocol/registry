"""Read-only production Gateway transition checks; never read private keys."""
import json
import subprocess

CONTEXT = 'gke_mcp-registry-prod_us-central1-b_mcp-registry-prod'
HOSTS = ('prod.registry.modelcontextprotocol.io', 'registry.modelcontextprotocol.io')
CERTIFICATES = (
    ('default', 'mcp-registry-prod-tls', 'mcp-registry', HOSTS),
    ('monitoring', 'grafana-tls', 'grafana-ingress', ('grafana.prod.registry.modelcontextprotocol.io',)),
)


def read_json(args, env=None):
    return json.loads(subprocess.check_output(args, text=True, stderr=subprocess.PIPE, timeout=60, env=env))


def check_endpoint(config, service):
    c = lambda key: config.get('mcp-registry:' + key, {}).get('value', '')
    spec = service['spec']
    if service['metadata']['uid'] != c('gatewayProductionServiceUID'):
        raise ValueError('Production public Service UID differs from the reviewed value')
    addresses = service.get('status', {}).get('loadBalancer', {}).get('ingress', [])
    if len(addresses) != 1 or addresses[0].get('ip') != c('gatewayProductionPublicIP'):
        raise ValueError('Production public IP differs from the reviewed value')
    ports = spec.get('ports', [])
    if (spec.get('type') != 'LoadBalancer' or spec.get('externalTrafficPolicy') != 'Local'
            or not spec.get('healthCheckNodePort') or len(ports) != 2
            or {p['port'] for p in ports} != {80, 443}
            or any(not p.get('nodePort') or p.get('protocol') != 'TCP' for p in ports)):
        raise ValueError('Production load-balancer connectivity contract changed')


def check_certificate(certificate, expected_hosts, require_detached=False):
    spec = certificate['spec']
    if set(spec.get('dnsNames', [])) != set(expected_hosts):
        raise ValueError('Production certificate hostname coverage changed')
    if spec.get('secretName') != certificate['metadata']['name']:
        raise ValueError('Production certificate Secret identity changed')
    if not any(c.get('type') == 'Ready' and c.get('status') == 'True' for c in certificate.get('status', {}).get('conditions', [])):
        raise ValueError('Production certificate is not Ready')
    if require_detached and certificate['metadata'].get('ownerReferences'):
        raise ValueError('Adopt/manage and detach production Certificates before standalone retirement')


def check(config):
    phase = config.get('mcp-registry:gatewayProductionPhase', {}).get('value', '')
    if not phase:
        return
    if phase not in ('adopt', 'managed', 'standalone'):
        raise ValueError('Unknown production Gateway phase')
    if config.get('gcp:project', {}).get('value') != 'mcp-registry-prod':
        raise ValueError('Production Gateway project mismatch')
    command = ['kubectl', '--context', CONTEXT, '--request-timeout=30s']
    service = read_json(command + ['-n', 'ingress-nginx', 'get', 'service', 'ingress-nginx-controller', '-o', 'json'])
    check_endpoint(config, service)
    for namespace, name, _, hosts in CERTIFICATES:
        cert = read_json(command + ['-n', namespace, 'get', 'certificate', name, '-o', 'json'])
        check_certificate(cert, hosts, require_detached=phase == 'standalone')
    print('Production Gateway endpoint and certificate preflight passed (' + phase + ')')
