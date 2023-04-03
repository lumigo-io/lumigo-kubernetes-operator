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
	"encoding/json"
	"fmt"
	"os"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers/conditions"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers/telemetryproxyconfigs"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/reference"
)

func BeActive() types.GomegaMatcher {
	return &beActive{}
}

type beActive struct {
}

func (m *beActive) Match(actual interface{}) (bool, error) {
	var lumigo *operatorv1alpha1.Lumigo
	switch a := actual.(type) {
	case *operatorv1alpha1.Lumigo:
		lumigo = a
	default:
		return false, fmt.Errorf("IsActive matcher expects a *operatorv1alpha1.Lumigo; got:\n%s", format.Object(actual, 1))
	}

	return conditions.IsActive(lumigo), nil
}

func (m *beActive) FailureMessage(actual interface{}) (message string) {
	return "is not active"
}

func (m *beActive) NegatedFailureMessage(actual interface{}) (message string) {
	return "is active"
}

func BeInErroneousState(expectedMessage string) types.GomegaMatcher {
	return &beInErroneousState{
		expectedMessage: expectedMessage,
	}
}

type beInErroneousState struct {
	expectedMessage string
}

func (m *beInErroneousState) Match(actual interface{}) (bool, error) {
	var lumigo *operatorv1alpha1.Lumigo
	switch a := actual.(type) {
	case *operatorv1alpha1.Lumigo:
		lumigo = a
	default:
		return false, fmt.Errorf("IsActive matcher expects a *operatorv1alpha1.Lumigo; got:\n%s", format.Object(actual, 1))
	}

	hasError, message := conditions.HasError(lumigo)

	if len(m.expectedMessage) == 0 {
		return hasError, nil
	} else {
		return m.expectedMessage == message, nil
	}
}

func (m *beInErroneousState) FailureMessage(actual interface{}) (message string) {
	if len(m.expectedMessage) == 0 {
		return "is not in an erroneous state"
	}

	return fmt.Sprintf("is not in an erroneous state with message '%s'", message)
}

func (m *beInErroneousState) NegatedFailureMessage(actual interface{}) (message string) {
	if len(m.expectedMessage) == 0 {
		return "is in an erroneous state"
	}

	return fmt.Sprintf("is in an erroneous state with message '%s'", message)
}

func HaveInstrumentedObjectReferenceFor(object runtime.Object) types.GomegaMatcher {
	return &haveInstrumentedObjectReferenceFor{
		object: object,
	}
}

type haveInstrumentedObjectReferenceFor struct {
	object runtime.Object
}

func (m *haveInstrumentedObjectReferenceFor) Match(actual interface{}) (bool, error) {
	var lumigo *operatorv1alpha1.Lumigo
	switch a := actual.(type) {
	case *operatorv1alpha1.Lumigo:
		lumigo = a
	default:
		return false, fmt.Errorf("HasInstrumentedObjectReference matcher expects a *operatorv1alpha1.Lumigo; got:\n%s", format.Object(actual, 1))
	}

	ref, err := reference.GetReference(scheme.Scheme, m.object)
	if err != nil {
		return false, err
	}

	for _, actualRef := range lumigo.Status.InstrumentedResources {
		if ref.Namespace == actualRef.Namespace && ref.Name == actualRef.Name && ref.Kind == actualRef.Kind {
			return true, nil
		}
	}

	return false, nil
}

func (m *haveInstrumentedObjectReferenceFor) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("does not have the following object as instrumented reference: %+v", m.object)
}

func (m *haveInstrumentedObjectReferenceFor) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("has the following object as instrumented reference: %+v", m.object)
}

func BeMonitoringNamespace(namespace string) types.GomegaMatcher {
	return &beMonitoringNamespace{
		expectedMonitoredNamespaceName: namespace,
	}
}

type beMonitoringNamespace struct {
	expectedMonitoredNamespaceName string
}

func (m *beMonitoringNamespace) Match(actual interface{}) (bool, error) {
	actualMonitoredNamespaces, err := parseJsonFile(actual)
	if err != nil {
		return false, err
	}

	for _, monitoredNamespace := range actualMonitoredNamespaces {
		if monitoredNamespace.Name == m.expectedMonitoredNamespaceName {
			return true, nil
		}
	}

	return false, nil
}

func (m *beMonitoringNamespace) FailureMessage(actual interface{}) (message string) {
	actualMonitoredNamespaces, err := parseJsonFile(actual)
	if err != nil {
		return err.Error()
	}

	return fmt.Sprintf(
		"is not monitoring the namespace '%s'; monitored namespaces: %v",
		m.expectedMonitoredNamespaceName,
		actualMonitoredNamespaces,
	)
}

func (m *beMonitoringNamespace) NegatedFailureMessage(actual interface{}) (message string) {
	actualMonitoredNamespaces, err := parseJsonFile(actual)
	if err != nil {
		return err.Error()
	}

	return fmt.Sprintf(
		"is monitoring the namespace '%s'; monitored namespaces: %v",
		m.expectedMonitoredNamespaceName,
		actualMonitoredNamespaces,
	)
}

func parseJsonFile(actual interface{}) ([]telemetryproxyconfigs.NamespaceMonitoringConfig, error) {
	var actualFile string
	switch a := actual.(type) {
	case string:
		actualFile = a
	default:
		return nil, fmt.Errorf("BeJsonFileMatching matcher expects a string; got:\n%s", format.Object(actual, 1))
	}

	actualBytes, err := os.ReadFile(actualFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make([]telemetryproxyconfigs.NamespaceMonitoringConfig, 0), nil
		} else {
			return nil, fmt.Errorf("cannot read the actual file '%s': %w", actualFile, err)
		}
	}

	var monitoredNamespaces []telemetryproxyconfigs.NamespaceMonitoringConfig
	if err := json.Unmarshal(actualBytes, &monitoredNamespaces); err != nil {
		return nil, fmt.Errorf("cannot unmarshal actual file '%s': %w", actualFile, err)
	}

	return monitoredNamespaces, nil
}
