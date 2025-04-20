package watchers

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	// Add OpenTelemetry SDK imports
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

type TopWatcher struct {
	clientset     *kubernetes.Clientset
	metricsClient *metricsv.Clientset
	namespace     string
	interval      time.Duration
	config        *config.Config
	meterProvider *sdkmetric.MeterProvider
	meter         metric.Meter
	cpuGauge      metric.Float64ObservableGauge
	memoryGauge   metric.Int64ObservableGauge
}

func NewTopWatcher(config *config.Config) (*TopWatcher, error) {
	k8sConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	if err != nil {
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}
	metricsClient, err := metricsv.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}

	w := &TopWatcher{
		clientset:     clientset,
		metricsClient: metricsClient,
		namespace:     config.LUMIGO_OPERATOR_NAMESPACE,
		interval:      time.Duration(config.TOP_WATCHER_INTERVAL),
		config:        config,
	}

	if w.config.LUMIGO_TOKEN == "" {
		stdlog.Println("Error: Lumigo token is missing. Please provide a valid token through the lumigoToken.value Helm setting.")
		stdlog.Println("Metrics collection will be skipped. For more information, visit https://github.com/lumigo-io/lumigo-kubernetes-operator")
		return w, nil
	}

	if err := w.initOpenTelemetry(); err != nil {
		stdlog.Printf("Failed to initialize OpenTelemetry: %v", err)
	}

	return w, nil
}

func (w *TopWatcher) sanitizeEndpointURL(endpoint string) string {
	// Remove https:// prefix if it exists, as the exporter will add it
	if len(endpoint) >= 8 && endpoint[:8] == "https://" {
		endpoint = endpoint[8:]
	}

	// Remove trailing slash if present
	if len(endpoint) > 0 && endpoint[len(endpoint)-1] == '/' {
		endpoint = endpoint[:len(endpoint)-1]
	}

	// Remove "/v1/metrics" suffix if present
	const metricsPath = "/v1/metrics"
	if len(endpoint) > len(metricsPath) && endpoint[len(endpoint)-len(metricsPath):] == metricsPath {
		endpoint = endpoint[:len(endpoint)-len(metricsPath)]
	}

	if w.config.DEBUG {
		stdlog.Printf("Original endpoint: %s, Sanitized endpoint: %s", w.config.LUMIGO_METRICS_ENDPOINT, endpoint)
	}

	return endpoint
}

func (w *TopWatcher) initOpenTelemetry() error {
	ctx := context.Background()
	version := w.getK8SVersion()
	cloudProvider := w.getK8SCloudProvider()

	endpoint := w.sanitizeEndpointURL(w.config.LUMIGO_METRICS_ENDPOINT)

	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithHeaders(map[string]string{
			"Authorization": fmt.Sprintf("LumigoToken %s", w.config.LUMIGO_TOKEN),
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("lumigo-kubernetes-operator-watchdog"),
		semconv.ServiceVersion("1.0.0"),
		attribute.String("k8s.cluster.version", version),
		attribute.String("k8s.cloud.provider", cloudProvider),
	)

	meterProviderOptions := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(w.interval*time.Second))),
	}

	if w.config.DEBUG {
		stdoutExporter, err := stdoutmetric.New()
		if err != nil {
			return fmt.Errorf("failed to create stdout exporter: %w", err)
		}
		meterProviderOptions = append(meterProviderOptions,
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(stdoutExporter, sdkmetric.WithInterval(w.interval*time.Second))))
	}

	w.meterProvider = sdkmetric.NewMeterProvider(meterProviderOptions...)
	otel.SetMeterProvider(w.meterProvider)
	w.meter = w.meterProvider.Meter("lumigo-io/lumigo-kubernetes-operator/watchdog")

	w.cpuGauge, err = w.meter.Float64ObservableGauge(
		"lumigo_operator_pod_container_resource_usage_cpu",
		metric.WithDescription("CPU usage in millicores"),
		metric.WithUnit("m"),
	)
	if err != nil {
		return fmt.Errorf("failed to create CPU gauge: %w", err)
	}

	w.memoryGauge, err = w.meter.Int64ObservableGauge(
		"lumigo_operator_pod_container_resource_usage_memory",
		metric.WithDescription("Memory usage in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return fmt.Errorf("failed to create memory gauge: %w", err)
	}

	// Register callback for collecting metrics
	_, err = w.meter.RegisterCallback(
		w.collectMetrics,
		w.cpuGauge,
		w.memoryGauge,
	)
	if err != nil {
		return fmt.Errorf("failed to register callback: %w", err)
	}

	return nil
}

func (w *TopWatcher) collectMetrics(ctx context.Context, observer metric.Observer) error {
	// This assumes that K8s metrics server (https://github.com/kubernetes-sigs/metrics-server) is installed
	// and running in the cluster. If not, you need to install it first.
	// Even though metrics-server is not advised for this use case, we are using it for this purpose
	// as scraping the kubelet directly (using /metrics/resource) is already done by the telemetry proxy, but
	// it will not be collected if it's down or misconfigured, and that's where the watchdog comes in.
	podMetricsList, err := w.metricsClient.MetricsV1beta1().PodMetricses(w.namespace).List(context.TODO(), v1.ListOptions{})
	if err != nil && w.config.DEBUG {
		stdlog.Printf("Warning: Unable to collect metrics: %v", err)
		stdlog.Println("This likely means the Metrics Server is not installed in your cluster.")
		stdlog.Println("To enable metrics collection, please install the Kubernetes Metrics Server: https://github.com/kubernetes-sigs/metrics-server")
		return nil
	}

	for _, podMetrics := range podMetricsList.Items {
		for _, container := range podMetrics.Containers {
			attrs := []attribute.KeyValue{
				attribute.String("namespace", w.namespace),
				attribute.String("pod", podMetrics.Name),
				attribute.String("container", container.Name),
			}

			cpuUsage := float64(container.Usage.Cpu().MilliValue())
			memUsage := container.Usage.Memory().Value()

			observer.ObserveFloat64(w.cpuGauge, cpuUsage, metric.WithAttributes(attrs...))
			observer.ObserveInt64(w.memoryGauge, memUsage, metric.WithAttributes(attrs...))
		}
	}

	return nil
}

func (w *TopWatcher) Watch() {
	if w.config.LUMIGO_TOKEN == "" {
		stdlog.Println("No token found, skipping metrics collection")
		return
	}

	// Create a context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a signal channel to handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Use a WaitGroup to track when it's safe to exit
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		<-ctx.Done()
		stdlog.Println("Shutting down watchdog metrics collection")

		if err := w.meterProvider.Shutdown(context.Background()); err != nil {
			stdlog.Printf("Error shutting down meter provider: %v", err)
		}
	}()

	// Block until we receive a signal
	<-sigChan
	cancel()
	wg.Wait()
}

func (w *TopWatcher) getK8SVersion() string {
	version, err := w.clientset.ServerVersion()
	if err != nil {
		return "unknown"
	}

	return version.String()
}

func (w *TopWatcher) getK8SCloudProvider() string {
	nodeList, err := w.clientset.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return "unknown"
	}

	provider := nodeList.Items[0].Spec.ProviderID
	if provider == "" {
		return "unknown"
	}

	return provider
}

func (w *TopWatcher) Shutdown(ctx context.Context) error {
	if err := w.meterProvider.Shutdown(ctx); err != nil {
		stdlog.Printf("Error shutting down meter provider: %v", err)
		return err
	}

	return nil
}
