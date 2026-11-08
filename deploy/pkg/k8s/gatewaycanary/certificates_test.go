package gatewaycanary

import (
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"testing"
)

func TestDetachedCertificate(t *testing.T) {
	for _, test := range []struct {
		name string
		meta *metav1.ObjectMeta
		want bool
	}{
		{"missing metadata", nil, false},
		{"no owner", &metav1.ObjectMeta{}, true},
		{"shim owner", &metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{Kind: "Ingress", Name: "mcp-registry"}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := detachedCertificate(test.meta)
			if got != test.want || (err == nil) != test.want {
				t.Fatalf("got %v, %v; want %v", got, err, test.want)
			}
		})
	}
}
