// Copyright 2023 Lumigo
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

package k8sdataenricherprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sdataenricherprocessor"

import (
	"context"
	"fmt"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sdataenricherprocessor/internal"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type kubernetesprocessor struct {
	kube   *internal.KubeClient
	logger *zap.Logger
}

func (kp *kubernetesprocessor) Start(_ context.Context, _ component.Host) error {
	if err := kp.kube.Start(); err != nil {
		return fmt.Errorf("cannot start Kube client: %w", err)
	}
	return nil
}

func (kp *kubernetesprocessor) Shutdown(_ context.Context) error {
	kp.kube.Stop()
	return nil
}

func (kp *kubernetesprocessor) processTraces(ctx context.Context, tr ptrace.Traces) (ptrace.Traces, error) {
	// kp.logger.Debug("Is LumigoToken there?", zap.Any("lumigo-token", ctx.Value("lumigo-token"))) // Seems that no: it is not there

	resourceSpans := tr.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		resourceAttributes := resourceSpans.At(i).Resource().Attributes()

		if namespaceName, hasNamespaceName := resourceAttributes.Get(string(semconv.K8SNamespaceNameKey)); hasNamespaceName {
			if namespaceUID, err := kp.kube.GetNamespaceUID(namespaceName.AsString()); err != nil {
				kp.logger.Error(
					"v1.Namespace not found when enriching trace data",
					zap.String("namespace-name", namespaceName.AsString()),
					zap.Any("resource-attributes", resourceAttributes.AsRaw()),
					zap.Error(err),
				)
			} else {
				resourceAttributes.PutStr("k8s.namespace.uid", string(namespaceUID))

				if deploymentName, hasDeploymentName := resourceAttributes.Get(string(semconv.K8SDeploymentNameKey)); hasDeploymentName {
					if deploymentUID, err := kp.kube.GetDeploymentUID(deploymentName.AsString(), namespaceName.AsString()); err != nil {
						kp.logger.Error(
							"apps/v1.Deployment not found when enriching trace data",
							zap.String("namespace-name", namespaceName.AsString()),
							zap.String("deployment-name", deploymentName.AsString()),
							zap.Any("resource-attributes", resourceAttributes.AsRaw()),
							zap.Error(err),
						)
					} else {
						resourceAttributes.PutStr(string(semconv.K8SDeploymentUIDKey), string(deploymentUID))
					}
				}

				if cronJobName, hasCronJobName := resourceAttributes.Get(string(semconv.K8SCronJobNameKey)); hasCronJobName {
					if cronJobUID, err := kp.kube.GetCronJobUID(cronJobName.AsString(), namespaceName.AsString()); err != nil {
						kp.logger.Error(
							"batch/v1.CronJob not found when enriching trace data",
							zap.String("namespace-name", namespaceName.AsString()),
							zap.String("cronjob-name", cronJobName.AsString()),
							zap.Any("resource-attributes", resourceAttributes),
						)

					} else {
						resourceAttributes.PutStr(string(semconv.K8SCronJobUIDKey), string(cronJobUID))
					}
				}
			}
		} else {
			kp.logger.Error(fmt.Sprintf("Resource attribute '%s' not found when enriching trace data", semconv.K8SNamespaceNameKey), zap.Any("resource-attributes", resourceAttributes))
		}
	}

	return tr, nil
}

func (kp *kubernetesprocessor) processLogs(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
	resourceLogs := ld.ResourceLogs()
	for i := 0; i < resourceLogs.Len(); i++ {
		rl := resourceLogs.At(i)
		kp.processResourceLogs(ctx, &rl)
	}

	return ld, nil
}

func (kp *kubernetesprocessor) processResourceLogs(ctx context.Context, resourceLogs *plog.ResourceLogs) {
	scopeLogs := resourceLogs.ScopeLogs()
	for i := 0; i < scopeLogs.Len(); i++ {
		sl := scopeLogs.At(i)

		switch sl.Scope().Name() {
		case "lumigo-operator.k8s-events":
			{
				kp.processKubernetesEventScopeLogs(ctx, &sl)
			}
		case "lumigo-operator.k8s-objects":
			{
				kp.processKubernetesObjectScopeLogs(ctx, &sl)
			}
		default:
			{
				kp.logger.Error("Unexpected logs scope", zap.String("scope-name", sl.Scope().Name()))
			}
		}
	}

	// Piggyback to downstream the new object resourceversions
	lookForMoreNewResourceVersions := true
	newResourceVersions := make([]runtime.Object, 0)
	for lookForMoreNewResourceVersions {
		select {
		case object := <-kp.kube.ObjectResourceVersions:
			{
				newResourceVersions = append(newResourceVersions, object)
			}
		default:
			lookForMoreNewResourceVersions = false
		}
	}

	if len(newResourceVersions) > 0 {
		newScopeLogs := resourceLogs.ScopeLogs().AppendEmpty()
		newScopeLogs.Scope().SetName("lumigo-operator.k8s-objects")
		for _, newResourceVersion := range newResourceVersions {
			if newResourceVersionRaw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(newResourceVersion); err != nil {
				kp.logger.Error(
					"Cannot convert to unstructured object to emit",
					zap.Any("object", newResourceVersion),
					zap.Error(err),
				)
			} else {
				kp.logger.Debug(
					"Emitting object",
					zap.Any("object", newResourceVersion),
				)

				newLogRecord := newScopeLogs.LogRecords().AppendEmpty()
				newLogRecord.Attributes().PutBool("emitted-by-enricher", true)
				dest := newLogRecord.Body()
				destMap := dest.SetEmptyMap()
				//nolint:errcheck
				destMap.FromRaw(newResourceVersionRaw)
			}
		}
	}
}

