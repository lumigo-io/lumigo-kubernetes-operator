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

package kube

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
)

func newFakeAPIClientset(_ k8sconfig.APIConfig) (kubernetes.Interface, error) {
	return fake.NewSimpleClientset(), nil
}

func newPodIdentifier(from string, name string, value string) PodIdentifier {
	if from == "connection" {
		name = ""
	}

	return PodIdentifier{
		{
			Source: AssociationSource{
				From: from,
				Name: name,
			},
			Value: value,
		},
	}
}

func podAddAndUpdateTest(t *testing.T, c *WatchClient, handler func(obj interface{})) {
	assert.Equal(t, 0, len(c.Pods))

	// pod without IP
	pod := &api_v1.Pod{}
	handler(pod)
	assert.Equal(t, 0, len(c.Pods))

	pod = &api_v1.Pod{}
	pod.Name = "podA"
	pod.Status.PodIP = "1.1.1.1"
	handler(pod)
	assert.Equal(t, 2, len(c.Pods))
	got := c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")]
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, "podA", got.Name)
	assert.Equal(t, "", got.PodUID)

	pod = &api_v1.Pod{}
	pod.Name = "podB"
	pod.Status.PodIP = "1.1.1.1"
	handler(pod)
	assert.Equal(t, 2, len(c.Pods))
	got = c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")]
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, "podB", got.Name)
	assert.Equal(t, "", got.PodUID)

	pod = &api_v1.Pod{}
	pod.Name = "podC"
	pod.Status.PodIP = "2.2.2.2"
	pod.UID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	handler(pod)
	assert.Equal(t, 5, len(c.Pods))
	got = c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "2.2.2.2")]
	assert.Equal(t, "2.2.2.2", got.Address)
	assert.Equal(t, "podC", got.Name)
	assert.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", got.PodUID)
	got = c.Pods[newPodIdentifier("resource_attribute", "k8s.pod.uid", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")]
	assert.Equal(t, "2.2.2.2", got.Address)
	assert.Equal(t, "podC", got.Name)
	assert.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", got.PodUID)

}

func namespaceAddAndUpdateTest(t *testing.T, c *WatchClient, handler func(obj interface{})) {
	assert.Equal(t, 0, len(c.Namespaces))

	namespace := &api_v1.Namespace{}
	handler(namespace)
	assert.Equal(t, 0, len(c.Namespaces))

	namespace = &api_v1.Namespace{}
	namespace.Name = "namespaceA"
	handler(namespace)
	assert.Equal(t, 1, len(c.Namespaces))
	got := c.Namespaces["namespaceA"]
	assert.Equal(t, "namespaceA", got.Name)
	assert.Equal(t, "", got.NamespaceUID)

	namespace = &api_v1.Namespace{}
	namespace.Name = "namespaceB"
	namespace.UID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	handler(namespace)
	assert.Equal(t, 2, len(c.Namespaces))
	got = c.Namespaces["namespaceB"]
	assert.Equal(t, "namespaceB", got.Name)
	assert.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", got.NamespaceUID)
}

func TestDefaultClientset(t *testing.T) {
	c, err := New(zap.NewNop(), k8sconfig.APIConfig{}, ExtractionRules{}, Filters{}, []Association{}, Excludes{}, nil, nil, nil, nil, nil)
	assert.Error(t, err)
	assert.Equal(t, "invalid authType for kubernetes: ", err.Error())
	assert.Nil(t, c)

	c, err = New(zap.NewNop(), k8sconfig.APIConfig{}, ExtractionRules{}, Filters{}, []Association{}, Excludes{}, newFakeAPIClientset, nil, nil, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, c)
}

func TestBadFilters(t *testing.T) {
	c, err := New(
		zap.NewNop(),
		k8sconfig.APIConfig{},
		ExtractionRules{},
		Filters{Fields: []FieldFilter{{Op: selection.Exists}}},
		[]Association{},
		Excludes{},
		newFakeAPIClientset,
		NewFakeInformer,
		NewFakeInformer,
		NewFakeInformer,
		NewFakeNamespaceInformer,
	)
	assert.Error(t, err)
	assert.Nil(t, c)
}

func TestClientStartStop(t *testing.T) {
	c, _ := newTestClient(t)
	ctr := c.podInformer.GetController()
	require.IsType(t, &FakeController{}, ctr)
	fctr := ctr.(*FakeController)
	require.NotNil(t, fctr)

	done := make(chan struct{})
	assert.False(t, fctr.HasStopped())
	go func() {
		c.Start()
		close(done)
	}()
	c.Stop()
	<-done
	time.Sleep(time.Second)
	assert.True(t, fctr.HasStopped())
}

func TestConstructorErrors(t *testing.T) {
	er := ExtractionRules{}
	ff := Filters{}
	t.Run("client-provider-call", func(t *testing.T) {
		var gotAPIConfig k8sconfig.APIConfig
		apiCfg := k8sconfig.APIConfig{
			AuthType: "test-auth-type",
		}
		clientProvider := func(c k8sconfig.APIConfig) (kubernetes.Interface, error) {
			gotAPIConfig = c
			return nil, fmt.Errorf("error creating k8s client")
		}
		c, err := New(zap.NewNop(), apiCfg, er, ff, []Association{}, Excludes{}, clientProvider, NewFakeInformer, NewFakeInformer, NewFakeInformer, NewFakeNamespaceInformer)
		assert.Nil(t, c)
		assert.Error(t, err)
		assert.Equal(t, "error creating k8s client", err.Error())
		assert.Equal(t, apiCfg, gotAPIConfig)
	})
}

func TestPodAdd(t *testing.T) {
	c, _ := newTestClient(t)
	podAddAndUpdateTest(t, c, c.handlePodAdd)
}

func TestNamespaceAdd(t *testing.T) {
	c, _ := newTestClient(t)
	namespaceAddAndUpdateTest(t, c, c.handleNamespaceAdd)
}

func TestPodHostNetwork(t *testing.T) {
	c, _ := newTestClient(t)
	assert.Equal(t, 0, len(c.Pods))

	// pod will not be added if no rule matches
	pod := &api_v1.Pod{}
	pod.Name = "podA"
	pod.Status.PodIP = "1.1.1.1"
	pod.Spec.HostNetwork = true
	c.handlePodAdd(pod)
	assert.Equal(t, 0, len(c.Pods))

	// pod will be added if rule matches
	pod.Name = "podB"
	pod.Status.PodIP = "2.2.2.2"
	pod.UID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	pod.Spec.HostNetwork = true
	c.handlePodAdd(pod)
	assert.Equal(t, 1, len(c.Pods))
	got := c.Pods[newPodIdentifier("resource_attribute", "k8s.pod.uid", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")]
	assert.Equal(t, "2.2.2.2", got.Address)
	assert.Equal(t, "podB", got.Name)
	assert.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", got.PodUID)
	assert.False(t, got.Ignore)
}

// TestPodCreate tests that a new pod, created after otel-collector starts, has its attributes set
// correctly
func TestPodCreate(t *testing.T) {
	c, _ := newTestClient(t)
	assert.Equal(t, 0, len(c.Pods))

	// pod is created in Pending phase. At this point it has a UID but no start time or pod IP address
	pod := &api_v1.Pod{}
	pod.Name = "podD"
	pod.UID = "11111111-2222-3333-4444-555555555555"
	c.handlePodAdd(pod)
	assert.Equal(t, 1, len(c.Pods))
	got := c.Pods[newPodIdentifier("resource_attribute", "k8s.pod.uid", "11111111-2222-3333-4444-555555555555")]
	assert.Equal(t, "", got.Address)
	assert.Equal(t, "podD", got.Name)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", got.PodUID)

	// pod is scheduled onto to a node (no changes relevant to this test happen in that event)
	// pod is started, and given a startTime but not an IP address - it's still Pending at this point
	startTime := meta_v1.NewTime(time.Now())
	pod.Status.StartTime = &startTime
	c.handlePodUpdate(&api_v1.Pod{}, pod)
	assert.Equal(t, 1, len(c.Pods))
	got = c.Pods[newPodIdentifier("resource_attribute", "k8s.pod.uid", "11111111-2222-3333-4444-555555555555")]
	assert.Equal(t, "", got.Address)
	assert.Equal(t, "podD", got.Name)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", got.PodUID)

	// pod is Running and has an IP address
	pod.Status.PodIP = "3.3.3.3"
	c.handlePodUpdate(&api_v1.Pod{}, pod)
	assert.Equal(t, 3, len(c.Pods))
	got = c.Pods[newPodIdentifier("resource_attribute", "k8s.pod.uid", "11111111-2222-3333-4444-555555555555")]
	assert.Equal(t, "3.3.3.3", got.Address)
	assert.Equal(t, "podD", got.Name)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", got.PodUID)
	got = c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "3.3.3.3")]
	assert.Equal(t, "3.3.3.3", got.Address)
	assert.Equal(t, "podD", got.Name)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", got.PodUID)

	got = c.Pods[newPodIdentifier("resource_attribute", "k8s.pod.ip", "3.3.3.3")]
	assert.Equal(t, "3.3.3.3", got.Address)
	assert.Equal(t, "podD", got.Name)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", got.PodUID)
}

func TestPodAddOutOfSync(t *testing.T) {
	c, _ := newTestClient(t)
	c.Associations = append(c.Associations, Association{
		Name: "name",
		Sources: []AssociationSource{
			{
				From: ResourceSource,
				Name: "k8s.pod.name",
			},
		},
	})
	assert.Equal(t, 0, len(c.Pods))

	pod := &api_v1.Pod{}
	pod.Name = "podA"
	pod.Status.PodIP = "1.1.1.1"
	startTime := meta_v1.NewTime(time.Now())
	pod.Status.StartTime = &startTime
	c.handlePodAdd(pod)
	assert.Equal(t, 3, len(c.Pods))
	got := c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")]
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, "podA", got.Name)
	got = c.Pods[newPodIdentifier(ResourceSource, "k8s.pod.name", "podA")]
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, "podA", got.Name)

	pod2 := &api_v1.Pod{}
	pod2.Name = "podB"
	pod2.Status.PodIP = "1.1.1.1"
	startTime2 := meta_v1.NewTime(time.Now().Add(-time.Second * 10))
	pod2.Status.StartTime = &startTime2
	c.handlePodAdd(pod2)
	assert.Equal(t, 4, len(c.Pods))
	got = c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")]
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, "podA", got.Name)
	got = c.Pods[newPodIdentifier(ResourceSource, "k8s.pod.name", "podB")]
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, "podB", got.Name)
}

