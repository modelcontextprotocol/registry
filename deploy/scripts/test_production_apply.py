import contextlib
import copy
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest

from production_apply import ANTI_AFFINITY, APP, COMPLETED_HOOKS, HPA, HPA_SPEC, POOL_MAX, POOL_MIN, WORKERS, Blocked, quiet, validate_preview, parse_preview_events, apply_failure_category, apply_error_messages, dependency_signature


def app_inputs(prepared):
    app = {"name": "mcp-registry", "image": "fixture:1.8.7", "resources": {"requests": {"cpu": "800m", "memory": "512Mi"}, "limits": {"memory": "1Gi"}}, "env": [{"name": "UNCHANGED", "value": "private-sentinel"}]}
    spec = {"replicas": 2, "template": {"spec": {"containers": [app]}}}
    if prepared:
        spec["minReadySeconds"] = 35
        spec["template"]["spec"].update({"terminationGracePeriodSeconds": 30, "affinity": copy.deepcopy(ANTI_AFFINITY)})
        app["resources"]["requests"]["cpu"] = "1200m"
        app["env"].extend([{"name": POOL_MAX, "value": "30"}, {"name": POOL_MIN, "value": "5"}])
    return {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": "mcp-registry", "namespace": "default"}, "spec": spec}


def step(urn, before, after, op="update"):
    return {"urn": urn, "op": op, "oldState": {"inputs": before} if before else None, "newState": {"inputs": after} if after else None}


def preview(steps):
    return {"steps": steps, "changeSummary": {"update": len(steps)}}


def hpa_inputs():
    return {"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscaler", "metadata": {"name": "mcp-registry", "namespace": "default"}, "spec": copy.deepcopy(HPA_SPEC)}


class PrivateProductionPreview(unittest.TestCase):
    def test_prepare_exact_app_and_worker_changes(self):
        changes = [step(APP, app_inputs(False), app_inputs(True)), step(WORKERS, {"nodeCount": 2, "autoscaling": {"minNodeCount": 2, "maxNodeCount": 3}}, {"nodeCount": 2, "autoscaling": {"minNodeCount": 2, "maxNodeCount": 4}})]
        self.assertEqual(validate_preview(preview(changes), "prepare"), ["application scaling settings", "worker autoscaling ceiling"])

    def test_activation_only_bounded_hpa(self):
        self.assertEqual(validate_preview(preview([step(HPA, None, hpa_inputs(), "create")]), "enabled"), ["bounded application autoscaler"])
        invalid = hpa_inputs()
        invalid["spec"]["maxReplicas"] = 5
        with self.assertRaises(Blocked):
            validate_preview(preview([step(HPA, None, invalid, "create")]), "enabled")

    def test_unrelated_app_changes_fail_closed(self):
        for field in ("image", "secret", "memory", "affinity"):
            after = app_inputs(True)
            container = after["spec"]["template"]["spec"]["containers"][0]
            if field == "image":
                container["image"] = "fixture:unreviewed"
            elif field == "secret":
                container["env"][0]["value"] = "changed-private-sentinel"
            elif field == "memory":
                container["resources"]["limits"]["memory"] = "2Gi"
            else:
                after["spec"]["template"]["spec"]["affinity"] = {}
            with self.subTest(field=field), self.assertRaises(Blocked):
                validate_preview(preview([step(APP, app_inputs(False), after)]), "prepare")

    def test_node_count_or_minimum_cannot_change(self):
        before = {"nodeCount": 3, "autoscaling": {"minNodeCount": 2, "maxNodeCount": 3}}
        for field in ("nodeCount", "minNodeCount"):
            after = copy.deepcopy(before)
            after["autoscaling"]["maxNodeCount"] = 4
            if field == "nodeCount":
                after[field] = 2
            else:
                after["autoscaling"][field] = 3
            with self.subTest(field=field), self.assertRaises(Blocked):
                validate_preview(preview([step(WORKERS, before, after)]), "prepare")

    def test_deletions_replacements_and_other_resources_block(self):
        for change in (step(APP, app_inputs(False), None, "delete"), step(APP, app_inputs(False), app_inputs(True), "replace"), step("private-database-urn", {}, {}, "update")):
            with self.subTest(op=change["op"]), self.assertRaises(Blocked):
                validate_preview(preview([change]), "prepare")

    def test_hpa_removal_and_legacy_rollback(self):
        validate_preview(preview([step(HPA, hpa_inputs(), None, "delete")]), "prepare")
        validate_preview(preview([step(APP, app_inputs(True), app_inputs(False))]), "off")

    def test_empty_or_failed_preview_is_not_a_pass(self):
        for value in ({}, {"steps": [], "changeSummary": {}}, {"steps": [{"op": "same", "urn": APP}], "changeSummary": {"same": 1}, "maybeCorrupt": True}):
            with self.assertRaises(Blocked):
                validate_preview(value, "prepare")

    def test_refresh_status_updates_are_not_planned_changes(self):
        events = [
            {"resourcePreEvent": {"metadata": {"op": "refresh", "urn": "private-other-resource"}}},
            {"resOutputsEvent": {"metadata": {"op": "update", "urn": "private-other-resource"}}},
            {"resourcePreEvent": {"metadata": {"op": "create", "urn": HPA, "new": {"inputs": hpa_inputs()}}}},
            {"summaryEvent": {"resourceChanges": {"create": 1}, "maybeCorrupt": False}},
        ]
        parsed = parse_preview_events("\n".join(json.dumps(e) for e in events))
        self.assertEqual(validate_preview(parsed, "enabled"), ["bounded application autoscaler"])
        events.insert(2, {"resourcePreEvent": {"metadata": {"op": "update", "urn": "private-other-resource", "old": {}, "new": {}}}})
        with self.assertRaises(Blocked):
            validate_preview(parse_preview_events("\n".join(json.dumps(e) for e in events)), "enabled")

    def test_truncated_stream_is_not_a_pass(self):
        with self.assertRaises(Blocked):
            parse_preview_events(json.dumps({"resourcePreEvent": {"metadata": {"op": "same", "urn": APP}}}))

    def test_only_unchanged_completed_ttl_hooks_can_be_excluded(self):
        urn = sorted(COMPLETED_HOOKS)[0]
        inputs = {"metadata": {"name": "fixture"}, "spec": {"ttlSecondsAfterFinished": 0}}
        previous = {urn: {"inputs": copy.deepcopy(inputs), "outputs": {"status": {"succeeded": 1, "completionTime": "fixture"}}}}
        change = step(urn, None, copy.deepcopy(inputs), "create")
        validate_preview(preview([change]), "prepare", completed_hooks=previous)
        # The actual targeted plan must still reject the setup job.
        with self.assertRaises(Blocked):
            validate_preview(preview([change]), "prepare")
        change["newState"]["inputs"]["spec"]["template"] = {"changed": True}
        with self.assertRaises(Blocked):
            validate_preview(preview([change]), "prepare", completed_hooks=previous)
        change["newState"]["inputs"] = inputs
        previous[urn]["outputs"]["status"]["succeeded"] = 0
        with self.assertRaises(Blocked):
            validate_preview(preview([change]), "prepare", completed_hooks=previous)

    def test_unchanged_rbac_is_excluded_only_from_full_review(self):
        urn = "fixture-role"
        inputs = {"metadata": {"name": "fixture"}, "rules": []}
        previous = {urn: {"type": "kubernetes:rbac.authorization.k8s.io/v1:Role", "inputs": copy.deepcopy(inputs)}}
        change = step(urn, copy.deepcopy(inputs), copy.deepcopy(inputs))
        validate_preview(preview([change]), "prepare", completed_hooks=previous)
        with self.assertRaises(Blocked):
            validate_preview(preview([change]), "prepare")
        change["newState"]["inputs"]["rules"] = [{"verbs": ["*"]}]
        with self.assertRaises(Blocked):
            validate_preview(preview([change]), "prepare", completed_hooks=previous)

    def test_dependency_comparison_ignores_order(self):
        self.assertEqual(dependency_signature({"dependencies": ["a", "b"]}), dependency_signature({"dependencies": ["b", "a"]}))
        self.assertNotEqual(dependency_signature({"dependencies": ["a"]}), dependency_signature({"dependencies": ["b"]}))

    def test_only_actual_error_diagnostics_are_classified(self):
        events = [
            {"resourcePreEvent": {"metadata": {"inputs": {"private": "forbidden conflict timeout invalid"}}}},
            {"diagnosticEvent": {"severity": "info", "message": "dependency timeout"}},
            {"diagnosticEvent": {"severity": "error", "message": "private resource violates plan: secret-value"}},
        ]
        text = "\n".join(json.dumps(e) for e in events)
        self.assertEqual(apply_failure_category(apply_error_messages(text, "")), "plan-constraint")
        self.assertEqual(apply_error_messages(json.dumps(events[0]), ""), "")

    def test_apply_failure_categories_never_echo_details(self):
        self.assertEqual(apply_failure_category("private resource violates plan: secret-value"), "plan-constraint")
        self.assertEqual(apply_failure_category("unknown private diagnostic secret-value"), "unclassified")
        self.assertEqual(apply_failure_category("field manager conflict with secret-value"), "field-ownership")

    def test_child_errors_do_not_echo_private_output(self):
        captured = io.StringIO()
        with tempfile.TemporaryDirectory() as directory, contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
            with self.assertRaisesRegex(Blocked, "fixture-failed"):
                quiet([sys.executable, "-c", "import sys; print('private-sentinel'); print('private-sentinel',file=sys.stderr); sys.exit(1)"], Path(directory), "fixture", os.environ)
        self.assertEqual(captured.getvalue(), "")
        self.assertFalse(Path(directory).exists())


if __name__ == "__main__":
    unittest.main()
