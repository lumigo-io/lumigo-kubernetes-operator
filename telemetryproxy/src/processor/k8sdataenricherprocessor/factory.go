// Copyright 2023 Lumigo
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sdataenricherprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sdataenricherprocessor"

import (
	"context"
	"fmt"
	"sync"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sdataenricherprocessor/internal"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
)

var (
	// The value of "type" key in configuration.
	componentType = component.MustNewType("k8sdataenricherprocessor")
	// The stability level of the processor.
	stability = component.StabilityLevelBeta
)

var (
	consumerCapabilities = consumer.Capabilities{MutatesData: true}
	singletonKube        *internal.KubeClient
	singletonKubeMutex   sync.Mutex
)

func NewFactory() processor.Factory {
	return processor.NewFactory(
		componentType,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, stability),
		processor.WithLogs(createLogsProcessor, stability),
		processor.WithMetrics(createMetricsProcessor, stability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		APIConfig: k8sconfig.APIConfig{AuthType: k8sconfig.AuthTypeServiceAccount},
	}
}

func createTracesProcessor(
	ctx context.Context,
	params processor.Settings,
	cfg component.Config,
	next consumer.Traces,
) (processor.Traces, error) {
	return createTracesProcessorWithOptions(ctx, params, cfg, next)
}

func createTracesProcessorWithOptions(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	next consumer.Traces,
) (processor.Traces, error) {
	if kp, err := createKubernetesProcessor(set, cfg); err != nil {
		return nil, err
	} else {
		return processorhelper.NewTraces(
			ctx,
			set,
			cfg,
			next,
			kp.processTraces,
			processorhelper.WithCapabilities(consumerCapabilities),
			processorhelper.WithStart(kp.Start),
			processorhelper.WithShutdown(kp.Shutdown))
	}
}

func createLogsProcessor(
	ctx context.Context,
	params processor.Settings,
	cfg component.Config,
	nextLogsConsumer consumer.Logs,
) (processor.Logs, error) {
	return createLogsProcessorWithOptions(ctx, params, cfg, nextLogsConsumer)
}

func createLogsProcessorWithOptions(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextLogsConsumer consumer.Logs,
) (processor.Logs, error) {
	if kp, err := createKubernetesProcessor(set, cfg); err != nil {
		return nil, err
	} else {
		return processorhelper.NewLogs(
			ctx,
			set,
			cfg,
			nextLogsConsumer,
			kp.processLogs,
			processorhelper.WithCapabilities(consumerCapabilities),
			processorhelper.WithStart(kp.Start),
			processorhelper.WithShutdown(kp.Shutdown))
	}
}

func createMetricsProcessor(
	ctx context.Context,
	params processor.Settings,
	cfg component.Config,
	nextMetricsConsumer consumer.Metrics,
) (processor.Metrics, error) {
	return createMetricsProcessorWithOptions(ctx, params, cfg, nextMetricsConsumer)
}

func createMetricsProcessorWithOptions(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextMetricsConsumer consumer.Metrics,
) (processor.Metrics, error) {
	if kp, err := createKubernetesProcessor(set, cfg); err != nil {
		return nil, err
	} else {
		return processorhelper.NewMetrics(
			ctx,
			set,
			cfg,
			nextMetricsConsumer,
			kp.processMetrics,
			processorhelper.WithCapabilities(consumerCapabilities),
			processorhelper.WithStart(kp.Start),
			processorhelper.WithShutdown(kp.Shutdown))
	}
}

func createKubernetesProcessor(
	params processor.Settings,
	cfg component.Config,
) (*kubernetesprocessor, error) {
	apiConfig := cfg.(*Config).APIConfig
	if apiConfig.AuthType != k8sconfig.AuthTypeServiceAccount {
		return nil, fmt.Errorf("The only type of AuthConfig supported is '%s'; found: '%s'", k8sconfig.AuthTypeServiceAccount, apiConfig.AuthType)
	}

	kubeClient, err := initKubeClient(apiConfig, params.Logger)
	if err != nil {
		return nil, err
	}

	return &kubernetesprocessor{
		kube:   kubeClient,
		logger: params.Logger,
	}, nil
}

func initKubeClient(k8sConfig k8sconfig.APIConfig, logger *zap.Logger) (*internal.KubeClient, error) {
	// Synchronize singleton instantiation, as there might be multiple
	// instances of the processor being bootstrapped on different pipelines
	if singletonKube != nil {
		return singletonKube, nil
	}

	singletonKubeMutex.Lock()
	defer singletonKubeMutex.Unlock()

	client, err := k8sconfig.MakeClient(k8sConfig)
	if err != nil {
		return nil, err
	}

	kubeLogger := logger.With(
		// the 'kind' would be one of processor, receiver, exporter, or extention;
		// but KubeClient is shared among all of the above and deserves its own kind
		zap.String("kind", "kube-client"),
		zap.String("name", "kube-client"),
		// KubeClient is shared among pipelines
		zap.String("pipeline", "all"),
	)

	kube, err := internal.New(kubeLogger, client)
	if err != nil {
		return nil, err
	}

	if err := kube.Start(); err != nil {
		return nil, err
	}

	singletonKube = kube

	return singletonKube, nil
}
