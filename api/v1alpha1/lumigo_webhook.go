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
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	decoder = scheme.Codecs.UniversalDecoder()
)

func (r *Lumigo) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: &lumigoWebhookHandler{},
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/mutate", handler)

	return nil
}

// +kubebuilder:webhook:path=/v1alpha1/mutate,mutating=true,failurePolicy=fail,sideEffects=None,groups=apps,resources=daemonsets;deployments;replicasets;statefulsets,verbs=create;update,versions=v1;v1beta1,name=lumigoinjector.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/v1alpha1/mutate,mutating=true,failurePolicy=fail,sideEffects=None,groups=batch,resources=cronjobs;jobs,verbs=create;update,versions=v1;v1beta1,name=lumigoinjector.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/v1alpha1/mutate,mutating=true,failurePolicy=fail,sideEffects=None,groups=,resources=pods,verbs=create;update,versions=v1;v1beta1,name=lumigoinjector.kb.io,admissionReviewVersions=v1
type lumigoWebhookHandler struct {
	client  client.Client
	decoder *admission.Decoder
}

// The client is automatically injected by the Webhook machinery
func (v *lumigoWebhookHandler) InjectClient(c client.Client) error {
	v.client = c
	return nil
}

// The decoder is automatically injected by the Webhook machinery
func (v *lumigoWebhookHandler) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

func (h *lumigoWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	log := logf.Log.WithName("lumigo-injector-webhook").WithValues("resource_gvk", request.Kind)

	if request.Operation == admissionv1.Delete {
		// Nothing to mutate on deletions
		return admission.Allowed("Mutating webhooks have nothing to do on deletions")
	}

	resourceAdaper, err := newResourceAdatper(request.Kind, request.Object.Raw)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error while parsing the resource: %w", err))
	}

	if resourceAdaper == nil {
		// Resource not supported
		return admission.Allowed(fmt.Sprintf("The Lumigo Injector webhook does not mutate resources of type %s", request.Kind))
	}

	namespace := resourceAdaper.GetNamespace()

	// Check if we have a Lumigo instance in the object's namespace
	lumigos := &LumigoList{}
	if err := h.client.List(ctx, lumigos, &client.ListOptions{
		Namespace: namespace,
	}); err != nil {
		if apierrors.IsNotFound(err) {
			// TODO The Lumigo CRD is not register. Catastrophic error?
			return admission.Allowed(fmt.Sprintf("No Lumigo configuration for the '%s' namespace; resource will not be mutated", namespace))
		}

		log.Error(err, "failed to retrieve Lumigo instance in namespace", "namespace", namespace)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("cannot retrieve Lumigo instances in namespace %s: %w", namespace, err))
	}

	if len(lumigos.Items) < 1 {
		return admission.Allowed(fmt.Sprintf("No Lumigo configuration for the '%s' namespace; resource will not be mutated", namespace))
	}

	// TODO Mutate
	resourceAdaper.GetObjectMeta().Labels["lumigo.auto-trace"] = "true"

	marshalled, err := resourceAdaper.Marshal()
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("cannot marshal object %w", err))
	}

	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

type supportedResourceTypes interface {
	appsv1.DaemonSet | appsv1.Deployment | appsv1.ReplicaSet | appsv1.StatefulSet | batchv1.CronJob | batchv1.Job
}

type resourceAdapter interface {
	GetNamespace() string
	GetObjectMeta() metav1.ObjectMeta
	GetPodSpec() corev1.PodSpec
	Marshal() ([]byte, error)
}

// Inline implementation of resourceAdapter inspired by https://stackoverflow.com/a/43420100/6188451
type resourceAdapterImpl[T supportedResourceTypes] struct {
	resource *T

	getNamespace  func() string
	getObjectMeta func() metav1.ObjectMeta
	getPodSpec    func() corev1.PodSpec
}

func (r *resourceAdapterImpl[T]) GetNamespace() string {
	return r.getNamespace()
}

func (r *resourceAdapterImpl[T]) GetObjectMeta() metav1.ObjectMeta {
	return r.getObjectMeta()
}

func (r *resourceAdapterImpl[T]) GetPodSpec() corev1.PodSpec {
	return r.getPodSpec()
}

func (r *resourceAdapterImpl[T]) Marshal() ([]byte, error) {
	return json.Marshal(r.resource)
}

func newResourceAdatper(gvk metav1.GroupVersionKind, raw []byte) (resourceAdapter, error) {
	sGVK := fmt.Sprintf("%s/%s.%s", gvk.Group, gvk.Version, gvk.Kind)
	switch sGVK {
	case "apps/v1.Daemonset":
		{
			resource := &appsv1.DaemonSet{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl[appsv1.DaemonSet]{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() metav1.ObjectMeta {
					return resource.ObjectMeta
				},
				getPodSpec: func() corev1.PodSpec {
					return resource.Spec.Template.Spec
				},
			}, nil
		}
	case "apps/v1.Deployment":
		{
			resource := &appsv1.Deployment{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl[appsv1.Deployment]{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() metav1.ObjectMeta {
					return resource.ObjectMeta
				},
				getPodSpec: func() corev1.PodSpec {
					return resource.Spec.Template.Spec
				},
			}, nil
		}
	case "apps/v1.StatefulSet":
		{
			resource := &appsv1.StatefulSet{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl[appsv1.StatefulSet]{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() metav1.ObjectMeta {
					return resource.ObjectMeta
				},
				getPodSpec: func() corev1.PodSpec {
					return resource.Spec.Template.Spec
				},
			}, nil
		}
	case "apps/v1.ReplicaSet":
		{
			resource := &appsv1.ReplicaSet{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl[appsv1.ReplicaSet]{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() metav1.ObjectMeta {
					return resource.ObjectMeta
				},
				getPodSpec: func() corev1.PodSpec {
					return resource.Spec.Template.Spec
				},
			}, nil
		}
	case "batch/v1.CronJob":
		{
			resource := &batchv1.CronJob{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl[batchv1.CronJob]{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() metav1.ObjectMeta {
					return resource.ObjectMeta
				},
				getPodSpec: func() corev1.PodSpec {
					return resource.Spec.JobTemplate.Spec.Template.Spec
				},
			}, nil
		}
	case "batch/v1.Job":
		{
			resource := &batchv1.Job{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl[batchv1.Job]{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() metav1.ObjectMeta {
					return resource.ObjectMeta
				},
				getPodSpec: func() corev1.PodSpec {
					return resource.Spec.Template.Spec
				},
			}, nil
		}
	default:
		{
			return nil, nil
		}
	}

}
