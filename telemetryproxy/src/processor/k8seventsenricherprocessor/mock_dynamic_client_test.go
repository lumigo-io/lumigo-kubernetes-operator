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

package k8seventsenricherprocessor

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
		{Group: "", Version: "v1", Resource: "pods"}:            "PodList",
		{Group: "", Version: "appsv1", Resource: "deployments"}: "DeploymentList",
		{Group: "", Version: "appsv1", Resource: "replicasets"}: "ReplicasetList",
		{Group: "", Version: "v1", Resource: "events"}:          "EventList",
	}

	fakeClient := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, objs...)
	return mockDynamicClient{
		client: fakeClient,
	}

}

func (c mockDynamicClient) getMockDynamicClient() (dynamic.Interface, error) {
	return c.client, nil
}

func (c mockDynamicClient) createDeployment(deployment *appsv1.Deployment) {
	deployments := c.client.Resource(schema.GroupVersionResource{
		Version:  "apps/v1",
		Resource: "deployments",
	})
	raw, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	_, _ = deployments.Namespace(deployment.GetNamespace()).Create(context.Background(), &unstructured.Unstructured{
		Object: raw,
	}, v1.CreateOptions{})
}

func (c mockDynamicClient) createReplicaSet(replicaSet *appsv1.ReplicaSet) {
	replicaSets := c.client.Resource(schema.GroupVersionResource{
		Version:  "apps/v1",
		Resource: "replicasets",
	})
	raw, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(replicaSet)
	_, _ = replicaSets.Namespace(replicaSet.GetNamespace()).Create(context.Background(), &unstructured.Unstructured{
		Object: raw,
	}, v1.CreateOptions{})
}

func (c mockDynamicClient) createPod(pod *corev1.Pod) {
	pods := c.client.Resource(schema.GroupVersionResource{
		Version:  "v1",
		Resource: "pods",
	})
	raw, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	_, _ = pods.Namespace(pod.GetNamespace()).Create(context.Background(), &unstructured.Unstructured{
		Object: raw,
	}, v1.CreateOptions{})
}

func generateDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID(uuid.New().String()),
		},
	}
}

func generateDeploymentReplicaSet(deployment *appsv1.Deployment) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: v1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      deployment.GetName() + "-" + fmt.Sprintf("%06d", rand.Int31n(999999)),
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: deployment.APIVersion,
					Kind:       deployment.Kind,
					Name:       deployment.ObjectMeta.Name,
					UID:        deployment.ObjectMeta.UID,
				},
			},
			UID: types.UID(uuid.New().String()),
		},
	}
}

func generateReplicaSetPod(replicaSet *appsv1.ReplicaSet) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Namespace: replicaSet.ObjectMeta.Namespace,
			Name:      replicaSet.ObjectMeta.Name + "-" + fmt.Sprintf("%06d", rand.Int31n(999999)),
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: replicaSet.APIVersion,
					Kind:       replicaSet.Kind,
					Name:       replicaSet.ObjectMeta.Name,
					UID:        replicaSet.ObjectMeta.UID,
				},
			},
			UID: types.UID(uuid.New().String()),
		},
	}
}

func generateEvent(reason, eventType string, involvedObject corev1.ObjectReference) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: v1.ObjectMeta{
			Namespace: involvedObject.Namespace,
			Name:      involvedObject.Name + "." + fmt.Sprintf("%06d", rand.Int31n(999999)),
			UID:       types.UID(uuid.New().String()),
		},
		InvolvedObject: involvedObject,
		Reason:         reason,
		Type:           eventType,
	}
}
