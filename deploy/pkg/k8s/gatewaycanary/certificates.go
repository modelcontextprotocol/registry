package gatewaycanary

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func ManagedMTLS(ctx *pulumi.Context) bool {
	return config.New(ctx, "mcp-registry").GetBool("gatewayManagedMTLS")
}

func internalCertificates() []map[string]any {
	object := func(kind, name string, spec map[string]any) map[string]any {
		return map[string]any{"apiVersion": "cert-manager.io/v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": Namespace, "annotations": map[string]any{"pulumi.com/waitFor": "condition=Ready"}}, "spec": spec}
	}
	issuer := func(name string) map[string]any {
		return map[string]any{"name": name, "kind": "Issuer", "group": "cert-manager.io"}
	}
	leaf := func(name, dns, usage string) map[string]any {
		return object("Certificate", name, map[string]any{"secretName": name, "dnsNames": []any{dns}, "duration": "720h", "renewBefore": "240h", "privateKey": map[string]any{"algorithm": "RSA", "size": 2048, "rotationPolicy": "Always"}, "usages": []any{"digital signature", "key encipherment", usage}, "issuerRef": issuer("gateway-internal-ca")})
	}
	return []map[string]any{
		object("Issuer", "gateway-bootstrap", map[string]any{"selfSigned": map[string]any{}}),
		object("Certificate", "gateway-internal-ca", map[string]any{"secretName": "gateway-internal-ca", "isCA": true, "commonName": "registry-staging-gateway", "duration": "87600h", "renewBefore": "8760h", "privateKey": map[string]any{"algorithm": "RSA", "size": 2048, "rotationPolicy": "Never"}, "issuerRef": issuer("gateway-bootstrap")}),
		object("Issuer", "gateway-internal-ca", map[string]any{"ca": map[string]any{"secretName": "gateway-internal-ca"}}),
		leaf("gateway-server-tls", "registry-gateway-canary."+Namespace+".svc", "server auth"),
		leaf("gateway-agent-tls", "*.cluster.local", "client auth"),
	}
}

func deployInternalCertificates(ctx *pulumi.Context, provider *kubernetes.Provider, deps []pulumi.Resource) ([]pulumi.Resource, error) {
	for _, o := range internalCertificates() {
		r, err := register(ctx, provider, o, deps)
		if err != nil {
			return nil, err
		}
		deps = append(deps, r)
	}
	return deps, nil
}

func managedControllerTLS(o map[string]any) {
	spec := o["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	for _, c := range spec["containers"].([]any) {
		args := c.(map[string]any)["args"].([]any)
		for i, arg := range args {
			if arg == "--agent-tls-secret=agent-tls" {
				args[i] = "--agent-tls-secret=gateway-agent-tls"
			}
		}
	}
	for _, v := range spec["volumes"].([]any) {
		volume := v.(map[string]any)
		if volume["name"] == "nginx-agent-tls" {
			volume["secret"].(map[string]any)["secretName"] = "gateway-server-tls"
		}
	}
}

func PublicCertificates(ctx *pulumi.Context) string {
	return config.New(ctx, "mcp-registry").Get("gatewayPublicCertificates")
}

// Import existing shim-created Certificates first; detach their Ingress ownership
// in a later full-stack update before the Ingress objects can be removed.
func DeployPublicCertificates(ctx *pulumi.Context, provider *kubernetes.Provider) error {
	mode := PublicCertificates(ctx)
	if mode == "" {
		return nil
	}
	for _, item := range []struct{ namespace, name, host string }{
		{"default", "mcp-registry-staging-tls", "staging.registry.modelcontextprotocol.io"},
		{"monitoring", "grafana-tls", "grafana.staging.registry.modelcontextprotocol.io"},
	} {
		meta := map[string]any{"name": item.name, "namespace": item.namespace}
		opts := []pulumi.ResourceOption{pulumi.Protect(true)}
		if mode == "adopt" {
			opts = append(opts, pulumi.Import(pulumi.ID(item.namespace+"/"+item.name)), pulumi.IgnoreChanges([]string{"metadata.ownerReferences"}))
		} else {
			meta["ownerReferences"] = []any{}
		}
		o := map[string]any{"apiVersion": "cert-manager.io/v1", "kind": "Certificate", "metadata": meta, "spec": map[string]any{"secretName": item.name, "dnsNames": []any{item.host}, "issuerRef": map[string]any{"name": "letsencrypt-prod", "kind": "ClusterIssuer", "group": "cert-manager.io"}, "usages": []any{"digital signature", "key encipherment"}}}
		if _, err := register(ctx, provider, o, nil, opts...); err != nil {
			return err
		}
	}
	return nil
}
