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
	"reflect"
	"regexp"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers/conditions"
	"github.com/lumigo-io/lumigo-kubernetes-operator/mutation"
)

const (
	defaultRequeuePeriod    = 10 * time.Second
	defaultErrRequeuePeriod = 1 * time.Second
	maxTriggeredStateGroups = 10
)

var LumigoControllerFinalizer = "operator.lumigo.io/finalizer"

// LumigoReconciler reconciles a Lumigo object
type LumigoReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	Log                          logr.Logger
	LumigoOperatorVersion        string
	LumigoInjectorImage          string
	TelemetryProxyOtlpServiceUrl string
}

// SetupWithManager sets up the controller with the Manager.
func (r *LumigoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Lumigo{}).
		// Watch for changes in secrets that are referenced in Lumigo instances as containing the Lumigo token
		Watches(&source.Kind{Type: &corev1.Secret{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfSecretReferencedByLumigo)).
		// Watch for resources injected by the webhook, so that we can add an event to them about the injection
		// Watches(&source.Kind{Type: &appsv1.DaemonSet{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfInjectedByLumigo)).
		// Watches(&source.Kind{Type: &appsv1.Deployment{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfInjectedByLumigo)).
		// Watches(&source.Kind{Type: &appsv1.ReplicaSet{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfInjectedByLumigo)).
		// Watches(&source.Kind{Type: &appsv1.StatefulSet{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfInjectedByLumigo)).
		// Watches(&source.Kind{Type: &batchv1.CronJob{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfInjectedByLumigo)).
		// Watches(&source.Kind{Type: &batchv1.Job{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfInjectedByLumigo)).
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
	// TODO Aff finalizer to ensure we can untrace all resources in namespace on Lumigo instance removal
	log := r.Log.WithValues("lumigo-instance", req.NamespacedName)
	now := metav1.NewTime(time.Now())

	var result reconcile.Result
	lumigoInstance := &operatorv1alpha1.Lumigo{} // TODO Match object name
	if err := r.Client.Get(ctx, req.NamespacedName, lumigoInstance); err != nil {
		if apierrors.IsNotFound(err) {
			// Request object may have been deleted after the reconcile request has been issued,
			// e.g., due to garbage collection.
			log.Info("Lumigo instance deleted")
			return result, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: defaultErrRequeuePeriod,
		}, err
	}

	if lumigoInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		// The Lumigo instance is not being deleted, so ensure it has our finalizer
		if !controllerutil.ContainsFinalizer(lumigoInstance, LumigoControllerFinalizer) {
			controllerutil.AddFinalizer(lumigoInstance, LumigoControllerFinalizer)
			if err := r.Update(ctx, lumigoInstance); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The Lumigo instance is being deleted
		if controllerutil.ContainsFinalizer(lumigoInstance, LumigoControllerFinalizer) {
			log.Info("Lumigo instance is being deleted, removing instrumentation from resources in namespace")
			if err := r.removeLumigoFromResources(ctx, req.Namespace, &log); err != nil {
				log.Error(err, "cannot remove instrumentation from resources", "namespace", req.Namespace)
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(lumigoInstance, LumigoControllerFinalizer)
			if err := r.Update(ctx, lumigoInstance); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Stop reconciliation as the Lumigo instance is being deleted
		return ctrl.Result{}, nil
	}

	// Validate there is only one Lumigo instance in any one namespace
	lumigoesInNamespace := &operatorv1alpha1.LumigoList{}
	if err := r.Client.List(ctx, lumigoesInNamespace, &client.ListOptions{Namespace: req.Namespace}); err != nil {
		// Error retrieving Lumigo instances in namespace - requeue the request.
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: defaultErrRequeuePeriod,
		}, err
	}

	// Remove from the slice the current Lumigo instance we are processing
	otherLumigoesInNamespace := []operatorv1alpha1.Lumigo{}
	for _, otherLumigoInstance := range lumigoesInNamespace.Items {
		if otherLumigoInstance.Name != lumigoInstance.Name {
			otherLumigoesInNamespace = append(otherLumigoesInNamespace, otherLumigoInstance)
		}
	}

	if len(otherLumigoesInNamespace) > 0 {
		// Requeue reconciling this instance after the error delay, it might be that the multiple-instances
		// was a transitory issue due to a renaming.
		result, err := r.updateStatusIfNeeded(log, lumigoInstance, now, fmt.Errorf("multiple Lumigo instances in this namespace"), result)
		if err != nil {
			log.Error(err, "Cannot update this Lumigo in namespace")

			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: defaultErrRequeuePeriod,
			}, err
		}

		return result, nil
	}

	if lumigoInstance.Spec == (operatorv1alpha1.LumigoSpec{}) {
		return ctrl.Result{}, fmt.Errorf("the Lumigo spec is empty")
	}

	if err := r.validateCredentials(ctx, req.Namespace, lumigoInstance.Spec.LumigoToken); err != nil {
		log.Info("Invalid Lumigo token secret reference", "error", err.Error())
		return r.updateStatusIfNeeded(log, lumigoInstance, now, fmt.Errorf("the Lumigo token is not valid: %w", err), result)
	}

	// TODO Do only on creation of the Lumigo instance
	if err := r.injectLumigoIntoResources(ctx, lumigoInstance, &log); err != nil {
		log.Error(err, "cannot inject resources", "namespace", req.Namespace)
	}

	// Clear errors if any, all is fine
	return r.updateStatusIfNeeded(log, lumigoInstance, now, nil, result)
}

