"""Run inside a temporary GKE Pod with the usage KSA under test. See deploy/README.md."""
import json
import sys
from urllib.parse import urlencode
from urllib.request import Request, urlopen


def read(request):
    with urlopen(request, timeout=20) as response:
        return json.load(response)


def check(project, account):
    if project not in ('mcp-registry-prod', 'mcp-registry-staging') or account not in (
            'registry-usage-collector', 'registry-usage-history-backup'):
        raise ValueError('Expected a hosted project and usage service account')
    token = read(Request('http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token',
                         headers={'Metadata-Flavor': 'Google'}))['access_token']
    headers = {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'}
    log_permission = 'logging.logEntries.list'
    result = read(Request(f'https://cloudresourcemanager.googleapis.com/v1/projects/{project}:testIamPermissions',
                          headers=headers, data=json.dumps({'permissions': [log_permission]}).encode()))
    collector = account == 'registry-usage-collector'
    if (log_permission in result.get('permissions', [])) != collector:
        raise ValueError('Unexpected project log-reading permission')
    objects = {'storage.objects.' + action for action in ('get', 'list', 'create', 'delete', 'update')}
    query = urlencode({'permissions': sorted(objects)}, doseq=True)
    for bucket in (project + '-usage-backups', project + '-backups'):
        result = read(Request(f'https://storage.googleapis.com/storage/v1/b/{bucket}/iam/testPermissions?{query}',
                              headers=headers))
        expected = objects if not collector and bucket == project + '-usage-backups' else set()
        if set(result.get('permissions', [])) != expected:
            raise ValueError(f'Unexpected object permissions for {bucket}')
    print(f'{account} permissions verified in {project}')


if __name__ == '__main__':
    check(*sys.argv[1:])
