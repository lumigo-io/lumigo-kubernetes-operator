package k8sanalyticsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sanalyticsreceiver"

import (
	"context"
	"fmt"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/obsreport"
	"go.opentelemetry.io/collector/receiver"
	"k8s.io/client-go/dynamic"
	"sync"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
)

const (
	typeStr = "k8sanalytics"
)

var (
	singletonKube      dynamic.Interface
	singletonKubeMutex sync.Mutex
)

func createDefaultConfig() component.Config {
	return &Config{
		APIConfig: k8sconfig.APIConfig{AuthType: k8sconfig.AuthTypeServiceAccount},
	}
}

func createK8sanalyticsReceiver(_ context.Context, params receiver.CreateSettings, baseCfg component.Config, consumer consumer.Logs) (receiver.Logs, error) {
	if consumer == nil {
		return nil, component.ErrNilNextConsumer
	}

	cfg := baseCfg.(*Config)

	apiConfig := cfg.APIConfig
	if apiConfig.AuthType != k8sconfig.AuthTypeServiceAccount {
		return nil, fmt.Errorf("The only type of AuthConfig supported is '%s'; found: '%s'", k8sconfig.AuthTypeServiceAccount, apiConfig.AuthType)
	}

	kubeClient, err := initKubeClient(apiConfig)
	if err != nil {
		return nil, err
	}

	obsrecv, err := obsreport.NewReceiver(obsreport.ReceiverSettings{
		ReceiverID:             params.ID,
		Transport:              "http",
		ReceiverCreateSettings: params,
	})
	if err != nil {
		return nil, fmt.Errorf("cant create NewReceiver")
	}

	logsRcvr := &k8sanalyticsReceiver{
		kube:     kubeClient,
		config:   cfg,
		consumer: consumer,
		obsrecv:  obsrecv,
	}

	return logsRcvr, nil
}

func initKubeClient(k8sConfig k8sconfig.APIConfig) (dynamic.Interface, error) {
	// Synchronize singleton instantiation, as there might be multiple
	// instances of the processor being bootstrapped on different pipelines
	if singletonKube != nil {
		return singletonKube, nil
	}

	singletonKubeMutex.Lock()
	defer singletonKubeMutex.Unlock()

	client, err := k8sconfig.MakeDynamicClient(k8sConfig)
	if err != nil {
		return nil, err
	}

	singletonKube = client

	return client, nil
}

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		typeStr,
		createDefaultConfig,
		receiver.WithLogs(createK8sanalyticsReceiver, component.StabilityLevelAlpha))
}
