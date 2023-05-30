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

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
)

type mockDynamicClient struct {
	client dynamic.Interface
}

func newMockDynamicClient() mockDynamicClient {
	scheme := runtime.NewScheme()
	objs := []runtime.Object{}

	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "", Version: "v1", Resource: "pods"}:   "PodList",
		{Group: "", Version: "v1", Resource: "events"}: "EventList",
	}

	fakeClient := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, objs...)
	return mockDynamicClient{
		client: fakeClient,
	}

}

func (c mockDynamicClient) getMockDynamicClient() (dynamic.Interface, error) {
	return c.client, nil
}

func (c mockDynamicClient) createEvents(objects ...*unstructured.Unstructured) {
	events := c.client.Resource(schema.GroupVersionResource{
		Version:  "v1",
		Resource: "events",
	})
	for _, event := range objects {
		_, _ = events.Namespace(event.GetNamespace()).Create(context.Background(), event, v1.CreateOptions{})
	}
}

func (c mockDynamicClient) createPods(objects ...*unstructured.Unstructured) {
	pods := c.client.Resource(schema.GroupVersionResource{
		Version:  "v1",
		Resource: "pods",
	})
	for _, pod := range objects {
		_, _ = pods.Namespace(pod.GetNamespace()).Create(context.Background(), pod, v1.CreateOptions{})
	}
}

func generateEvent(name, namespace string, labels map[string]interface{}, unstructuredInvolvedObject *unstructured.Unstructured) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Events",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
				"labels":    labels,
			},
			"involvedObject": map[string]interface{}{
				"apiVersion":      unstructuredInvolvedObject.GetAPIVersion(),
				"kind":            unstructuredInvolvedObject.GetKind(),
				"namespace":       unstructuredInvolvedObject.GetNamespace(),
				"name":            unstructuredInvolvedObject.GetName(),
				"uid":             string(unstructuredInvolvedObject.GetUID()),
				"resourceVersion": unstructuredInvolvedObject.GetResourceVersion(),
			},
		},
	}

}

func generatePod(name, namespace string, labels map[string]interface{}, resourceVersion string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pods",
			"metadata": map[string]interface{}{
				"namespace":       namespace,
				"name":            name,
				"labels":          labels,
				"resourceVersion": resourceVersion,
				"uid":             name + "4567389457638945",
			},
		},
	}

}
