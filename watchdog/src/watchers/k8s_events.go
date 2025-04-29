package watchers

import (
	"context"
	"fmt"
	stdlog "log"
	"strings"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	log "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubeEventsWatcher watches for kubernetes events and exports them as otel logs
type KubeEventsWatcher struct {
	kubeclient kubernetes.Interface
	otelLogger log.Logger
	namespace  string
	config     *config.Config
	k8sContext *watchdogContext
}

func NewKubeWatcher(ctx context.Context, config *config.Config, watchdogCtx *watchdogContext) (*KubeEventsWatcher, error) {
	w := &KubeEventsWatcher{
		kubeclient: watchdogCtx.Clientset,
		namespace:  config.LumigoOperatorNamespace,
		config:     config,
		k8sContext: watchdogCtx,
	}

	if config.LumigoToken != "" {
		if err := w.initOpenTelemetry(ctx); err != nil {
			stdlog.Printf("Failed to initialize OpenTelemetry: %v", err)
		}
	}

	return w, nil
}

func (w *KubeEventsWatcher) initOpenTelemetry(ctx context.Context) error {
	opts := LogsExporterConfigOptions(w.config.LumigoLogsEndpoint, w.config.LumigoToken)
	exporter, err := otlploghttp.New(ctx, *opts...)
	if err != nil {
		return fmt.Errorf("failed to create OTLP logs exporter: %w", err)
	}

	loggerProviderOptions := []sdklog.LoggerProviderOption{
		sdklog.WithResource(w.k8sContext.OtelResource),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	}

	if w.config.Debug {
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

func (w *KubeEventsWatcher) Watch(ctx context.Context) {
	watcher, err := w.kubeclient.CoreV1().Events(w.namespace).Watch(ctx, v1.ListOptions{})
	if err != nil {
		stdlog.Printf("Error starting watch: %s\n", err.Error())
		go w.Watch(ctx) // Start watch again in a new goroutine in case of error
		return
	}

	ch := watcher.ResultChan()

	stdlog.Printf("Watching for namespace changes in %s...\n", w.namespace)
	for event := range ch {
		if w.config.LumigoToken != "" {
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

	var rootOwnerReference log.KeyValue

	if len(ev.OwnerReferences) == 0 {
		rootOwnerReference = log.KeyValue{Key: "rootOwnerReference", Value: log.MapValue(
			log.KeyValue{Key: "name", Value: log.StringValue("unknown")},
			log.KeyValue{Key: "kind", Value: log.StringValue("unknown")},
			log.KeyValue{Key: "uid", Value: log.StringValue("unknown")},
		)}
	} else {
		rootOwnerReference = log.KeyValue{Key: "rootOwnerReference", Value: log.MapValue(
			log.KeyValue{Key: "name", Value: log.StringValue(ev.OwnerReferences[0].Name)},
			log.KeyValue{Key: "kind", Value: log.StringValue(ev.OwnerReferences[0].Kind)},
			log.KeyValue{Key: "uid", Value: log.StringValue(string(ev.OwnerReferences[0].UID))},
		)}
	}

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
		rootOwnerReference,
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
