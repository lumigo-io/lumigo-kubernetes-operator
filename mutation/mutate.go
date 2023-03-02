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
	"strings"

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
const LumigoAutoTraceLabelSkipNextInjectorValue = "skip-next-injector"

const TargetDirectoryEnvVarName = "TARGET_DIRECTORY"
const TargetDirectoryPath = "/target"
const LumigoInjectorContainerName = "lumigo-injector"
const LumigoInjectorVolumeName = "lumigo-injector"
const LumigoInjectorVolumeMountPoint = "/opt/lumigo"
const LumigoTracerTokenEnvVarName = "LUMIGO_TRACER_TOKEN"
const LumigoEndpointEnvVarName = "LUMIGO_ENDPOINT"
const LdPreloadEnvVarName = "LD_PRELOAD"
const LdPreloadEnvVarValue = LumigoInjectorVolumeMountPoint + "/injector/lumigo_injector.so"

type Mutator interface {
	GetAutotraceLabelValue() string
	InjectLumigoInto(resource interface{}) error
	InjectLumigoIntoAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) error
	InjectLumigoIntoAppsV1Deployment(deployment *appsv1.Deployment) error
	InjectLumigoIntoAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) error
	InjectLumigoIntoAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) error
	InjectLumigoIntoBatchV1CronJob(deployment *batchv1.CronJob) error
	InjectLumigoIntoBatchV1Job(deployment *batchv1.Job) error
	RemoveLumigoFrom(resource interface{}) error
	RemoveLumigoFromAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) error
	RemoveLumigoFromAppsV1Deployment(deployment *appsv1.Deployment) error
	RemoveLumigoFromAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) error
	RemoveLumigoFromAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) error
	RemoveLumigoFromBatchV1CronJob(deployment *batchv1.CronJob) error
	RemoveLumigoFromBatchV1Job(deployment *batchv1.Job) error
}

type mutatorImpl struct {
	log                       *logr.Logger
	lumigoAutotraceLabelValue string
	lumigoEndpoint            string
	lumigoToken               *operatorv1alpha1.Credentials
	lumigoInjectorImage       string
}

func (m *mutatorImpl) GetAutotraceLabelValue() string {
	return m.lumigoAutotraceLabelValue
}

func NewMutator(Log *logr.Logger, LumigoToken *operatorv1alpha1.Credentials, LumigoOperatorVersion string, LumigoInjectorImage string, TelemetryProxyOtlpServiceUrl string) (Mutator, error) {
	version := LumigoOperatorVersion

	if len(version) > 8 {
		version = version[0:7] // Label values have a limit of 63 characters, we stay well below that
	}

	return &mutatorImpl{
		log:                       Log,
		lumigoAutotraceLabelValue: "lumigo-operator.v" + version,
		lumigoEndpoint:            TelemetryProxyOtlpServiceUrl,
		lumigoToken:               LumigoToken,
		lumigoInjectorImage:       LumigoInjectorImage,
	}, nil
}

func (m *mutatorImpl) InjectLumigoInto(resource interface{}) error {
	switch a := resource.(type) {
	case *appsv1.DaemonSet:
		return m.InjectLumigoIntoAppsV1DaemonSet(a)
	case *appsv1.Deployment:
		return m.InjectLumigoIntoAppsV1Deployment(a)
	case *appsv1.ReplicaSet:
		return m.InjectLumigoIntoAppsV1ReplicaSet(a)
	case *appsv1.StatefulSet:
		return m.InjectLumigoIntoAppsV1StatefulSet(a)
	case *batchv1.CronJob:
		return m.InjectLumigoIntoBatchV1CronJob(a)
	case *batchv1.Job:
		return m.InjectLumigoIntoBatchV1Job(a)
	default:
		return fmt.Errorf("unexpected resource type to mutate: %+v", a)
	}
}

func (m *mutatorImpl) RemoveLumigoFrom(resource interface{}) error {
	switch a := resource.(type) {
	case *appsv1.DaemonSet:
		return m.RemoveLumigoFromAppsV1DaemonSet(a)
	case *appsv1.Deployment:
		return m.RemoveLumigoFromAppsV1Deployment(a)
	case *appsv1.ReplicaSet:
		return m.RemoveLumigoFromAppsV1ReplicaSet(a)
	case *appsv1.StatefulSet:
		return m.RemoveLumigoFromAppsV1StatefulSet(a)
	case *batchv1.CronJob:
		return m.RemoveLumigoFromBatchV1CronJob(a)
	case *batchv1.Job:
		return m.RemoveLumigoFromBatchV1Job(a)
	default:
		return fmt.Errorf("unexpected resource type to mutate: %+v", a)
	}

}

func (m *mutatorImpl) InjectLumigoIntoAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) error {
	return m.injectLumigoInto(&daemonSet.ObjectMeta, &daemonSet.Spec.Template)
}

func (m *mutatorImpl) RemoveLumigoFromAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) error {
	return m.removeLumigoFrom(&daemonSet.ObjectMeta, &daemonSet.Spec.Template)
}

