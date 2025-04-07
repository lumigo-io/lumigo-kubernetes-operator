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

package controllers

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers/conditions"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers/internal/sorting"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers/telemetryproxyconfigs"
	"github.com/lumigo-io/lumigo-kubernetes-operator/mutation"
	try "gopkg.in/matryer/try.v1"
)

const (
	defaultRequeuePeriod     = 10 * time.Second
	defaultErrRequeuePeriod  = 1 * time.Second
	maxTriggeredStateGroups  = 10
	maxMutationRetryAttempts = 5
)

// LumigoReconciler reconciles a Lumigo object
type LumigoReconciler struct {
	// "One Controller to use them all [clients], One Controller to find them, One Controller to mangle them all and in the kubelet bind them."
	// You may wonder: WHY ALL THESE CLIENTS?!? Well, because each fills a different niche:
	client.Client                           // Deal with typed resources that you know the type for
	*kubernetes.Clientset                   // Deal with events
	DynamicClient         dynamic.Interface // Look up object references of resources we don't want to treat in a typed fashion
	// End of clients (?)
	record.EventRecorder
	Scheme                                    *runtime.Scheme
	Log                                       logr.Logger
	LumigoOperatorVersion                     string
	LumigoInjectorImage                       string
	TelemetryProxyOtlpServiceUrl              string
	TelemetryProxyOtlpLogsServiceUrl          string
	TelemetryProxyNamespaceConfigurationsPath string
}

