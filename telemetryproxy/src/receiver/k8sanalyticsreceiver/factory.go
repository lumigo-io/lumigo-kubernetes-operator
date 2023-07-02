package k8sanalyticsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sanalyticsreceiver"

import (
	"context"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

const (
	typeStr = "k8sanalytics"
)

func createDefaultConfig() component.Config {
	return &Config{}
}

func createK8sanalyticsReceiver(_ context.Context, params receiver.CreateSettings, baseCfg component.Config, consumer consumer.Logs) (receiver.Logs, error) {
	if consumer == nil {
		return nil, component.ErrNilNextConsumer
	}

	cfg := baseCfg.(*Config)

	logsRcvr := &k8sanalyticsReceiver{
		config: cfg,
	}

	return logsRcvr, nil
}

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		typeStr,
		createDefaultConfig,
		receiver.WithLogs(createK8sanalyticsReceiver, component.StabilityLevelAlpha))
}