func (m *mutatorImpl) InjectLumigoIntoAppsV1Deployment(deployment *appsv1.Deployment) error {
	return m.injectLumigoInto(&deployment.ObjectMeta, &deployment.Spec.Template)
}

func (m *mutatorImpl) RemoveLumigoFromAppsV1Deployment(deployment *appsv1.Deployment) error {
	return m.removeLumigoFrom(&deployment.ObjectMeta, &deployment.Spec.Template)
}

func (m *mutatorImpl) InjectLumigoIntoAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) error {
	return m.injectLumigoInto(&replicaSet.ObjectMeta, &replicaSet.Spec.Template)
}

func (m *mutatorImpl) RemoveLumigoFromAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) error {
	return m.removeLumigoFrom(&replicaSet.ObjectMeta, &replicaSet.Spec.Template)
}

func (m *mutatorImpl) InjectLumigoIntoAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) error {
	return m.injectLumigoInto(&statefulSet.ObjectMeta, &statefulSet.Spec.Template)
}

func (m *mutatorImpl) RemoveLumigoFromAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) error {
	return m.removeLumigoFrom(&statefulSet.ObjectMeta, &statefulSet.Spec.Template)
}

func (m *mutatorImpl) InjectLumigoIntoBatchV1CronJob(batchJob *batchv1.CronJob) error {
	return m.injectLumigoInto(&batchJob.ObjectMeta, &batchJob.Spec.JobTemplate.Spec.Template)
}

func (m *mutatorImpl) RemoveLumigoFromBatchV1CronJob(batchJob *batchv1.CronJob) error {
	return m.removeLumigoFrom(&batchJob.ObjectMeta, &batchJob.Spec.JobTemplate.Spec.Template)
}

func (m *mutatorImpl) InjectLumigoIntoBatchV1Job(job *batchv1.Job) error {
	return m.injectLumigoInto(&job.ObjectMeta, &job.Spec.Template)
}

func (m *mutatorImpl) RemoveLumigoFromBatchV1Job(job *batchv1.Job) error {
	return m.removeLumigoFrom(&job.ObjectMeta, &job.Spec.Template)
}

func (m *mutatorImpl) injectLumigoInto(topLevelObjectMeta *metav1.ObjectMeta, podTemplateSpec *corev1.PodTemplateSpec) error {
	if err := m.validateShouldInjectLumigoInto(topLevelObjectMeta); err != nil {
		return err
	}

	if err := m.injectLumigoIntoPodSpec(&podTemplateSpec.Spec); err != nil {
		return err
	}

	addAutoTraceLabel(topLevelObjectMeta, m.lumigoAutotraceLabelValue)
	addAutoTraceLabel(&podTemplateSpec.ObjectMeta, m.lumigoAutotraceLabelValue)

	return nil
}

func addAutoTraceLabel(objectMeta *metav1.ObjectMeta, value string) {
	if objectMeta.Labels == nil {
		objectMeta.Labels = map[string]string{
			LumigoAutoTraceLabelKey: value,
		}
	} else {
		objectMeta.Labels[LumigoAutoTraceLabelKey] = value
	}
}

func (m *mutatorImpl) removeLumigoFrom(topLevelObjectMeta *metav1.ObjectMeta, podTemplateSpec *corev1.PodTemplateSpec) error {
	if err := m.removeLumigoFromPodSpec(&podTemplateSpec.Spec); err != nil {
		return err
	}

	removeAutoTraceLabel(topLevelObjectMeta)
	removeAutoTraceLabel(&podTemplateSpec.ObjectMeta)

	return nil
}

func removeAutoTraceLabel(objectMeta *metav1.ObjectMeta) {
	if objectMeta != nil && objectMeta.Labels != nil {
		delete(objectMeta.Labels, LumigoAutoTraceLabelKey)
	}
}

func (m *mutatorImpl) validateShouldInjectLumigoInto(resourceMeta *metav1.ObjectMeta) error {
	autoTraceLabelValue := resourceMeta.Labels[LumigoAutoTraceLabelKey]
	if strings.ToLower(autoTraceLabelValue) == "false" {
		// Opt-out for this resource, skip injection
		return fmt.Errorf("the resource has the '%s' label set to 'false'", LumigoAutoTraceLabelKey)
	}

	return nil
}