func (kp *kubernetesprocessor) processKubernetesEventScopeLogs(ctx context.Context, scopeLogs *plog.ScopeLogs) {
	// In this stream we try to "learn" abount objects before we need to reference them in root-owner references and the like
	logRecords := scopeLogs.LogRecords()
	logRecordsLength := logRecords.Len()

	for j := 0; j < logRecordsLength; j++ {
		logRecord := logRecords.At(j)

		if err := kp.processKubernetesEventLogRecord(ctx, &logRecord); err != nil {
			kp.logger.Error(
				"an error occurred while processing log record as event",
				zap.Any("log-record body", logRecord.Body().AsRaw()),
				zap.Error(err),
			)
		}
	}
}

func (kp *kubernetesprocessor) processKubernetesEventLogRecord(ctx context.Context, logRecord *plog.LogRecord) error {
	logRecordBody, isOk := logRecord.Body().AsRaw().(map[string]interface{})
	if !isOk {
		return fmt.Errorf("cannot access log record body as map")
	}

	// Check if it is an event we pull (GVK is v1/Event) or is it a watch event
	u := unstructured.Unstructured{
		Object: logRecordBody,
	}

	rootOwnerReferenceParent := logRecordBody
	eventFound := false

	event := corev1.Event{}
	gvk := u.GroupVersionKind()
	if gvk == internal.V1Event {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, &event); err != nil {
			return fmt.Errorf("cannot parse log-record body as v1.Event: %w", err)
		} else {
			eventFound = true
		}
	} else if gvk.Kind == "" {
		// Check whether it is a watch event
		if object, found := logRecordBody["object"]; found {
			if objectAsMap, isOK := object.(map[string]interface{}); isOK {
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(objectAsMap, &event); err != nil {
					return fmt.Errorf("cannot parse log-record body as watch event containing a v1.Event: %w", err)
				} else {
					rootOwnerReferenceParent = objectAsMap
					eventFound = true
				}
			}
		}
	}

	if !eventFound {
		return fmt.Errorf("the log record seems to be containing neither a v1.Event, nor a watch event for a v1.Event")
	}

	if rootOwnerReference, err := kp.kube.GetRootOwnerReference(ctx, &event); err != nil {
		return fmt.Errorf("cannot retrieve root-owner reference: %w", err)
	} else {
		rootOwnerReferenceParent["rootOwnerReference"] = rootOwnerReference
	}

	// Update the log record body
	destMap := logRecord.Body().SetEmptyMap()
	destMap.FromRaw(logRecordBody)

	return kp.kube.EnsureObjectReferenceIsKnown(event.InvolvedObject)
}

func (kp *kubernetesprocessor) processKubernetesObjectScopeLogs(ctx context.Context, scopeLogs *plog.ScopeLogs) {
	logRecords := scopeLogs.LogRecords()
	logRecordsLength := logRecords.Len()
	for j := 0; j < logRecordsLength; j++ {
		logRecord := logRecords.At(j)

		if err := kp.processKubernetesObjectLogRecord(ctx, &logRecord); err != nil {
			kp.logger.Error("An error occurred while processing a log record", zap.Any("logRecord attributes", logRecord.Attributes().AsRaw()), zap.Any("logRecord body", logRecord.Body().AsRaw()), zap.Error(err))
		}
	}
}

func (kp *kubernetesprocessor) processKubernetesObjectLogRecord(ctx context.Context, logRecord *plog.LogRecord) error {
	logRecordBody, isOk := logRecord.Body().AsRaw().(map[string]interface{})
	if !isOk {
		return fmt.Errorf("cannot access log record body as map: %v", logRecord.Body().AsRaw())
	}

	u := &unstructured.Unstructured{
		Object: logRecordBody,
	}

	// Since we are also receiving object resource versions in the log pipeline,
	// check if they are some we miss, as it might be the difference between
	// setting important resource attributes on traces or not.
	switch u.GroupVersionKind() {
	case internal.V1Pod:
		{
			pod := &corev1.Pod{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, pod); err != nil {
				return err
			} else {
				kp.kube.RegisterPod(pod)
			}
		}
	case internal.AppsV1DaemonSet:
		{
			daemonSet := &appsv1.DaemonSet{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, daemonSet); err != nil {
				return err
			} else {
				kp.kube.RegisterDaemonSet(daemonSet)
			}
		}
	case internal.AppsV1Deployment:
		{
			deployment := &appsv1.Deployment{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, deployment); err != nil {
				return err
			} else {
				kp.kube.RegisterDeployment(deployment)
			}
		}
	case internal.AppsV1ReplicaSet:
		{
			replicaSet := &appsv1.ReplicaSet{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, replicaSet); err != nil {
				return err
			} else {
				kp.kube.RegisterReplicaSet(replicaSet)
			}
		}
	case internal.AppsV1StatefulSet:
		{
			statefulSet := &appsv1.StatefulSet{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, statefulSet); err != nil {
				return err
			} else {
				kp.kube.RegisterStatefulSet(statefulSet)
			}
		}
	case internal.BatchV1CronJob:
		{
			cronJob := &batchv1.CronJob{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, cronJob); err != nil {
				return err
			} else {
				kp.kube.RegisterCronJob(cronJob)
			}
		}
	case internal.BatchV1Job:
		{
			job := &batchv1.Job{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, job); err != nil {
				return err
			} else {
				kp.kube.RegisterJob(job)
			}
		}
	case internal.V1Namespace:
		{
			namespace := &corev1.Namespace{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(logRecordBody, namespace); err != nil {
				return err
			} else {
				kp.kube.RegisterNamespace(namespace)
			}
		}
	}

	return nil
}
