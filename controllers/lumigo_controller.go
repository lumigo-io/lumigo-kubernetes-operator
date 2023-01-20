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
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
)

const (
	defaultRequeuePeriod    = 60 * time.Second
	defaultErrRequeuePeriod = 5 * time.Second
	maxTriggeredStateGroups = 10
)

// LumigoReconciler reconciles a Lumigo object
type LumigoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *LumigoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Lumigo{}).
		// Watch for changes in secrets that are referenced in Lumigo instances as containing the Lumigo token
		Watches(&source.Kind{Type: &corev1.Secret{}}, handler.EnqueueRequestsFromMapFunc(r.enqueueIfSecretReferencedByLumigo)).
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
	log := r.Log.WithValues("lumigo", req.NamespacedName)
	now := metav1.NewTime(time.Now())

	var result reconcile.Result
	lumigoInstance := &operatorv1alpha1.Lumigo{} // TODO Match object name
	if err := r.Client.Get(ctx, req.NamespacedName, lumigoInstance); err != nil {
		if apierrors.IsNotFound(err) {
			// Request object may have been deleted after the reconcile request has been issued,
			// e.g., due to garbage collection.
			return result, nil
		}
		// Error reading the object - requeue the request.
		return result, err
	}

	// TODO Validate there is only one Lumigo instance in any one namespace

	if lumigoInstance.Spec == (operatorv1alpha1.LumigoSpec{}) {
		return ctrl.Result{}, fmt.Errorf("the Lumigo spec is empty")
	}

	if err := r.validateCredentials(ctx, req.Namespace, lumigoInstance.Spec.LumigoToken); err != nil {
		return r.updateStatusIfNeeded(log, lumigoInstance, now, fmt.Errorf("the Lumigo token is not valid: %w", err), result)
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
				"it should be `t_` followed by of 21 alphanumeric characters",
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

func (r *LumigoReconciler) updateStatusIfNeeded(logger logr.Logger, instance *operatorv1alpha1.Lumigo, now metav1.Time, currentErr error, result ctrl.Result) (ctrl.Result, error) {
	// Updates the status of a Lumigo instance.
	// Unfortunately updates do not seem reliable due to some mismatch between the results
	// of apiequality.Semantic.DeepEqual() and Kubernetes' API (maybe due to bugs, maybe due
	// to eventual consistency), which causes updates to be lost. To reduce the risk of losing updates,
	// we reque the request after a grace period.

	updatedStatus := instance.Status.DeepCopy()

	setErrorActiveConditions(updatedStatus, now, currentErr)

	if !apiequality.Semantic.DeepEqual(&instance.Status, updatedStatus) {
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

	return result, nil
}

func setErrorActiveConditions(status *operatorv1alpha1.LumigoStatus, now metav1.Time, err error) {
	if err != nil {
		updateLumigoConditions(status, now, operatorv1alpha1.LumigoConditionTypeError, corev1.ConditionTrue, fmt.Sprintf("%v", err))
		updateLumigoConditions(status, now, operatorv1alpha1.LumigoConditionTypeActive, corev1.ConditionFalse, "Lumigo has an error")
	} else {
		// Clear the error status
		updateLumigoConditions(status, now, operatorv1alpha1.LumigoConditionTypeError, corev1.ConditionFalse, "")
		updateLumigoConditions(status, now, operatorv1alpha1.LumigoConditionTypeActive, corev1.ConditionTrue, "Lumigo is ready")
	}
}

func updateLumigoConditions(status *operatorv1alpha1.LumigoStatus, now metav1.Time, t operatorv1alpha1.LumigoConditionType, conditionStatus corev1.ConditionStatus, desc string) {
	conditionIndex := getConditionIndexByType(status, t)

	if conditionIndex > -1 {
		// No condition exists of the given type
		setLumigoCondition(&status.Conditions[conditionIndex], now, conditionStatus, desc)
	} else if conditionStatus == corev1.ConditionTrue {
		status.Conditions = append(status.Conditions, newLumigoCondition(t, conditionStatus, now, "", desc))
	}
}

func setLumigoCondition(condition *operatorv1alpha1.LumigoCondition, now metav1.Time, conditionStatus corev1.ConditionStatus, message string) *operatorv1alpha1.LumigoCondition {
	if condition.Status != conditionStatus {
		condition.LastTransitionTime = now
		condition.Status = conditionStatus
	}
	condition.LastUpdateTime = now
	condition.Message = message

	return condition
}

func newLumigoCondition(conditionType operatorv1alpha1.LumigoConditionType, conditionStatus corev1.ConditionStatus, now metav1.Time, reason, message string) operatorv1alpha1.LumigoCondition {
	return operatorv1alpha1.LumigoCondition{
		Type:               conditionType,
		Status:             conditionStatus,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Message:            message,
	}
}

func getConditionIndexByType(status *operatorv1alpha1.LumigoStatus, t operatorv1alpha1.LumigoConditionType) int {
	idx := -1
	if status == nil {
		return idx
	}

	for i, condition := range status.Conditions {
		if condition.Type == t {
			idx = i
			break
		}
	}

	return idx
}
