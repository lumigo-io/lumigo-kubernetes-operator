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
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// Exists in https://opentelemetry.io/docs/specs/otel/resource/semantic_conventions/k8s/ as of 2023/07/06,
	// but not in go.opentelemetry.io/otel/semconv/v1.20.0
	K8SClusterUIDKey = "k8s.cluster.uid"
)

type kubernetesprocessor struct {
	kube       *internal.KubeClient
	logger     *zap.Logger
	clusterUid types.UID
}

func (kp *kubernetesprocessor) Start(_ context.Context, _ component.Host) error {
	if err := kp.kube.Start(); err != nil {
		return fmt.Errorf("cannot start Kube client: %w", err)
	}

	clusterUid, clusterUidFound := kp.kube.GetClusterUid()
	if !clusterUidFound {
		return fmt.Errorf("cannot add cluster uid resource attributes to traces, 'kube-system' namespace not found")
	}

	kp.clusterUid = clusterUid

	return nil
}

func (kp *kubernetesprocessor) Shutdown(_ context.Context) error {
	kp.kube.Stop()
	return nil
}

func (kp *kubernetesprocessor) processTraces(ctx context.Context, tr ptrace.Traces) (ptrace.Traces, error) {
	resourceSpans := tr.ResourceSpans()
	resourceSpanLength := resourceSpans.Len()
	for i := 0; i < resourceSpanLength; i++ {
		resource := resourceSpans.At(i).Resource()
		resourceAttributes := resourceSpans.At(i).Resource().Attributes()

		resourceAttributes.PutStr(K8SClusterUIDKey, string(kp.clusterUid))

		pod, found := kp.getPod(ctx, &resource)
		if !found {
			kp.logger.Debug(
				"Cannot find pod by 'k8s.pod.uid' of by connection ip",
				zap.Any("resource-attributes", resourceAttributes.AsRaw()),
			)
			continue
		}

		// Ensure 'k8s.pod.uid' is set (we might have found the pod via the network connection ip)
		if _, found := resourceAttributes.Get(string(semconv.K8SPodUIDKey)); !found {
			resourceAttributes.PutStr(string(semconv.K8SPodUIDKey), string(pod.UID))
		}

		resourceAttributes.PutStr(string(semconv.K8SPodNameKey), pod.Name)

		resourceAttributes.PutStr(string(semconv.K8SNamespaceNameKey), pod.Namespace)
		if namespace, nsFound := kp.kube.GetNamespaceByName(pod.Namespace); nsFound {
			resourceAttributes.PutStr("k8s.namespace.uid", string(namespace.UID))
		} else {
			kp.logger.Error(
				"Cannot add namespace resource attributes to traces, namespace not found",
				zap.String("namespace", pod.Namespace),
			)
		}

		ownerObject, found, err := kp.kube.ResolveRelevantOwnerReference(ctx, pod)
		if err != nil {
			kp.logger.Error(
				"Cannot look up owner reference for pod",
				zap.Any("pod", pod),
				zap.Error(err),
			)
		}

		if !found {
			kp.logger.Debug(
				"Pod has no owner reference we can use to add other resource attributes",
				zap.Any("pod", pod),
			)
			continue
		}

		switch podOwner := ownerObject.(type) {
		case *appsv1.DaemonSet:
			{
				resourceAttributes.PutStr(string(semconv.K8SDaemonSetNameKey), podOwner.Name)
				resourceAttributes.PutStr(string(semconv.K8SDaemonSetUIDKey), string(podOwner.UID))
			}
		case *appsv1.ReplicaSet:
			{
				resourceAttributes.PutStr(string(semconv.K8SReplicaSetNameKey), podOwner.Name)
				resourceAttributes.PutStr(string(semconv.K8SReplicaSetUIDKey), string(podOwner.UID))

				if replicaSetOwner, found, err := kp.kube.ResolveRelevantOwnerReference(ctx, podOwner); found {
					if deployment, ok := replicaSetOwner.(*appsv1.Deployment); ok {
						resourceAttributes.PutStr(string(semconv.K8SDeploymentNameKey), deployment.Name)
						resourceAttributes.PutStr(string(semconv.K8SDeploymentUIDKey), string(deployment.UID))
					} else {
						kp.logger.Error(
							"Cannot add deployment resource attributes to traces, replicaset's owner object is not a *apps/v1.Deployment",
							zap.Any("owner-object", ownerObject),
							zap.Error(err),
						)
					}
				} else {
					kp.logger.Debug(
						"Cannot add deployment resource attributes to traces, replicaset has no deployment owner",
						zap.Any("replicaset", podOwner),
						zap.Error(err),
					)
				}
			}
		case *appsv1.StatefulSet:
			{
				resourceAttributes.PutStr(string(semconv.K8SStatefulSetNameKey), podOwner.Name)
				resourceAttributes.PutStr(string(semconv.K8SStatefulSetUIDKey), string(podOwner.UID))
			}
		case *batchv1.Job:
			{
				resourceAttributes.PutStr(string(semconv.K8SJobNameKey), podOwner.Name)
				resourceAttributes.PutStr(string(semconv.K8SJobUIDKey), string(podOwner.UID))

				if jobOwner, found, err := kp.kube.ResolveRelevantOwnerReference(ctx, podOwner); found {
					if cronJob, ok := jobOwner.(*batchv1.CronJob); ok {
						resourceAttributes.PutStr(string(semconv.K8SCronJobNameKey), cronJob.Name)
						resourceAttributes.PutStr(string(semconv.K8SCronJobUIDKey), string(cronJob.UID))
					} else {
						kp.logger.Error(
							"Cannot add cronjob resource attributes to traces, job's object is not a *batch/v1.CronJob",
							zap.Any("owner-object", ownerObject),
							zap.Error(err),
						)
					}
				} else {
					kp.logger.Debug(
						"Cannot add cronjob resource attributes to traces, job has no cronjob owner",
						zap.Any("job", podOwner),
						zap.Error(err),
					)
				}
			}
		default:
			{
				kp.logger.Error("Unexpected owner-object for pod", zap.Any("pod", pod), zap.Any("owner-object", ownerObject))
			}
		}
	}

	return tr, nil
}

