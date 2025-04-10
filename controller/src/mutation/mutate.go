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
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/types"
	"golang.org/x/exp/slices"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const LumigoAutoTraceLabelKey = "lumigo.auto-trace"
const LumigoAutoTraceTracesEnabledLabelKey = "lumigo.enable-traces"
const LumigoAutoTraceLogsEnabledLabelKey = "lumigo.enable-logs"
const LumigoAutoTraceTokenSecretNameKey = "lumigo.token-secret"
const LumigoAutoTraceTokenSecretKeyKey = "lumigo.token-key"
const LumigoAutoTraceLabelVersionPrefixValue = "lumigo-operator.v"
const LumigoAutoTraceLabelSkipNextInjectorValue = "skip-next-injector"

const TargetDirectoryEnvVarName = "TARGET_DIRECTORY"
const TargetDirectoryPath = "/target"
const LumigoInjectorContainerName = "lumigo-injector"
const LumigoInjectorVolumeName = "lumigo-injector"
const LumigoInjectorVolumeMountPoint = "/opt/lumigo"
const LumigoTracerTokenEnvVarName = "LUMIGO_TRACER_TOKEN"
const LumigoEndpointEnvVarName = "LUMIGO_ENDPOINT"
const LumigoLogsEndpointEnvVarName = "LUMIGO_LOGS_ENDPOINT"
const LumigoEnableLogsEnvVarName = "LUMIGO_ENABLE_LOGS"
const LumigoEnableTracesEnvVarName = "LUMIGO_ENABLE_TRACES"
const LumigoContainerNameEnvVarName = "LUMIGO_CONTAINER_NAME"
const LdPreloadEnvVarName = "LD_PRELOAD"
const LdPreloadEnvVarValue = LumigoInjectorVolumeMountPoint + "/injector/lumigo_injector.so"

var defaultLumigoInitContainerUser int64 = 1234
var defaultLumigoInitContainerGroup int64 = defaultLumigoInitContainerUser

type InjectingMutator interface {
	GetAutotraceLabelValue() string
	InjectLumigoInto(resource interface{}) (bool, error)
	InjectLumigoIntoAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) (bool, error)
	InjectLumigoIntoAppsV1Deployment(deployment *appsv1.Deployment) (bool, error)
	InjectLumigoIntoAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) (bool, error)
	InjectLumigoIntoAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) (bool, error)
	InjectLumigoIntoBatchV1CronJob(deployment *batchv1.CronJob) (bool, error)
	InjectLumigoIntoBatchV1Job(deployment *batchv1.Job) (bool, error)
}

type RemovalMutator interface {
	RemoveLumigoFrom(resource interface{}) (bool, error)
	RemoveLumigoFromAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) (bool, error)
	RemoveLumigoFromAppsV1Deployment(deployment *appsv1.Deployment) (bool, error)
	RemoveLumigoFromAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) (bool, error)
	RemoveLumigoFromAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) (bool, error)
	RemoveLumigoFromBatchV1CronJob(deployment *batchv1.CronJob) (bool, error)
	RemoveLumigoFromBatchV1Job(deployment *batchv1.Job) (bool, error)
}

var f = false
var t = true

type baseMutatorConfig struct {
	log                       *logr.Logger
	lumigoAutotraceLabelValue string
}

type injectingMutatorImpl struct {
	baseMutatorConfig
	lumigoEndpoint      string
	lumigoLogsEndpoint  string
	lumigoEnableLogs    bool
	lumigoEnableTraces  bool
	lumigoToken         *operatorv1alpha1.Credentials
	lumigoInjectorImage string
}

type removalMutatorImpl struct {
	baseMutatorConfig
}

func (m *injectingMutatorImpl) GetAutotraceLabelValue() string {
	return m.lumigoAutotraceLabelValue
}

func NewRemovalMutator(Log *logr.Logger, LumigoOperatorVersion string) RemovalMutator {
	version := LumigoOperatorVersion

	if len(version) > 8 {
		version = version[0:7] // Label values have a limit of 63 characters, we stay well below that
	}

	return &removalMutatorImpl{
		baseMutatorConfig: baseMutatorConfig{
			log:                       Log,
			lumigoAutotraceLabelValue: LumigoAutoTraceLabelVersionPrefixValue + version,
		},
	}
}

