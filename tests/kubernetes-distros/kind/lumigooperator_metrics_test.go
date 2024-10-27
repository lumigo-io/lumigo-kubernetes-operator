package kind

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/lumigo-io/lumigo-kubernetes-operator/tests/kubernetes-distros/kind/internal"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	apimachinerywait "k8s.io/apimachinery/pkg/util/wait"
)

// These tests assume:
// 1. A valid `kubectl` configuration available in the home directory of the user
// 2. A Lumigo operator installed into the Kubernetes cluster referenced by the
//    `kubectl` configuration

func TestLumigoOperatorInfraMetrics(t *testing.T) {
	testAppDeploymentFeature := features.New("TestApp").
		Assess("infra metrics are collected", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			metricsPath := filepath.Join(otlpSinkDataPath, "metrics.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				metricsBytes, err := os.ReadFile(metricsPath)
				if err != nil {
					return false, err
				}

				if len(metricsBytes) < 1 {
					return false, err
				}

				metrics := make([]pmetric.Metric, 0)

				/*
				 * Metrics come in multiple lines; we need to split by '\n'.
				 * bufio.NewScanner fails because our lines are "too long" (LOL).
				 */
				exportRequests := strings.Split(string(metricsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := pmetricotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					if m, err := exportRequestToMetricRecords(exportRequest); err != nil {
						t.Fatalf("Cannot extract metrics from export request: %v", err)
					} else {
						metrics = append(metrics, m...)
					}
				}

				if len(metrics) < 1 {
					// No metrics received yet
					return false, fmt.Errorf("no metrics received yet")
				}

				metricNames := make([]string, 0)
				for _, metric := range metrics {
					metricNames = append(metricNames, metric.Name())
				}

				if !strings.Contains(strings.Join(metricNames, " "), "container_fs_usage_bytes") {
					return false, fmt.Errorf("could not find container_fs_usage_bytes among collected metrics: %v", metricNames)
				}

				return true, nil
			}); err != nil {
				t.Fatalf("Failed to wait for metrics: %v", err)
			}

			return ctx
		}).
		Feature()

	testEnv.Test(t, testAppDeploymentFeature)
}

func exportRequestToMetricRecords(exportRequest pmetricotlp.ExportRequest) ([]pmetric.Metric, error) {
	allMetrics := make([]pmetric.Metric, 0)

	for i := 0; i < exportRequest.Metrics().ResourceMetrics().Len(); i++ {
		resourceMetric := exportRequest.Metrics().ResourceMetrics().At(i)
		for j := 0; j < resourceMetric.ScopeMetrics().Len(); j++ {
			scopeMetric := resourceMetric.ScopeMetrics().At(j)
			for k := 0; k < scopeMetric.Metrics().Len(); k++ {
				metric := scopeMetric.Metrics().At(k)
				allMetrics = append(allMetrics, metric)
			}
		}
	}

	return allMetrics, nil
}
