package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DEFAULT_CONTROLLER_IMG_NAME = "host.docker.internal:5000/controller"
	DEFAULT_PROXY_IMG_NAME      = "host.docker.internal:5000/telemetry-proxy"
	DEFAULT_IMG_VERSION         = "latest"
)

func LumigoOperatorFeature(lumigoNamespace string, otlpSinkUrl string, logger logr.Logger) features.Feature {
	return features.New("LumigoOperatorLocal").Setup(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
		controllerImageVal, isPresent := os.LookupEnv("CONTROLLER_IMG")
		if !isPresent {
			controllerImageVal = DEFAULT_CONTROLLER_IMG_NAME
		}

		telemetryProxyImageVal, isPresent := os.LookupEnv("PROXY_IMG")
		if !isPresent {
			telemetryProxyImageVal = DEFAULT_PROXY_IMG_NAME
		}

		imageVersionVal, isPresent := os.LookupEnv("IMG_VERSION")
		if !isPresent {
			imageVersionVal = DEFAULT_IMG_VERSION
		}

		var curDir, _ = os.Getwd()
		chartDir := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(curDir))), "charts", "lumigo-operator")
		logger.Info("Installing Helm", "Chart dir", chartDir)

		manager := helm.New(config.KubeconfigFile())
		if err := manager.RunInstall(
			helm.WithName("lumigo"),
			helm.WithChart(chartDir),
			helm.WithNamespace(lumigoNamespace),
			helm.WithArgs(fmt.Sprintf("--set controllerManager.manager.image.repository=%s", controllerImageVal)),
			helm.WithArgs(fmt.Sprintf("--set controllerManager.manager.image.tag=%s", imageVersionVal)),
			helm.WithArgs(fmt.Sprintf("--set controllerManager.telemetryProxy.image.repository=%s", telemetryProxyImageVal)),
			helm.WithArgs(fmt.Sprintf("--set controllerManager.telemetryProxy.image.tag=%s", imageVersionVal)),
			helm.WithArgs(fmt.Sprintf("--set endpoint.otlp.url=%s", otlpSinkUrl)),
			helm.WithArgs("--set debug.enabled=true"), // Operator debug logging at runtime
			helm.WithArgs("--debug"), // Helm debug output on install
			helm.WithWait(),
			helm.WithTimeout("10m"),
		); err != nil {
			t.Fatal("failed to invoke helm install operation due to an error", err)
		}

		client := config.Client()
		if err := wait.For(conditions.New(client.Resources()).DeploymentConditionMatch(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lumigo-lumigo-operator-controller-manager",
				Namespace: lumigoNamespace,
			},
		}, appsv1.DeploymentAvailable, corev1.ConditionTrue), wait.WithTimeout(time.Minute*5)); err != nil {
			t.Fatal(err)
		}

		return ctx
	}).Feature()
}
