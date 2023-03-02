/*
Copyright 2023 Lumigo.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IMPORTANT: Run "make" to regenerate code after modifying this file

const (
	LumigoResourceFinalizer = "operator.lumigo.io/lumigo-finalizer"
)

// Lumigo is the Schema for the lumigoes API
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Lumigo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LumigoSpec   `json:"spec,omitempty"`
	Status LumigoStatus `json:"status,omitempty"`
}

// LumigoList contains a list of Lumigo
// +kubebuilder:object:root=true
type LumigoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Lumigo `json:"items"`
}

// LumigoSpec defines the desired state of Lumigo
type LumigoSpec struct {
	// The Lumigo token to be used to authenticate against Lumigo.
	// For info on how to retrieve your Lumigo token, refer to:
	// https://docs.lumigo.io/docs/lumigo-tokens
	LumigoToken Credentials `json:"lumigoToken,omitempty"`
	Tracing     TracingSpec `json:"tracing,omitempty"`
}

type Credentials struct {
	// Reference to a Kubernetes secret that contains the credentials
	// for Lumigo. The secret must be in the same namespace as the
	// LumigoSpec referencing it.
	SecretRef KubernetesSecretRef `json:"secretRef,omitempty"`
}

type KubernetesSecretRef struct {
	// Name of a Kubernetes secret.
	Name string `json:"name"`
	// Key of the Kubernetes secret that contains the credential data.
	Key string `json:"key,omitempty"`
}

// TracingSpec specified how distributed tracing (for example: tracer injection)
// should be set up by the operator
type TracingSpec struct {
	Injection InjectionSpec `json:"injection"`
}

type InjectionSpec struct {
	// Whether Daemonsets, Deployments, ReplicaSets, StatefulSets, CronJobs and Jobs
	// that are created or updated after the creation of the Lumigo resource be injected.
	// If unspecified, defaults to `true`
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled"` // Using a pointer to support cases where the value is not set (and it counts as enabled)

	// Whether Daemonsets, Deployments, ReplicaSets, StatefulSets, CronJobs and Jobs
	// that already exist when the Lumigo resource is created, will be updated with
	// injection.
	// If unspecified, defaults to `true`. It requires `Enabled` to be set to `true`.
	// +kubebuilder:validation:Optional
	InjectLumigoIntoExistingResourcesOnCreation *bool `json:"injectLumigoIntoExistingResourcesOnCreation,omitempty"`

	// Whether Daemonsets, Deployments, ReplicaSets, StatefulSets, CronJobs and Jobs
	// that are injected with Lumigo will be updated to remove the injection when the
	// Lumigo resource is deleted.
	// If unspecified, defaults to `true`. It requires `Enabled` to be set to `true`.
	// +kubebuilder:validation:Optional
	RemoveLumigoFromResourcesOnDeletion *bool `json:"removeLumigoFromResourcesOnDeletion,omitempty"`
}

// LumigoStatus defines the observed state of Lumigo
type LumigoStatus struct {
	// The status of single Lumigo resources
	Conditions []LumigoCondition `json:"conditions"`
}

type LumigoCondition struct {
	Type               LumigoConditionType    `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastUpdateTime     metav1.Time            `json:"lastUpdateTime"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime"`
	Message            string                 `json:"message"`
}

type LumigoConditionType string

const (
	LumigoConditionTypeActive LumigoConditionType = "Active"
	LumigoConditionTypeError  LumigoConditionType = "Error"
)

func init() {
	SchemeBuilder.Register(&Lumigo{}, &LumigoList{})
}
