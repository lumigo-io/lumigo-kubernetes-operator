package watchers

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	// Add OpenTelemetry SDK imports
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
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
	watchdogCtx   *watchdogContext
	logger        logr.Logger
}

func NewTopWatcher(ctx context.Context, config *config.Config, watchdogCtx *watchdogContext) (*TopWatcher, error) {
	metricsClient, err := metricsv.NewForConfig(watchdogCtx.K8sConfig)
	if err != nil {
		return nil, err
	}

	topWatcherInterval, err := time.ParseDuration(config.TopWatcherInterval)
	if err != nil {
		watchdogCtx.Logger.Error(err, "Failed to parse TopWatcher interval, falling back to a default of 15 seconds")
		topWatcherInterval = 15 * time.Second
	}

	w := &TopWatcher{
		clientset:     watchdogCtx.Clientset,
		metricsClient: metricsClient,
		namespace:     config.LumigoOperatorNamespace,
		interval:      topWatcherInterval,
		config:        config,
		watchdogCtx:   watchdogCtx,
		logger:        watchdogCtx.Logger.WithName("TopWatcher"),
	}

	if w.config.LumigoToken == "" {
		w.logger.Error(fmt.Errorf("lumigo token is missing"), "Please provide a valid token through the lumigoToken.value Helm setting.\nMetrics collection will be skipped. For more information, visit https://github.com/lumigo-io/lumigo-kubernetes-operator")
		return w, nil
	}

	if err := w.initOpenTelemetry(ctx); err != nil {
		w.logger.Error(err, "Failed to initialize OpenTelemetry")
	}

	return w, nil
}

func (w *TopWatcher) initOpenTelemetry(ctx context.Context) error {
	opts := MetricsExporterConfigOptions(w.config.LumigoMetricsEndpoint, w.config.LumigoToken)
	exporter, err := otlpmetrichttp.New(ctx, *opts...)
	if err != nil {
		return fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	meterProviderOptions := []sdkmetric.Option{
		sdkmetric.WithResource(w.watchdogCtx.OtelResource),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(w.interval*time.Second))),
	}

	if w.config.Debug {
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
	if err != nil && w.config.Debug {
		w.logger.Error(err, "Unable to collect metrics. "+
			"This likely means the Metrics Server is not installed in your cluster "+
			"To enable metrics collection, please install the Kubernetes Metrics Server: https://github.com/kubernetes-sigs/metrics-server",
		)
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

func (w *TopWatcher) Watch(ctx context.Context) {
	if w.config.LumigoToken == "" {
		w.logger.Error(fmt.Errorf("no token provided"), "Skipping metrics collection")
		return
	}

	// Create a signal channel to handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Use a WaitGroup to track when it's safe to exit
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		<-ctx.Done()
		w.logger.Info("Shutting down watchdog metrics collection")

		w.Shutdown(ctx)
	}()

	// Block until we receive a signal
	<-sigChan
	wg.Wait()
}

func (w *TopWatcher) Shutdown(ctx context.Context) {
	if err := w.meterProvider.Shutdown(ctx); err != nil {
		w.logger.Error(err, "Error shutting down meter provider")
	}
}