// Check credentials existence
func (r *LumigoReconciler) validateCredentials(ctx context.Context, namespaceName string, credentials operatorv1alpha1.Credentials) error {
	if credentials.SecretRef == (operatorv1alpha1.KubernetesSecretRef{}) {
		return fmt.Errorf("no Kubernetes secret reference provided")
	}

	if credentials.SecretRef.Name == "" {
		return fmt.Errorf("cannot the secret name is not specified")
	}

	if credentials.SecretRef.Key == "" {
		return fmt.Errorf("no key is specified for the secret '%s/%s'", namespaceName, credentials.SecretRef.Name)
	}

	secret, err := r.fetchKubernetesSecret(ctx, namespaceName, credentials.SecretRef.Name)
	if err != nil {
		return fmt.Errorf("cannot retrieve secret '%s/%s'", namespaceName, credentials.SecretRef.Name)
	}

	// Check that the key exists in the secret and the content matches the general shape of a Lumigo token
	lumigoTokenValueBytes := secret.Data[credentials.SecretRef.Key]
	if lumigoTokenValueBytes == nil {
		return fmt.Errorf("the secret '%s/%s' does not have the key '%s'", namespaceName, credentials.SecretRef.Name, credentials.SecretRef.Key)
	}

	lumigoTokenValueDec := string(lumigoTokenValueBytes)

	matched, err := regexp.MatchString(`t_[[:xdigit:]]{21}`, lumigoTokenValueDec)
	if err != nil {
		return fmt.Errorf(
			"cannot match the value the field '%s' of the secret '%s/%s' against "+
				"the expected structure of Lumigo tokens", credentials.SecretRef.Key, namespaceName, credentials.SecretRef.Name)
	}

	if !matched {
		return fmt.Errorf(
			"the value of the field '%s' of the secret '%s/%s' does not match the expected structure of Lumigo tokens: "+
				"it should be `t_` followed by of 21 alphanumeric characters; see https://docs.lumigo.io/docs/lumigo-tokens "+
				"for instructions on how to retrieve your Lumigo token",
			credentials.SecretRef.Key, namespaceName, credentials.SecretRef.Name)
	}

	return nil
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

// func (r *LumigoReconciler) enqueueIfInjectedByLumigo(obj client.Object) []reconcile.Request {
// 	// Require the reconciliation for Lumigo instances that reference the provided secret
// 	reconcileRequests := []reconcile.Request{{}}

// 	namespace := obj.GetNamespace()
// 	lumigoInstances := &operatorv1alpha1.LumigoList{}

// 	if err := r.Client.List(context.TODO(), lumigoInstances, &client.ListOptions{Namespace: namespace}); err != nil {
// 		r.Log.Error(err, "unable to list Lumigo instances in namespace '%s'", namespace)
// 		// TODO Can we re-enqueue or something? Should we signal an error in the Lumigo operator?
// 		return reconcileRequests
// 	}

// 	// TODO Validate there is exactly one Lumigo instance and that it is active
// 	lumigoInstance := lumigoInstances.Items[0]
// 	reconcileRequests = append(reconcileRequests, reconcile.Request{NamespacedName: types.NamespacedName{
// 		Namespace: lumigoInstance.Namespace,
// 		Name:      lumigoInstance.Name,
// 	}})

// 	return reconcileRequests
// }

func (r *LumigoReconciler) enqueueIfSecretReferencedByLumigo(obj client.Object) []reconcile.Request {
	// Require the reconciliation for Lumigo instances that reference the provided secret
	reconcileRequests := []reconcile.Request{{}}

	namespace := obj.GetNamespace()
	lumigoInstances := &operatorv1alpha1.LumigoList{}

	if err := r.Client.List(context.TODO(), lumigoInstances, &client.ListOptions{Namespace: namespace}); err != nil {
		r.Log.Error(err, "unable to list Lumigo instances in namespace '%s'", namespace)
		// TODO Can we re-enqueue or something? Should we signal an error in the Lumigo operator?
		return reconcileRequests
	}

	for _, lumigoInstance := range lumigoInstances.Items {
		if lumigoToken := lumigoInstance.Spec.LumigoToken; lumigoToken != (operatorv1alpha1.Credentials{}) {
			if secretRef := lumigoToken.SecretRef; secretRef != (operatorv1alpha1.KubernetesSecretRef{}) {
				if secretRef.Name == obj.GetName() {
					reconcileRequests = append(reconcileRequests, reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: lumigoInstance.Namespace,
						Name:      lumigoInstance.Name,
					}})
				}
			}
		}
	}

	return reconcileRequests
}

