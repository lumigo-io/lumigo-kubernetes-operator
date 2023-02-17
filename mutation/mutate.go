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

package mutation

import (
	// appsv1 "k8s.io/api/apps/v1"
	// batchv1 "k8s.io/api/batch/v1"

	"fmt"

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"golang.org/x/exp/slices"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const LumigoAutoTraceLabelKey = "lumigo.auto-trace"

const targetDirectoryPath = "/target"
const lumigoInjectorVolumeMountPoint = "/opt/lumigo"

type Mutator interface {
	GetAutotraceLabelValue() string
	MutateAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) error
	MutateAppsV1Deployment(deployment *appsv1.Deployment) error
	MutateAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) error
	MutateAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) error
	MutateBatchV1CronJob(deployment *batchv1.CronJob) error
	MutateBatchV1Job(deployment *batchv1.Job) error
}

type mutatorImpl struct {
	log                         *logr.Logger
	lumigoAutotraceLabelVersion string
	lumigoEndpoint              string
	lumigoToken                 *operatorv1alpha1.Credentials
	lumigoInjectorImage         string
}

func (m *mutatorImpl) GetAutotraceLabelValue() string {
	return m.lumigoAutotraceLabelVersion
}

func NewMutator(Log *logr.Logger, LumigoToken *operatorv1alpha1.Credentials, LumigoOperatorVersion string, LumigoInjectorImage string, TelemetryProxyOtlpServiceUrl string) (Mutator, error) {
	return &mutatorImpl{
		log:                         Log,
		lumigoAutotraceLabelVersion: "lumigo-operator.v" + LumigoOperatorVersion,
		lumigoEndpoint:              TelemetryProxyOtlpServiceUrl,
		lumigoToken:                 LumigoToken,
		lumigoInjectorImage:         LumigoInjectorImage,
	}, nil
}

func (m *mutatorImpl) MutateAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) error {
	if err := m.validateShouldMutate(daemonSet.ObjectMeta); err != nil {
		return err
	}

	if err := m.mutatePodSpec(&daemonSet.Spec.Template.Spec); err != nil {
		return err
	}

	daemonSet.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion
	daemonSet.Spec.Template.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion

	return nil
}

func (m *mutatorImpl) MutateAppsV1Deployment(deployment *appsv1.Deployment) error {
	if err := m.validateShouldMutate(deployment.ObjectMeta); err != nil {
		return err
	}

	if err := m.mutatePodSpec(&deployment.Spec.Template.Spec); err != nil {
		return err
	}

	deployment.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion
	deployment.Spec.Template.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion

	return nil
}

func (m *mutatorImpl) MutateAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) error {
	if err := m.validateShouldMutate(replicaSet.ObjectMeta); err != nil {
		return err
	}

	if err := m.mutatePodSpec(&replicaSet.Spec.Template.Spec); err != nil {
		return err
	}

	replicaSet.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion
	replicaSet.Spec.Template.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion

	return nil
}

func (m *mutatorImpl) MutateAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) error {
	if err := m.validateShouldMutate(statefulSet.ObjectMeta); err != nil {
		return err
	}

	if err := m.mutatePodSpec(&statefulSet.Spec.Template.Spec); err != nil {
		return err
	}

	statefulSet.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion
	statefulSet.Spec.Template.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion

	return nil
}

func (m *mutatorImpl) MutateBatchV1CronJob(batchJob *batchv1.CronJob) error {
	if err := m.validateShouldMutate(batchJob.ObjectMeta); err != nil {
		return err
	}

	if err := m.mutatePodSpec(&batchJob.Spec.JobTemplate.Spec.Template.Spec); err != nil {
		return err
	}

	batchJob.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion
	batchJob.Spec.JobTemplate.Spec.Template.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion

	return nil
}

func (m *mutatorImpl) MutateBatchV1Job(job *batchv1.Job) error {
	if err := m.validateShouldMutate(job.ObjectMeta); err != nil {
		return err
	}

	if err := m.mutatePodSpec(&job.Spec.Template.Spec); err != nil {
		return err
	}

	job.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion
	job.Spec.Template.ObjectMeta.Labels[LumigoAutoTraceLabelKey] = m.lumigoAutotraceLabelVersion

	return nil
}

func (m *mutatorImpl) validateShouldMutate(resourceMeta metav1.ObjectMeta) error {
	autoTraceLabelValue := resourceMeta.Labels[LumigoAutoTraceLabelKey]
	if autoTraceLabelValue == "false" {
		// Opt-out for this resource, skip injection
		return fmt.Errorf("the resource has the '%s' label set to 'false'", LumigoAutoTraceLabelKey)
	}

	return nil
}

