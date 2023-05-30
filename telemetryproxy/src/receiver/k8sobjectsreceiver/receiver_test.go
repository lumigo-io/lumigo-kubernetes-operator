// Copyright  The OpenTelemetry Authors
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

package k8sobjectsreceiver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestNewReceiver(t *testing.T) {
	t.Parallel()

	rCfg := createDefaultConfig().(*Config)
	rCfg.makeDynamicClient = newMockDynamicClient().getMockDynamicClient
	r, err := newReceiver(
		receivertest.NewNopCreateSettings(),
		rCfg,
		consumertest.NewNop(),
	)

	require.NoError(t, err)
	require.NotNil(t, r)
	require.NoError(t, r.Start(context.Background(), componenttest.NewNopHost()))
	assert.NoError(t, r.Shutdown(context.Background()))
}

func TestPullObject(t *testing.T) {
	t.Parallel()

	mockClient := newMockDynamicClient()
	mockClient.createPods(
		generatePod("pod1", "default", map[string]interface{}{
			"environment": "production",
		}, "1234"),
		generatePod("pod2", "default", map[string]interface{}{
			"environment": "test",
		}, "5678"),
		generatePod("pod3", "default_ignore", map[string]interface{}{
			"environment": "production",
		}, "9012"),
	)

	rCfg := createDefaultConfig().(*Config)
	rCfg.makeDynamicClient = mockClient.getMockDynamicClient
	rCfg.makeDiscoveryClient = getMockDiscoveryClient

	rCfg.Objects = []*K8sObjectsConfig{
		{
			Name:          "pods",
			Mode:          PullMode,
			Interval:      time.Second * 30,
			LabelSelector: "environment=production",
		},
	}

	err := rCfg.Validate()
	require.NoError(t, err)

	consumer := newMockLogConsumer()
	r, err := newReceiver(
		receivertest.NewNopCreateSettings(),
		rCfg,
		consumer,
	)
	require.NoError(t, err)
	require.NotNil(t, r)
	require.NoError(t, r.Start(context.Background(), componenttest.NewNopHost()))
	time.Sleep(time.Second)
	assert.Len(t, consumer.Logs(), 1)
	assert.Equal(t, 2, consumer.Count())
	assert.NoError(t, r.Shutdown(context.Background()))
}

func TestWatchObject(t *testing.T) {
	t.Parallel()

	mockClient := newMockDynamicClient()

	rCfg := createDefaultConfig().(*Config)
	rCfg.makeDynamicClient = mockClient.getMockDynamicClient
	rCfg.makeDiscoveryClient = getMockDiscoveryClient

	rCfg.Objects = []*K8sObjectsConfig{
		{
			Name:       "pods",
			Mode:       WatchMode,
			Namespaces: []string{"default"},
		},
	}

	err := rCfg.Validate()
	require.NoError(t, err)

	consumer := newMockLogConsumer()
	r, err := newReceiver(
		receivertest.NewNopCreateSettings(),
		rCfg,
		consumer,
	)

	ctx := context.Background()
	require.NoError(t, err)
	require.NotNil(t, r)
	require.NoError(t, r.Start(ctx, componenttest.NewNopHost()))

	time.Sleep(time.Millisecond * 100)

	mockClient.createPods(
		generatePod("pod1", "default", map[string]interface{}{
			"environment": "production",
		}, "1234"),
		generatePod("pod2", "default", map[string]interface{}{
			"environment": "test",
		}, "5678"),
		generatePod("pod3", "default_ignore", map[string]interface{}{
			"environment": "production",
		}, "9012"),
	)
	time.Sleep(time.Millisecond * 100)
	assert.Len(t, consumer.Logs(), 2)
	assert.Equal(t, 2, consumer.Count())

	mockClient.createPods(
		generatePod("pod4", "default", map[string]interface{}{
			"environment": "production",
		}, "3456"),
	)
	time.Sleep(time.Millisecond * 100)
	assert.Len(t, consumer.Logs(), 3)
	assert.Equal(t, 3, consumer.Count())

	assert.NoError(t, r.Shutdown(ctx))
}
