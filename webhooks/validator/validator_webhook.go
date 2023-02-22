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

package validator

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/go-logr/logr"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
)

var (
	decoder = scheme.Codecs.UniversalDecoder()
)

type LumigoValidatorWebhookHandler struct {
	client                client.Client
	decoder               *admission.Decoder
	LumigoOperatorVersion string
	Log                   logr.Logger
}

func (h *LumigoValidatorWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/validate", handler)

	return nil
}

// The client is automatically injected by the Webhook machinery
func (h *LumigoValidatorWebhookHandler) InjectClient(c client.Client) error {
	h.client = c
	return nil
}

// The decoder is automatically injected by the Webhook machinery
func (h *LumigoValidatorWebhookHandler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}

func (h *LumigoValidatorWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	log := logf.Log.WithName("lumigo-validator-webhook").WithValues("resource_gvk", request.Kind)

	if request.Operation == admissionv1.Delete {
		// Nothing to mutate on deletions
		return admission.Allowed("Mutating webhooks have nothing to do on deletions")
	}

	lumigoGVK := metav1.GroupVersionKind{
		Group:   "operator.lumigo.io",
		Version: "v1alpha1",
		Kind:    "Lumigo",
	}

	if !reflect.DeepEqual(request.Kind, lumigoGVK) {
		return admission.Allowed("Not a operator.lumigo.io/v1alpha1.Lumigo resource, nothing to validate")
	}

	// Check whether we have already Lumigoes laying around
	newLumigo := &operatorv1alpha1.Lumigo{}
	if _, _, err := decoder.Decode(request.Object.Raw, nil, newLumigo); err != nil {
		log.Error(err, "cannot parse resource")
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("cannot parse resource: %w", err))
	}

	namespace := newLumigo.Namespace
	otherLumigos := &operatorv1alpha1.LumigoList{}
	if err := h.client.List(ctx, otherLumigos, &client.ListOptions{
		Namespace: namespace,
	}); err != nil {
		log.Error(err, "failed to retrieve Lumigo instance in namespace", "namespace", namespace)
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("cannot retrieve Lumigo instances in namespace %s: %w", namespace, err))
	}

	if len(otherLumigos.Items) > 0 {
		log.Info("Denied the creation of an instance of Lumigo in a namespace that already had one", "namespace", namespace)
		return admission.Denied(fmt.Sprintf("There is already an instance of operator.lumigo.io/v1alpha1.Lumigo in the '%s' namespace", namespace))
	}

	return admission.Allowed("Have fun with Lumigo!")
}
