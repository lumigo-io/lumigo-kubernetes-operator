package internal

import (
	"context"
	"fmt"
	"os/user"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DEFAULT_LUMIGO_ENDPOINT = "https://ga-otlp.lumigo-tracer-edge.golumigo.com"

	otlpSinkPort        = int32(4318)
	otlpSinkServiceName = "otlp-sink"
)

func OtlpSinkEnvFunc(namespaceName string, deploymentName string, otlpCollectorImage string, logger logr.Logger) (func(ctx context.Context, cfg *envconf.Config) (context.Context, error), string) {
	return func(ctx context.Context, config *envconf.Config) (context.Context, error) {
		client, err := config.NewClient()
		if err != nil {
			return ctx, err
		}

		return doOtlpSink(ctx, client, namespaceName, deploymentName, otlpCollectorImage, logger)
	}, fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", otlpSinkServiceName, namespaceName, otlpSinkPort)
}

func OtlpSinkFeature(namespaceName string, deploymentName string, otlpCollectorImage string, logger logr.Logger) (features.Feature, string) {
	return features.New("OtlpSink").Setup(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
		client, err := config.NewClient()
		if err != nil {
			t.Fatal(err)
		}

		ctx, err = doOtlpSink(ctx, client, namespaceName, deploymentName, otlpCollectorImage, logger)
		if err != nil {
			t.Fatal(err)
		}

		return ctx
	}).Feature(), fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", otlpSinkServiceName, namespaceName, otlpSinkPort)

}

func doOtlpSink(ctx context.Context, client klient.Client, namespaceName string, deploymentName string, otlpCollectorImage string, logger logr.Logger) (context.Context, error) {
	// We create a local tmp folder to be mounted into Kind on the virtual node
	// (https://kind.sigs.k8s.io/docs/user/configuration/#extra-mounts), which will
	// be then exposed to the deployment of an OpenTelemetry collector that will take
	// whatever comes through OTLP and dump it to file with a FileExporter, so that
	// we can then validate which data would be sent upstream from the comfiness of
	// Go test code :-)

	labels := map[string]string{
		"app": "otlp-sink",
	}
	replicas := int32(1)

	otlpSinkDataDir := "/var/otlp-sink"

	currentUser, err := user.Current()
	if err != nil {
		return ctx, err
	}

	otlpUid, err := strconv.ParseInt(currentUser.Uid, 10, 64)
	if err != nil {
		return ctx, err
	}

	otlpGid, err := strconv.ParseInt(currentUser.Gid, 10, 64)
	if err != nil {
		return ctx, err
	}

	otlpSinkDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespaceName,
		},
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
							Env: []corev1.EnvVar{
								{
									Name:  "TRACES_PATH",
									Value: fmt.Sprintf("%s/traces.json", otlpSinkDataDir),
								},
								{
									Name:  "LOGS_PATH",
									Value: fmt.Sprintf("%s/logs.json", otlpSinkDataDir),
								},
								{
									Name:  "OTLP_PORT",
									Value: strconv.Itoa(int(otlpSinkPort)),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Protocol:      corev1.ProtocolTCP,
									HostPort:      otlpSinkPort,
									ContainerPort: otlpSinkPort,
								},
							},
							// The FileExporter does not have configurable file permissions, so we need to grant to
							// 'group' and 'other' at least read permissions programmatically.
							// See https://github.com/lumigo-io/opentelemetry-collector-contrib/blob/a39e9cc396f23c85aab766526bdde0a0fdd67673/exporter/fileexporter/factory.go#LL149C42-L149C42
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  &otlpUid,
								RunAsGroup: &otlpGid,
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

	if err := client.Resources().Create(ctx, otlpSinkDeployment); err != nil {
		return ctx, err
	}

	if err := wait.For(conditions.New(client.Resources()).DeploymentConditionMatch(otlpSinkDeployment, appsv1.DeploymentAvailable, corev1.ConditionTrue), wait.WithTimeout(time.Minute*5)); err != nil {
		return ctx, err
	}

	if err := client.Resources().Create(ctx, otlpSinkService); err != nil {
		return ctx, err
	}

	return ctx, nil
}
