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

package injector

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

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers/conditions"
	"github.com/lumigo-io/lumigo-kubernetes-operator/mutation"
)

var (
	decoder = scheme.Codecs.UniversalDecoder()
)

type LumigoInjectorWebhookHandler struct {
	client                       client.Client
	decoder                      *admission.Decoder
	LumigoOperatorVersion        string
	LumigoInjectorImage          string
	TelemetryProxyOtlpServiceUrl string
	Log                          logr.Logger
}

func (h *LumigoInjectorWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/inject", handler)

	return nil
}

// The client is automatically injected by the Webhook machinery
func (h *LumigoInjectorWebhookHandler) InjectClient(c client.Client) error {
	h.client = c
	return nil
}

// The decoder is automatically injected by the Webhook machinery
func (h *LumigoInjectorWebhookHandler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}

func (h *LumigoInjectorWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
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
	lumigos := &operatorv1alpha1.LumigoList{}
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

	lumigo := lumigos.Items[0]
	// Check if tracer injection is enabled (so the injection _should_ be performed)
	enabled := lumigo.Spec.Tracing.Injection.Enabled
	if enabled != nil && !*enabled {
		return admission.Allowed(fmt.Sprintf("Tracing injection is disabled in the '%s' namespace; resource will not be mutated", namespace))
	}

	if err := validateMutationShouldOccur(&lumigo); err != nil {
		// If we find a reason why the mutation should not occur, pass that as reason why the admission is allowed without modifications
		return admission.Allowed(err.Error())
	}

	mutator, err := mutation.NewMutator(&log, &lumigo.Spec.LumigoToken, h.LumigoOperatorVersion, h.LumigoInjectorImage, h.TelemetryProxyOtlpServiceUrl)
	if err != nil {
		return admission.Allowed(fmt.Errorf("cannot instantiate mutator: %w", err).Error())
	}

	if err = resourceAdaper.Mutate(mutator); err != nil {
		return admission.Allowed(fmt.Errorf("cannot inject Lumigo tracing in the pod spec %w", err).Error())
	}

	marshalled, err := resourceAdaper.Marshal()
	if err != nil {
		return admission.Allowed(fmt.Errorf("cannot marshal object %w", err).Error())
	}

	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

type resourceAdapter interface {
	GetNamespace() string
	Mutate(mutation.Mutator) error
	Marshal() ([]byte, error)
}

type supportedResourceTypes interface {
	appsv1.DaemonSet | appsv1.Deployment | appsv1.ReplicaSet | appsv1.StatefulSet | batchv1.CronJob | batchv1.Job
}

// Inline implementation of resourceAdapter inspired by https://stackoverflow.com/a/43420100/6188451
type resourceAdapterImpl[T supportedResourceTypes] struct {
	resource     *T
	mutate       func(mutation.Mutator) error
	getNamespace func() string
}

func (r *resourceAdapterImpl[T]) GetNamespace() string {
	return r.getNamespace()
}

func (r *resourceAdapterImpl[T]) Mutate(mutator mutation.Mutator) error {
	return r.mutate(mutator)
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
				mutate: func(mutator mutation.Mutator) error {
					return mutator.MutateAppsV1DaemonSet(resource)
				},
				getNamespace: func() string {
					return resource.Namespace
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
				mutate: func(mutator mutation.Mutator) error {
					return mutator.MutateAppsV1Deployment(resource)
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
				mutate: func(mutator mutation.Mutator) error {
					return mutator.MutateAppsV1ReplicaSet(resource)
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
				mutate: func(mutator mutation.Mutator) error {
					return mutator.MutateAppsV1StatefulSet(resource)
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
				mutate: func(mutator mutation.Mutator) error {
					return mutator.MutateBatchV1CronJob(resource)
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
				mutate: func(mutator mutation.Mutator) error {
					return mutator.MutateBatchV1Job(resource)
				},
			}, nil
		}
	default:
		{
			return nil, nil
		}
	}

}

func validateMutationShouldOccur(lumigo *operatorv1alpha1.Lumigo) error {
	namespace := lumigo.ObjectMeta.Namespace

	status := &lumigo.Status

	// Check if the Lumigo status is active (so the injection _could_ be performed)
	activeCondition := conditions.GetLumigoConditionByType(status, operatorv1alpha1.LumigoConditionTypeActive)
	if activeCondition == nil || activeCondition.Status != corev1.ConditionTrue {
		return fmt.Errorf("the Lumigo object in the '%s' namespace is not active; resource will not be mutated", namespace)
	}

	errorCondition := conditions.GetLumigoConditionByType(status, operatorv1alpha1.LumigoConditionTypeError)
	if errorCondition != nil && errorCondition.Status == corev1.ConditionTrue {
		return fmt.Errorf("the Lumigo object in the '%s' namespace is in an erroneous status; resource will not be mutated", namespace)
	}

	return nil
}