func (m *mutatorImpl) injectLumigoIntoPodSpec(podSpec *corev1.PodSpec) error {
	lumigoInjectorVolume := &corev1.Volume{
		Name: LumigoInjectorVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: resource.NewScaledQuantity(200, resource.Mega),
			},
		},
	}

	volumes := podSpec.Volumes
	if volumes == nil {
		volumes = []corev1.Volume{}
	}

	lumigoInjectorVolumeIndex := slices.IndexFunc(podSpec.Volumes, func(c corev1.Volume) bool { return c.Name == LumigoInjectorVolumeName })
	if lumigoInjectorVolumeIndex < 0 {
		volumes = append(volumes, *lumigoInjectorVolume)
	} else {
		volumes[lumigoInjectorVolumeIndex] = *lumigoInjectorVolume
	}
	podSpec.Volumes = volumes

	lumigoInjectorContainer := &corev1.Container{
		Name:  LumigoInjectorContainerName,
		Image: m.lumigoInjectorImage,
		Env: []corev1.EnvVar{
			{
				Name:  TargetDirectoryEnvVarName,
				Value: TargetDirectoryPath,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      LumigoInjectorVolumeName,
				ReadOnly:  false,
				MountPath: TargetDirectoryPath,
			},
		},
	}

	initContainers := podSpec.InitContainers
	if initContainers == nil {
		initContainers = []corev1.Container{}
	}

	lumigoInjectorContainerIndex := slices.IndexFunc(initContainers, func(c corev1.Container) bool { return c.Name == LumigoInjectorContainerName })
	if lumigoInjectorContainerIndex < 0 {
		initContainers = append(initContainers, *lumigoInjectorContainer)
	} else {
		initContainers[lumigoInjectorContainerIndex] = *lumigoInjectorContainer
	}
	podSpec.InitContainers = initContainers

	patchedContainers := []corev1.Container{}
	for _, container := range podSpec.Containers {
		lumigoInjectorVolumeMount := &corev1.VolumeMount{
			Name:      LumigoInjectorVolumeName,
			ReadOnly:  true,
			MountPath: LumigoInjectorVolumeMountPoint,
		}

		volumeMounts := container.VolumeMounts
		if volumeMounts == nil {
			volumeMounts = []corev1.VolumeMount{}
		}

		lumigoInjectorVolumeMountIndex := slices.IndexFunc(volumeMounts, func(c corev1.VolumeMount) bool { return c.MountPath == LumigoInjectorVolumeMountPoint })
		if lumigoInjectorVolumeMountIndex < 0 {
			volumeMounts = append(volumeMounts, *lumigoInjectorVolumeMount)
		} else {
			volumeMounts[lumigoInjectorVolumeMountIndex] = *lumigoInjectorVolumeMount
		}
		container.VolumeMounts = volumeMounts

		envVars := container.Env
		if envVars == nil {
			envVars = []corev1.EnvVar{}
		}

		ldPreloadEnvVar := &corev1.EnvVar{
			Name:  LdPreloadEnvVarName,
			Value: LdPreloadEnvVarValue,
		}
		ldPreloadEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == LdPreloadEnvVarName })
		if ldPreloadEnvVarIndex < 0 {
			envVars = append(envVars, *ldPreloadEnvVar)
		} else {
			envVars[ldPreloadEnvVarIndex] = *ldPreloadEnvVar
		}

		lumigoTracerTokenEnvVar := &corev1.EnvVar{
			Name: LumigoTracerTokenEnvVarName,
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
		lumigoTracerTokenEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == LumigoTracerTokenEnvVarName })
		if lumigoTracerTokenEnvVarIndex < 0 {
			envVars = append(envVars, *lumigoTracerTokenEnvVar)
		} else {
			envVars[lumigoTracerTokenEnvVarIndex] = *lumigoTracerTokenEnvVar
		}

		lumigoEndpointEnvVar := &corev1.EnvVar{
			Name:  LumigoEndpointEnvVarName,
			Value: m.lumigoEndpoint,
		}
		lumigoEndpointEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == LumigoEndpointEnvVarName })
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

func (m *mutatorImpl) removeLumigoFromPodSpec(podSpec *corev1.PodSpec) error {
	if podSpec.InitContainers != nil {
		newInitContainers := []corev1.Container{}
		for _, initContainer := range podSpec.InitContainers {
			if isLumigoInjectorContainer, _ := BeTheLumigoInjectorContainer("").Match(initContainer); !isLumigoInjectorContainer {
				newInitContainers = append(newInitContainers, initContainer)
			}
		}
		podSpec.InitContainers = newInitContainers
	}

	if podSpec.Volumes != nil {
		newVolumes := []corev1.Volume{}
		for _, volume := range podSpec.Volumes {
			if isLumigoInjectorVolume, _ := BeTheLumigoInjectorVolume().Match(volume); !isLumigoInjectorVolume {
				newVolumes = append(newVolumes, volume)
			}
		}
		podSpec.Volumes = newVolumes
	}

	envVarsToRemove := []string{LumigoTracerTokenEnvVarName, LumigoEndpointEnvVarName, LdPreloadEnvVarName}
	newContainers := []corev1.Container{}
	for _, container := range podSpec.Containers {
		if container.VolumeMounts != nil {
			newVolumeMounts := []corev1.VolumeMount{}
			for _, volumeMount := range container.VolumeMounts {
				if volumeMount.Name != LumigoInjectorVolumeName {
					newVolumeMounts = append(newVolumeMounts, volumeMount)
				}
			}
			container.VolumeMounts = newVolumeMounts
		}

		newEnvVar := []corev1.EnvVar{}
		for _, envVar := range container.Env {
			if !slices.Contains(envVarsToRemove, envVar.Name) {
				newEnvVar = append(newEnvVar, envVar)
			}
		}

		container.Env = newEnvVar

		newContainers = append(newContainers, container)
	}
	podSpec.Containers = newContainers

	return nil
}

func newTrue() *bool {
	b := true
	return &b
}
