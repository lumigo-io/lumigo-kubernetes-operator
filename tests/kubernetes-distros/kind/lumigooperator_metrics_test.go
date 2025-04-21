package kind

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

			if err := apimachinerywait.PollImmediateWithContext(ctx, 10*time.Second, 4*time.Minute, func(context.Context) (bool, error) {
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

					allResourceMetrics, err := exportRequestToResourceMetrics(exportRequest)
					if err != nil {
						return false, fmt.Errorf("Cannot extract resource metrics from export request: %v", err)
					}

					for _, resourceMetrics := range allResourceMetrics {
						clusterName, exists := resourceMetrics.Resource().Attributes().Get("k8s.cluster.name")
						if !exists {
							return false, fmt.Errorf("Cannot find cluster name in resource metrics")
						}
						if clusterName.AsString() != ctx.Value(internal.ContextKeyKubernetesClusterName) {
							return false, fmt.Errorf("Cluster name mismatch: actual %v, expected: %v", clusterName, internal.ContextKeyKubernetesClusterName)
						}
						clusterUid, exists := resourceMetrics.Resource().Attributes().Get("k8s.cluster.uid")
						if !exists {
							return false, fmt.Errorf("Cannot find cluster UID in resource metrics")
						}
						if !isValidUUID(clusterUid.AsString()) {
							return false, fmt.Errorf("Invalid cluster UID: %v", clusterUid)
						}
					}

					if m, err := extractMetrics(allResourceMetrics); err != nil {
						t.Fatalf("Cannot extract metrics from export request: %v", err)
					} else {
						metrics = append(metrics, m...)
					}
				}

				if len(metrics) < 1 {
					return false, fmt.Errorf("no metrics received yet")
				}

				seenMetricNames := make(map[string]bool)
				uniqueMetricNames := make([]string, 0)
				for _, metric := range metrics {
					if !seenMetricNames[metric.Name()] {
						seenMetricNames[metric.Name()] = true
						uniqueMetricNames = append(uniqueMetricNames, metric.Name())
					}
				}

				prometheusNodeExporterMetricsFound := false
				ownerMetricsFound := false
				labelMetricsFound := false
				containerMetricsWithCorrectJobFound := false

				// For essentialOnly mode
				essentialMetricsPrefixes := []string{
					"node_",
					"container_",
					"kube_",
					"otelcol_",
					"http_client_",
					"http_server_",
				}

				for _, metric := range metrics {
					if metric.Name() == "node_cpu_seconds_total" {
						prometheusNodeExporterMetricsFound = true
						for i := 0; i < metric.Sum().DataPoints().Len(); i++ {
							attributes := metric.Sum().DataPoints().At(i).Attributes()
							_, nodeAttributeExists := attributes.Get("node")
							if !nodeAttributeExists {
								t.Logf("could not find attribute 'node' for metric 'node_cpu_seconds_total'")
								return false, nil
							}
						}
					} else if metric.Name() == "kube_pod_owner" {
						ownerMetricsFound = true
						for i := 0; i < metric.Gauge().DataPoints().Len(); i++ {
							attributes := metric.Gauge().DataPoints().At(i).Attributes()
							ownerKindAttr, found := attributes.Get("owner_kind")
							if !found {
								t.Logf("could not find attribute 'owner_kind' for metric 'kube_pod_owner'")
								return false, nil
							} else if ownerKindAttr.AsString() == "ReplicaSet" {
								_, ownerUidFound := attributes.Get("owner_uid")
								if !ownerUidFound {
									t.Logf("could not find attribute 'owner_uid' for metric 'kube_pod_owner'")
									return false, nil
								}
							}
						}
					} else if metric.Name() == "container_cpu_usage_seconds_total" {
						containerMetricsWithCorrectJobFound = true
						exportedFromExpectedJobOnly, err := checkMetricIsExportedFromExpectedJobOnly(t, metric, "k8s-infra-metrics-cadvisor")
						if !exportedFromExpectedJobOnly || err != nil {
							return false, err
						}
					} else {
						isEssentialMetric := false
						for _, prefix := range essentialMetricsPrefixes {
							if strings.HasPrefix(metric.Name(), prefix) {
								isEssentialMetric = true
							}
						}
						if !isEssentialMetric {
							return false, fmt.Errorf("unexpected metric: %v", metric.Name())
						}

						if strings.HasSuffix(metric.Name(), "_labels") {
							labelMetricsFound = true
						}
					}
				}

				if !prometheusNodeExporterMetricsFound {
					t.Logf("could not find Prometheus Node Exporter metrics. Seen metrics: %v\n retrying...", uniqueMetricNames)
					return false, nil
				}

				if !labelMetricsFound {
					t.Logf("could not find label metrics. Seen metrics: %v\n retrying...", uniqueMetricNames)
					return false, nil
				}

				if !ownerMetricsFound {
					t.Logf("could not find owner metrics. Seen metrics: %v\n retrying...", uniqueMetricNames)
					return false, nil
				}

				if !containerMetricsWithCorrectJobFound {
					t.Logf("could not find container metrics with correct job attribute. Seen metrics: %v\n retrying...", uniqueMetricNames)
					return false, nil
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

func exportRequestToResourceMetrics(exportRequest pmetricotlp.ExportRequest) ([]pmetric.ResourceMetrics, error) {
	allResourceMetrics := make([]pmetric.ResourceMetrics, 0)

	for i := 0; i < exportRequest.Metrics().ResourceMetrics().Len(); i++ {
		allResourceMetrics = append(allResourceMetrics, exportRequest.Metrics().ResourceMetrics().At(i))
	}

	return allResourceMetrics, nil
}

func extractMetrics(resourceMetricsList []pmetric.ResourceMetrics) ([]pmetric.Metric, error) {
	allMetrics := make([]pmetric.Metric, 0)

	for i := 0; i < len(resourceMetricsList); i++ {
		for j := 0; j < resourceMetricsList[i].ScopeMetrics().Len(); j++ {
			scopeMetric := resourceMetricsList[i].ScopeMetrics().At(j)
			for k := 0; k < scopeMetric.Metrics().Len(); k++ {
				metric := scopeMetric.Metrics().At(k)
				allMetrics = append(allMetrics, metric)
			}
		}
	}

	return allMetrics, nil
}

func isValidUUID(uuid string) bool {
	regex := `^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[1-5][a-fA-F0-9]{3}-[89abAB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$`
	r := regexp.MustCompile(regex)
	return r.MatchString(uuid)
}

func checkMetricIsExportedFromExpectedJobOnly(t *testing.T, metric pmetric.Metric, expectedJobValue string) (bool, error) {
	var dataPoints pmetric.NumberDataPointSlice

	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		dataPoints = metric.Gauge().DataPoints()
	case pmetric.MetricTypeSum:
		dataPoints = metric.Sum().DataPoints()
	default:
		// Skip other metric types (like histogram)
		return true, nil
	}

	// Check all data points for unexpected job values
	for i := 0; i < dataPoints.Len(); i++ {
		attributes := dataPoints.At(i).Attributes()
		jobAttr, found := attributes.Get("job")
		if found && jobAttr.AsString() != expectedJobValue {
			return false, fmt.Errorf("found %s exported from job='%s', expected only job='%s'",
				metric.Name(), jobAttr.AsString(), expectedJobValue)
		}
	}

	// If we only found the expected job values, it's valid
	return true, nil
}