func TestPodUpdate(t *testing.T) {
	c, _ := newTestClient(t)
	podAddAndUpdateTest(t, c, func(obj interface{}) {
		// first argument (old pod) is not used right now
		c.handlePodUpdate(&api_v1.Pod{}, obj)
	})
}

func TestNamespaceUpdate(t *testing.T) {
	c, _ := newTestClient(t)
	namespaceAddAndUpdateTest(t, c, func(obj interface{}) {
		// first argument (old namespace) is not used right now
		c.handleNamespaceUpdate(&api_v1.Namespace{}, obj)
	})
}

func TestPodDelete(t *testing.T) {
	c, _ := newTestClient(t)
	podAddAndUpdateTest(t, c, c.handlePodAdd)
	assert.Equal(t, 5, len(c.Pods))
	assert.Equal(t, "1.1.1.1", c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")].Address)

	// delete empty IP pod
	c.handlePodDelete(&api_v1.Pod{})

	// delete non-existent IP
	c.deletePodQueue = c.deletePodQueue[:0]
	pod := &api_v1.Pod{}
	pod.Status.PodIP = "9.9.9.9"
	c.handlePodDelete(pod)
	assert.Equal(t, 5, len(c.Pods))
	got := c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")]
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, 0, len(c.deletePodQueue))

	// delete matching IP with wrong name/different pod
	c.deletePodQueue = c.deletePodQueue[:0]
	pod = &api_v1.Pod{}
	pod.Status.PodIP = "1.1.1.1"
	c.handlePodDelete(pod)
	got = c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")]
	assert.Equal(t, 5, len(c.Pods))
	assert.Equal(t, "1.1.1.1", got.Address)
	assert.Equal(t, 0, len(c.deletePodQueue))

	// delete matching IP and name
	c.deletePodQueue = c.deletePodQueue[:0]
	pod = &api_v1.Pod{}
	pod.Name = "podB"
	pod.Status.PodIP = "1.1.1.1"
	tsBeforeDelete := time.Now()
	c.handlePodDelete(pod)
	assert.Equal(t, 5, len(c.Pods))
	assert.Equal(t, 3, len(c.deletePodQueue))
	deleteRequest := c.deletePodQueue[0]
	assert.Equal(t, newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1"), deleteRequest.id)
	assert.Equal(t, "podB", deleteRequest.podName)
	assert.False(t, deleteRequest.ts.Before(tsBeforeDelete))
	assert.False(t, deleteRequest.ts.After(time.Now()))

	c.deletePodQueue = c.deletePodQueue[:0]
	pod = &api_v1.Pod{}
	pod.Name = "podC"
	pod.Status.PodIP = "2.2.2.2"
	pod.UID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	tsBeforeDelete = time.Now()
	c.handlePodDelete(pod)
	assert.Equal(t, 5, len(c.Pods))
	assert.Equal(t, 5, len(c.deletePodQueue))
	deleteRequest = c.deletePodQueue[0]
	assert.Equal(t, newPodIdentifier("connection", "k8s.pod.ip", "2.2.2.2"), deleteRequest.id)
	assert.Equal(t, "podC", deleteRequest.podName)
	assert.False(t, deleteRequest.ts.Before(tsBeforeDelete))
	assert.False(t, deleteRequest.ts.After(time.Now()))
	deleteRequest = c.deletePodQueue[1]
	assert.Equal(t, newPodIdentifier("resource_attribute", "k8s.pod.uid", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"), deleteRequest.id)
	assert.Equal(t, "podC", deleteRequest.podName)
	assert.False(t, deleteRequest.ts.Before(tsBeforeDelete))
	assert.False(t, deleteRequest.ts.After(time.Now()))
}

func TestNamespaceDelete(t *testing.T) {
	c, _ := newTestClient(t)
	namespaceAddAndUpdateTest(t, c, c.handleNamespaceAdd)
	assert.Equal(t, 2, len(c.Namespaces))
	assert.Equal(t, "namespaceA", c.Namespaces["namespaceA"].Name)

	// delete empty namespace
	c.handleNamespaceDelete(&api_v1.Namespace{})

	// delete non-existent namespace
	namespace := &api_v1.Namespace{}
	namespace.Name = "namespaceC"
	c.handleNamespaceDelete(namespace)
	assert.Equal(t, 2, len(c.Namespaces))
	got := c.Namespaces["namespaceA"]
	assert.Equal(t, "namespaceA", got.Name)
}

func TestDeleteQueue(t *testing.T) {
	c, _ := newTestClient(t)
	podAddAndUpdateTest(t, c, c.handlePodAdd)
	assert.Equal(t, 5, len(c.Pods))
	assert.Equal(t, "1.1.1.1", c.Pods[newPodIdentifier("connection", "k8s.pod.ip", "1.1.1.1")].Address)

	// delete pod
	pod := &api_v1.Pod{}
	pod.Name = "podB"
	pod.Status.PodIP = "1.1.1.1"
	c.handlePodDelete(pod)
	assert.Equal(t, 5, len(c.Pods))
	assert.Equal(t, 3, len(c.deletePodQueue))
}

func TestDeleteLoop(t *testing.T) {
	// go c.deleteLoop(time.Second * 1)
	c, _ := newTestClient(t)

	pod := &api_v1.Pod{}
	pod.Status.PodIP = "1.1.1.1"
	c.handlePodAdd(pod)
	assert.Equal(t, 2, len(c.Pods))
	assert.Equal(t, 0, len(c.deletePodQueue))

	c.handlePodDelete(pod)
	assert.Equal(t, 2, len(c.Pods))
	assert.Equal(t, 3, len(c.deletePodQueue))

	gracePeriod := time.Millisecond * 500
	go c.deleteLoop(time.Millisecond, gracePeriod)
	go func() {
		time.Sleep(time.Millisecond * 50)
		c.m.Lock()
		assert.Equal(t, 2, len(c.Pods))
		c.m.Unlock()
		c.deleteMut.Lock()
		assert.Equal(t, 3, len(c.deletePodQueue))
		c.deleteMut.Unlock()

		time.Sleep(gracePeriod + (time.Millisecond * 50))
		c.m.Lock()
		assert.Equal(t, 0, len(c.Pods))
		c.m.Unlock()
		c.deleteMut.Lock()
		assert.Equal(t, 0, len(c.deletePodQueue))
		c.deleteMut.Unlock()
		close(c.stopCh)
	}()
	<-c.stopCh
}

func TestGetIgnoredPod(t *testing.T) {
	c, _ := newTestClient(t)
	pod := &api_v1.Pod{}
	pod.Status.PodIP = "1.1.1.1"
	c.handlePodAdd(pod)
	c.Pods[newPodIdentifier("connection", "k8s.pod.ip", pod.Status.PodIP)].Ignore = true
	got, ok := c.GetPod(newPodIdentifier("connection", "k8s.pod.ip", pod.Status.PodIP))
	assert.Nil(t, got)
	assert.False(t, ok)
}

func TestHandlerWrongType(t *testing.T) {
	c, logs := newTestClientWithRulesAndFilters(t, ExtractionRules{}, Filters{})
	assert.Equal(t, 0, logs.Len())
	c.handlePodAdd(1)
	c.handlePodDelete(1)
	c.handlePodUpdate(1, 2)
	assert.Equal(t, 3, logs.Len())
	for _, l := range logs.All() {
		assert.Equal(t, "object received was not of type api_v1.Pod", l.Message)
	}
}

func TestExtractionRules(t *testing.T) {
	c, _ := newTestClientWithRulesAndFilters(t, ExtractionRules{}, Filters{})
	// Disable saving ip into k8s.pod.ip
	c.Associations[0].Sources[0].Name = ""

	pod := &api_v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:              "auth-service-abc12-xyz3",
			UID:               "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			Namespace:         "ns1",
			CreationTimestamp: meta_v1.Now(),
			Labels: map[string]string{
				"label1": "lv1",
				"label2": "k1=v1 k5=v5 extra!",
			},
			Annotations: map[string]string{
				"annotation1": "av1",
			},
			OwnerReferences: []meta_v1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       "auth-service-66f5996c7c",
					UID:        "207ea729-c779-401d-8347-008ecbc137e3",
				},
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Name:       "auth-daemonset",
					UID:        "c94d3814-2253-427a-ab13-2cf609e4dafa",
				},
				{
					APIVersion: "batch/v1",
					Kind:       "Job",
					Name:       "auth-cronjob-27667920",
					UID:        "59f27ac1-5c71-42e5-abe9-2c499d603706",
				},
				{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       "pi-statefulset",
					UID:        "03755eb1-6175-47d5-afd5-05cfc30244d7",
				},
			},
		},
		Spec: api_v1.PodSpec{
			NodeName: "node1",
		},
		Status: api_v1.PodStatus{
			PodIP: "1.1.1.1",
		},
	}

	testCases := []struct {
		name       string
		rules      ExtractionRules
		attributes map[string]string
	}{{
		name:       "no-rules",
		rules:      ExtractionRules{},
		attributes: nil,
	}, {
		name: "deployment",
		rules: ExtractionRules{
			DeploymentName: true,
		},
		attributes: map[string]string{
			"k8s.deployment.name": "auth-service",
		},
	}, {
		name: "replicasetId",
		rules: ExtractionRules{
			ReplicaSetUID: true,
		},
		attributes: map[string]string{
			"k8s.replicaset.uid": "207ea729-c779-401d-8347-008ecbc137e3",
		},
	}, {
		name: "replicasetName",
		rules: ExtractionRules{
			ReplicaSetName: true,
		},
		attributes: map[string]string{
			"k8s.replicaset.name": "auth-service-66f5996c7c",
		},
	}, {
		name: "daemonsetUID",
		rules: ExtractionRules{
			DaemonSetUID: true,
		},
		attributes: map[string]string{
			"k8s.daemonset.uid": "c94d3814-2253-427a-ab13-2cf609e4dafa",
		},
	}, {
		name: "daemonsetName",
		rules: ExtractionRules{
			DaemonSetName: true,
		},
		attributes: map[string]string{
			"k8s.daemonset.name": "auth-daemonset",
		},
	}, {
		name: "jobUID",
		rules: ExtractionRules{
			JobUID: true,
		},
		attributes: map[string]string{
			"k8s.job.uid": "59f27ac1-5c71-42e5-abe9-2c499d603706",
		},
	}, {
		name: "jobName",
		rules: ExtractionRules{
			JobName: true,
		},
		attributes: map[string]string{
			"k8s.job.name": "auth-cronjob-27667920",
		},
	}, {
		name: "cronJob",
		rules: ExtractionRules{
			CronJobName: true,
		},
		attributes: map[string]string{
			"k8s.cronjob.name": "auth-cronjob",
		},
	}, {
		name: "statefulsetUID",
		rules: ExtractionRules{
			StatefulSetUID: true,
		},
		attributes: map[string]string{
			"k8s.statefulset.uid": "03755eb1-6175-47d5-afd5-05cfc30244d7",
		},
	}, {
		name: "jobName",
		rules: ExtractionRules{
			StatefulSetName: true,
		},
		attributes: map[string]string{
			"k8s.statefulset.name": "pi-statefulset",
		},
	}, {
		name: "metadata",
		rules: ExtractionRules{
			DeploymentName: true,
			NamespaceName:  true,
			PodName:        true,
			PodUID:         true,
			Node:           true,
			StartTime:      true,
		},
		attributes: map[string]string{
			"k8s.deployment.name": "auth-service",
			"k8s.namespace.name":  "ns1",
			"k8s.node.name":       "node1",
			"k8s.pod.name":        "auth-service-abc12-xyz3",
			"k8s.pod.uid":         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			"k8s.pod.start_time":  pod.GetCreationTimestamp().String(),
		},
	}, {
		name: "labels",
		rules: ExtractionRules{
			Annotations: []FieldExtractionRule{{
				Name: "a1",
				Key:  "annotation1",
				From: MetadataFromPod,
			},
			},
			Labels: []FieldExtractionRule{{
				Name: "l1",
				Key:  "label1",
				From: MetadataFromPod,
			}, {
				Name:  "l2",
				Key:   "label2",
				Regex: regexp.MustCompile(`k5=(?P<value>[^\s]+)`),
				From:  MetadataFromPod,
			},
			},
		},
		attributes: map[string]string{
			"l1": "lv1",
			"l2": "v5",
			"a1": "av1",
		},
	}, {
		// By default if the From field is not set for labels and annotations we want to extract them from pod
		name: "labels-annotations-default-pod",
		rules: ExtractionRules{
			Annotations: []FieldExtractionRule{{
				Name: "a1",
				Key:  "annotation1",
			},
			},
			Labels: []FieldExtractionRule{{
				Name: "l1",
				Key:  "label1",
			}, {
				Name:  "l2",
				Key:   "label2",
				Regex: regexp.MustCompile(`k5=(?P<value>[^\s]+)`),
			},
			},
		},
		attributes: map[string]string{
			"l1": "lv1",
			"l2": "v5",
			"a1": "av1",
		},
	},
		{
			name: "all-labels",
			rules: ExtractionRules{
				Labels: []FieldExtractionRule{{
					KeyRegex: regexp.MustCompile("^(?:la.*)$"),
					From:     MetadataFromPod,
				},
				},
			},
			attributes: map[string]string{
				"k8s.pod.labels.label1": "lv1",
				"k8s.pod.labels.label2": "k1=v1 k5=v5 extra!",
			},
		},
		{
			name: "all-annotations",
			rules: ExtractionRules{
				Annotations: []FieldExtractionRule{{
					KeyRegex: regexp.MustCompile("^(?:an.*)$"),
					From:     MetadataFromPod,
				},
				},
			},
			attributes: map[string]string{
				"k8s.pod.annotations.annotation1": "av1",
			},
		},
		{
			name: "all-annotations-not-match",
			rules: ExtractionRules{
				Annotations: []FieldExtractionRule{{
					KeyRegex: regexp.MustCompile("^(?:an*)$"),
					From:     MetadataFromPod,
				},
				},
			},
			attributes: map[string]string{},
		},
		{
			name: "captured-groups",
			rules: ExtractionRules{
				Annotations: []FieldExtractionRule{{
					Name:                 "$1",
					KeyRegex:             regexp.MustCompile(`^(?:annotation(\d+))$`),
					HasKeyRegexReference: true,
					From:                 MetadataFromPod,
				},
				},
			},
			attributes: map[string]string{
				"1": "av1",
			},
		},
		{
			name: "captured-groups-$0",
			rules: ExtractionRules{
				Annotations: []FieldExtractionRule{{
					Name:                 "prefix-$0",
					KeyRegex:             regexp.MustCompile(`^(?:annotation(\d+))$`),
					HasKeyRegexReference: true,
					From:                 MetadataFromPod,
				},
				},
			},
			attributes: map[string]string{
				"prefix-annotation1": "av1",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c.Rules = tc.rules
			c.handlePodAdd(pod)
			p, ok := c.GetPod(newPodIdentifier("connection", "", pod.Status.PodIP))
			require.True(t, ok)

			assert.Equal(t, len(tc.attributes), len(p.Attributes))
			for k, v := range tc.attributes {
				got, ok := p.Attributes[k]
				assert.True(t, ok)
				assert.Equal(t, v, got)
			}
		})
	}
}