func NewInjectingMutatorFromSpec(Log *logr.Logger, LumigoSpec *operatorv1alpha1.LumigoSpec, LumigoOperatorVersion string, LumigoInjectorImage string, TelemetryProxyOtlpServiceUrl string, TelemetryProxyOtlpLogsServiceUrl string) (InjectingMutator, error) {
	version := LumigoOperatorVersion

	if len(version) > 8 {
		version = version[0:7] // Label values have a limit of 63 characters, we stay well below that
	}

	baseConfig := baseMutatorConfig{
		log:                       Log,
		lumigoAutotraceLabelValue: LumigoAutoTraceLabelVersionPrefixValue + version,
	}

	lumigoEnableLogs := false
	if LumigoSpec != nil && LumigoSpec.Logging.Enabled != nil {
		lumigoEnableLogs = *LumigoSpec.Logging.Enabled
	}

	lumigoEnableTraces := true
	if LumigoSpec != nil && LumigoSpec.Tracing.Enabled != nil {
		lumigoEnableTraces = *LumigoSpec.Tracing.Enabled
	}

	lumigoToken := &operatorv1alpha1.Credentials{}
	if LumigoSpec != nil {
		lumigoToken = &LumigoSpec.LumigoToken
	}

	injectingMutator := &injectingMutatorImpl{
		baseMutatorConfig:   baseConfig,
		lumigoEndpoint:      TelemetryProxyOtlpServiceUrl,
		lumigoLogsEndpoint:  TelemetryProxyOtlpLogsServiceUrl,
		lumigoEnableLogs:    lumigoEnableLogs,
		lumigoEnableTraces:  lumigoEnableTraces,
		lumigoToken:         lumigoToken,
		lumigoInjectorImage: LumigoInjectorImage,
	}

	return injectingMutator, nil
}

func NewMutatorFromAutoTraceSettings(Log *logr.Logger, autoTraceSettings *types.AutoTraceSettings, LumigoOperatorVersion string, LumigoInjectorImage string, TelemetryProxyOtlpServiceUrl string, TelemetryProxyOtlpLogsServiceUrl string) (InjectingMutator, error) {
	version := LumigoOperatorVersion

	if len(version) > 8 {
		version = version[0:7] // Label values have a limit of 63 characters, we stay well below that
	}

	baseConfig := baseMutatorConfig{
		log:                       Log,
		lumigoAutotraceLabelValue: LumigoAutoTraceLabelVersionPrefixValue + version,
	}

	lumigoToken := &operatorv1alpha1.Credentials{
		SecretRef: operatorv1alpha1.KubernetesSecretRef{
			Name: autoTraceSettings.SecretName,
			Key:  autoTraceSettings.SecretKey,
		},
	}

	injectingMutator := &injectingMutatorImpl{
		baseMutatorConfig:   baseConfig,
		lumigoEndpoint:      TelemetryProxyOtlpServiceUrl,
		lumigoLogsEndpoint:  TelemetryProxyOtlpLogsServiceUrl,
		lumigoEnableLogs:    autoTraceSettings.LogsEnabled,
		lumigoEnableTraces:  autoTraceSettings.TracesEnabled,
		lumigoToken:         lumigoToken,
		lumigoInjectorImage: LumigoInjectorImage,
	}

	return injectingMutator, nil
}

func (m *injectingMutatorImpl) InjectLumigoInto(resource interface{}) (bool, error) {
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
		return false, fmt.Errorf("unexpected resource type to mutate: %+v", a)
	}
}

func (m *removalMutatorImpl) RemoveLumigoFrom(resource interface{}) (bool, error) {
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
		return false, fmt.Errorf("unexpected resource type to mutate: %+v", a)
	}
}

func (m *injectingMutatorImpl) InjectLumigoIntoAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) (bool, error) {
	return m.injectLumigoInto(&daemonSet.ObjectMeta, &daemonSet.Spec.Template)
}

func (m *removalMutatorImpl) RemoveLumigoFromAppsV1DaemonSet(daemonSet *appsv1.DaemonSet) (bool, error) {
	return m.removeLumigoFrom(&daemonSet.ObjectMeta, &daemonSet.Spec.Template)
}

func (m *injectingMutatorImpl) InjectLumigoIntoAppsV1Deployment(deployment *appsv1.Deployment) (bool, error) {
	return m.injectLumigoInto(&deployment.ObjectMeta, &deployment.Spec.Template)
}

func (m *removalMutatorImpl) RemoveLumigoFromAppsV1Deployment(deployment *appsv1.Deployment) (bool, error) {
	return m.removeLumigoFrom(&deployment.ObjectMeta, &deployment.Spec.Template)
}