func (r *LumigoReconciler) updateStatusIfNeeded(logger logr.Logger, instance *operatorv1alpha1.Lumigo, now metav1.Time, newErr error, result ctrl.Result) (ctrl.Result, error) {
	// Updates the status of a Lumigo instance. Unfortunately updates do not seem reliable due
	// to some mismatch between the results of apiequality.Semantic.DeepEqual() and Kubernetes'
	// API (maybe due to bugs, maybe due to eventual consistency), which causes updates to be lost.
	// To reduce the risk of losing updates, we reque the request after a grace period.
	currentErrorCondition := conditions.GetLumigoConditionByType(&instance.Status, operatorv1alpha1.LumigoConditionTypeError)
	if currentErrorCondition != nil {
		var expectedMessage string
		if newErr != nil {
			expectedMessage = newErr.Error()
		}

		if currentErrorCondition.Status == corev1.ConditionTrue && currentErrorCondition.Message == expectedMessage {
			// No update necessary, the error is already the right one
			return result, nil
		}

	}

	updatedStatus := instance.Status.DeepCopy()

	conditions.SetErrorActiveConditions(updatedStatus, now, newErr)

	instance.Status = *updatedStatus
	if err := r.Client.Status().Update(context.TODO(), instance); err != nil {
		if apierrors.IsConflict(err) {
			logger.Error(err, "unable to update Lumigo's status")

			return ctrl.Result{Requeue: true, RequeueAfter: defaultErrRequeuePeriod}, nil
		}
		logger.Error(err, "unable to update Lumigo's status")

		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true, RequeueAfter: defaultRequeuePeriod}, nil
}

