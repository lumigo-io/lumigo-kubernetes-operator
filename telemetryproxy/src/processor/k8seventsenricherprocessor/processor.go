// Copyright 2020 OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8seventsenricherprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8seventsenricherprocessor"

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	podv1         = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	eventv1       = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Event"}
	daemonsetv1   = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}
	deploymentv1  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	replicasetv1  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	statefulsetv1 = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	cronjobv1     = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "CronJob"}
	jobv1         = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}
)

var validOwnerReferenceGroupVersionKinds = []schema.GroupVersionKind{
	podv1,
	daemonsetv1,
	deploymentv1,
	replicasetv1,
	statefulsetv1,
	cronjobv1,
	jobv1,
}

type kubernetesprocessor struct {
	client dynamic.Interface
	logger *zap.Logger
}

func (kp *kubernetesprocessor) Start(_ context.Context, _ component.Host) error {
	return nil
}

func (kp *kubernetesprocessor) Shutdown(_ context.Context) error {
	return nil
}

func (kp *kubernetesprocessor) processLogs(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
	resourceLogs := ld.ResourceLogs()
	for i := 0; i < resourceLogs.Len(); i++ {
		rl := resourceLogs.At(i)
		if err := kp.processResourceLogs(ctx, &rl); err != nil {
			/*
			 * We never make the pipeline fail (returning an error out of this method);
			 * rather, we log.
			 */
			kp.logger.Error("an error occurred while adding the root owner reference in some logs", zap.Error(err))
		}
	}

	return ld, nil
}

func (kp *kubernetesprocessor) processResourceLogs(ctx context.Context, resourceLogs *plog.ResourceLogs) error {
	scopeLogs := resourceLogs.ScopeLogs()
	for i := 0; i < scopeLogs.Len(); i++ {
		sl := scopeLogs.At(i)

		logRecords := sl.LogRecords()
		logRecordsLength := logRecords.Len()
		for j := 0; j < logRecordsLength; j++ {
			logRecord := logRecords.At(j)

			if err := kp.addRootOwnerReference(ctx, &logRecord); err != nil {
				return err
			}
		}
	}

	return nil
}

func (kp *kubernetesprocessor) addRootOwnerReference(ctx context.Context, logRecord *plog.LogRecord) error {
	/*
	 * Try to parse the log record's body as an unstructured object of GVK v1.Event.
	 * If the GVK is right, look up recursively the ownerreference of the involved object.
	 */
	logRecordBody := logRecord.Body().AsRaw().(map[string]interface{})

	unstructured := &unstructured.Unstructured{
		Object: logRecordBody,
	}

	logRecordBodyGvk := unstructured.GroupVersionKind()

	if eventv1 != logRecordBodyGvk {
		// Skip this object, is likely a pulled object
		return nil
	}

	event := &corev1.Event{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, &event); err != nil {
		return err
	}

	currentObjectReference := &event.InvolvedObject
	for currentObjectReference != nil {
		ownerReference, err := kp.getFirstRelevantOwnerReferenceAsObjectReference(ctx, currentObjectReference)
		if err != nil {
			return fmt.Errorf("cannot retrieve owner reference of object reference %v: %w", currentObjectReference, err)
		}

		if ownerReference == nil {
			/*
			 * The object pointed at by the currentObjectReference does not have an owner reference,
			 * so it is our root.
			 */
			break
		}

		currentObjectReference = ownerReference
	}

	// Marshal currentObjectReference, if it exists, as root owner reference
	if currentObjectReference != nil {
		unstructuredRootOwnerReference, err := runtime.DefaultUnstructuredConverter.ToUnstructured(currentObjectReference)
		if err != nil {
			return fmt.Errorf("cannot convert root owner reference to unstructured: %v; error: %w", currentObjectReference, err)
		}

		logRecordBody["rootOwnerReference"] = unstructuredRootOwnerReference

		destMap := logRecord.Body().SetEmptyMap()
		//nolint:errcheck
		destMap.FromRaw(logRecordBody)
	}

	return nil
}

func (kp *kubernetesprocessor) getFirstRelevantOwnerReferenceAsObjectReference(ctx context.Context, objectReference *corev1.ObjectReference) (*corev1.ObjectReference, error) {
	/*
	 * Check via GVK if the object pointed at by the object reference is relevant for us and,
	 * if so, retrieve the object, and return an object reference to the first owner reference
	 * that has an applicable GVK.
	 */
	objRefGvk := objectReference.GroupVersionKind()

	if !isGVKRelevant(objRefGvk) {
		return nil, nil
	}

	objectRefGvr, _ := toGroupVersionResource(objRefGvk)
	object, err := kp.client.Resource(objectRefGvr).Namespace(objectReference.Namespace).Get(ctx, objectReference.Name, v1.GetOptions{})
	if err != nil {
		return nil, err
	}

	for _, ownerReference := range object.GetOwnerReferences() {
		ownerReferenceGvk := schema.FromAPIVersionAndKind(ownerReference.APIVersion, ownerReference.Kind)

		if isGVKRelevant(ownerReferenceGvk) {
			return &corev1.ObjectReference{
				APIVersion: ownerReference.APIVersion,
				Kind:       ownerReference.Kind,
				// Owner references are always in the same namespace as the owned object
				Namespace: objectReference.Namespace,
				Name:      ownerReference.Name,
				UID:       ownerReference.UID,
			}, nil
		}
	}

	return nil, nil
}

func isGVKRelevant(gvk schema.GroupVersionKind) bool {
	for _, validGvk := range validOwnerReferenceGroupVersionKinds {
		if validGvk == gvk {
			return true
		}
	}

	return false
}

func toGroupVersionResource(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	resourceName := ""
	switch gvk {
	case podv1:
		resourceName = "pods"
	case eventv1:
		resourceName = "events"
	case daemonsetv1:
		resourceName = "daemonsets"
	case deploymentv1:
		resourceName = "deployments"
	case replicasetv1:
		resourceName = "replicasets"
	case statefulsetv1:
		resourceName = "statefulsets"
	case cronjobv1:
		resourceName = "cronjobs"
	case jobv1:
		resourceName = "jobs"
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unexpected GroupVersionKind: %+v", gvk)
	}

	return gvk.GroupVersion().WithResource(resourceName), nil
}