func TestNamespaceExtractionRules(t *testing.T) {
	c, _ := newTestClientWithRulesAndFilters(t, ExtractionRules{}, Filters{})

	namespace := &api_v1.Namespace{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:              "auth-service-namespace-abc12-xyz3",
			UID:               "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			CreationTimestamp: meta_v1.Now(),
			Labels: map[string]string{
				"label1": "lv1",
			},
			Annotations: map[string]string{
				"annotation1": "av1",
			},
		},
	}

	testCases := []struct {
		name       string
		rules      ExtractionRules
		attributes map[string]string
	}{{
		name:       "no-rules",
		rules:      ExtractionRules{},
		attributes: nil,
	}, {
		name: "labels",
		rules: ExtractionRules{
			Annotations: []FieldExtractionRule{{
				Name: "a1",
				Key:  "annotation1",
				From: MetadataFromNamespace,
			},
			},
			Labels: []FieldExtractionRule{{
				Name: "l1",
				Key:  "label1",
				From: MetadataFromNamespace,
			},
			},
		},
		attributes: map[string]string{
			"l1": "lv1",
			"a1": "av1",
		},
	},
		{
			name: "all-labels",
			rules: ExtractionRules{
				Labels: []FieldExtractionRule{{
					KeyRegex: regexp.MustCompile("^(?:la.*)$"),
					From:     MetadataFromNamespace,
				},
				},
			},
			attributes: map[string]string{
				"k8s.namespace.labels.label1": "lv1",
			},
		},
		{
			name: "all-annotations",
			rules: ExtractionRules{
				Annotations: []FieldExtractionRule{{
					KeyRegex: regexp.MustCompile("^(?:an.*)$"),
					From:     MetadataFromNamespace,
				},
				},
			},
			attributes: map[string]string{
				"k8s.namespace.annotations.annotation1": "av1",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c.Rules = tc.rules
			c.handleNamespaceAdd(namespace)
			p, ok := c.GetNamespace(namespace.Name)
			require.True(t, ok)

			assert.Equal(t, len(tc.attributes), len(p.Attributes))
			for k, v := range tc.attributes {
				got, ok := p.Attributes[k]
				assert.True(t, ok)
				assert.Equal(t, v, got)
			}
		})
	}
}

