// Copyright 2020 OpenTelemetry Authors
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

package k8seventsenricherprocessor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/processor/processortest"
	corev1 "k8s.io/api/core/v1"
)

func TestNewProcessor(t *testing.T) {
	t.Parallel()

	rCfg := createDefaultConfig().(*Config)
	rCfg.makeDynamicClient = newMockDynamicClient().getMockDynamicClient
	r, err := createKubernetesProcessor(
		processortest.NewNopCreateSettings(),
		rCfg,
	)

	require.NoError(t, err)
	require.NotNil(t, r)
	require.NoError(t, r.Start(context.Background(), componenttest.NewNopHost()))
	assert.NoError(t, r.Shutdown(context.Background()))
}

func TestEnrichDeploymentPodEvent(t *testing.T) {
	t.Parallel()

	mockClient := newMockDynamicClient()

	deployment := generateDeployment("deployment1", "default")
	mockClient.createDeployment(deployment)

	replicaSet := generateDeploymentReplicaSet(deployment)
	mockClient.createReplicaSet(replicaSet)

	pod := generateReplicaSetPod(replicaSet)
	mockClient.createPod(pod)

	rCfg := createDefaultConfig().(*Config)
	rCfg.makeDynamicClient = mockClient.getMockDynamicClient

	err := rCfg.Validate()
	require.NoError(t, err)

	r, err := createKubernetesProcessor(
		processortest.NewNopCreateSettings(),
		rCfg,
	)
	require.NoError(t, err)

	event := generateEvent("ImagePullBackoff", "Warning", corev1.ObjectReference{
		APIVersion: pod.APIVersion,
		Kind:       pod.Kind,
		Namespace:  pod.ObjectMeta.Namespace,
		Name:       pod.ObjectMeta.Name,
		UID:        pod.ObjectMeta.UID,
	})

	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()
	logRecord := scopeLogs.LogRecords().AppendEmpty()
	body := logRecord.Body()
	destMap := body.SetEmptyMap()

	err = destMap.FromRaw(map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Event",
		"metadata": map[string]interface{}{
			"namespace": event.ObjectMeta.Namespace,
			"name":      event.ObjectMeta.Name,
		},
		"involvedObject": map[string]interface{}{
			"apiVersion":      event.InvolvedObject.APIVersion,
			"kind":            event.InvolvedObject.Kind,
			"namespace":       event.InvolvedObject.Namespace,
			"name":            event.InvolvedObject.Name,
			"uid":             string(event.InvolvedObject.UID),
			"resourceVersion": event.InvolvedObject.ResourceVersion,
		},
	})
	require.NoError(t, err)

	logs, err = r.processLogs(context.TODO(), logs)

	require.NoError(t, err)
	require.NotNil(t, r)
	require.NoError(t, r.Start(context.Background(), componenttest.NewNopHost()))
	time.Sleep(time.Second)
	assert.NoError(t, r.Shutdown(context.Background()))

	assert.Equal(t, logs.LogRecordCount(), 1)
}
