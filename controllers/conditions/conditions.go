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

package conditions

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
)

func GetLumigoConditionByType(lumigo *operatorv1alpha1.Lumigo, t operatorv1alpha1.LumigoConditionType) *operatorv1alpha1.LumigoCondition {
	status := &lumigo.Status
	if index := getConditionIndexByType(status, t); index > -1 {
		return &status.Conditions[index]
	}

	return nil
}

func SetActiveAndErrorConditions(lumigo *operatorv1alpha1.Lumigo, now metav1.Time, err error) {
	if err != nil {
		updateLumigoConditions(lumigo, operatorv1alpha1.LumigoConditionTypeError, now, corev1.ConditionTrue, fmt.Sprintf("%v", err))
		SetErrorAndActiveConditions(lumigo, now, err)
	} else {
		// Clear the error status
		ClearErrorCondition(lumigo, now)
		SetActiveCondition(lumigo, now, true)
	}
}

func SetActiveCondition(lumigo *operatorv1alpha1.Lumigo, now metav1.Time, isActive bool) {
	var message string
	if isActive {
		message = "Lumigo is ready"
	} else {
		message = "Lumigo not ready"
	}

	SetActiveConditionWithMessage(lumigo, now, isActive, message)
}

func SetActiveConditionWithMessage(lumigo *operatorv1alpha1.Lumigo, now metav1.Time, isActive bool, message string) {
	if isActive {
		updateLumigoConditions(lumigo, operatorv1alpha1.LumigoConditionTypeActive, now, corev1.ConditionTrue, message)
	} else {
		updateLumigoConditions(lumigo, operatorv1alpha1.LumigoConditionTypeActive, now, corev1.ConditionFalse, message)
	}
}

func SetErrorAndActiveConditions(lumigo *operatorv1alpha1.Lumigo, now metav1.Time, err error) {
	SetActiveConditionWithMessage(lumigo, now, false, fmt.Sprintf("This Lumigo has an error, see the '%s' condition", operatorv1alpha1.LumigoConditionTypeError))
	updateLumigoConditions(lumigo, operatorv1alpha1.LumigoConditionTypeError, now, corev1.ConditionTrue, fmt.Sprintf("%v", err))
}

func ClearErrorCondition(lumigo *operatorv1alpha1.Lumigo, now metav1.Time) {
	updateLumigoConditions(lumigo, operatorv1alpha1.LumigoConditionTypeError, now, corev1.ConditionFalse, "")
}

func IsActive(lumigo *operatorv1alpha1.Lumigo) bool {
	if activeCondition := GetLumigoConditionByType(lumigo, operatorv1alpha1.LumigoConditionTypeActive); activeCondition != nil {
		return activeCondition.Status == corev1.ConditionTrue
	}

	return false
}

func HasError(lumigo *operatorv1alpha1.Lumigo) (bool, string) {
	if errorCondition := GetLumigoConditionByType(lumigo, operatorv1alpha1.LumigoConditionTypeError); errorCondition != nil {
		return errorCondition.Status == corev1.ConditionTrue, errorCondition.Message
	}

	return false, ""
}

func updateLumigoConditions(lumigo *operatorv1alpha1.Lumigo, t operatorv1alpha1.LumigoConditionType, now metav1.Time, conditionStatus corev1.ConditionStatus, desc string) {
	status := &lumigo.Status
	conditionIndex := getConditionIndexByType(status, t)

	if conditionIndex > -1 {
		setLumigoCondition(&status.Conditions[conditionIndex], now, conditionStatus, desc)
	} else if conditionStatus == corev1.ConditionTrue {
		// No condition exists of the given type
		status.Conditions = append(status.Conditions, newLumigoCondition(t, conditionStatus, now, "", desc))
	}
}

func setLumigoCondition(condition *operatorv1alpha1.LumigoCondition, now metav1.Time, conditionStatus corev1.ConditionStatus, message string) {
	if condition.Status != conditionStatus {
		condition.LastTransitionTime = now
		condition.Status = conditionStatus
	}
	condition.LastUpdateTime = now
	condition.Message = message
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
