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

func GetLumigoConditionByType(status *operatorv1alpha1.LumigoStatus, t operatorv1alpha1.LumigoConditionType) *operatorv1alpha1.LumigoCondition {
	if index := getConditionIndexByType(status, t); index > -1 {
		return &status.Conditions[index]
	}

	return nil
}

func UpdateLumigoConditions(status *operatorv1alpha1.LumigoStatus, t operatorv1alpha1.LumigoConditionType, now metav1.Time, conditionStatus corev1.ConditionStatus, desc string) {
	conditionIndex := getConditionIndexByType(status, t)

	if conditionIndex > -1 {
		SetLumigoCondition(&status.Conditions[conditionIndex], now, conditionStatus, desc)
	} else if conditionStatus == corev1.ConditionTrue {
		// No condition exists of the given type
		status.Conditions = append(status.Conditions, NewLumigoCondition(t, conditionStatus, now, "", desc))
	}
}

func SetErrorActiveConditions(status *operatorv1alpha1.LumigoStatus, now metav1.Time, err error) {
	if err != nil {
		UpdateLumigoConditions(status, operatorv1alpha1.LumigoConditionTypeError, now, corev1.ConditionTrue, fmt.Sprintf("%v", err))
		UpdateLumigoConditions(status, operatorv1alpha1.LumigoConditionTypeActive, now, corev1.ConditionFalse, "Lumigo has an error")
	} else {
		// Clear the error status
		UpdateLumigoConditions(status, operatorv1alpha1.LumigoConditionTypeError, now, corev1.ConditionFalse, "")
		UpdateLumigoConditions(status, operatorv1alpha1.LumigoConditionTypeActive, now, corev1.ConditionTrue, "Lumigo is ready")
	}
}

func SetLumigoCondition(condition *operatorv1alpha1.LumigoCondition, now metav1.Time, conditionStatus corev1.ConditionStatus, message string) {
	if condition.Status != conditionStatus {
		condition.LastTransitionTime = now
		condition.Status = conditionStatus
	}
	condition.LastUpdateTime = now
	condition.Message = message
}

func NewLumigoCondition(conditionType operatorv1alpha1.LumigoConditionType, conditionStatus corev1.ConditionStatus, now metav1.Time, reason, message string) operatorv1alpha1.LumigoCondition {
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
