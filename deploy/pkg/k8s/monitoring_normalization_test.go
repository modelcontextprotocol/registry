package k8s

import (
	"reflect"
	"testing"
)

func TestGrafanaNormalizationPreservesPermissionsAndOtherResources(t *testing.T) {
	permission := []interface{}{map[string]interface{}{"verbs": []interface{}{"get"}, "resources": []interface{}{"pods"}}}
	for _, test := range []struct {
		kind, name, namespace string
		rules                 interface{}
		omit                  bool
	}{
		{"ClusterRole", "grafana-clusterrole", "", []interface{}{}, true},
		{"Role", "grafana", "monitoring", []interface{}{}, true},
		{"Role", "grafana", "monitoring", permission, false},
		{"ClusterRole", "grafana-clusterrole", "", permission, false},
		{"Role", "grafana", "default", []interface{}{}, false},
		{"ClusterRole", "other", "", []interface{}{}, false},
		{"Role", "grafana", "monitoring", nil, false},
	} {
		object := map[string]interface{}{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": test.kind,
			"metadata": map[string]interface{}{"name": test.name, "namespace": test.namespace}, "rules": test.rules}
		omitEmptyGrafanaRules(object)
		rules, present := object["rules"]
		if present == test.omit || (!test.omit && !reflect.DeepEqual(rules, test.rules)) {
			t.Fatalf("unexpected permissions change for %s/%s: %v", test.namespace, test.name, object)
		}
	}
}
