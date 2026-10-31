import copy
import unittest

from check_production_scaling import POOL_MAX, POOL_MIN, check_transition


def resources(count=2, bounded=False, hpa=False):
    container = {"name": "mcp-registry", "image": "ghcr.io/modelcontextprotocol/registry:1.8.7"}
    if bounded:
        container["env"] = [{"name": POOL_MAX, "value": "30"}, {"name": POOL_MIN, "value": "5"}]
    spec = {"containers": [container]}
    deployment = {"metadata": {"generation": 2}, "spec": {"replicas": count, "template": {"spec": copy.deepcopy(spec)}}, "status": {
        "observedGeneration": 2, "replicas": count, "updatedReplicas": count, "readyReplicas": count, "availableReplicas": count,
    }}
    pods = [{"metadata": {}, "spec": copy.deepcopy(spec), "status": {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}} for _ in range(count)]
    hpas = [{"spec": {"scaleTargetRef": {"name": "mcp-registry"}}}] if hpa else []
    return deployment, pods, hpas


class ProductionScalingTransitions(unittest.TestCase):
    def test_prepare_existing_fixed_apps(self):
        check_transition("prepare", *resources())

    def test_enable_only_after_pool_rollout(self):
        with self.assertRaises(ValueError):
            check_transition("enabled", *resources())
        check_transition("enabled", *resources(bounded=True))
        check_transition("enabled", *resources(4, bounded=True, hpa=True))

    def test_old_live_pod_blocks_even_if_template_is_new(self):
        deployment, pods, hpas = resources(bounded=True)
        pods[0]["spec"]["containers"][0].pop("env")
        with self.assertRaises(ValueError):
            check_transition("enabled", deployment, pods, hpas)

    def test_old_binary_cannot_claim_small_pools(self):
        deployment, pods, hpas = resources(bounded=True)
        pods[0]["spec"]["containers"][0]["image"] = "ghcr.io/modelcontextprotocol/registry:1.8.6"
        with self.assertRaises(ValueError):
            check_transition("enabled", deployment, pods, hpas)

    def test_rollback_requires_two_settled_apps_and_no_hpa(self):
        check_transition("prepare", *resources(4, bounded=True, hpa=True))
        for count, hpa in ((4, True), (2, True), (4, False)):
            with self.subTest(count=count, hpa=hpa), self.assertRaises(ValueError):
                check_transition("off", *resources(count, bounded=True, hpa=hpa))
        check_transition("off", *resources(bounded=True))

    def test_incomplete_or_terminating_rollout_blocks(self):
        for change in ("old_generation", "not_available", "terminating", "extra_pod", "not_ready"):
            deployment, pods, hpas = resources(bounded=True)
            if change == "old_generation":
                deployment["status"]["observedGeneration"] = 1
            elif change == "not_available":
                deployment["status"]["availableReplicas"] = 1
            elif change == "terminating":
                pods[0]["metadata"]["deletionTimestamp"] = "2026-01-01T00:00:00Z"
            elif change == "extra_pod":
                pods.append(copy.deepcopy(pods[0]))
            else:
                pods[0]["status"]["conditions"] = []
            with self.subTest(change=change), self.assertRaises(ValueError):
                check_transition("enabled", deployment, pods, hpas)


if __name__ == "__main__":
    unittest.main()
