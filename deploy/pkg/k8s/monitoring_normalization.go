package k8s

import "github.com/pulumi/pulumi/sdk/v3/go/pulumi"

// Kubernetes omits empty RBAC rules. Match that representation for Grafana's
// two empty roles without ignoring changes to any actual permission rules.
func omitEmptyGrafanaRules(object map[string]interface{}, _ ...pulumi.ResourceOption) {
	if object["apiVersion"] != "rbac.authorization.k8s.io/v1" {
		return
	}
	meta, _ := object["metadata"].(map[string]interface{})
	if !((object["kind"] == "ClusterRole" && meta["name"] == "grafana-clusterrole") ||
		(object["kind"] == "Role" && meta["name"] == "grafana" && meta["namespace"] == "monitoring")) {
		return
	}
	if rules, ok := object["rules"].([]interface{}); ok && len(rules) == 0 {
		delete(object, "rules")
	}
}
