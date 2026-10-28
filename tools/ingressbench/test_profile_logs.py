import json
import unittest
from profile_logs import Profile, request_shape, parse_timestamp
from collect_profile_logs import collect


def record(uri='/v0/servers?limit=100&cursor=secret', request_bytes=200, protocol='HTTP/2.0'):
    return f'192.0.2.1 - - [28/Oct/2026:18:30:00 +0000] "GET {uri} {protocol}" 200 2048 "private-referrer" "private-agent" {request_bytes} 0.100 [upstream] [] - - - - private-request-id cache_status="HIT"'


class ProfileTests(unittest.TestCase):
    def test_cloud_nanosecond_timestamp(self):
        self.assertEqual(parse_timestamp('2026-10-28T18:30:00.123456789Z').microsecond, 123456)
        with self.assertRaises(ValueError):
            parse_timestamp('2026-10-28T18:30:00')

    def test_background_is_not_client_demand_and_no_identifiers_escape(self):
        p = Profile()
        p.add(record(), 100, 'ingress-a')
        p.add(record(request_bytes=0), 100, 'ingress-a')
        p.add('unrecognized', 100, 'ingress-a')
        d = p.result()
        self.assertEqual(d['populations']['client']['count'], 1)
        self.assertEqual(d['populations']['zero_request_length']['count'], 1)
        self.assertEqual(d['client_completions_per_second'], {100: 1})
        self.assertEqual(d['unparsed'], 1)
        for secret in ['192.0.2.1', 'secret', 'private-agent', 'private-referrer', 'private-request-id']:
            self.assertNotIn(secret, json.dumps(d))

    def test_keys_preserve_reuse_without_exposing_values(self):
        p = Profile()
        for uri in ['/v0/servers?cursor=a', '/v0/servers?cursor=a', '/v0/servers?cursor=b']:
            p.add(record(uri), 100, 'ingress-a')
        self.assertEqual(p.result()['page_keys']['distinct'], 2)
        self.assertEqual(p.result()['page_keys']['singleton_keys'], 1)

    def test_bounded_shapes_and_search_precedence(self):
        self.assertEqual(request_shape('/v0.1/servers?search=private&updated_since=private&limit=secret')[:3],
                         ('search', 'v0.1', 'invalid'))
        p = Profile()
        p.add(record('/v0.1/servers?search=private'), 100, 'ingress-a')
        self.assertEqual(p.result()['search_length_counts'], {'7': 1})
        self.assertNotIn('private', json.dumps(p.result()))

    def test_pagination_and_caps_are_not_reported_as_complete_traffic(self):
        row = {'textPayload': record(), 'timestamp': '2026-10-28T18:30:00.123456789Z'}
        def fetch(body):
            if body.get('pageToken'):
                return {'entries': [row]}
            return {'entries': [row], 'nextPageToken': 'next'}
        complete = collect('2026-10-28T18:30:00Z', 60, 'private-token', fetch)
        self.assertTrue(complete['query_complete'])
        self.assertEqual(complete['entries'], 2)
        capped = collect('2026-10-28T18:30:00Z', 60, 'private-token', fetch, max_records=1)
        self.assertFalse(capped['query_complete'])
        self.assertIsNone(capped['populations']['client']['records_per_second'])
        self.assertNotIn('private-token', json.dumps(complete))


if __name__ == '__main__':
    unittest.main()