func (m *injectingMutatorImpl) InjectLumigoIntoAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) (bool, error) {
	if hasDeploymentOwner, err := hasDeploymentOwnerReference(replicaSet.OwnerReferences); err != nil {
		return false, err
	} else if hasDeploymentOwner {
		return false, nil
	}

	return m.injectLumigoInto(&replicaSet.ObjectMeta, &replicaSet.Spec.Template)
}

func (m *removalMutatorImpl) RemoveLumigoFromAppsV1ReplicaSet(replicaSet *appsv1.ReplicaSet) (bool, error) {
	if hasDeploymentOwner, err := hasDeploymentOwnerReference(replicaSet.OwnerReferences); err != nil {
		return false, err
	} else if hasDeploymentOwner {
		return false, nil
	}

	return m.removeLumigoFrom(&replicaSet.ObjectMeta, &replicaSet.Spec.Template)
}

func (m *injectingMutatorImpl) InjectLumigoIntoAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) (bool, error) {
	return m.injectLumigoInto(&statefulSet.ObjectMeta, &statefulSet.Spec.Template)
}

func (m *removalMutatorImpl) RemoveLumigoFromAppsV1StatefulSet(statefulSet *appsv1.StatefulSet) (bool, error) {
	return m.removeLumigoFrom(&statefulSet.ObjectMeta, &statefulSet.Spec.Template)
}

func (m *injectingMutatorImpl) InjectLumigoIntoBatchV1CronJob(batchJob *batchv1.CronJob) (bool, error) {
	return m.injectLumigoInto(&batchJob.ObjectMeta, &batchJob.Spec.JobTemplate.Spec.Template)
}

func (m *removalMutatorImpl) RemoveLumigoFromBatchV1CronJob(batchJob *batchv1.CronJob) (bool, error) {
	return m.removeLumigoFrom(&batchJob.ObjectMeta, &batchJob.Spec.JobTemplate.Spec.Template)
}

func (m *injectingMutatorImpl) InjectLumigoIntoBatchV1Job(job *batchv1.Job) (bool, error) {
	return m.injectLumigoInto(&job.ObjectMeta, &job.Spec.Template)
}

func (m *removalMutatorImpl) RemoveLumigoFromBatchV1Job(job *batchv1.Job) (bool, error) {
	return m.removeLumigoFrom(&job.ObjectMeta, &job.Spec.Template)
}

