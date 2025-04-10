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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/mutation"
)

// LumigoResourceManager handles operations on Kubernetes resources for Lumigo instrumentation
type LumigoResourceManager struct {
	Client          client.Client
	Clientset       *kubernetes.Clientset
	EventRecorder   record.EventRecorder
	Log             *logr.Logger
	OperatorVersion string
}

func NewLumigoResourceManager(client client.Client, clientset *kubernetes.Clientset, eventRecorder record.EventRecorder, operatorVersion string, log *logr.Logger) *LumigoResourceManager {
	return &LumigoResourceManager{
		Client:          client,
		Clientset:       clientset,
		EventRecorder:   eventRecorder,
		Log:             log,
		OperatorVersion: operatorVersion,
	}
}

func (m *LumigoResourceManager) RemoveLumigoFromResources(ctx context.Context, namespace string, trigger string) error {
	mutator := mutation.NewRemovalMutator(m.Log, m.OperatorVersion)

	lumigoAutotracedListOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%[1]s,%[1]s != false", mutation.LumigoAutoTraceLabelKey),
	}

	daemonsets, err := m.Clientset.AppsV1().DaemonSets(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return fmt.Errorf("cannot list autotraced daemonsets: %w", err)
	}

	for _, daemonset := range daemonsets.Items {
		if err := retry(fmt.Sprintf("remove instrumentation from the %s/%s daemonset", daemonset.Namespace, daemonset.Name), func() error {
			if err := m.Client.Get(ctx, client.ObjectKey{
				Namespace: daemonset.Namespace,
				Name:      daemonset.Name,
			}, &daemonset); err != nil {
				return fmt.Errorf("cannot retrieve details of daemonset '%s': %w", daemonset.GetName(), err)
			}

			mutatedDaemonset := daemonset.DeepCopy()
			if mutationOccurred, err := mutator.RemoveLumigoFromAppsV1DaemonSet(mutatedDaemonset); err != nil {
				return fmt.Errorf("cannot prepare mutation of daemonset '%s': %w", mutatedDaemonset.Name, err)
			} else if mutationOccurred {
				addAutoTraceSkipNextInjectorLabel(&mutatedDaemonset.ObjectMeta)
				return m.Client.Update(ctx, mutatedDaemonset)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, m.Log); err != nil {
			operatorv1alpha1.RecordCannotRemoveInstrumentationEvent(m.EventRecorder, &daemonset, trigger, err)
			return fmt.Errorf("cannot remove instrumentation from daemonset '%s': %w", daemonset.Name, err)
		} else {
			m.Log.Info("Removed instrumentation from daemonset", "namespace", daemonset.Namespace, "name", daemonset.Name)
			operatorv1alpha1.RecordRemovedInstrumentationEvent(m.EventRecorder, &daemonset, trigger)
		}
	}

	deployments, err := m.Clientset.AppsV1().Deployments(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return fmt.Errorf("cannot list autotraced deployments: %w", err)
	}

	for _, deployment := range deployments.Items {
		if err := retry(fmt.Sprintf("remove instrumentation from the %s/%s deployment", deployment.Namespace, deployment.Name), func() error {
			if err := m.Client.Get(ctx, client.ObjectKey{
				Namespace: deployment.Namespace,
				Name:      deployment.Name,
			}, &deployment); err != nil {
				return fmt.Errorf("cannot retrieve details of deployment '%s': %w", deployment.GetName(), err)
			}

			mutatedDeployment := deployment.DeepCopy()
			if mutationOccurred, err := mutator.RemoveLumigoFromAppsV1Deployment(mutatedDeployment); err != nil {
				return fmt.Errorf("cannot prepare mutation of deployment '%s': %w", mutatedDeployment.Name, err)
			} else if mutationOccurred {
				addAutoTraceSkipNextInjectorLabel(&mutatedDeployment.ObjectMeta)
				return m.Client.Update(ctx, mutatedDeployment)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, m.Log); err != nil {
			operatorv1alpha1.RecordCannotRemoveInstrumentationEvent(m.EventRecorder, &deployment, trigger, err)
			return fmt.Errorf("cannot remove instrumentation from deployment '%s': %w", deployment.Name, err)
		} else {
			m.Log.Info("Removed instrumentation from deployment", "namespace", deployment.Namespace, "name", deployment.Name)
			operatorv1alpha1.RecordRemovedInstrumentationEvent(m.EventRecorder, &deployment, trigger)
		}
	}

	replicasets, err := m.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return fmt.Errorf("cannot list autotraced replicasets: %w", err)
	}

	for _, replicaset := range replicasets.Items {
		if err := retry(fmt.Sprintf("remove instrumentation from the %s/%s replicaset", replicaset.Namespace, replicaset.Name), func() error {
			if err := m.Client.Get(ctx, client.ObjectKey{
				Namespace: replicaset.Namespace,
				Name:      replicaset.Name,
			}, &replicaset); err != nil {
				return fmt.Errorf("cannot retrieve details of replicaset '%s': %w", replicaset.GetName(), err)
			}

			mutatedReplicaset := replicaset.DeepCopy()
			if mutationOccurred, err := mutator.RemoveLumigoFromAppsV1ReplicaSet(mutatedReplicaset); err != nil {
				return fmt.Errorf("cannot prepare mutation of replicaset '%s': %w", mutatedReplicaset.Name, err)
			} else if mutationOccurred {
				addAutoTraceSkipNextInjectorLabel(&mutatedReplicaset.ObjectMeta)
				return m.Client.Update(ctx, mutatedReplicaset)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, m.Log); err != nil {
			operatorv1alpha1.RecordCannotRemoveInstrumentationEvent(m.EventRecorder, &replicaset, trigger, err)
			return fmt.Errorf("cannot remove instrumentation from replicaset '%s': %w", replicaset.Name, err)
		} else {
			m.Log.Info("Removed instrumentation from replicaset", "namespace", replicaset.Namespace, "name", replicaset.Name)
			operatorv1alpha1.RecordRemovedInstrumentationEvent(m.EventRecorder, &replicaset, trigger)
		}
	}

	statefulsets, err := m.Clientset.AppsV1().StatefulSets(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return fmt.Errorf("cannot list autotraced statefulsets: %w", err)
	}

	for _, statefulset := range statefulsets.Items {
		if err := retry(fmt.Sprintf("remove instrumentation from the %s/%s statefulset", statefulset.Namespace, statefulset.Name), func() error {
			if err := m.Client.Get(ctx, client.ObjectKey{
				Namespace: statefulset.Namespace,
				Name:      statefulset.Name,
			}, &statefulset); err != nil {
				return fmt.Errorf("cannot retrieve details of statefulset '%s': %w", statefulset.GetName(), err)
			}

			mutatedStatefulset := statefulset.DeepCopy()
			if mutationOccurred, err := mutator.RemoveLumigoFromAppsV1StatefulSet(mutatedStatefulset); err != nil {
				return fmt.Errorf("cannot prepare mutation of statefulset '%s': %w", mutatedStatefulset.Name, err)
			} else if mutationOccurred {
				addAutoTraceSkipNextInjectorLabel(&mutatedStatefulset.ObjectMeta)
				return m.Client.Update(ctx, mutatedStatefulset)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, m.Log); err != nil {
			operatorv1alpha1.RecordCannotRemoveInstrumentationEvent(m.EventRecorder, &statefulset, trigger, err)
			return fmt.Errorf("cannot remove instrumentation from statefulset '%s': %w", statefulset.Name, err)
		} else {
			m.Log.Info("Removed instrumentation from statefulset", "namespace", statefulset.Namespace, "name", statefulset.Name)
			operatorv1alpha1.RecordRemovedInstrumentationEvent(m.EventRecorder, &statefulset, trigger)
		}
	}

	cronjobs, err := m.Clientset.BatchV1().CronJobs(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return fmt.Errorf("cannot list autotraced cronjobs: %w", err)
	}

	for _, cronjob := range cronjobs.Items {
		if err := retry(fmt.Sprintf("remove instrumentation from the %s/%s cronjob", cronjob.Namespace, cronjob.Name), func() error {
			if err := m.Client.Get(ctx, client.ObjectKey{
				Namespace: cronjob.Namespace,
				Name:      cronjob.Name,
			}, &cronjob); err != nil {
				return fmt.Errorf("cannot retrieve details of cronjob '%s': %w", cronjob.GetName(), err)
			}

			mutatedCronjob := cronjob.DeepCopy()
			if mutationOccurred, err := mutator.RemoveLumigoFromBatchV1CronJob(mutatedCronjob); err != nil {
				return fmt.Errorf("cannot prepare mutation of cronjob '%s': %w", mutatedCronjob.Name, err)
			} else if mutationOccurred {
				addAutoTraceSkipNextInjectorLabel(&mutatedCronjob.ObjectMeta)
				return m.Client.Update(ctx, mutatedCronjob)
			} else {
				return nil
			}
		}, maxMutationRetryAttempts, retryOnMutationErrorMatcher, m.Log); err != nil {
			operatorv1alpha1.RecordCannotRemoveInstrumentationEvent(m.EventRecorder, &cronjob, trigger, err)
			return fmt.Errorf("cannot remove instrumentation from cronjob '%s': %w", cronjob.Name, err)
		} else {
			m.Log.Info("Removed instrumentation from cronjob", "namespace", cronjob.Namespace, "name", cronjob.Name)
			operatorv1alpha1.RecordRemovedInstrumentationEvent(m.EventRecorder, &cronjob, trigger)
		}
	}

	// Cannot mutate existing jobs: their PodSpecs are immutable!
	jobs, err := m.Clientset.BatchV1().Jobs(namespace).List(ctx, lumigoAutotracedListOptions)
	if err != nil {
		return fmt.Errorf("cannot list autotraced jobs: %w", err)
	}

	for _, job := range jobs.Items {
		operatorv1alpha1.RecordCannotRemoveInstrumentationEvent(m.EventRecorder, &job, trigger, fmt.Errorf("the PodSpec of batchv1.Job resources is immutable once the job has been created"))
		m.Log.Info("Cannot remove instrumentation from job: jobs are immutable once created", "namespace", job.Namespace, "name", job.Name)
	}

	return nil
}

func (m *LumigoResourceManager) RemoveLumigoFromAllNamespaces(ctx context.Context, trigger string) error {
	namespaces, err := m.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	m.Log.Info("Starting removal of Lumigo instrumentation from all namespaces", "namespaceCount", len(namespaces.Items))

	for _, namespace := range namespaces.Items {
		namespaceName := namespace.Name
		m.Log.Info("Removing Lumigo instrumentation from namespace", "namespace", namespaceName)

		if err := m.RemoveLumigoFromResources(ctx, namespaceName, trigger); err != nil {
			m.Log.Error(err, "Failed to remove Lumigo instrumentation from namespace", "namespace", namespaceName)
		}
	}

	m.Log.Info("Completed removal of Lumigo instrumentation from all namespaces")
	return nil
}
