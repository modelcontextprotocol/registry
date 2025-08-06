package main

import (
	"fmt"

	appsv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// deployMongoDB deploys MongoDB to the Kubernetes cluster
func deployMongoDB(ctx *pulumi.Context, cluster *ClusterInfo, environment string) error {
	appName := "mongodb"
	namespace := environment

	// Create PersistentVolumeClaim for MongoDB data
	_, err := corev1.NewPersistentVolumeClaim(ctx, fmt.Sprintf("%s-pvc", appName), &corev1.PersistentVolumeClaimArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(fmt.Sprintf("%s-data", appName)),
			Namespace: pulumi.String(namespace),
			Labels: pulumi.StringMap{
				"app":         pulumi.String(appName),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &corev1.PersistentVolumeClaimSpecArgs{
			AccessModes: pulumi.StringArray{
				pulumi.String("ReadWriteOnce"),
			},
			Resources: &corev1.ResourceRequirementsArgs{
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
	_, err = appsv1.NewDeployment(ctx, fmt.Sprintf("%s-deployment", appName), &appsv1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(appName),
			Namespace: pulumi.String(namespace),
			Labels: pulumi.StringMap{
				"app":         pulumi.String(appName),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &appsv1.DeploymentSpecArgs{
			Replicas: pulumi.Int(1),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String(appName),
				},
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app":         pulumi.String(appName),
						"environment": pulumi.String(environment),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String(appName),
							Image: pulumi.String("mongo:latest"),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(27017),
									Name:          pulumi.String("mongodb"),
								},
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name:  pulumi.String("PUID"),
									Value: pulumi.String("1000"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("PGID"),
									Value: pulumi.String("1000"),
								},
							},
							VolumeMounts: corev1.VolumeMountArray{
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("mongodb-data"),
									MountPath: pulumi.String("/data/db"),
								},
							},
							Resources: &corev1.ResourceRequirementsArgs{
								Requests: pulumi.StringMap{
									"cpu":    pulumi.String("500m"),
									"memory": pulumi.String("512Mi"),
								},
								Limits: pulumi.StringMap{
									"cpu":    pulumi.String("1000m"),
									"memory": pulumi.String("1Gi"),
								},
							},
						},
					},
					Volumes: corev1.VolumeArray{
						&corev1.VolumeArgs{
							Name: pulumi.String("mongodb-data"),
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSourceArgs{
								ClaimName: pulumi.String(fmt.Sprintf("%s-data", appName)),
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
	_, err = corev1.NewService(ctx, fmt.Sprintf("%s-service", appName), &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(appName),
			Namespace: pulumi.String(namespace),
			Labels: pulumi.StringMap{
				"app":         pulumi.String(appName),
				"environment": pulumi.String(environment),
			},
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String(appName),
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
		return err
	}

	return nil
}