func TestFilters(t *testing.T) {
	testCases := []struct {
		name    string
		filters Filters
		labels  string
		fields  string
	}{{
		name:    "no-filters",
		filters: Filters{},
	}, {
		name: "namespace",
		filters: Filters{
			Namespace: "default",
		},
	}, {
		name: "node",
		filters: Filters{
			Node: "ec2-test",
		},
		fields: "spec.nodeName=ec2-test",
	}, {
		name: "labels-and-fields",
		filters: Filters{
			Labels: []FieldFilter{
				{
					Key:   "k1",
					Value: "v1",
					Op:    selection.Equals,
				},
				{
					Key:   "k2",
					Value: "v2",
					Op:    selection.NotEquals,
				},
			},
			Fields: []FieldFilter{
				{
					Key:   "k1",
					Value: "v1",
					Op:    selection.Equals,
				},
				{
					Key:   "k2",
					Value: "v2",
					Op:    selection.NotEquals,
				},
			},
		},
		labels: "k1=v1,k2!=v2",
		fields: "k1=v1,k2!=v2",
	},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClientWithRulesAndFilters(t, ExtractionRules{}, tc.filters)
			inf := c.podInformer.(*FakeInformer)
			assert.Equal(t, tc.filters.Namespace, inf.namespace)
			assert.Equal(t, tc.labels, inf.labelSelector.String())
			assert.Equal(t, tc.fields, inf.fieldSelector.String())
		})
	}

}