func (kp *kubernetesprocessor) getPod(ctx context.Context, resource *pcommon.Resource) (*corev1.Pod, bool) {
	// Try to look for k8s.pod.uid and see if we have a match in our cache
	if podUID, found := resource.Attributes().Get(string(semconv.K8SPodUIDKey)); found {
		podUISAsStr := podUID.AsString()

		if pod, found := kp.kube.GetPodByUID(types.UID(podUISAsStr)); found {
			return pod, true
		}
	}

	// Try to look by connection ip, matching the pod ips known by the Kube API
	if pod, found := kp.kube.GetPodByNetworkConnection(ctx); found {
		return pod, true
	}

	return nil, false
}

func (kp *kubernetesprocessor) processLogs(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
	resourceLogs := ld.ResourceLogs()

	for i := 0; i < resourceLogs.Len(); i++ {
		rl := resourceLogs.At(i)
		rl.Resource().Attributes().PutStr(K8SClusterUIDKey, string(kp.clusterUid))
		kp.processResourceLogs(ctx, &rl)
	}

	return ld, nil
}

func (kp *kubernetesprocessor) processResourceLogs(ctx context.Context, resourceLogs *plog.ResourceLogs) {
	scopeLogs := resourceLogs.ScopeLogs()
	for i := 0; i < scopeLogs.Len(); i++ {
		sl := scopeLogs.At(i)

		switch sl.Scope().Name() {
		case "lumigo-operator.heartbeat":
			{
				return
			}
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
			kp.logger.Error(
				"An error occurred while processing a log record",
				zap.Any("logRecord attributes", logRecord.Attributes().AsRaw()),
				zap.Any("logRecord body", logRecord.Body().AsRaw()),
				zap.Error(err),
			)
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
