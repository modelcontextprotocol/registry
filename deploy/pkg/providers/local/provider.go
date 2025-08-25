package local

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	appsv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	batchv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/batch/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// Provider implements the ClusterProvider interface for local Kubernetes clusters
type Provider struct{}

// getClusterName attempts to get the current Kubernetes cluster name from kubectl config
func getClusterName() string {
	cmd := exec.Command("kubectl", "config", "current-context")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to default if we can't get the context
		return "local"
	}
	return strings.TrimSpace(string(output))
}

// CreateCluster configures access to a local Kubernetes cluster via kubeconfig
func (p *Provider) CreateCluster(ctx *pulumi.Context, environment string) (*providers.ProviderInfo, error) {
	// Create Kubernetes provider for local cluster
	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{})
	if err != nil {
		return nil, err
	}

	// Get the actual cluster name from kubectl config
	clusterName := getClusterName()

	return &providers.ProviderInfo{
		Name:     pulumi.String(clusterName).ToStringOutput(),
		Provider: k8sProvider,
	}, nil
}

// CreateBackupStorage creates MinIO for S3-compatible backup storage
func (p *Provider) CreateBackupStorage(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) (*providers.BackupStorageInfo, error) {
	// Create MinIO namespace
	minioNamespace, err := corev1.NewNamespace(ctx, "minio", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("minio"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO namespace: %w", err)
	}

	// Create MinIO credentials secret
	minioSecret, err := corev1.NewSecret(ctx, "minio-credentials", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("minio-credentials"),
			Namespace: minioNamespace.Metadata.Name(),
		},
		Type: pulumi.String("Opaque"),
		StringData: pulumi.StringMap{
			"accesskey": pulumi.String("minioadmin"),
			"secretkey": pulumi.String("minioadmin"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO credentials: %w", err)
	}

	// Create MinIO PVC for data storage
	_, err = corev1.NewPersistentVolumeClaim(ctx, "minio-pvc", &corev1.PersistentVolumeClaimArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("minio-pvc"),
			Namespace: minioNamespace.Metadata.Name(),
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
		return nil, fmt.Errorf("failed to create MinIO PVC: %w", err)
	}

	// Deploy MinIO
	minioDeployment, err := appsv1.NewDeployment(ctx, "minio", &appsv1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("minio"),
			Namespace: minioNamespace.Metadata.Name(),
		},
		Spec: &appsv1.DeploymentSpecArgs{
			Replicas: pulumi.Int(1),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String("minio"),
				},
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("minio"),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("minio"),
							Image: pulumi.String("minio/minio:RELEASE.2025-07-23T15-54-02Z"),
							Args: pulumi.StringArray{
								pulumi.String("server"),
								pulumi.String("/data"),
								pulumi.String("--console-address"),
								pulumi.String(":9001"),
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name: pulumi.String("MINIO_ROOT_USER"),
									ValueFrom: &corev1.EnvVarSourceArgs{
										SecretKeyRef: &corev1.SecretKeySelectorArgs{
											Name: minioSecret.Metadata.Name(),
											Key:  pulumi.String("accesskey"),
										},
									},
								},
								&corev1.EnvVarArgs{
									Name: pulumi.String("MINIO_ROOT_PASSWORD"),
									ValueFrom: &corev1.EnvVarSourceArgs{
										SecretKeyRef: &corev1.SecretKeySelectorArgs{
											Name: minioSecret.Metadata.Name(),
											Key:  pulumi.String("secretkey"),
										},
									},
								},
							},
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(9000),
									Name:          pulumi.String("api"),
								},
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(9001),
									Name:          pulumi.String("console"),
								},
							},
							VolumeMounts: corev1.VolumeMountArray{
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("data"),
									MountPath: pulumi.String("/data"),
								},
							},
						},
					},
					Volumes: corev1.VolumeArray{
						&corev1.VolumeArgs{
							Name: pulumi.String("data"),
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSourceArgs{
								ClaimName: pulumi.String("minio-pvc"),
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO deployment: %w", err)
	}

	// Create MinIO service
	minioService, err := corev1.NewService(ctx, "minio-service", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("minio"),
			Namespace: minioNamespace.Metadata.Name(),
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String("minio"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Name:       pulumi.String("api"),
					Port:       pulumi.Int(9000),
					TargetPort: pulumi.Int(9000),
				},
				&corev1.ServicePortArgs{
					Name:       pulumi.String("console"),
					Port:       pulumi.Int(9001),
					TargetPort: pulumi.Int(9001),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO service: %w", err)
	}

	// Create a Job to initialize the MinIO bucket
	_, err = batchv1.NewJob(ctx, "minio-bucket-init", &batchv1.JobArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("minio-bucket-init"),
			Namespace: minioNamespace.Metadata.Name(),
		},
		Spec: &batchv1.JobSpecArgs{
			Template: &corev1.PodTemplateSpecArgs{
				Spec: &corev1.PodSpecArgs{
					RestartPolicy: pulumi.String("OnFailure"),
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("mc"),
							Image: pulumi.String("minio/mc:RELEASE.2025-07-19T03-52-40Z"),
							Command: pulumi.StringArray{
								pulumi.String("/bin/sh"),
								pulumi.String("-c"),
							},
							Args: pulumi.StringArray{
								pulumi.String(`
									until mc alias set myminio http://minio:9000 minioadmin minioadmin; do
										echo 'Waiting for MinIO to be ready...'
										sleep 5
									done
									mc mb -p myminio/k8up-backups
									echo 'Bucket created successfully'
								`),
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{minioService}))
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket initialization job: %w", err)
	}

	// Create backup credentials secret for S3 access
	backupSecret, err := corev1.NewSecret(ctx, "k8up-backup-credentials", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("k8up-backup-credentials"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"k8up.io/backup": pulumi.String("true"),
				"environment":    pulumi.String(environment),
			},
		},
		Type: pulumi.String("Opaque"),
		StringData: pulumi.StringMap{
			"AWS_ACCESS_KEY_ID":     pulumi.String("minioadmin"),
			"AWS_SECRET_ACCESS_KEY": pulumi.String("minioadmin"),
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{minioDeployment}))
	if err != nil {
		return nil, fmt.Errorf("failed to create backup credentials secret: %w", err)
	}

	return &providers.BackupStorageInfo{
		Endpoint:    "http://minio.minio:9000",
		BucketName:  "k8up-backups",
		Credentials: backupSecret,
	}, nil
}
