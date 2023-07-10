package lumigooperatorheartbeatreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/lumigooperatorheartbeatreceiver"

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/obsreport"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
	"k8s.io/client-go/dynamic"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
)

const (
	typeStr = "lumigooperatorheartbeat"
)

var (
	singletonKube      dynamic.Interface
	singletonKubeMutex sync.Mutex
)

func createDefaultConfig() component.Config {
	return &Config{}
}

func createLumigooperatorheartbeatReceiver(_ context.Context, params receiver.CreateSettings, baseCfg component.Config, consumer consumer.Logs) (receiver.Logs, error) {
	if consumer == nil {
		return nil, component.ErrNilNextConsumer
	}

	cfg := baseCfg.(*Config)

	kubeClient, err := initKubeClient()
	if err != nil {
		return nil, err
	}

	obsrecv, err := obsreport.NewReceiver(obsreport.ReceiverSettings{
		ReceiverID:             params.ID,
		Transport:              "http",
		ReceiverCreateSettings: params,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create a new receiver: %w", err)
	}

	logsRcvr := &lumigooperatorheartbeatReceiver{
		kube:     kubeClient,
		config:   cfg,
		consumer: consumer,
		obsrecv:  obsrecv,
		logger:   params.Logger.With(zap.String("namespace", cfg.Namespace)),
	}

	return logsRcvr, nil
}

func initKubeClient() (dynamic.Interface, error) {
	// Synchronize singleton instantiation, as there might be multiple
	// instances of the processor being bootstrapped on different pipelines
	if singletonKube != nil {
		return singletonKube, nil
	}

	singletonKubeMutex.Lock()
	defer singletonKubeMutex.Unlock()

	client, err := k8sconfig.MakeDynamicClient(k8sconfig.APIConfig{AuthType: k8sconfig.AuthTypeServiceAccount})
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
		receiver.WithLogs(createLumigooperatorheartbeatReceiver, component.StabilityLevelAlpha))
}
