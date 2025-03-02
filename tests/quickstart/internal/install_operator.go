package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
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

func installOrUpgradeLumigoOperator(ctx context.Context, client klient.Client, kubeconfigFilePath string, lumigoNamespace string, otlpSinkUrl string, logger logr.Logger, applyMonitoredNamespaces bool) (context.Context, error) {
	controllerImageName, controllerImageTag := splitContainerImageNameAndTag(ctx.Value(ContextKeyOperatorControllerImage).(string))
	telemetryProxyImageName, telemetryProxyImageTag := splitContainerImageNameAndTag(ctx.Value(ContextKeyOperatorTelemetryProxyImage).(string))
	operatorDebug := ctx.Value(ContextKeyLumigoOperatorDebug).(bool)
	kubernetesClusterName := ctx.Value(ContextKeyKubernetesClusterName).(string)
	lumigoToken := ctx.Value(ContextKeyLumigoToken).(string)
	quickstartNamespace := ctx.Value(ContextQuickstartNamespace).(string)

	var curDir, _ = os.Getwd()
	chartDir := filepath.Join(filepath.Dir(filepath.Dir(curDir)), "charts", "lumigo-operator")
	logger.Info("Installing Operator via Helm", "Chart dir", chartDir)

	manager := helm.New(kubeconfigFilePath)
	err := exec.Command("helm", "dependency", "update", chartDir).Run()
	if err != nil {
		return ctx, fmt.Errorf("failed to run helm dependency update: %w", err)
	}
	helmArgs := []string{
		"--install",
		"--create-namespace",
		fmt.Sprintf("--set cluster.name=%s", kubernetesClusterName),
		fmt.Sprintf("--set controllerManager.manager.image.repository=%s", controllerImageName),
		fmt.Sprintf("--set controllerManager.manager.image.tag=%s", controllerImageTag),
		fmt.Sprintf("--set controllerManager.telemetryProxy.image.repository=%s", telemetryProxyImageName),
		fmt.Sprintf("--set controllerManager.telemetryProxy.image.tag=%s", telemetryProxyImageTag),
		fmt.Sprintf("--set endpoint.otlp.url=%s", otlpSinkUrl),
		fmt.Sprintf("--set endpoint.otlp.logs_url=%s", otlpSinkUrl),
		fmt.Sprintf("--set endpoint.otlp.metrics_url=%s", otlpSinkUrl),
		fmt.Sprintf("--set lumigoToken.value=%s", lumigoToken), // Use the the test-token for infra metrics as well
		fmt.Sprintf("--set debug.enabled=%v", operatorDebug),   // Operator debug logging at runtime
		"--debug", // Helm debug output on install
	}
	if applyMonitoredNamespaces {
		monitoredNamespacesArgs := []string{
			fmt.Sprintf("--set monitoredNamespaces[0].namespace=%s", quickstartNamespace),
			fmt.Sprintf("--set monitoredNamespaces[0].loggingEnabled=%t", true),
			fmt.Sprintf("--set monitoredNamespaces[0].tracingEnabled=%t", true),
		}
		// Enable monitoring of a namespace on upgrade time
		helmArgs = append(helmArgs, monitoredNamespacesArgs...)
	}
	if err := manager.RunUpgrade(
		helm.WithName("lumigo"),
		helm.WithChart(chartDir),
		helm.WithNamespace(lumigoNamespace),
		helm.WithArgs(helmArgs...),
	); err != nil {
		return ctx, fmt.Errorf("failed to invoke helm install operation due to an error: %w", err)
	}

	if err := wait.For(conditions.New(client.Resources()).DeploymentConditionMatch(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lumigo-lumigo-operator-controller-manager",
			Namespace: lumigoNamespace,
		},
	}, appsv1.DeploymentAvailable, corev1.ConditionTrue), wait.WithTimeout(time.Minute*5)); err != nil {
		return ctx, err
	}

	return ctx, nil
}

func LumigoOperatorEnvFunc(lumigoNamespace string, otlpSinkUrl string, logger logr.Logger, applyMonitoredNamespaces bool) func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	return func(ctx context.Context, config *envconf.Config) (context.Context, error) {
		client, err := config.NewClient()
		if err != nil {
			return ctx, err
		}

		return installOrUpgradeLumigoOperator(ctx, client, config.KubeconfigFile(), lumigoNamespace, otlpSinkUrl, logger, applyMonitoredNamespaces)
	}
}

func splitContainerImageNameAndTag(imageName string) (string, string) {
	lastColonIndex := strings.LastIndex(imageName, ":")
	lastSlashIndex := strings.LastIndex(imageName, "/")

	if lastColonIndex < 0 || lastSlashIndex > lastColonIndex {
		// No tag in the image: if there is a colon character, must be a port in the domain
		return imageName, ""
	}

	return imageName[:lastColonIndex], imageName[lastColonIndex+1:]
}
