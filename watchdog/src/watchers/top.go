package watchers

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/reporters"
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
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

type TopWatcher struct {
	clientset     *kubernetes.Clientset
	metricsClient *metricsv.Clientset
	namespace     string
	reporter      *reporters.MetricsReporter
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

	// Create a new watcher
	w := &TopWatcher{
		clientset:     clientset,
		metricsClient: metricsClient,
		reporter:      reporters.NewMetricsReporter(config),
		namespace:     config.LUMIGO_OPERATOR_NAMESPACE,
		interval:      time.Duration(config.TOP_WATCHER_INTERVAL),
		config:        config,
	}

	// Initialize OpenTelemetry if token exists
	if config.LUMIGO_TOKEN != "" {
		if err := w.initOpenTelemetry(); err != nil {
			log.Printf("Failed to initialize OpenTelemetry: %v", err)
		}
	}

	return w, nil
}

func (w *TopWatcher) initOpenTelemetry() error {
	ctx := context.Background()
	version := w.getK8SVersion()
	cloudProvider := w.getK8SCloudProvider()

	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint(w.config.LUMIGO_METRICS_ENDPOINT),
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

	w.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(w.interval*time.Second))),
	)

	otel.SetMeterProvider(w.meterProvider)
	w.meter = w.meterProvider.Meter("lumigo-io/lumigo-kubernetes-operator/watchdog")

	w.cpuGauge, err = w.meter.Float64ObservableGauge(
		"kube_pod_container_resource_usage_cpu",
		metric.WithDescription("CPU usage in millicores"),
		metric.WithUnit("m"),
	)
	if err != nil {
		return fmt.Errorf("failed to create CPU gauge: %w", err)
	}

	w.memoryGauge, err = w.meter.Int64ObservableGauge(
		"kube_pod_container_resource_usage_memory",
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
	podMetricsList, err := w.metricsClient.MetricsV1beta1().PodMetricses(w.namespace).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error getting pod metrics: %w", err)
	}

	// Collect metrics for each container
	for _, podMetrics := range podMetricsList.Items {
		for _, container := range podMetrics.Containers {
			cpuUsage := float64(container.Usage.Cpu().MilliValue())
			memUsage := container.Usage.Memory().Value()

			attrs := []attribute.KeyValue{
				attribute.String("namespace", w.namespace),
				attribute.String("pod", podMetrics.Name),
				attribute.String("container", container.Name),
			}

			observer.ObserveFloat64(w.cpuGauge, cpuUsage, metric.WithAttributes(attrs...))
			observer.ObserveInt64(w.memoryGauge, memUsage, metric.WithAttributes(attrs...))
		}
	}

	return nil
}

func (w *TopWatcher) Watch() {
	if w.config.LUMIGO_TOKEN == "" {
		log.Println("No token found, skipping metrics collection")
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
		log.Println("Shutting down watchdog metrics collection")

		if err := w.meterProvider.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down meter provider: %v", err)
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