// SetupWithManager sets up the controller with the Manager.
func (r *LumigoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Lumigo{}).
		// Watch for changes in secrets that are referenced in Lumigo instances as containing the Lumigo token
		Watches(&source.Kind{Type: &corev1.Secret{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfSecretReferencedByLumigo)).
		Watches(&source.Kind{Type: &appsv1.DaemonSet{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfHasLumigoAutotraceLabel)).
		Watches(&source.Kind{Type: &appsv1.Deployment{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfHasLumigoAutotraceLabel)).
		Watches(&source.Kind{Type: &appsv1.ReplicaSet{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfHasLumigoAutotraceLabel)).
		Watches(&source.Kind{Type: &appsv1.StatefulSet{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfHasLumigoAutotraceLabel)).
		Watches(&source.Kind{Type: &batchv1.CronJob{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfHasLumigoAutotraceLabel)).
		Watches(&source.Kind{Type: &batchv1.Job{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfHasLumigoAutotraceLabel)).
		Complete(r)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims
// to move the current state of the cluster closer to the desired state. The
// Reconcile function must compare the state specified by the Lumigo object
// against the actual cluster state, and then perform operations to make the
// cluster state reflect the state specified by the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
//
// +kubebuilder:rbac:groups=operator.lumigo.io,resources=lumigoes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.lumigo.io,resources=lumigoes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.lumigo.io,resources=lumigoes/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
func (r *LumigoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("name", req.NamespacedName.Name, "namespace", req.NamespacedName.Namespace)
	now := metav1.NewTime(time.Now())

	namespace, err := r.Clientset.CoreV1().Namespaces().Get(ctx, req.NamespacedName.Namespace, metav1.GetOptions{})
	namespaceUid := ""
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The namespace has been meanwhile deleted, but we will still need to reconcile the telemetry-proxy configs
		} else {
			// Error reading the namespace - requeue the request.
			return ctrl.Result{
				RequeueAfter: defaultErrRequeuePeriod,
			}, nil
		}
	} else {
		namespaceUid = string(namespace.GetUID())
	}

	var result reconcile.Result
	lumigo := &operatorv1alpha1.Lumigo{}
	if err := r.Client.Get(ctx, req.NamespacedName, lumigo); err != nil {
		if apierrors.IsNotFound(err) {
			// Request object may have been deleted after the reconcile request has been issued,
			// e.g., due to garbage collection.
			log.Info("Discarding reconciliation event, Lumigo instance no longer exists")
			return result, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{
			RequeueAfter: defaultErrRequeuePeriod,
		}, nil
	}

	// The active condition has never been set, so this instance has just been created
	isLumigoJustCreated := conditions.GetLumigoConditionByType(lumigo, operatorv1alpha1.LumigoConditionTypeActive) == nil
	if isLumigoJustCreated {
		log = log.WithValues("new-lumigo", true)
	}

	if lumigo.ObjectMeta.DeletionTimestamp.IsZero() {
		// The Lumigo instance is not being deleted, so ensure it has our finalizer
		if !controllerutil.ContainsFinalizer(lumigo, operatorv1alpha1.LumigoResourceFinalizer) {
			controllerutil.AddFinalizer(lumigo, operatorv1alpha1.LumigoResourceFinalizer)
			if err := r.Update(ctx, lumigo); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else if controllerutil.ContainsFinalizer(lumigo, operatorv1alpha1.LumigoResourceFinalizer) {
		injectionSpec := lumigo.Spec.Tracing.Injection

		if conditions.IsActive(lumigo) {
			log.Info("Lumigo instance is being deleted, removing instrumentation from resources in namespace")
			if isTruthy(injectionSpec.Enabled, true) && isTruthy(injectionSpec.RemoveLumigoFromResourcesOnDeletion, true) {
				if err := r.removeLumigoFromResources(ctx, lumigo, &log); err != nil {
					log.Error(err, "cannot remove instrumentation from resources", "namespace", req.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				log.Info(
					"Lumigo instance is being deleted, but instrumentation from resources in namespace will not be removed",
					"Injection.Enabled", injectionSpec.Enabled,
					"Injection.RemoveLumigoFromResourcesOnDeletion", injectionSpec.RemoveLumigoFromResourcesOnDeletion,
				)
			}
		} else {
			log.Info("Lumigo instance is being deleted, but its status is not active so the instrumentation will not be removed from resources in namespace")
		}

		// remove our finalizer from the list and update it.
		controllerutil.RemoveFinalizer(lumigo, operatorv1alpha1.LumigoResourceFinalizer)
		if err := r.Update(ctx, lumigo); err != nil {
			return ctrl.Result{}, err
		}

		// Update telemetry-proxy not to collect Kube Events for this namespace
		isChanged, err := telemetryproxyconfigs.RemoveTelemetryProxyMonitoringOfNamespace(ctx, r.TelemetryProxyNamespaceConfigurationsPath, lumigo.Namespace, &log)
		if err != nil {
			log.Error(err, "Cannot update the telemetry-proxy configurations to remove the monitoring of the namespace")
		} else if isChanged {
			log.Info("Updated the telemetry-proxy configurations to remove the monitoring of the namespace")
		}

		// Set the lumigo instance as inactive
		conditions.SetActiveConditionWithMessage(lumigo, now, false, "This Lumigo instance is being deleted")
		conditions.ClearErrorCondition(lumigo, now)
		return r.updateStatusIfNeeded(ctx, log, lumigo, result)
	}

	// Validate there is only one Lumigo instance in any one namespace
	lumigoesInNamespace := &operatorv1alpha1.LumigoList{}
	if err := r.Client.List(ctx, lumigoesInNamespace, &client.ListOptions{Namespace: req.Namespace}); err != nil {
		// Error retrieving Lumigo instances in namespace - requeue the request.
		return ctrl.Result{
			RequeueAfter: defaultErrRequeuePeriod,
		}, err
	}

	otherLumigoesInNamespace := []string{}
	for _, otherLumigoes := range lumigoesInNamespace.Items {
		if otherLumigoes.Name != lumigo.Name {
			otherLumigoesInNamespace = append(otherLumigoesInNamespace, otherLumigoes.Name)
		}
	}

	if len(otherLumigoesInNamespace) > 0 {
		// We set the error if this Lumigo instance is not the first one that has been created
		sort.Sort(sorting.ByCreationTime(lumigoesInNamespace.Items))

		if lumigoesInNamespace.Items[0].UID != lumigo.UID {
			log.Info("Other Lumigo instances in this namespace", "otherLumigoNames", otherLumigoesInNamespace)
			conditions.SetErrorAndActiveConditions(lumigo, now, fmt.Errorf("other Lumigo instances in this namespace"))

			return r.updateStatusIfNeeded(ctx, log, lumigo, result)
		}
	}

	if lumigo.Spec == (operatorv1alpha1.LumigoSpec{}) {
		// This could happen if somehow the defaulter webhook is malfunctioning or turned off
		return ctrl.Result{}, fmt.Errorf("the Lumigo spec is empty")
	}

	token, err := r.validateCredentials(ctx, req.Namespace, &lumigo.Spec.LumigoToken)
	if err != nil {
		conditions.SetErrorAndActiveConditions(lumigo, now, fmt.Errorf("invalid Lumigo token secret reference: %w", err))
		log.Info("Invalid Lumigo token secret reference", "error", err.Error(), "status", &lumigo.Status)
		return r.updateStatusIfNeeded(ctx, log, lumigo, result)
	}

	if isLumigoJustCreated {
		log.Info("New Lumigo instance found")
		injectionSpec := lumigo.Spec.Tracing.Injection
		if isTruthy(injectionSpec.Enabled, true) && isTruthy(injectionSpec.InjectLumigoIntoExistingResourcesOnCreation, true) {
			log.Info("Injecting instrumentation into resources in namespace")
			if err := r.injectLumigoIntoResources(ctx, lumigo, &log); err != nil {
				log.Error(err, "cannot inject resources")
			}
		} else {
			log.Info(
				"Skipping instrumentation from resources in namespace",
				"Injection.Enabled", injectionSpec.Enabled,
				"Injection.InjectLumigoIntoExistingResourcesOnCreation", injectionSpec.InjectLumigoIntoExistingResourcesOnCreation,
			)
		}
	}

	// Update telemetry-proxy to ensure that Kube Events are collected correctly for this namespace
	if isTruthy(lumigo.Spec.Infrastructure.Enabled, true) && isTruthy(lumigo.Spec.Infrastructure.KubeEvents.Enabled, true) {
		isChanged, err := telemetryproxyconfigs.UpsertTelemetryProxyMonitoringOfNamespace(ctx, r.TelemetryProxyNamespaceConfigurationsPath, lumigo.Namespace, namespaceUid, token, &log)
		if err != nil {
			log.Error(err, "Cannot update the telemetry-proxy configurations to monitor the namespace")
		} else if isChanged {
			log.Info("Updated the telemetry-proxy configurations to monitor the namespace")
		}
	} else {
		if _, err := telemetryproxyconfigs.RemoveTelemetryProxyMonitoringOfNamespace(ctx, r.TelemetryProxyNamespaceConfigurationsPath, lumigo.Namespace, &log); err != nil {
			log.Error(err, "Cannot update the telemetry-proxy configurations to remove the monitoring of the namespace")
		} else {
			log.Info(
				"Removing infrastructure monitoring of the namespace",
				"Infrastructure.Enabled", lumigo.Spec.Infrastructure.Enabled,
				"Infrastructure.KubeEvents.Enabled", lumigo.Spec.Infrastructure.KubeEvents.Enabled,
			)
		}
	}

	// Validate that Lumigo events are all correctly associated with objects,
	// as the webhook cannot correctly associate events with objects that do
	// not yet exist.
	namespaceEvents := r.Clientset.CoreV1().Events(lumigo.Namespace)
	lumigoEvents, err := namespaceEvents.List(ctx, metav1.ListOptions{
		// Get all LumigoAddedInstrumentation events without the UID of the involved object
		FieldSelector: "reason=LumigoAddedInstrumentation,involvedObject.uid=",
	})
	if err != nil {
		log.Error(err, "Cannot look up dangling LumigoAddedInstrumentation events")
	} else {
		for _, lumigoEvent := range lumigoEvents.Items {
			if err := r.rebindLumigoEvent(ctx, namespaceEvents, &lumigoEvent); err != nil {
				log.Error(err, fmt.Sprintf("Cannot rebind dangling LumigoAddedInstrumentation event '%+v'", &lumigoEvent.UID))
			} else {
				log.Info(
					"Rebound dangling LumigoAddedInstrumentation event",
					"event.uid", lumigoEvent.UID,
					"involvedObject.kind", lumigoEvent.InvolvedObject.Kind,
					"involvedObject.namespace", lumigoEvent.InvolvedObject.Namespace,
					"involvedObject.name", lumigoEvent.InvolvedObject.Name,
				)
			}
		}
	}

	// Clear errors if any, mark instance as active, all is fine
	conditions.SetActiveCondition(lumigo, now, true)
	conditions.ClearErrorCondition(lumigo, now)

	var instrumentedResources *[]corev1.ObjectReference
	// Update autotraced resource references
	if instrumentedResources, err = r.getInstrumentedObjectReferences(ctx, lumigo.Namespace); err != nil {
		log.Error(err, "Cannot put together the instrumented resource references")
		return ctrl.Result{
			RequeueAfter: defaultErrRequeuePeriod,
		}, nil
	}

	lumigo.Status.InstrumentedResources = *instrumentedResources
	return r.updateStatusIfNeeded(ctx, log, lumigo, result)
}

func (r *LumigoReconciler) rebindLumigoEvent(ctx context.Context, eventInterface v1.EventInterface, event *corev1.Event) error {
	if err := r.fillOutReference(ctx, &event.InvolvedObject); err != nil {
		return fmt.Errorf("cannot fill out the 'InvolvedObject' reference: %w", err)
	}

	if _, err := eventInterface.Update(ctx, event, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("cannot update the dangling event %v: %w", event.UID, err)
	}

	return nil
}

func (r *LumigoReconciler) fillOutReference(ctx context.Context, reference *corev1.ObjectReference) error {
	var schema schema.GroupVersion
	switch reference.APIVersion {
	case "apps/v1":
		schema = appsv1.SchemeGroupVersion
	case "batch/v1":
		schema = batchv1.SchemeGroupVersion
	default:
		return fmt.Errorf("unexpected APIVersion for referenced object: '%s'", reference.APIVersion)
	}

	obj, err := r.DynamicClient.Resource(schema.WithResource(strings.ToLower(reference.Kind)+"s")).Namespace(reference.Namespace).Get(ctx, reference.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot retrieve '%s/%s' %s: %w", reference.Namespace, reference.Name, reference.Kind, err)
	}

	reference.UID = obj.GetUID()
	reference.ResourceVersion = obj.GetResourceVersion()

	return nil
}

// Check credentials existence
func (r *LumigoReconciler) validateCredentials(ctx context.Context, namespaceName string, credentials *operatorv1alpha1.Credentials) (string, error) {
	if credentials.SecretRef == (operatorv1alpha1.KubernetesSecretRef{}) {
		return "", fmt.Errorf("no Kubernetes secret reference provided")
	}

	if credentials.SecretRef.Name == "" {
		return "", fmt.Errorf("cannot the secret name is not specified")
	}

	if credentials.SecretRef.Key == "" {
		return "", fmt.Errorf("no key is specified for the secret '%s/%s'", namespaceName, credentials.SecretRef.Name)
	}

	secret, err := r.fetchKubernetesSecret(ctx, namespaceName, credentials.SecretRef.Name)
	if err != nil {
		return "", fmt.Errorf("cannot retrieve secret '%s/%s'", namespaceName, credentials.SecretRef.Name)
	}

	// Check that the key exists in the secret and the content matches the general shape of a Lumigo token
	lumigoTokenEnc := secret.Data[credentials.SecretRef.Key]
	if lumigoTokenEnc == nil {
		return "", fmt.Errorf("the secret '%s/%s' does not have the key '%s'", namespaceName, credentials.SecretRef.Name, credentials.SecretRef.Key)
	}

	lumigoToken := string(lumigoTokenEnc)

	matched, err := regexp.MatchString(`t_[[:xdigit:]]{21}`, lumigoToken)
	if err != nil {
		return "", fmt.Errorf(
			"cannot match the value the field '%s' of the secret '%s/%s' against "+
				"the expected structure of Lumigo tokens", credentials.SecretRef.Key, namespaceName, credentials.SecretRef.Name)
	}

	if !matched {
		return "", fmt.Errorf(
			"the value of the field '%s' of the secret '%s/%s' does not match the expected structure of Lumigo tokens: "+
				"it should be `t_` followed by 21 alphanumeric characters; see https://docs.lumigo.io/docs/lumigo-tokens "+
				"for instructions on how to retrieve your Lumigo token",
			credentials.SecretRef.Key, namespaceName, credentials.SecretRef.Name)
	}

	return lumigoToken, nil
}

func (r *LumigoReconciler) fetchKubernetesSecret(ctx context.Context, namespaceName string, secretName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: namespaceName,
		Name:      secretName,
	}, secret); err != nil {
		return secret, err
	}

	return secret, nil
}

func (r *LumigoReconciler) enqueueIfSecretReferencedByLumigo(obj client.Object) []reconcile.Request {
	// Require the reconciliation for Lumigo instances that reference the provided secret
	reconcileRequests := []reconcile.Request{{}}

	namespace := obj.GetNamespace()
	lumigoes := &operatorv1alpha1.LumigoList{}

	if err := r.Client.List(context.TODO(), lumigoes, &client.ListOptions{Namespace: namespace}); err != nil {
		r.Log.Error(err, "unable to list Lumigo instances in namespace '%s'", namespace)
		// TODO Can we re-enqueue or something? Should we signal an error in the Lumigo operator?
		return reconcileRequests
	}

	for _, lumigo := range lumigoes.Items {
		if lumigoToken := lumigo.Spec.LumigoToken; lumigoToken != (operatorv1alpha1.Credentials{}) {
			if secretRef := lumigoToken.SecretRef; secretRef != (operatorv1alpha1.KubernetesSecretRef{}) {
				if secretRef.Name == obj.GetName() {
					reconcileRequests = append(reconcileRequests, reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: lumigo.Namespace,
						Name:      lumigo.Name,
					}})
				}
			}
		}
	}

	return reconcileRequests
}

func (r *LumigoReconciler) enqueueIfHasLumigoAutotraceLabel(obj client.Object) []reconcile.Request {
	reconcileRequests := []reconcile.Request{{}}

	if _, ok := obj.GetLabels()[mutation.LumigoAutoTraceLabelKey]; ok {
		namespace := obj.GetNamespace()
		lumigoes := &operatorv1alpha1.LumigoList{}

		if err := r.Client.List(context.TODO(), lumigoes, &client.ListOptions{Namespace: namespace}); err != nil {
			r.Log.Error(err, "unable to list Lumigo instances in namespace '%s'", namespace)
			// TODO Can we re-enqueue or something? Should we signal an error in the Lumigo operator?
			return reconcileRequests
		}

		for _, lumigo := range lumigoes.Items {
			if conditions.IsActive(&lumigo) {
				reconcileRequests = append(reconcileRequests, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: lumigo.Namespace,
					Name:      lumigo.Name,
				}})
			}
		}
	}

	return reconcileRequests
}

func (r *LumigoReconciler) updateStatusIfNeeded(ctx context.Context, logger logr.Logger, instance *operatorv1alpha1.Lumigo, result ctrl.Result) (ctrl.Result, error) {
	// Updates the status of a Lumigo instance. Unfortunately updates do not seem reliable due
	// to some mismatch between the results of apiequality.Semantic.DeepEqual() and Kubernetes'
	// API (maybe due to bugs, maybe due to eventual consistency), which causes updates to be lost.
	// To reduce the risk of losing updates, we reque the request after a grace period.

	// Ensure status is valid (e.g., Lumigo instance just created and has no resources instrumented)
	if instance.Status.InstrumentedResources == nil {
		instance.Status.InstrumentedResources = make([]corev1.ObjectReference, 0)
	}

	if err := r.Client.Status().Update(ctx, instance); err != nil {
		logger.Error(err, "unable to update Lumigo instance's status")
		return ctrl.Result{RequeueAfter: defaultErrRequeuePeriod}, nil
	}

	logger.Info("Status updated", "status", &instance.Status)

	if hasError, _ := conditions.HasError(instance); hasError {
		return ctrl.Result{RequeueAfter: defaultErrRequeuePeriod}, nil
	}

	return ctrl.Result{RequeueAfter: defaultRequeuePeriod}, nil
}

func (r *LumigoReconciler) injectLumigoIntoResources(ctx context.Context, lumigo *operatorv1alpha1.Lumigo, log *logr.Logger) error {
	mutator, err := mutation.NewInjectingMutatorFromSpec(log, &lumigo.Spec, r.LumigoOperatorVersion, r.LumigoInjectorImage, r.TelemetryProxyOtlpServiceUrl, r.TelemetryProxyOtlpLogsServiceUrl)
	if err != nil {
		return fmt.Errorf("cannot instantiate mutator: %w", err)
	}

	namespace := lumigo.Namespace

	lumigoWithoutAutotraceLabelListOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("!%s", mutation.LumigoAutoTraceLabelKey),
	}

	// Ensure that all the resources that could be injected, are injected
	// TODO What to do about upgrades from former controller versions?
	lumigoNotAutotracedLabelFalseOrNotSet, err := labels.NewRequirement(mutation.LumigoAutoTraceLabelKey, selection.NotIn, []string{"false", mutator.GetAutotraceLabelValue()})
	if err != nil {
		return fmt.Errorf("cannot create label selector for non-autotraced objects: %w", err)
	}

	lumigoNotAutotracedLabelSelector := labels.NewSelector()
	lumigoNotAutotracedLabelSelector.Add(*lumigoNotAutotracedLabelFalseOrNotSet)

	eventTrigger := fmt.Sprintf("controller, acting on behalf of the '%s/%s' Lumigo resource", lumigo.Namespace, lumigo.Name)

	// Mutate daemonsets
	daemonsets, err := r.Clientset.AppsV1().DaemonSets(namespace).List(ctx, lumigoWithoutAutotraceLabelListOptions)
	if err != nil {
		return fmt.Errorf("cannot list non-autotraced daemonsets: %w", err)
	}

	for _, daemonset := range daemonsets.Items {
		if err := retry(fmt.Sprintf("inject instrumentation into the %s/%s daemonset", daemonset.Namespace, daemonset.Name), func() error {
			if err := r.Client.Get(ctx, client.ObjectKey{
				Namespace: daemonset.Namespace,
				Name:      daemonset.Name,
			}, &daemonset); err != nil {
				return fmt.Errorf("cannot retrieve details of daemonset '%s': %w", daemonset.GetName(), err)
			}

			mutatedDaemonset := daemonset.DeepCopy()
			if mutationOccurred, err := mutator.InjectLumigoIntoAppsV1DaemonSet(mutatedDaemonset); err != nil {
				return fmt.Errorf("cannot prepare mutation of daemonset '%s': %w", daemonset.GetName(), err)
			} else if mutationOccurred {
				return r.Client.Update(ctx, mutatedDaemonset)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, log); err != nil {
			operatorv1alpha1.RecordCannotAddInstrumentationEvent(r.EventRecorder, &daemonset, eventTrigger, err)
			return fmt.Errorf("cannot add instrumentation to daemonset '%s': %w", daemonset.GetName(), err)
		} else {
			log.Info("Added instrumentation to daemonset", "name", daemonset.Name)
			operatorv1alpha1.RecordAddedInstrumentationEvent(r.EventRecorder, &daemonset, eventTrigger)
		}
	}

	// Mutate deployments
	deployments, err := r.Clientset.AppsV1().Deployments(namespace).List(ctx, lumigoWithoutAutotraceLabelListOptions)
	if err != nil {
		return fmt.Errorf("cannot list non-autotraced deployments: %w", err)
	}

	for _, deployment := range deployments.Items {
		if err := retry(fmt.Sprintf("inject instrumentation into the %s/%s deployment", deployment.Namespace, deployment.Name), func() error {
			if err := r.Client.Get(ctx, client.ObjectKey{
				Namespace: deployment.Namespace,
				Name:      deployment.Name,
			}, &deployment); err != nil {
				return fmt.Errorf("cannot retrieve details of deployment '%s': %w", deployment.GetName(), err)
			}

			mutatedDeployment := deployment.DeepCopy()
			if mutationOccurred, err := mutator.InjectLumigoIntoAppsV1Deployment(mutatedDeployment); err != nil {
				return fmt.Errorf("cannot prepare mutation of deployment '%s': %w", deployment.GetName(), err)
			} else if mutationOccurred {
				return r.Client.Update(ctx, mutatedDeployment)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, log); err != nil {
			operatorv1alpha1.RecordCannotAddInstrumentationEvent(r.EventRecorder, &deployment, eventTrigger, err)
			return fmt.Errorf("cannot add instrumentation to deployment '%s': %w", deployment.GetName(), err)
		} else {
			log.Info("Added instrumentation to deployment", "name", deployment.Name)
			operatorv1alpha1.RecordAddedInstrumentationEvent(r.EventRecorder, &deployment, eventTrigger)
		}
	}

	// Mutate replicasets
	replicasets, err := r.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, lumigoWithoutAutotraceLabelListOptions)
	if err != nil {
		return fmt.Errorf("cannot list non-autotraced replicasets: %w", err)
	}

	for _, replicaset := range replicasets.Items {
		if err := retry(fmt.Sprintf("inject instrumentation into the %s/%s replicaset", replicaset.Namespace, replicaset.Name), func() error {
			if err := r.Client.Get(ctx, client.ObjectKey{
				Namespace: replicaset.Namespace,
				Name:      replicaset.Name,
			}, &replicaset); err != nil {
				return fmt.Errorf("cannot retrieve details of replicaset '%s': %w", replicaset.GetName(), err)
			}

			mutatedReplicaset := replicaset.DeepCopy()
			if mutationOccurred, err := mutator.InjectLumigoIntoAppsV1ReplicaSet(mutatedReplicaset); err != nil {
				return fmt.Errorf("cannot prepare mutation of replicaset '%s': %w", replicaset.GetName(), err)
			} else if mutationOccurred {
				return r.Client.Update(ctx, mutatedReplicaset)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, log); err != nil {
			operatorv1alpha1.RecordCannotAddInstrumentationEvent(r.EventRecorder, &replicaset, eventTrigger, err)
			return fmt.Errorf("cannot add instrumentation to replicaset '%s': %w", replicaset.GetName(), err)
		} else {
			log.Info("Added instrumentation to replicaset", "name", replicaset.Name)
			operatorv1alpha1.RecordAddedInstrumentationEvent(r.EventRecorder, &replicaset, eventTrigger)
		}
	}

	// Mutate statefulsets
	statefulsets, err := r.Clientset.AppsV1().StatefulSets(namespace).List(ctx, lumigoWithoutAutotraceLabelListOptions)
	if err != nil {
		return fmt.Errorf("cannot list non-autotraced statefulsets: %w", err)
	}

	for _, statefulset := range statefulsets.Items {
		if err := retry(fmt.Sprintf("inject instrumentation into the %s/%s statefulset", statefulset.Namespace, statefulset.Name), func() error {
			if err := r.Client.Get(ctx, client.ObjectKey{
				Namespace: statefulset.Namespace,
				Name:      statefulset.Name,
			}, &statefulset); err != nil {
				return fmt.Errorf("cannot retrieve details of statefulset '%s': %w", statefulset.GetName(), err)
			}

			mutatedStatefulset := statefulset.DeepCopy()
			if mutationOccurred, err := mutator.InjectLumigoIntoAppsV1StatefulSet(mutatedStatefulset); err != nil {
				return fmt.Errorf("cannot prepare mutation of statefulset '%s': %w", statefulset.GetName(), err)
			} else if mutationOccurred {
				return r.Client.Update(ctx, mutatedStatefulset)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, log); err != nil {
			operatorv1alpha1.RecordCannotAddInstrumentationEvent(r.EventRecorder, &statefulset, eventTrigger, err)
			return fmt.Errorf("cannot add instrumentation to statefulset '%s': %w", statefulset.GetName(), err)
		} else {
			log.Info("Added instrumentation to statefulset", "name", statefulset.Name)
			operatorv1alpha1.RecordAddedInstrumentationEvent(r.EventRecorder, &statefulset, eventTrigger)
		}
	}

	// Mutate cronjobs
	cronjobs, err := r.Clientset.BatchV1().CronJobs(namespace).List(ctx, lumigoWithoutAutotraceLabelListOptions)
	if err != nil {
		return fmt.Errorf("cannot list non-autotraced cronjobs: %w", err)
	}

	for _, cronjob := range cronjobs.Items {
		if err := retry(fmt.Sprintf("inject instrumentation into the %s/%s cronjob", cronjob.Namespace, cronjob.Name), func() error {
			if err := r.Client.Get(ctx, client.ObjectKey{
				Namespace: cronjob.Namespace,
				Name:      cronjob.Name,
			}, &cronjob); err != nil {
				return fmt.Errorf("cannot retrieve details of cronjob '%s': %w", cronjob.GetName(), err)
			}

			mutatedCronjob := cronjob.DeepCopy()
			if mutationOccurred, err := mutator.InjectLumigoIntoBatchV1CronJob(mutatedCronjob); err != nil {
				return fmt.Errorf("cannot prepare mutation of cronjob '%s': %w", cronjob.GetName(), err)
			} else if mutationOccurred {
				return r.Client.Update(ctx, mutatedCronjob)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, log); err != nil {
			operatorv1alpha1.RecordCannotAddInstrumentationEvent(r.EventRecorder, &cronjob, eventTrigger, err)
			return fmt.Errorf("cannot add instrumentation to cronjob '%s': %w", cronjob.GetName(), err)
		} else {
			log.Info("Added instrumentation to cronjob", "name", cronjob.Name)
			operatorv1alpha1.RecordAddedInstrumentationEvent(r.EventRecorder, &cronjob, eventTrigger)
		}
	}

	// Cannot mutate existing jobs: their PodSpecs are immutable!
	jobs, err := r.Clientset.BatchV1().Jobs(namespace).List(ctx, lumigoWithoutAutotraceLabelListOptions)
	if err != nil {
		return fmt.Errorf("cannot list autotraced jobs: %w", err)
	}

	for _, job := range jobs.Items {
		operatorv1alpha1.RecordCannotAddInstrumentationEvent(r.EventRecorder, &job, eventTrigger, fmt.Errorf("the PodSpec of batchv1.Job resources is immutable once the job has been created"))
		log.Info("Cannot instrumentation job: jobs are immutable once created", "namespace", job.Namespace, "name", job.Name)
	}

	return nil
}

func (r *LumigoReconciler) removeLumigoFromResources(ctx context.Context, lumigo *operatorv1alpha1.Lumigo, log *logr.Logger) error {
	resourceManager := NewLumigoResourceManager(r.Client, r.Clientset, r.DynamicClient, r.EventRecorder, log, r.LumigoOperatorVersion)
	eventTrigger := fmt.Sprintf("controller, acting on behalf of the '%s/%s' Lumigo resource", lumigo.Namespace, lumigo.Name)
	return resourceManager.RemoveLumigoFromResources(ctx, lumigo.Namespace, eventTrigger)
}

func (r *LumigoReconciler) getInstrumentedObjectReferences(ctx context.Context, namespace string) (*[]corev1.ObjectReference, error) {
	objectReferences := make([]corev1.ObjectReference, 0)

	lumigoAutotracedListOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%[1]s,%[1]s != false", mutation.LumigoAutoTraceLabelKey),
	}

	daemonSets, err := r.Clientset.AppsV1().DaemonSets(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot list autotraced daemonsets: %w", err)
	}

	sort.Sort(sorting.ByDaemonsetName(daemonSets.Items))
	for _, daemonSet := range daemonSets.Items {
		objectReference, err := reference.GetReference(scheme.Scheme, &daemonSet)
		if err != nil {
			return nil, err
		}
		objectReferences = append(objectReferences, *objectReference)
	}

	deployments, err := r.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%[1]s,%[1]s != false", mutation.LumigoAutoTraceLabelKey),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list autotraced deployments: %w", err)
	}

	sort.Sort(sorting.ByDeploymentName(deployments.Items))
	for _, deployment := range deployments.Items {
		objectReference, err := reference.GetReference(scheme.Scheme, &deployment)
		if err != nil {
			return nil, err
		}
		objectReferences = append(objectReferences, *objectReference)
	}

	replicaSets, err := r.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot list autotraced replicasets: %w", err)
	}

	sort.Sort(sorting.ByReplicaSetName(replicaSets.Items))
	for _, replicaSet := range replicaSets.Items {
		objectReference, err := reference.GetReference(scheme.Scheme, &replicaSet)
		if err != nil {
			return nil, err
		}
		objectReferences = append(objectReferences, *objectReference)
	}

	statefulSets, err := r.Clientset.AppsV1().StatefulSets(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot list autotraced statefulsets: %w", err)
	}

	sort.Sort(sorting.ByStatefulSetName(statefulSets.Items))
	for _, statefulSet := range statefulSets.Items {
		objectReference, err := reference.GetReference(scheme.Scheme, &statefulSet)
		if err != nil {
			return nil, err
		}
		objectReferences = append(objectReferences, *objectReference)
	}

	cronJobs, err := r.Clientset.BatchV1().CronJobs(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot list autotraced cronjobs: %w", err)
	}

	sort.Sort(sorting.ByCronJobName(cronJobs.Items))
	for _, cronJob := range cronJobs.Items {
		objectReference, err := reference.GetReference(scheme.Scheme, &cronJob)
		if err != nil {
			return nil, err
		}
		objectReferences = append(objectReferences, *objectReference)
	}

	jobs, err := r.Clientset.BatchV1().Jobs(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot list autotraced jobs: %w", err)
	}

	sort.Sort(sorting.ByJobName(jobs.Items))
	for _, job := range jobs.Items {
		objectReference, err := reference.GetReference(scheme.Scheme, &job)
		if err != nil {
			return nil, err
		}
		objectReferences = append(objectReferences, *objectReference)
	}

	return &objectReferences, nil
}

func retry(description string, function func() error, maxAttempts int, retryOnErrorMatcher func(error) bool, log *logr.Logger) error {
	return try.Do(func(currentAttempt int) (bool, error) {
		if err := function(); err != nil {
			if retryOnErrorMatcher(err) {
				log.Error(err, fmt.Sprintf("Failed to %s (attempt %d/%d)", description, currentAttempt, maxAttempts))
				return maxAttempts > currentAttempt, err
			} else {
				// This is not an error we want to retry on
				return false, err
			}
		} else {
			// nil error means we are done and do not need to retry further
			return false, nil
		}
	})
}

func retryOnMutationErrorMatcher(err error) bool {
	// Only match issues with intervealing object updates
	return true
}

func addAutoTraceSkipNextInjectorLabel(objectMeta *metav1.ObjectMeta) {
	objectMeta.Labels[mutation.LumigoAutoTraceLabelKey] = mutation.LumigoAutoTraceLabelSkipNextInjectorValue
}

func isTruthy(value *bool, defaultIfNil bool) bool {
	v := defaultIfNil

	if value != nil {
		v = *value
	}

	return v
}
