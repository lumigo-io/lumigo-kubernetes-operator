package watchers

import (
	"context"
	"fmt"
	stdlog "log"
	"path/filepath"
	"strings"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	log "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// KubeWatcher watches for kubernetes events and exports them as otel logs
type KubeWatcher struct {
	kubeclient kubernetes.Interface
	otelLogger log.Logger
	namespace  string
	config     *config.Config
}

func sanitizeEndpointURL(endpoint string, debug bool) string {
	// Remove https:// prefix if it exists, as the exporter will add it
	if len(endpoint) >= 8 && endpoint[:8] == "https://" {
		endpoint = endpoint[8:]
	}

	// Remove trailing slash if present
	if len(endpoint) > 0 && endpoint[len(endpoint)-1] == '/' {
		endpoint = endpoint[:len(endpoint)-1]
	}

	if debug {
		stdlog.Printf("Sanitized endpoint: %s", endpoint)
	}

	return endpoint
}

func NewKubeWatcher(config *config.Config) (*KubeWatcher, error) {
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

	w := &KubeWatcher{
		kubeclient: clientset,
		namespace:  config.LUMIGO_OPERATOR_NAMESPACE,
		config:     config,
	}

	if config.LUMIGO_TOKEN != "" {
		if err := w.initOpenTelemetry(); err != nil {
			stdlog.Printf("Failed to initialize OpenTelemetry: %v", err)
		}
	}

	return w, nil
}

func (w *KubeWatcher) initOpenTelemetry() error {
	ctx := context.Background()
	endpoint := sanitizeEndpointURL(w.config.LUMIGO_LOGS_ENDPOINT, w.config.DEBUG)

	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpoint(endpoint),
		otlploghttp.WithHeaders(map[string]string{
			"Authorization": fmt.Sprintf("LumigoToken %s", w.config.LUMIGO_TOKEN),
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTLP logs exporter: %w", err)
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("lumigo-kubernetes-operator-watchdog"),
		semconv.ServiceVersion(w.config.LUMIGO_OPERATOR_VERSION),
		semconv.OtelScopeName("lumigo-operator.k8s-events"), // So the Lumigo backend will process it as a Lumigo operator event rather than a generic log
		attribute.String("k8s.namespace.name", w.namespace),
	)

	loggerProviderOptions := []sdklog.LoggerProviderOption{
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	}

	if w.config.DEBUG {
		stdoutExporter, err := stdoutlog.New()
		if err != nil {
			return fmt.Errorf("failed to create stdout exporter: %w", err)
		}
		loggerProviderOptions = append(loggerProviderOptions, sdklog.WithProcessor(sdklog.NewBatchProcessor(stdoutExporter)))
	}

	otelLoggerProvider := sdklog.NewLoggerProvider(loggerProviderOptions...)

	// The logger name "lumigo-operator.k8s-events" is propagated to the scope.name, attribute in the log,
	// making the Lumigo backend process it as a K8s event rather than a generic customer log
	w.otelLogger = otelLoggerProvider.Logger("lumigo-operator.k8s-events")

	return nil
}

func (w *KubeWatcher) Watch() {
	ctx := context.TODO()
	watcher, err := w.kubeclient.CoreV1().Events(w.namespace).Watch(ctx, v1.ListOptions{})
	if err != nil {
		stdlog.Printf("Error starting watch: %s\n", err.Error())
		go w.Watch() // Start watch again in a new goroutine in case of error
		return
	}

	ch := watcher.ResultChan()

	stdlog.Printf("Watching for namespace changes in %s...\n", w.namespace)
	for event := range ch {
		if w.config.LUMIGO_TOKEN != "" {
			eventAsLog := w.k8sEventToLogRecord(event.Object.(*coreV1.Event))
			w.otelLogger.Emit(ctx, eventAsLog)
		} else {
			stdlog.Printf("No token found, skipping event collection. Retrying in 5 seconds...\n")
			time.Sleep(5 * time.Second)
		}
	}
}

var severityMap = map[string]log.Severity{
	"normal":  log.SeverityInfo,
	"warning": log.SeverityError,
}

// Taken from https://github.com/lumigo-io/opentelemetry-collector-contrib/blob/lumigo-main/receiver/k8seventsreceiver/receiver.go#L147
func getEventTimestamp(event *coreV1.Event) time.Time {
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	return event.ObjectMeta.CreationTimestamp.Time
}

func (w *KubeWatcher) k8sEventToLogRecord(ev *coreV1.Event) log.Record {
	rec := log.Record{}
	rec.SetTimestamp(getEventTimestamp(ev))
	rec.SetSeverityText(ev.Type)

	eventData := log.MapValue(
		log.KeyValue{Key: "type", Value: log.StringValue(ev.Type)},
		log.KeyValue{Key: "involvedObject", Value: log.MapValue(
			log.KeyValue{Key: "kind", Value: log.StringValue(ev.InvolvedObject.Kind)},
			log.KeyValue{Key: "namespace", Value: log.StringValue(ev.InvolvedObject.Namespace)},
			log.KeyValue{Key: "name", Value: log.StringValue(ev.InvolvedObject.Name)},
			log.KeyValue{Key: "uid", Value: log.StringValue(string(ev.InvolvedObject.UID))},
			log.KeyValue{Key: "apiVersion", Value: log.StringValue(ev.InvolvedObject.APIVersion)},
			log.KeyValue{Key: "resourceVersion", Value: log.StringValue(ev.InvolvedObject.ResourceVersion)},
			log.KeyValue{Key: "fieldPath", Value: log.StringValue(ev.InvolvedObject.FieldPath)},
		)},
		log.KeyValue{Key: "reason", Value: log.StringValue(ev.Reason)},
		log.KeyValue{Key: "message", Value: log.StringValue(ev.Message)},
		log.KeyValue{Key: "source", Value: log.MapValue(
			log.KeyValue{Key: "component", Value: log.StringValue(ev.Source.Component)},
			log.KeyValue{Key: "host", Value: log.StringValue(ev.Source.Host)},
		)},
	)

	rec.SetBody(eventData)

	rec.SetEventName(ev.ObjectMeta.Name)
	rec.SetObservedTimestamp(time.Now())

	if severity, ok := severityMap[strings.ToLower(ev.Type)]; ok {
		rec.SetSeverity(severity)
	}

	return rec
}
