package k8s

import (
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/usagecollector"
)

type usagePodIdentity struct {
	account  pulumi.StringPtrInput
	selector pulumi.StringMap
	env      corev1.EnvVarArray
	mounts   corev1.VolumeMountArray
	volumes  corev1.VolumeArray
}

func usageIdentityForPod(ctx *pulumi.Context, cluster *providers.ProviderInfo, namespace pulumi.StringPtrInput, name string, legacy *corev1.Secret) (*usagePodIdentity, error) {
	identity := &usagePodIdentity{selector: workerNodeSelector(ctx)}
	phase := usagecollector.Identity(ctx)
	if phase != usagecollector.Legacy {
		account, err := corev1.NewServiceAccount(ctx, name, &corev1.ServiceAccountArgs{
			Metadata:                     &metav1.ObjectMetaArgs{Name: pulumi.String(name), Namespace: namespace},
			AutomountServiceAccountToken: pulumi.Bool(false),
		}, pulumi.Provider(cluster.Provider), pulumi.DependsOn(cluster.UsageAccess.Grants))
		if err != nil {
			return nil, err
		}
		if phase.UsesMetadata() {
			identity.account = account.Metadata.Name()
			if identity.selector == nil {
				identity.selector = pulumi.StringMap{}
			}
			identity.selector["iam.gke.io/gke-metadata-server-enabled"] = pulumi.String("true")
			return identity, nil
		}
	}

	identity.env = corev1.EnvVarArray{&corev1.EnvVarArgs{
		Name: pulumi.String("GOOGLE_APPLICATION_CREDENTIALS"), Value: pulumi.String(usageGoogleKeyPath + "/key.json"),
	}}
	identity.mounts = corev1.VolumeMountArray{&corev1.VolumeMountArgs{
		Name: pulumi.String("google"), MountPath: pulumi.String(usageGoogleKeyPath), ReadOnly: pulumi.Bool(true),
	}}
	identity.volumes = corev1.VolumeArray{&corev1.VolumeArgs{Name: pulumi.String("google"), Secret: &corev1.SecretVolumeSourceArgs{
		SecretName: legacy.Metadata.Name(), DefaultMode: pulumi.Int(0o440),
	}}}
	return identity, nil
}
