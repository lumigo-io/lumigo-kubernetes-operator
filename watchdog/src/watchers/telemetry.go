package watchers

import (
	"io"
	"log"
	"net/http"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/reporters"
)

type TelemetryWatcher struct {
	endpoint string
	interval time.Duration
	reporter *reporters.MetricsReporter
	config   *config.Config
}

func NewTelemetryWatcher(config *config.Config) *TelemetryWatcher {
	return &TelemetryWatcher{
		endpoint: config.TELEMETRY_PROXY_ENDPOINT,
		interval: time.Duration(config.TELEMETRY_INTERVAL),
		reporter: reporters.NewMetricsReporter(config),
		config:   config,
	}
}

func (w *TelemetryWatcher) Watch() {
	for {
		if w.config.LUMIGO_TOKEN != "" {
			w.GetTelemetryMetrics()
		} else {
			log.Printf("No token found, skipping telemetry metrics collection")
		}
		time.Sleep(w.interval * time.Second)
	}
}

func (w *TelemetryWatcher) GetTelemetryMetrics() {
	resp, err := http.Get(w.endpoint)
	if err != nil {
		log.Printf("Error making HTTP request: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return
	}

	w.reporter.Report(string(body))
}