func (r *LumigoReconciler) injectLumigoIntoResources(ctx context.Context, lumigoInstance *operatorv1alpha1.Lumigo, log *logr.Logger) error {
	// TODO Make it less chatty, avoid unnecessary updates

	mutator, err := mutation.NewMutator(log, &lumigoInstance.Spec.LumigoToken, r.LumigoOperatorVersion, r.LumigoInjectorImage, r.TelemetryProxyOtlpServiceUrl)
	if err != nil {
		return fmt.Errorf("cannot instantiate mutator: %w", err)
	}

	// Ensure that all the resources that could be injected, are injected
	// TODO What to do about upgrades from former controller versions?
	lumigoNotAutotracedLabelFalseOrNotSet, err := labels.NewRequirement(mutation.LumigoAutoTraceLabelKey, selection.NotIn, []string{"false", mutator.GetAutotraceLabelValue()})
	if err != nil {
		return fmt.Errorf("cannot create label selector for non-autotraced objects: %w", err)
	}

	lumigoNotAutotracedLabelSelector := labels.NewSelector()
	lumigoNotAutotracedLabelSelector.Add(*lumigoNotAutotracedLabelFalseOrNotSet)

	// Mutate daemonsets
	daemonsets := &appsv1.DaemonSetList{}
	if err := r.Client.List(ctx, daemonsets, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     lumigoInstance.GetNamespace(),
	}); err != nil {
		return fmt.Errorf("cannot list non-autotraced daemonsets: %w", err)
	}

	for _, daemonset := range daemonsets.Items {
		mutatedDaemonset := daemonset.DeepCopy()
		if err := mutator.InjectLumigoIntoAppsV1DaemonSet(mutatedDaemonset); err != nil {
			return fmt.Errorf("cannot prepare mutation of daemonset '%s': %w", daemonset.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedDaemonset, &daemonset) {
			if err := r.Client.Update(ctx, mutatedDaemonset); err != nil {
				return fmt.Errorf("cannot add instrumentation to daemonset '%s': %w", daemonset.GetName(), err)
			}

			log.Info("Added instrumentation to daemonset", "name", daemonset.Name)
		}
	}

	// Mutate deployments
	deployments := &appsv1.DeploymentList{}
	if err := r.Client.List(ctx, deployments, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     lumigoInstance.GetNamespace(),
	}); err != nil {
		return fmt.Errorf("cannot list non-autotraced deployments: %w", err)
	}

	for _, deployment := range deployments.Items {
		mutatedDeployment := deployment.DeepCopy()
		if err := mutator.InjectLumigoIntoAppsV1Deployment(mutatedDeployment); err != nil {
			return fmt.Errorf("cannot prepare mutation of deployment '%s': %w", deployment.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedDeployment, &deployment) {
			if err := r.Client.Update(ctx, mutatedDeployment); err != nil {
				return fmt.Errorf("cannot add instrumentation to deployment '%s': %w", deployment.GetName(), err)
			}

			log.Info("Added instrumentation to deployment", "name", deployment.Name)
		}
	}

	// Mutate replicasets
	replicasets := &appsv1.ReplicaSetList{}
	if err := r.Client.List(ctx, replicasets, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     lumigoInstance.GetNamespace(),
	}); err != nil {
		return fmt.Errorf("cannot list non-autotraced replicasets: %w", err)
	}

	for _, replicaset := range replicasets.Items {
		mutatedReplicaset := replicaset.DeepCopy()
		if err := mutator.InjectLumigoIntoAppsV1ReplicaSet(mutatedReplicaset); err != nil {
			return fmt.Errorf("cannot prepare mutation of replicaset '%s': %w", replicaset.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedReplicaset, &replicaset) {
			if err := r.Client.Update(ctx, mutatedReplicaset); err != nil {
				return fmt.Errorf("cannot add instrumentation to replicaset '%s': %w", replicaset.GetName(), err)
			}

			log.Info("Added instrumentation to replicaset", "name", replicaset.Name)
		}
	}

	// Mutate statefulsets
	statefulsets := &appsv1.StatefulSetList{}
	if err := r.Client.List(ctx, statefulsets, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     lumigoInstance.GetNamespace(),
	}); err != nil {
		return fmt.Errorf("cannot list non-autotraced statefulsets: %w", err)
	}

	for _, statefulset := range statefulsets.Items {
		mutatedStatefulset := statefulset.DeepCopy()
		if err := mutator.InjectLumigoIntoAppsV1StatefulSet(mutatedStatefulset); err != nil {
			return fmt.Errorf("cannot prepare mutation of statefulset '%s': %w", statefulset.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedStatefulset, &statefulset) {
			if err := r.Client.Update(ctx, mutatedStatefulset); err != nil {
				return fmt.Errorf("cannot add instrumentation to statefulset '%s': %w", statefulset.GetName(), err)
			}

			log.Info("Added instrumentation to statefulset", "name", statefulset.Name)
		}
	}

	// Mutate cronjobs
	cronjobs := &batchv1.CronJobList{}
	if err := r.Client.List(ctx, cronjobs, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     lumigoInstance.GetNamespace(),
	}); err != nil {
		return fmt.Errorf("cannot list non-autotraced cronjobs: %w", err)
	}

	for _, cronjob := range cronjobs.Items {
		mutatedCronjob := cronjob.DeepCopy()
		if err := mutator.InjectLumigoIntoBatchV1CronJob(mutatedCronjob); err != nil {
			return fmt.Errorf("cannot prepare mutation of cronjob '%s': %w", cronjob.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedCronjob, &cronjob) {
			if err := r.Client.Update(ctx, mutatedCronjob); err != nil {
				return fmt.Errorf("cannot add instrumentation to cronjob '%s': %w", cronjob.GetName(), err)
			}

			log.Info("Added instrumentation to cronjob", "name", cronjob.Name)
		}
	}

	// Mutate jobs
	jobs := &batchv1.JobList{}
	if err := r.Client.List(ctx, jobs, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     lumigoInstance.GetNamespace(),
	}); err != nil {
		return fmt.Errorf("cannot list non-autotraced jobs: %w", err)
	}

	for _, job := range jobs.Items {
		mutatedJob := job.DeepCopy()
		if err := mutator.InjectLumigoIntoBatchV1Job(mutatedJob); err != nil {
			return fmt.Errorf("cannot prepare mutation of job '%s': %w", job.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedJob, &job) {
			if err := r.Client.Update(ctx, mutatedJob); err != nil {
				return fmt.Errorf("cannot add instrumentation to job '%s': %w", job.GetName(), err)
			}

			log.Info("Added instrumentation to job", "name", job.Name)
		}
	}

	return nil
}