func TestPodIgnorePatterns(t *testing.T) {
	testCases := []struct {
		ignore bool
		pod    api_v1.Pod
	}{{
		ignore: false,
		pod:    api_v1.Pod{},
	}, {
		ignore: false,
		pod: api_v1.Pod{
			Spec: api_v1.PodSpec{
				HostNetwork: true,
			},
		},
	}, {
		ignore: true,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Annotations: map[string]string{
					"opentelemetry.io/k8s-processor/ignore": "True ",
				},
			},
		},
	}, {
		ignore: true,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Annotations: map[string]string{
					"opentelemetry.io/k8s-processor/ignore": "true",
				},
			},
		},
	}, {
		ignore: false,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Annotations: map[string]string{
					"opentelemetry.io/k8s-processor/ignore": "false",
				},
			},
		},
	}, {
		ignore: false,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Annotations: map[string]string{
					"opentelemetry.io/k8s-processor/ignore": "",
				},
			},
		},
	}, {
		ignore: true,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "jaeger-agent",
			},
		},
	}, {
		ignore: true,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "jaeger-collector",
			},
		},
	}, {
		ignore: true,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "jaeger-agent-b2zdv",
			},
		},
	}, {
		ignore: false,
		pod: api_v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "test-pod-name",
			},
		},
	},
	}

	c, _ := newTestClient(t)
	for _, tc := range testCases {
		assert.Equal(t, tc.ignore, c.shouldIgnorePod(&tc.pod))
	}
}

