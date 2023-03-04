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
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
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
	client.Client
	record.EventRecorder
	*admission.Decoder
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
	h.Client = c
	return nil
}

// The decoder is automatically injected by the Webhook machinery
func (h *LumigoInjectorWebhookHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
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
	if err := h.Client.List(ctx, lumigos, &client.ListOptions{
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

	if !conditions.IsActive(&lumigo) {
		return admission.Allowed(fmt.Sprintf("The Lumigo object in the '%s' namespace is not active; resource will not be mutated", namespace))
	}

	mutator, err := mutation.NewMutator(&log, &lumigo.Spec.LumigoToken, h.LumigoOperatorVersion, h.LumigoInjectorImage, h.TelemetryProxyOtlpServiceUrl)
	if err != nil {
		return admission.Allowed(fmt.Errorf("cannot instantiate mutator: %w", err).Error())
	}

	objectMeta := resourceAdaper.GetObjectMeta()
	hadAlreadyInstrumentation := strings.HasPrefix(objectMeta.Labels[mutation.LumigoAutoTraceLabelKey], mutation.LumigoAutoTraceLabelVersionPrefixValue)
	injectionOccurred := false
	if objectMeta.Labels[mutation.LumigoAutoTraceLabelKey] == mutation.LumigoAutoTraceLabelSkipNextInjectorValue {
		h.Log.Info(fmt.Sprintf("Skipping injection: '%s' label set to '%s'", mutation.LumigoAutoTraceLabelKey, mutation.LumigoAutoTraceLabelSkipNextInjectorValue))
		delete(objectMeta.Labels, mutation.LumigoAutoTraceLabelKey)
	} else if injectionOccurred, err = resourceAdaper.InjectLumigoInto(mutator); err != nil {
		if !hadAlreadyInstrumentation {
			operatorv1alpha1.RecordCannotAddInstrumentationEvent(h.EventRecorder, resourceAdaper.GetResource(), fmt.Sprintf("injector webhook, acting on behalf of the '%s/%s' Lumigo resource", lumigo.Namespace, lumigo.Name), err)
		} else {
			operatorv1alpha1.RecordCannotUpdateInstrumentationEvent(h.EventRecorder, resourceAdaper.GetResource(), fmt.Sprintf("injector webhook, acting on behalf of the '%s/%s' Lumigo resource", lumigo.Namespace, lumigo.Name), err)
		}
		return admission.Allowed(fmt.Errorf("cannot inject Lumigo tracing in the pod spec %w", err).Error())
	}

	marshalled, err := resourceAdaper.Marshal()
	if err != nil {
		return admission.Allowed(fmt.Errorf("cannot marshal object %w", err).Error())
	}

	if injectionOccurred {
		if !hadAlreadyInstrumentation {
			operatorv1alpha1.RecordAddedInstrumentationEvent(h.EventRecorder, resourceAdaper.GetResource(), fmt.Sprintf("injector webhook, acting on behalf of the '%s/%s' Lumigo resource", lumigo.Namespace, lumigo.Name))
		} else {
			operatorv1alpha1.RecordUpdatedInstrumentationEvent(h.EventRecorder, resourceAdaper.GetResource(), fmt.Sprintf("injector webhook, acting on behalf of the '%s/%s' Lumigo resource", lumigo.Namespace, lumigo.Name))
		}
	}

	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

type resourceAdapter interface {
	GetResource() runtime.Object
	GetObjectMeta() *metav1.ObjectMeta
	GetNamespace() string
	InjectLumigoInto(mutation.Mutator) (bool, error)
	Marshal() ([]byte, error)
}

// Inline implementation of resourceAdapter inspired by https://stackoverflow.com/a/43420100/6188451
type resourceAdapterImpl struct {
	resource      runtime.Object
	getNamespace  func() string
	getObjectMeta func() *metav1.ObjectMeta
}

func (r *resourceAdapterImpl) GetResource() runtime.Object {
	return r.resource
}

func (r *resourceAdapterImpl) GetNamespace() string {
	return r.getNamespace()
}

func (r *resourceAdapterImpl) GetObjectMeta() *metav1.ObjectMeta {
	return r.getObjectMeta()
}

func (r *resourceAdapterImpl) InjectLumigoInto(mutator mutation.Mutator) (bool, error) {
	return mutator.InjectLumigoInto(r.resource)
}

func (r *resourceAdapterImpl) Marshal() ([]byte, error) {
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

			return &resourceAdapterImpl{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() *metav1.ObjectMeta {
					return &resource.ObjectMeta
				},
			}, nil
		}
	case "apps/v1.Deployment":
		{
			resource := &appsv1.Deployment{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() *metav1.ObjectMeta {
					return &resource.ObjectMeta
				},
			}, nil
		}
	case "apps/v1.ReplicaSet":
		{
			resource := &appsv1.ReplicaSet{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() *metav1.ObjectMeta {
					return &resource.ObjectMeta
				},
			}, nil
		}
	case "apps/v1.StatefulSet":
		{
			resource := &appsv1.StatefulSet{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() *metav1.ObjectMeta {
					return &resource.ObjectMeta
				},
			}, nil
		}
	case "batch/v1.CronJob":
		{
			resource := &batchv1.CronJob{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() *metav1.ObjectMeta {
					return &resource.ObjectMeta
				},
			}, nil
		}
	case "batch/v1.Job":
		{
			resource := &batchv1.Job{}

			if _, _, err := decoder.Decode(raw, nil, resource); err != nil {
				return nil, fmt.Errorf("cannot parse resource into a %s: %w", sGVK, err)
			}

			return &resourceAdapterImpl{
				resource: resource,
				getNamespace: func() string {
					return resource.Namespace
				},
				getObjectMeta: func() *metav1.ObjectMeta {
					return &resource.ObjectMeta
				},
			}, nil
		}
	default:
		{
			return nil, nil
		}
	}

}