func (r *LumigoReconciler) removeLumigoFromResources(ctx context.Context, namespace string, log *logr.Logger) error {
	mutator, err := mutation.NewMutator(log, nil, r.LumigoOperatorVersion, r.LumigoInjectorImage, r.TelemetryProxyOtlpServiceUrl)
	if err != nil {
		return fmt.Errorf("cannot instantiate mutator: %w", err)
	}

	// Ensure that all the resources that could be injected, are injected
	// TODO What to do about upgrades from former controller versions?
	lumigoAutotracedLabelSet, err := labels.NewRequirement(mutation.LumigoAutoTraceLabelKey, selection.Exists, []string{})
	if err != nil {
		return fmt.Errorf("cannot create label selector for autotraced objects: %w", err)
	}

	lumigoNotAutotracedLabelSelector := labels.NewSelector()
	lumigoNotAutotracedLabelSelector.Add(*lumigoAutotracedLabelSet)

	// Mutate daemonsets
	daemonsets := &appsv1.DaemonSetList{}
	if err := r.Client.List(ctx, daemonsets, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     namespace,
	}); err != nil {
		return fmt.Errorf("cannot list autotraced daemonsets: %w", err)
	}

	for _, daemonset := range daemonsets.Items {
		mutatedDaemonset := daemonset.DeepCopy()
		if err := mutator.RemoveLumigoFromAppsV1DaemonSet(mutatedDaemonset); err != nil {
			return fmt.Errorf("cannot prepare mutation of daemonset '%s': %w", daemonset.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedDaemonset, &daemonset) {
			if err := r.Client.Update(ctx, &daemonset); err != nil {
				return fmt.Errorf("cannot remove instrumentation from daemonset '%s': %w", daemonset.GetName(), err)
			}

			log.Info("Removed instrumentation from daemonset", "name", daemonset.Name)
		}
	}

	// Mutate deployments
	deployments := &appsv1.DeploymentList{}
	if err := r.Client.List(ctx, deployments, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     namespace,
	}); err != nil {
		return fmt.Errorf("cannot list autotraced deployments: %w", err)
	}

	for _, deployment := range deployments.Items {
		mutatedDeployment := deployment.DeepCopy()
		if err := mutator.RemoveLumigoFromAppsV1Deployment(mutatedDeployment); err != nil {
			return fmt.Errorf("cannot prepare mutation of deployment '%s': %w", deployment.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedDeployment, deployment) {
			if err := r.Client.Update(ctx, mutatedDeployment); err != nil {
				return fmt.Errorf("cannot remove instrumentation from deployment '%s': %w", deployment.GetName(), err)
			}

			log.Info("Removed instrumentation from deployment", "name", deployment.Name, "d", mutatedDeployment)
		}
	}

	// Mutate replicasets
	replicasets := &appsv1.ReplicaSetList{}
	if err := r.Client.List(ctx, replicasets, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     namespace,
	}); err != nil {
		return fmt.Errorf("cannot list autotraced replicasets: %w", err)
	}

	for _, replicaset := range replicasets.Items {
		mutatedReplicaset := replicaset.DeepCopy()
		if err := mutator.RemoveLumigoFromAppsV1ReplicaSet(mutatedReplicaset); err != nil {
			return fmt.Errorf("cannot prepare mutation of replicaset '%s': %w", replicaset.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedReplicaset, &replicaset) {
			if err := r.Client.Update(ctx, &replicaset); err != nil {
				return fmt.Errorf("cannot remove instrumentation from replicaset '%s': %w", replicaset.GetName(), err)
			}

			log.Info("Removed instrumentation from replicaset", "name", replicaset.Name)
		}
	}

	// Mutate statefulsets
	statefulsets := &appsv1.StatefulSetList{}
	if err := r.Client.List(ctx, statefulsets, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     namespace,
	}); err != nil {
		return fmt.Errorf("cannot list autotraced statefulsets: %w", err)
	}

	for _, statefulset := range statefulsets.Items {
		mutatedStatefulset := statefulset.DeepCopy()
		if err := mutator.RemoveLumigoFromAppsV1StatefulSet(mutatedStatefulset); err != nil {
			return fmt.Errorf("cannot prepare mutation of statefulset '%s': %w", statefulset.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedStatefulset, &statefulset) {
			if err := r.Client.Update(ctx, &statefulset); err != nil {
				return fmt.Errorf("cannot remove instrumentation from statefulset '%s': %w", statefulset.GetName(), err)
			}

			log.Info("Removed instrumentation from statefulset", "name", statefulset.Name)
		}
	}

	// Mutate cronjobs
	cronjobs := &batchv1.CronJobList{}
	if err := r.Client.List(ctx, cronjobs, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     namespace,
	}); err != nil {
		return fmt.Errorf("cannot list autotraced cronjobs: %w", err)
	}

	for _, cronjob := range cronjobs.Items {
		mutatedCronjob := cronjob.DeepCopy()
		if mutator.RemoveLumigoFromBatchV1CronJob(mutatedCronjob); err != nil {
			return fmt.Errorf("cannot prepare mutation of cronjob '%s': %w", cronjob.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedCronjob, &cronjob) {
			if err := r.Client.Update(ctx, &cronjob); err != nil {
				return fmt.Errorf("cannot remove instrumentation from cronjob '%s': %w", cronjob.GetName(), err)
			}

			log.Info("Removed instrumentation from cronjob", "name", cronjob.Name)
		}
	}

	// Mutate jobs
	jobs := &batchv1.JobList{}
	if err := r.Client.List(ctx, jobs, &client.ListOptions{
		LabelSelector: lumigoNotAutotracedLabelSelector,
		Namespace:     namespace,
	}); err != nil {
		return fmt.Errorf("cannot list autotraced jobs: %w", err)
	}

	for _, job := range jobs.Items {
		mutatedJob := job.DeepCopy()
		if err := mutator.RemoveLumigoFromBatchV1Job(mutatedJob); err != nil {
			return fmt.Errorf("cannot prepare mutation of job '%s': %w", job.GetName(), err)
		}

		if !reflect.DeepEqual(mutatedJob, &job) {
			if err := r.Client.Update(ctx, &job); err != nil {
				return fmt.Errorf("cannot remove instrumentation from job '%s': %w", job.GetName(), err)
			}

			log.Info("Removed instrumentation from job", "name", job.Name)
		}
	}

	return nil
}
