import copy
import json
import re
import unittest

import public_read_apply as policy
from production_apply import Blocked


class PublicReadPolicyTests(unittest.TestCase):
    def setUp(self):
        self.pattern = re.compile(policy.POLICY['read_log_regex'])
        self.line = '192.0.2.1 - - [05/Nov/2026:00:00:00 +0000] "GET /v0.1/servers?limit=100 HTTP/2.0" 200 12345 "-" "client" 100 0.125 [default-mcp-registry-8080] [] 10.0.0.1:8080 12345 0.100 200 request-id cache_status="MISS" scheme="https"'

    def test_only_fast_successful_listing_reads_eligible(self):
        for line in [self.line, self.line.replace('GET /v0.1/', 'HEAD /v0/'), self.line.replace('0.125', '0.999'), self.line.replace(' 200 12345', ' 304 12345'), self.line.replace('10.0.0.1:8080 12345 0.100 200', '- - - -')]:
            self.assertIsNotNone(self.pattern.search(line))
        for line in [self.line.replace(' 200 12345', ' 499 12345'), self.line.replace(' 200 12345', ' 429 12345'), self.line.replace(' 200 12345', ' 500 12345'), self.line.replace('0.125', '1.000'), self.line.replace('GET ', 'POST '), self.line.replace('/servers?', '/publish?'), self.line.replace('/servers?', '/auth/token?'), self.line.replace('/servers?', '/servers/name/versions/latest?'), self.line.replace('0.100 200 request-id', '0.100 502 request-id'), self.line.replace('0.100 200 request-id', '0.100, 0.200 502, 200 request-id'), self.line.replace('"client"', '"client\\"malformed"'), 'unparsed error']:
            self.assertIsNone(self.pattern.search(line), line)

    def test_configmap_scope_and_rollback(self):
        _, cm, _ = policy.identities('staging')
        old = {'inputs': {'metadata': {'name': 'ingress-nginx-controller'}, 'data': {'http-snippet': 'existing\n', 'location-snippet': 'cache\n'}}}
        new = copy.deepcopy(old)
        for field, key in [('http-snippet', 'http_snippet'), ('location-snippet', 'location_snippet')]:
            new['inputs']['data'][field] += policy.POLICY[key]
        step = {'urn': cm, 'op': 'update', 'oldState': old, 'newState': new}
        def validate(s, enabled=True):
            return policy.validate({'steps': [s], 'changeSummary': {'update': 1}}, 'staging', enabled, True)
        self.assertTrue(validate(step))
        self.assertTrue(validate(dict(step, oldState=new, newState=old), False))
        bad = copy.deepcopy(step); bad['newState']['inputs']['data']['location-snippet'] += 'proxy_cache off;'
        with self.assertRaises(Blocked): validate(bad)
        for op in ['delete', 'replace', 'create']:
            with self.assertRaises(Blocked): validate(dict(step, op=op))
        with self.assertRaises(Blocked): validate(dict(step, urn='database'))

    def test_exclusion_must_match_exact_policy(self):
        _, _, exclusion = policy.identities('prod')
        inputs = {'project': 'mcp-registry-prod', 'name': policy.POLICY['exclusion_name'], 'description': policy.POLICY['exclusion_description'], 'filter': policy.POLICY['exclusion_filter'], 'disabled': False}
        step = {'urn': exclusion, 'op': 'create', 'newState': {'inputs': inputs}}
        self.assertTrue(policy.validate({'steps': [step], 'changeSummary': {'create': 1}}, 'prod', True, True))
        inputs['filter'] = 'severity < ERROR'
        with self.assertRaises(Blocked): policy.validate({'steps': [step], 'changeSummary': {'create': 1}}, 'prod', True, True)


if __name__ == '__main__': unittest.main()
