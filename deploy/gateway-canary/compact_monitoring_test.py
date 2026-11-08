"""Collector discovery must cover every live collector without adding nodes."""
import unittest
from unittest.mock import patch
from monitoring_staging import collection_nodes


class CompactMonitoringTest(unittest.TestCase):
    def test_supported_topologies_and_missing_collector(self):
        for role, roles in [('worker', ['worker', 'database']), ('gateway-canary', ['worker', 'database', 'gateway-canary', 'gateway-canary'])]:
            nodes = [{'metadata': {'name': f'node-{i}', 'labels': {'registry-role': value}}} for i, value in enumerate(roles)]
            selected = [n['metadata']['name'] for n in nodes if n['metadata']['labels']['registry-role'] == role]
            pods = [{'metadata': {}, 'spec': {'nodeName': name}, 'status': {'conditions': [{'type': 'Ready', 'status': 'True'}]}} for name in selected]
            ds = {'spec': {'template': {'spec': {'nodeSelector': {'registry-role': role}}}}}
            with patch('monitoring_staging.get', side_effect=[ds, {'items': pods}]):
                self.assertEqual(collection_nodes(nodes), selected)
            with patch('monitoring_staging.get', side_effect=[ds, {'items': pods[:-1]}]):
                with self.assertRaises(AssertionError):
                    collection_nodes(nodes)

    def test_compact_rejects_added_worker(self):
        nodes = [{'metadata': {'name': f'node-{i}', 'labels': {'registry-role': role}}} for i, role in enumerate(['worker', 'worker', 'database'])]
        ds = {'spec': {'template': {'spec': {'nodeSelector': {'registry-role': 'worker'}}}}}
        with patch('monitoring_staging.get', return_value=ds):
            with self.assertRaises(AssertionError):
                collection_nodes(nodes)


if __name__ == '__main__':
    unittest.main()
