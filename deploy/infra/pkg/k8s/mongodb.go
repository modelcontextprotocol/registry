package k8s

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployMongoDB deploys MongoDB to the Kubernetes cluster
func DeployMongoDB(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) error {
	// Create PersistentVolumeClaim for MongoDB data
	_, err := corev1.NewPersistentVolumeClaim(ctx, "mongodb-pvc", &corev1.PersistentVolumeClaimArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mongodb-pvc"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mongodb"),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &corev1.PersistentVolumeClaimSpecArgs{
			AccessModes: pulumi.StringArray{
				pulumi.String("ReadWriteOnce"),
			},
			Resources: &corev1.VolumeResourceRequirementsArgs{
				Requests: pulumi.StringMap{
					"storage": pulumi.String("10Gi"),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create MongoDB Deployment
	_, err = v1.NewDeployment(ctx, "mongodb", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mongodb"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mongodb"),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &v1.DeploymentSpecArgs{
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String("mongodb"),
				},
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("mongodb"),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("mongodb"),
							Image: pulumi.String("mongo:7.0"),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(27017),
									Name:          pulumi.String("mongodb"),
								},
							},
							VolumeMounts: corev1.VolumeMountArray{
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("mongodb-data"),
									MountPath: pulumi.String("/data/db"),
								},
							},
							LivenessProbe: &corev1.ProbeArgs{
								TcpSocket: &corev1.TCPSocketActionArgs{
									Port: pulumi.Int(27017),
								},
								InitialDelaySeconds: pulumi.Int(30),
								TimeoutSeconds:      pulumi.Int(5),
							},
							ReadinessProbe: &corev1.ProbeArgs{
								TcpSocket: &corev1.TCPSocketActionArgs{
									Port: pulumi.Int(27017),
								},
								InitialDelaySeconds: pulumi.Int(5),
								TimeoutSeconds:      pulumi.Int(1),
							},
						},
					},
					Volumes: corev1.VolumeArray{
						&corev1.VolumeArgs{
							Name: pulumi.String("mongodb-data"),
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSourceArgs{
								ClaimName: pulumi.String("mongodb-pvc"),
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create MongoDB Service
	_, err = corev1.NewService(ctx, "mongodb", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mongodb"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("mongodb"),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String("mongodb"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Port:       pulumi.Int(27017),
					TargetPort: pulumi.Int(27017),
					Name:       pulumi.String("mongodb"),
				},
			},
			Type: pulumi.String("ClusterIP"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return fmt.Errorf("failed to create MongoDB service: %w", err)
	}

	return nil
}