func (m *mutatorImpl) mutatePodSpec(podSpec *corev1.PodSpec) error {
	lumigoInjectorVolume := &corev1.Volume{
		Name: "lumigo-injector",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: resource.NewScaledQuantity(200, resource.Mega),
			},
		},
	}

	volumes := podSpec.Volumes
	lumigoInjectorVolumeIndex := slices.IndexFunc(podSpec.Volumes, func(c corev1.Volume) bool { return c.Name == "lumigo-injector" })
	if lumigoInjectorVolumeIndex < 0 {
		volumes = append(volumes, *lumigoInjectorVolume)
	} else {
		volumes[lumigoInjectorVolumeIndex] = *lumigoInjectorVolume
	}
	podSpec.Volumes = volumes

	lumigoInjectorContainer := &corev1.Container{
		Name:  "lumigo-injector",
		Image: m.lumigoInjectorImage,
		Env: []corev1.EnvVar{
			{
				Name:  "TARGET_DIRECTORY",
				Value: targetDirectoryPath,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "lumigo-injector",
				ReadOnly:  false,
				MountPath: targetDirectoryPath,
			},
		},
	}

	initContainers := podSpec.InitContainers
	lumigoInjectorContainerIndex := slices.IndexFunc(initContainers, func(c corev1.Container) bool { return c.Name == "lumigo-injector" })
	if lumigoInjectorContainerIndex < 0 {
		initContainers = append(initContainers, *lumigoInjectorContainer)
	} else {
		initContainers[lumigoInjectorContainerIndex] = *lumigoInjectorContainer
	}
	podSpec.InitContainers = initContainers

	patchedContainers := make([]corev1.Container, 0)
	for _, container := range podSpec.Containers {
		lumigoInjectorVolumeMount := &corev1.VolumeMount{
			Name:      "lumigo-injector",
			ReadOnly:  true,
			MountPath: lumigoInjectorVolumeMountPoint,
		}

		volumeMounts := container.VolumeMounts
		lumigoInjectorVolumeMountIndex := slices.IndexFunc(volumeMounts, func(c corev1.VolumeMount) bool { return c.MountPath == lumigoInjectorVolumeMountPoint })
		if lumigoInjectorVolumeMountIndex < 0 {
			volumeMounts = append(volumeMounts, *lumigoInjectorVolumeMount)
		} else {
			volumeMounts[lumigoInjectorVolumeMountIndex] = *lumigoInjectorVolumeMount
		}
		container.VolumeMounts = volumeMounts

		envVars := container.Env

		ldPreloadEnvVar := &corev1.EnvVar{
			Name:  "LD_PRELOAD",
			Value: lumigoInjectorVolumeMountPoint + "/injector/lumigo_injector.so",
		}
		ldPreloadEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == "LD_PRELOAD" })
		if ldPreloadEnvVarIndex < 0 {
			envVars = append(envVars, *ldPreloadEnvVar)
		} else {
			envVars[ldPreloadEnvVarIndex] = *ldPreloadEnvVar
		}

		lumigoTracerTokenEnvVar := &corev1.EnvVar{
			Name: "LUMIGO_TRACER_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: m.lumigoToken.SecretRef.Name,
					},
					Key:      m.lumigoToken.SecretRef.Key,
					Optional: newTrue(),
				},
			},
		}
		lumigoTracerTokenEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == "LUMIGO_TRACER_TOKEN" })
		if lumigoTracerTokenEnvVarIndex < 0 {
			envVars = append(envVars, *lumigoTracerTokenEnvVar)
		} else {
			envVars[lumigoTracerTokenEnvVarIndex] = *lumigoTracerTokenEnvVar
		}

		lumigoEndpointEnvVar := &corev1.EnvVar{
			Name:  "LUMIGO_ENDPOINT",
			Value: m.lumigoEndpoint,
		}
		lumigoEndpointEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == "LUMIGO_ENDPOINT" })
		if lumigoEndpointEnvVarIndex < 0 {
			envVars = append(envVars, *lumigoEndpointEnvVar)
		} else {
			envVars[lumigoEndpointEnvVarIndex] = *lumigoEndpointEnvVar
		}

		container.Env = envVars

		patchedContainers = append(patchedContainers, container)
	}
	podSpec.Containers = patchedContainers

	return nil
}

func newTrue() *bool {
	b := true
	return &b
}
