import copy
import unittest
from production_gateway import check_endpoint, check_certificate


class ConnectivityGuardTests(unittest.TestCase):
    def setUp(self):
        self.config = {'mcp-registry:gatewayProductionServiceUID': {'value': 'known'}, 'mcp-registry:gatewayProductionPublicIP': {'value': '192.0.2.1'}}
        self.service = {'metadata': {'uid': 'known'}, 'status': {'loadBalancer': {'ingress': [{'ip': '192.0.2.1'}]}}, 'spec': {'type': 'LoadBalancer', 'externalTrafficPolicy': 'Local', 'healthCheckNodePort': 30123, 'ports': [{'port': 80, 'nodePort': 31935, 'protocol': 'TCP'}, {'port': 443, 'nodePort': 30874, 'protocol': 'TCP'}]}}
        self.cert = {'metadata': {'name': 'tls', 'ownerReferences': [{'kind': 'Ingress'}]}, 'spec': {'secretName': 'tls', 'dnsNames': ['primary', 'alias']}, 'status': {'conditions': [{'type': 'Ready', 'status': 'True'}]}}

    def test_identity_changes_fail_closed(self):
        check_endpoint(self.config, self.service)
        for field in ('uid', 'ip', 'port', 'health', 'policy'):
            s = copy.deepcopy(self.service)
            if field == 'uid': s['metadata']['uid'] = 'recreated'
            if field == 'ip': s['status']['loadBalancer']['ingress'][0]['ip'] = '192.0.2.2'
            if field == 'port': s['spec']['ports'][0]['port'] = 8080
            if field == 'health': del s['spec']['healthCheckNodePort']
            if field == 'policy': s['spec']['externalTrafficPolicy'] = 'Cluster'
            with self.subTest(field=field), self.assertRaises(ValueError):
                check_endpoint(self.config, s)

    def test_adoption_preserves_alias_and_requires_detachment_before_retirement(self):
        check_certificate(self.cert, ['primary', 'alias'])
        with self.assertRaises(ValueError): check_certificate(self.cert, ['primary', 'alias'], True)
        self.cert['metadata']['ownerReferences'] = []
        check_certificate(self.cert, ['primary', 'alias'], True)
        with self.assertRaises(ValueError): check_certificate(self.cert, ['primary'], True)
