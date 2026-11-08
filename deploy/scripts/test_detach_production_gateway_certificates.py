import base64
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import detach_production_gateway_certificates as helper
from production_gateway import CERTIFICATES


class DetachmentTests(unittest.TestCase):
    def run_detach(self, apply, bad_second_owner=False, protected=True, shim=False):
        owners = {name: [{'kind': 'Ingress', 'name': ingress, 'uid': 'ingress-' + name}]
                  for _, name, ingress, _ in CERTIFICATES}
        if bad_second_owner:
            owners['grafana-tls'][0]['uid'] = 'unexpected'
        mutations = []
        public_cert = base64.b64encode(b'public certificate').decode()

        def run(args, env):
            if args[:2] == ['pulumi', 'config']:
                return json.dumps({'mcp-registry:gatewayProductionPhase': {'value': 'managed'}})
            if args[:3] == ['pulumi', 'stack', 'export']:
                return json.dumps({'deployment': {'resources': [
                    {'type': 'kubernetes:cert-manager.io/v1:Certificate', 'id': ns + '/' + name,
                     'protect': protected, 'inputs': {'metadata': {'ownerReferences': []}}}
                    for ns, name, _, _ in CERTIFICATES]}})
            self.assertEqual(args[0], 'kubectl')
            if 'patch' in args:
                name = args[args.index('patch') + 2]
                ops = json.loads(args[-1])
                self.assertEqual(ops[0], {'op': 'test', 'path': '/metadata/uid', 'value': 'cert-' + name})
                self.assertEqual(ops[1]['value'], owners[name])
                self.assertEqual(ops[2], {'op': 'remove', 'path': '/metadata/ownerReferences'})
                mutations.append(name)
                owners[name] = []
                return ''
            kind, name = args[args.index('get') + 1:args.index('get') + 3]
            if kind == 'secret':
                self.assertEqual(args[-1], r'jsonpath={.data.tls\.crt}')
                return public_cert
            if kind == 'certificate':
                return json.dumps({'metadata': {'uid': 'cert-' + name, 'ownerReferences': owners[name]}})
            self.assertEqual(kind, 'ingress')
            cert_name = next(cert for _, cert, ingress, _ in CERTIFICATES if ingress == name)
            return json.dumps({'metadata': {'uid': 'ingress-' + cert_name, 'annotations':
                                           {'cert-manager.io/cluster-issuer': 'letsencrypt-prod'} if shim else {}}})

        with tempfile.TemporaryDirectory() as directory, patch.object(helper, 'run', side_effect=run), patch.object(helper, 'check'):
            output = Path(directory) / 'report.json'
            if bad_second_owner or not protected or shim:
                with self.assertRaises(ValueError):
                    helper.detach(apply, output)
                self.assertEqual(mutations, [])
            else:
                helper.detach(apply, output)
                self.assertEqual(len(mutations), 2 if apply else 0)
                self.assertTrue(all(r['owner_detached'] == apply for r in json.loads(output.read_text())))

    def test_read_only_by_default(self):
        self.run_detach(False)

    def test_guarded_detachment(self):
        self.run_detach(True)

    def test_all_certificates_validated_before_first_patch(self):
        self.run_detach(True, bad_second_owner=True)

    def test_requires_protection_and_disabled_shim(self):
        self.run_detach(True, protected=False)
        self.run_detach(True, shim=True)
