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

// KubeEventsWatcher watches for kubernetes events and exports them as otel logs
type KubeEventsWatcher struct {
	kubeclient kubernetes.Interface
	otelLogger log.Logger
	namespace  string
	config     *config.Config
}

func NewKubeWatcher(config *config.Config) (*KubeEventsWatcher, error) {
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

	w := &KubeEventsWatcher{
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

func (w *KubeEventsWatcher) initOpenTelemetry() error {
	ctx := context.Background()

	opts := LogsExporterConfigOptions(w.config.LUMIGO_LOGS_ENDPOINT, w.config.LUMIGO_TOKEN)
	exporter, err := otlploghttp.New(ctx, *opts...)
	if err != nil {
		return fmt.Errorf("failed to create OTLP logs exporter: %w", err)
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("lumigo-kubernetes-operator-watchdog"),
		attribute.String("lumigo.k8s_operator.version", w.config.LUMIGO_OPERATOR_VERSION),
		attribute.String("k8s.namespace.name", w.namespace),

		// This is mandatory for the Lumigo backend to process the logs as K8s events,
		// but we don't have it available in this part of the operator - for now we'll keep it empty.
		attribute.String("k8s.namespace.uid", ""),
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

func (w *KubeEventsWatcher) Watch() {
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

func (w *KubeEventsWatcher) k8sEventToLogRecord(ev *coreV1.Event) log.Record {
	rec := log.Record{}
	rec.SetTimestamp(getEventTimestamp(ev))
	rec.SetSeverityText(ev.Type)

	eventData := log.MapValue(
		log.KeyValue{Key: "metadata", Value: log.MapValue(log.KeyValue{Key: "uid", Value: log.StringValue(string(ev.UID))})},
		log.KeyValue{Key: "lastTimestamp", Value: log.StringValue(ev.LastTimestamp.Format("2006-01-02T15:04:05Z"))},
		log.KeyValue{Key: "eventTime", Value: log.StringValue(ev.FirstTimestamp.Format("2006-01-02T15:04:05.000000Z"))},
		log.KeyValue{Key: "creationTimestamp", Value: log.StringValue(ev.CreationTimestamp.Format("2006-01-02T15:04:05Z"))},
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
