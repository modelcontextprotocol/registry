import unittest

from adopt_staging_capacity import PREFIX, RESOURCES, pending_imports


class AdoptionTests(unittest.TestCase):
    def state(self, adopted=()):
        resources = [{"urn": PREFIX + "pulumi:pulumi:Stack::mcp-registry-infra-gcpStaging",
                      "type": "pulumi:pulumi:Stack"}]
        for r in RESOURCES:
            resources.append({"urn": PREFIX + r["provider"], "type": "provider"})
        for r in adopted:
            resources.append({"urn": PREFIX + r["type"] + "::" + r["name"],
                              "type": r["type"], "id": r["id"],
                              "provider": PREFIX + r["provider"] + "::provider-id"})
        return {"deployment": {"resources": resources}}

    def test_partial_retry_only_imports_missing_resource(self):
        self.assertEqual(pending_imports(self.state(RESOURCES[:1])), [RESOURCES[1]])
        self.assertEqual(pending_imports(self.state(RESOURCES)), [])

    def test_rejects_wrong_stack(self):
        with self.assertRaises(ValueError):
            pending_imports({"deployment": {"resources": []}})

    def test_rejects_existing_different_identity(self):
        state = self.state(RESOURCES)
        state["deployment"]["resources"][-1]["id"] = "default/another-hpa"
        with self.assertRaises(ValueError):
            pending_imports(state)

    def test_rejects_duplicate_ownership(self):
        state = self.state(RESOURCES)
        state["deployment"]["resources"][-1]["urn"] += "-other-name"
        with self.assertRaises(ValueError):
            pending_imports(state)

    def test_rejects_different_provider(self):
        state = self.state(RESOURCES)
        state["deployment"]["resources"][-1]["provider"] = "another-cluster-provider"
        with self.assertRaises(ValueError):
            pending_imports(state)


if __name__ == "__main__":
    unittest.main()
