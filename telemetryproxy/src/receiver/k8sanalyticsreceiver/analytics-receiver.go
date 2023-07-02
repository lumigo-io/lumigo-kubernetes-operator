package k8sanalyticsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sanalyticsreceiver"

import (
	"context"
	"fmt"
	"go.opentelemetry.io/collector/component"
)

type k8sanalyticsReceiver struct {
	config *Config
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Start(ctx context.Context, host component.Host) error {
	fmt.Println("k8sanalyticsReceiver start function")
	return nil
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Shutdown(ctx context.Context) error {
	fmt.Println("k8sanalyticsReceiver shutdown function")
	return nil
}
