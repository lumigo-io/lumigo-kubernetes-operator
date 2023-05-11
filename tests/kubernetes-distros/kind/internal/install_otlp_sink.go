package internal

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func OtlpSinkFeature(namespaceName string, deploymentName string, otlpCollectorImage string, logger logr.Logger) (features.Feature, string) {
	otlpSinkPort := int32(4318)
	otlpSinkServiceName := "otlp-sink"

	return features.New("OtlpSink").Setup(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
		// We create a local tmp folder to be mounted into Kind on the virtual node
		// (https://kind.sigs.k8s.io/docs/user/configuration/#extra-mounts), which will
		// be then exposed to the deployment of an OpenTelemetry collector that will take
		// whatever comes through OTLP and dump it to file with a FileExporter, so that
		// we can then validate which data would be sent upstream from the comfiness of
		// Go test code :-)
		runId := ctx.Value(ContextKeyRunId).(string)

		labels := map[string]string{
			"app": "otlp-sink",
		}
		replicas := int32(1)

		otlpSinkDataDir := "/var/otlp-sink"

		otlpSinkDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: namespaceName},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "otelcol",
								Image: otlpCollectorImage,
								Env: []corev1.EnvVar{
									{
										Name:  "OTLP_PORT",
										Value: strconv.Itoa(int(otlpSinkPort)),
									},
									{
										Name:  "LOGS_DUMP_PATH",
										Value: fmt.Sprintf("%s/logs-%s.json", otlpSinkDataDir, runId),
									},
									{
										Name:  "TRACES_DUMP_PATH",
										Value: fmt.Sprintf("%s/traces-%s.json", otlpSinkDataDir, runId),
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/etc/otelcol",
										ReadOnly:  true,
									},
									{
										Name:      "data",
										MountPath: otlpSinkDataDir,
										ReadOnly:  false,
									},
								},
								Ports: []corev1.ContainerPort{
									{
										Protocol:      corev1.ProtocolTCP,
										HostPort:      otlpSinkPort,
										ContainerPort: otlpSinkPort,
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/lumigo/otlp-sink/config",
									},
								},
							},
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									HostPath: &corev1.HostPathVolumeSource{
										Path: "/lumigo/otlp-sink/data",
									},
								},
							},
						},
					},
				},
			},
		}

		otlpSinkService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: otlpSinkServiceName, Namespace: namespaceName},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{
					{
						Protocol:   corev1.ProtocolTCP,
						Port:       otlpSinkPort,
						TargetPort: intstr.FromInt(int(otlpSinkPort)),
					},
				},
			},
		}

		client, err := config.NewClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := client.Resources().Create(ctx, otlpSinkDeployment); err != nil {
			t.Fatal(err)
		}

		err = wait.For(conditions.New(client.Resources()).DeploymentConditionMatch(otlpSinkDeployment, appsv1.DeploymentAvailable, corev1.ConditionTrue), wait.WithTimeout(time.Minute*5))
		if err != nil {
			t.Fatal(err)
		}

		if err := client.Resources().Create(ctx, otlpSinkService); err != nil {
			t.Fatal(err)
		}

		return ctx
	}).Feature(), fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", otlpSinkServiceName, namespaceName, otlpSinkPort)
}