func Test_extractPodContainersAttributes(t *testing.T) {
	pod := api_v1.Pod{
		Spec: api_v1.PodSpec{
			Containers: []api_v1.Container{
				{
					Name:  "container1",
					Image: "test/image1:0.1.0",
				},
				{
					Name:  "container2",
					Image: "test/image2:0.2.0",
				},
			},
			InitContainers: []api_v1.Container{
				{
					Name:  "init_container",
					Image: "test/init-image:1.0.2",
				},
			},
		},
		Status: api_v1.PodStatus{
			ContainerStatuses: []api_v1.ContainerStatus{
				{
					Name:         "container1",
					ContainerID:  "docker://container1-id-123",
					RestartCount: 0,
				},
				{
					Name:         "container2",
					ContainerID:  "docker://container2-id-456",
					RestartCount: 2,
				},
			},
			InitContainerStatuses: []api_v1.ContainerStatus{
				{
					Name:         "init_container",
					ContainerID:  "containerd://init-container-id-123",
					RestartCount: 0,
				},
			},
		},
	}
	tests := []struct {
		name  string
		rules ExtractionRules
		pod   api_v1.Pod
		want  map[string]*Container
	}{
		{
			name: "no-data",
			rules: ExtractionRules{
				ContainerImageName: true,
				ContainerImageTag:  true,
				ContainerID:        true,
			},
			pod:  api_v1.Pod{},
			want: map[string]*Container{},
		},
		{
			name:  "no-rules",
			rules: ExtractionRules{},
			pod:   pod,
			want:  map[string]*Container{},
		},
		{
			name: "image-name-only",
			rules: ExtractionRules{
				ContainerImageName: true,
			},
			pod: pod,
			want: map[string]*Container{
				"container1":     {ImageName: "test/image1"},
				"container2":     {ImageName: "test/image2"},
				"init_container": {ImageName: "test/init-image"},
			},
		},
		{
			name: "no-image-tag-available",
			rules: ExtractionRules{
				ContainerImageName: true,
			},
			pod: api_v1.Pod{
				Spec: api_v1.PodSpec{
					Containers: []api_v1.Container{
						{
							Name:  "test-container",
							Image: "test/image",
						},
					},
				},
			},
			want: map[string]*Container{
				"test-container": {ImageName: "test/image"},
			},
		},
		{
			name: "container-id-only",
			rules: ExtractionRules{
				ContainerID: true,
			},
			pod: pod,
			want: map[string]*Container{
				"container1": {
					Statuses: map[int]ContainerStatus{
						0: {ContainerID: "container1-id-123"},
					},
				},
				"container2": {
					Statuses: map[int]ContainerStatus{
						2: {ContainerID: "container2-id-456"},
					},
				},
				"init_container": {
					Statuses: map[int]ContainerStatus{
						0: {ContainerID: "init-container-id-123"},
					},
				},
			},
		},
		{
			name: "all-container-attributes",
			rules: ExtractionRules{
				ContainerImageName: true,
				ContainerImageTag:  true,
				ContainerID:        true,
			},
			pod: pod,
			want: map[string]*Container{
				"container1": {
					ImageName: "test/image1",
					ImageTag:  "0.1.0",
					Statuses: map[int]ContainerStatus{
						0: {ContainerID: "container1-id-123"},
					},
				},
				"container2": {
					ImageName: "test/image2",
					ImageTag:  "0.2.0",
					Statuses: map[int]ContainerStatus{
						2: {ContainerID: "container2-id-456"},
					},
				},
				"init_container": {
					ImageName: "test/init-image",
					ImageTag:  "1.0.2",
					Statuses: map[int]ContainerStatus{
						0: {ContainerID: "init-container-id-123"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := WatchClient{Rules: tt.rules}
			assert.Equal(t, tt.want, c.extractPodContainersAttributes(&tt.pod))
		})
	}
}

func Test_extractField(t *testing.T) {
	type args struct {
		v string
		r FieldExtractionRule
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"no-regex",
			args{
				"str",
				FieldExtractionRule{Regex: nil},
			},
			"str",
		},
		{
			"basic",
			args{
				"str",
				FieldExtractionRule{Regex: regexp.MustCompile("field=(?P<value>.+)")},
			},
			"",
		},
		{
			"basic",
			args{
				"field=val1",
				FieldExtractionRule{Regex: regexp.MustCompile("field=(?P<value>.+)")},
			},
			"val1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.args.r.extractField(tt.args.v); got != tt.want {
				t.Errorf("extractField() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorSelectorsFromFilters(t *testing.T) {
	tests := []struct {
		name    string
		filters Filters
	}{
		{
			name: "label/invalid-op",
			filters: Filters{
				Labels: []FieldFilter{{Op: "invalid-op"}},
			},
		},
		{
			name: "fields/invalid-op",
			filters: Filters{
				Fields: []FieldFilter{{Op: selection.Exists}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := selectorsFromFilters(tt.filters)
			assert.Error(t, err)
		})
	}
}

func TestExtractNamespaceLabelsAnnotations(t *testing.T) {
	c, _ := newTestClientWithRulesAndFilters(t, ExtractionRules{}, Filters{})
	testCases := []struct {
		name                   string
		shouldExtractNamespace bool
		rules                  ExtractionRules
	}{{
		name:                   "empty-rules",
		shouldExtractNamespace: false,
		rules:                  ExtractionRules{},
	}, {
		name:                   "pod-rules",
		shouldExtractNamespace: false,
		rules: ExtractionRules{
			Annotations: []FieldExtractionRule{{
				Name: "a1",
				Key:  "annotation1",
				From: MetadataFromPod,
			},
			},
			Labels: []FieldExtractionRule{{
				Name: "l1",
				Key:  "label1",
				From: MetadataFromPod,
			},
			},
		},
	}, {
		name:                   "namespace-rules-only-annotations",
		shouldExtractNamespace: true,
		rules: ExtractionRules{
			Annotations: []FieldExtractionRule{{
				Name: "a1",
				Key:  "annotation1",
				From: MetadataFromNamespace,
			},
			},
		},
	}, {
		name:                   "namespace-rules-only-labels",
		shouldExtractNamespace: true,
		rules: ExtractionRules{
			Labels: []FieldExtractionRule{{
				Name: "l1",
				Key:  "label1",
				From: MetadataFromNamespace,
			},
			},
		},
	},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c.Rules = tc.rules
			assert.Equal(t, tc.shouldExtractNamespace, c.extractNamespaceLabelsAnnotations())
		})
	}
}

func newTestClientWithRulesAndFilters(t *testing.T, e ExtractionRules, f Filters) (*WatchClient, *observer.ObservedLogs) {
	observedLogger, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(observedLogger)
	exclude := Excludes{
		Pods: []ExcludePods{
			{Name: regexp.MustCompile(`jaeger-agent`)},
			{Name: regexp.MustCompile(`jaeger-collector`)},
		},
	}
	associations := []Association{
		{
			Sources: []AssociationSource{
				{
					From: "connection",
				},
			},
		},
		{
			Sources: []AssociationSource{
				{
					From: "resource_attribute",
					Name: "k8s.pod.uid",
				},
			},
		},
	}
	c, err := New(logger, k8sconfig.APIConfig{}, e, f, associations, exclude, newFakeAPIClientset, NewFakeInformer, NewFakeInformer, NewFakeInformer, NewFakeNamespaceInformer)
	require.NoError(t, err)
	return c.(*WatchClient), logs
}

func newTestClient(t *testing.T) (*WatchClient, *observer.ObservedLogs) {
	return newTestClientWithRulesAndFilters(t, ExtractionRules{}, Filters{})
}