func (m *injectingMutatorImpl) injectLumigoInto(topLevelObjectMeta *metav1.ObjectMeta, podTemplateSpec *corev1.PodTemplateSpec) (bool, error) {
	if err := m.validateShouldInjectLumigoInto(topLevelObjectMeta); err != nil {
		return false, err
	}

	originalSpec := podTemplateSpec.Spec.DeepCopy()

	if err := m.injectLumigoIntoPodSpec(&podTemplateSpec.Spec); err != nil {
		return false, err
	}

	if reflect.DeepEqual(originalSpec, &podTemplateSpec.Spec) {
		return false, nil
	}

	addAutoTraceLabel(topLevelObjectMeta, m.lumigoAutotraceLabelValue)
	addAutoTraceLabel(&podTemplateSpec.ObjectMeta, m.lumigoAutotraceLabelValue)

	return true, nil
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

func (m *removalMutatorImpl) removeLumigoFrom(topLevelObjectMeta *metav1.ObjectMeta, podTemplateSpec *corev1.PodTemplateSpec) (bool, error) {
	originalSpec := podTemplateSpec.Spec.DeepCopy()

	if err := m.removeLumigoFromPodSpec(&podTemplateSpec.Spec); err != nil {
		return false, err
	}

	if reflect.DeepEqual(originalSpec, &podTemplateSpec.Spec) {
		return false, nil
	}

	removeAutoTraceLabel(topLevelObjectMeta)
	removeAutoTraceLabel(&podTemplateSpec.ObjectMeta)

	return true, nil
}

func removeAutoTraceLabel(objectMeta *metav1.ObjectMeta) {
	if objectMeta != nil && objectMeta.Labels != nil {
		delete(objectMeta.Labels, LumigoAutoTraceLabelKey)
	}
}

func (m *injectingMutatorImpl) validateShouldInjectLumigoInto(resourceMeta *metav1.ObjectMeta) error {
	autoTraceLabelValue := resourceMeta.Labels[LumigoAutoTraceLabelKey]
	if strings.ToLower(autoTraceLabelValue) == "false" {
		// Opt-out for this resource, skip injection
		return fmt.Errorf("the resource has the '%s' label set to 'false'", LumigoAutoTraceLabelKey)
	}

	return nil
}

func (m *injectingMutatorImpl) injectLumigoIntoPodSpec(podSpec *corev1.PodSpec) error {
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

	// The `lumigo-injector` init-container must be able to write to the `lumigo-injector`` volume.
	// To ensure that, if FSGroup is set, the `lumigo-injector` init-container should use it as group.
	initContainerUser := &defaultLumigoInitContainerUser
	initContainerGroup := &defaultLumigoInitContainerGroup
	if podSpec.SecurityContext.FSGroup != nil {
		initContainerUser = podSpec.SecurityContext.FSGroup
		initContainerGroup = podSpec.SecurityContext.FSGroup
	}

	lumigoInjectorContainer := &corev1.Container{
		Name:  LumigoInjectorContainerName,
		Image: m.lumigoInjectorImage,
		Env: []corev1.EnvVar{
			{
				Name:  TargetDirectoryEnvVarName,
				Value: TargetDirectoryPath,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &f,
			Privileged:               &f,
			ReadOnlyRootFilesystem:   &t,
			// We need to have no more privileges than the rest of the pod
			RunAsNonRoot: podSpec.SecurityContext.RunAsNonRoot,
			RunAsUser:    initContainerUser,
			RunAsGroup:   initContainerGroup,
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

		lumigoLogsEndpointEnvVar := &corev1.EnvVar{
			Name:  LumigoLogsEndpointEnvVarName,
			Value: m.lumigoLogsEndpoint,
		}
		lumigoLogsEndpointEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == LumigoLogsEndpointEnvVarName })
		if lumigoLogsEndpointEnvVarIndex < 0 {
			envVars = append(envVars, *lumigoLogsEndpointEnvVar)
		} else {
			envVars[lumigoLogsEndpointEnvVarIndex] = *lumigoLogsEndpointEnvVar
		}

		lumigoEnableLogsEnvVar := &corev1.EnvVar{
			Name:  LumigoEnableLogsEnvVarName,
			Value: strconv.FormatBool(m.lumigoEnableLogs),
		}
		lumigoEnableLogsEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == LumigoEnableLogsEnvVarName })
		if lumigoEnableLogsEnvVarIndex < 0 {
			envVars = append(envVars, *lumigoEnableLogsEnvVar)
		} else {
			envVars[lumigoEnableLogsEnvVarIndex] = *lumigoEnableLogsEnvVar
		}

		lumigoEnableTracesEnvVar := &corev1.EnvVar{
			Name:  LumigoEnableTracesEnvVarName,
			Value: strconv.FormatBool(m.lumigoEnableTraces),
		}
		lumigoEnableTracesEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == LumigoEnableTracesEnvVarName })
		if lumigoEnableTracesEnvVarIndex < 0 {
			envVars = append(envVars, *lumigoEnableTracesEnvVar)
		} else {
			envVars[lumigoEnableTracesEnvVarIndex] = *lumigoEnableTracesEnvVar
		}

		lumigoContainerNameEnvVar := &corev1.EnvVar{
			Name:  LumigoContainerNameEnvVarName,
			Value: container.Name,
		}
		lumigoContainerNameEnvVarIndex := slices.IndexFunc(envVars, func(c corev1.EnvVar) bool { return c.Name == LumigoContainerNameEnvVarName })
		if lumigoContainerNameEnvVarIndex < 0 {
			envVars = append(envVars, *lumigoContainerNameEnvVar)
		} else {
			envVars[lumigoContainerNameEnvVarIndex] = *lumigoContainerNameEnvVar
		}
		container.Env = envVars

		patchedContainers = append(patchedContainers, container)
	}
	podSpec.Containers = patchedContainers

	return nil
}

func (m *removalMutatorImpl) removeLumigoFromPodSpec(podSpec *corev1.PodSpec) error {
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

	envVarsToRemove := []string{
		LumigoTracerTokenEnvVarName,
		LumigoEndpointEnvVarName,
		LumigoLogsEndpointEnvVarName,
		LumigoEnableLogsEnvVarName,
		LumigoEnableTracesEnvVarName,
		LumigoContainerNameEnvVarName,
		LdPreloadEnvVarName,
	}
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

func hasDeploymentOwnerReference(ownerReferences []metav1.OwnerReference) (bool, error) {
	for _, ownerReference := range ownerReferences {
		gv, err := schema.ParseGroupVersion(ownerReference.APIVersion)
		if err != nil {
			return false, err
		}

		if gv.Group == "apps" && gv.Version == "v1" && ownerReference.Kind == "Deployment" {
			return true, nil
		}
	}

	return false, nil
}
