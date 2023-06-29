package mutation

import (
	"fmt"
	"reflect"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	errAutotraceLabelNotFound        = fmt.Errorf("'%s' label not found", LumigoAutoTraceLabelKey)
	errLdPreloadEnvVarNotSet         = fmt.Errorf("the environment variable '%s' is not set in the container's Env", LdPreloadEnvVarName)
	errLumigoTracerTokenEnvVarNotSet = fmt.Errorf("the environment variable '%s' is not set in the container's Env", LumigoTracerTokenEnvVarName)
	errLumigoEndpointEnvVarNotSet    = fmt.Errorf("the environment variable '%s' is not set in the container's Env", LumigoEndpointEnvVarName)
)

func BeInstrumentedWithLumigo(lumigoOperatorVersion string, lumigoInjectorImage string, lumigoEndpointUrl string) types.GomegaMatcher {
	return &beInstrumentedWithLumigo{
		lumigoOperatorVersion: lumigoOperatorVersion,
		lumigoInjectorImage:   lumigoInjectorImage,
		lumigoEndpointUrl:     lumigoEndpointUrl,
	}
}

type beInstrumentedWithLumigo struct {
	lumigoOperatorVersion string
	lumigoInjectorImage   string
	lumigoEndpointUrl     string
}

func (m *beInstrumentedWithLumigo) Match(actual interface{}) (bool, error) {
	success, err := m.doMatch(actual)

	if !success {
		return success, nil
	}

	return success, err
}

func (m *beInstrumentedWithLumigo) doMatch(actual interface{}) (bool, error) {
	switch a := actual.(type) {
	case *appsv1.DaemonSet:
		if areAllContainerInstrumented, err := m.areAllContainersInstrumentedWithLumigo(&a.Spec.Template.Spec.Containers); !areAllContainerInstrumented || err != nil {
			return areAllContainerInstrumented, err
		}

		if hasInjectorContainer, err := m.containsLumigoInjectorInitContainer(&a.Spec.Template.Spec.InitContainers); hasInjectorContainer || err != nil {
			return hasInjectorContainer, err
		}

		if hasInjectorVolume, err := m.containsLumigoInjectorVolume(&a.Spec.Template.Spec.Volumes); !hasInjectorVolume || err != nil {
			return hasInjectorVolume, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.ObjectMeta); err != nil {
			return false, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.Spec.Template.ObjectMeta); err != nil {
			return false, err
		}

		return true, nil
	case *appsv1.Deployment:
		if areAllContainerInstrumented, err := m.areAllContainersInstrumentedWithLumigo(&a.Spec.Template.Spec.Containers); !areAllContainerInstrumented || err != nil {
			return areAllContainerInstrumented, err
		}

		if hasInjectorContainer, err := m.containsLumigoInjectorInitContainer(&a.Spec.Template.Spec.InitContainers); hasInjectorContainer || err != nil {
			return hasInjectorContainer, err
		}

		if hasInjectorVolume, err := m.containsLumigoInjectorVolume(&a.Spec.Template.Spec.Volumes); !hasInjectorVolume || err != nil {
			return hasInjectorVolume, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.ObjectMeta); err != nil {
			return false, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.Spec.Template.ObjectMeta); err != nil {
			return false, err
		}

		return true, nil
	case *appsv1.ReplicaSet:
		if areAllContainerInstrumented, err := m.areAllContainersInstrumentedWithLumigo(&a.Spec.Template.Spec.Containers); !areAllContainerInstrumented || err != nil {
			return areAllContainerInstrumented, err
		}

		if hasInjectorContainer, err := m.containsLumigoInjectorInitContainer(&a.Spec.Template.Spec.InitContainers); hasInjectorContainer || err != nil {
			return hasInjectorContainer, err
		}

		if hasInjectorVolume, err := m.containsLumigoInjectorVolume(&a.Spec.Template.Spec.Volumes); !hasInjectorVolume || err != nil {
			return hasInjectorVolume, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.ObjectMeta); err != nil {
			return false, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.Spec.Template.ObjectMeta); err != nil {
			return false, err
		}

		return true, nil
	case *appsv1.StatefulSet:
		if areAllContainerInstrumented, err := m.areAllContainersInstrumentedWithLumigo(&a.Spec.Template.Spec.Containers); !areAllContainerInstrumented || err != nil {
			return areAllContainerInstrumented, err
		}

		if hasInjectorContainer, err := m.containsLumigoInjectorInitContainer(&a.Spec.Template.Spec.InitContainers); hasInjectorContainer || err != nil {
			return hasInjectorContainer, err
		}

		if hasInjectorVolume, err := m.containsLumigoInjectorVolume(&a.Spec.Template.Spec.Volumes); !hasInjectorVolume || err != nil {
			return hasInjectorVolume, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.ObjectMeta); err != nil {
			return false, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.Spec.Template.ObjectMeta); err != nil {
			return false, err
		}

		return true, nil
	case *batchv1.CronJob:
		if areAllContainerInstrumented, err := m.areAllContainersInstrumentedWithLumigo(&a.Spec.JobTemplate.Spec.Template.Spec.Containers); !areAllContainerInstrumented || err != nil {
			return areAllContainerInstrumented, err
		}

		if hasInjectorContainer, err := m.containsLumigoInjectorInitContainer(&a.Spec.JobTemplate.Spec.Template.Spec.InitContainers); hasInjectorContainer || err != nil {
			return hasInjectorContainer, err
		}

		if hasInjectorVolume, err := m.containsLumigoInjectorVolume(&a.Spec.JobTemplate.Spec.Template.Spec.Volumes); !hasInjectorVolume || err != nil {
			return hasInjectorVolume, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.ObjectMeta); err != nil {
			return false, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.Spec.JobTemplate.Spec.Template.ObjectMeta); err != nil {
			return false, err
		}

		return true, nil
	case *batchv1.Job:
		if areAllContainerInstrumented, err := m.areAllContainersInstrumentedWithLumigo(&a.Spec.Template.Spec.Containers); !areAllContainerInstrumented || err != nil {
			return areAllContainerInstrumented, err
		}

		if hasInjectorContainer, err := m.containsLumigoInjectorInitContainer(&a.Spec.Template.Spec.InitContainers); hasInjectorContainer || err != nil {
			return hasInjectorContainer, err
		}

		if hasInjectorVolume, err := m.containsLumigoInjectorVolume(&a.Spec.Template.Spec.Volumes); !hasInjectorVolume || err != nil {
			return hasInjectorVolume, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.ObjectMeta); err != nil {
			return false, err
		}

		if err := m.hasTheAutoTraceLabelSet(&a.Spec.Template.ObjectMeta); err != nil {
			return false, err
		}

		return true, nil
	default:
		return false, fmt.Errorf("BeInstrumentedWithLumigo matcher expects one of: *appsv1.DaemonSet, *appsv1.Deployment, *appsv1.ReplicaSet, *appsv1.StatefulSet, *batchv1.CronJob or *batchv1.Job; got:\n%s", format.Object(actual, 1))
	}
}

func (m *beInstrumentedWithLumigo) hasTheAutoTraceLabelSet(objectMeta *metav1.ObjectMeta) error {
	version := m.lumigoOperatorVersion

	if len(version) == 0 {
		// Just check that the key exists
		_, ok := objectMeta.Labels[LumigoAutoTraceLabelKey]
		if ok {
			return nil
		} else {
			return errAutotraceLabelNotFound
		}
	}

	if len(version) > 8 {
		version = version[0:7] // Label values have a limit of 63 characters, we stay well below that
	}

	expectedAutoTraceLabelValue := "lumigo-operator.v" + version

	podTemplateHasAutoTraceLabels, err := gomega.HaveKeyWithValue(LumigoAutoTraceLabelKey, expectedAutoTraceLabelValue).Match(objectMeta)
	if err != nil {
		return fmt.Errorf("cannot lookup '%s' label: %w", LumigoAutoTraceLabelKey, err)
	}
	if !podTemplateHasAutoTraceLabels {
		return errAutotraceLabelNotFound
	}

	return nil
}

func (m *beInstrumentedWithLumigo) containsLumigoInjectorInitContainer(containers *[]corev1.Container) (bool, error) {
	for _, container := range *containers {
		if isTheInjectorContainer, err := BeTheLumigoInjectorContainer(m.lumigoInjectorImage).Match(container); isTheInjectorContainer && err == nil {
			return true, nil
		}
	}

	return false, fmt.Errorf("no Lumigo injector container found")
}

func (m *beInstrumentedWithLumigo) containsLumigoInjectorVolume(volumes *[]corev1.Volume) (bool, error) {
	for _, volume := range *volumes {
		if isTheInjectorVolume, err := BeTheLumigoInjectorVolume().Match(volume); isTheInjectorVolume && err == nil {
			return true, nil
		}
	}

	return false, fmt.Errorf("no Lumigo injector volume found")
}

func (m *beInstrumentedWithLumigo) areAllContainersInstrumentedWithLumigo(containers *[]corev1.Container) (bool, error) {
	for i, container := range *containers {
		isContainerInstrumented, err := m.isContainerInstrumentedWithLumigo(&container)
		if !isContainerInstrumented || err != nil {
			return isContainerInstrumented, fmt.Errorf("container[%v] is not instrumented with Lumigo: %w", i, err)
		}
	}

	return true, nil
}

func (m *beInstrumentedWithLumigo) isContainerInstrumentedWithLumigo(container *corev1.Container) (bool, error) {
	ldPreloadEnvVarFound := false
	lumigoTracerTokenEnvVarFound := false
	lumigoEndpointEnvVarFound := false
	for _, envVar := range container.Env {
		switch envVar.Name {
		case LdPreloadEnvVarName:
			if envVar.Value != LdPreloadEnvVarValue {
				return false, fmt.Errorf("unexpected value for '%s' env var: expected '%s', found '%s'", LdPreloadEnvVarName, LdPreloadEnvVarValue, envVar.Value)
			}
			ldPreloadEnvVarFound = true

		case LumigoTracerTokenEnvVarName:
			if envVar.ValueFrom == nil {
				return false, fmt.Errorf("unexpected value for '%s' env var: expected value from secret, found %+v", LumigoTracerTokenEnvVarName, envVar.Value)
			}
			lumigoTracerTokenEnvVarFound = true

		case LumigoEndpointEnvVarName:
			if envVar.Value != m.lumigoEndpointUrl {
				return false, fmt.Errorf("unexpected value for '%s' env var: expected '%s', found '%s'", LumigoEndpointEnvVarName, m.lumigoEndpointUrl, envVar.Value)
			}
			lumigoEndpointEnvVarFound = true
		}
	}

	if !ldPreloadEnvVarFound {
		return false, errLdPreloadEnvVarNotSet
	}

	if !lumigoTracerTokenEnvVarFound {
		return false, errLumigoTracerTokenEnvVarNotSet
	}

	if !lumigoEndpointEnvVarFound {
		return false, errLumigoEndpointEnvVarNotSet
	}

	volumeMountFound := false
	for _, volumeMount := range container.VolumeMounts {
		if volumeMount.Name == LumigoInjectorVolumeName {
			volumeMountFound = true

			if !volumeMount.ReadOnly {
				return false, fmt.Errorf("unexpected 'ReadOnly' value found on '%s' volume mount: expected '%t', found '%t'", volumeMount.Name, true, volumeMount.ReadOnly)
			}

			if volumeMount.MountPath != LumigoInjectorVolumeMountPoint {
				return false, fmt.Errorf("unexpected 'MountPath' value found on '%s' volume mount: expected '%s', found '%s'", volumeMount.Name, LumigoInjectorVolumeMountPoint, volumeMount.MountPath)
			}
		}
	}

	if !volumeMountFound {
		return false, fmt.Errorf("no '%s' volume mount found", LumigoInjectorVolumeName)
	}

	return true, nil
}

func (m *beInstrumentedWithLumigo) FailureMessage(actual interface{}) (message string) {
	_, err := m.doMatch(actual)

	if err != nil {
		return fmt.Errorf("is not instrumented with the Lumigo injector: %w", err).Error()
	} else {
		return "is not instrumented with the Lumigo injector"
	}
}

func (m *beInstrumentedWithLumigo) NegatedFailureMessage(actual interface{}) (message string) {
	return "is instrumented with the Lumigo injector"
}

func BeTheLumigoInjectorContainer(lumigoInjectorImage string) types.GomegaMatcher {
	return &beTheLumigoInjectorContainer{
		lumigoInjectorImage: lumigoInjectorImage,
	}
}

type beTheLumigoInjectorContainer struct {
	// If empty, not matching is performed
	lumigoInjectorImage string
}

func (m *beTheLumigoInjectorContainer) Match(actual interface{}) (success bool, err error) {
	var container corev1.Container

	switch a := actual.(type) {
	case corev1.Container:
		container = a
	default:
		return false, fmt.Errorf("BeLumigoInjectorContainerMatcher matcher expects a *corev1.Container; got:\n%s", format.Object(actual, 1))
	}

	if container.Name != LumigoInjectorContainerName {
		return false, fmt.Errorf("has an unexpected container name: expected '%s'; found: '%s'", LumigoInjectorContainerName, container.Name)
	}

	if len(m.lumigoInjectorImage) > 0 && container.Image != m.lumigoInjectorImage {
		return false, fmt.Errorf("has an unexpected container image: expected '%s'; found: '%s'", m.lumigoInjectorImage, container.Image)
	}

	expectedEnvironment := []corev1.EnvVar{
		{
			Name:  "TARGET_DIRECTORY",
			Value: TargetDirectoryPath,
		},
	}
	if !reflect.DeepEqual(container.Env, expectedEnvironment) {
		return false, fmt.Errorf("has an unexpected container environment: expected '%+v'; found: '%+v'", expectedEnvironment, &container.Env)
	}

	expectedVolumeMounts := []corev1.VolumeMount{
		{
			Name:      LumigoInjectorVolumeName,
			ReadOnly:  false,
			MountPath: TargetDirectoryPath,
		},
	}
	if !reflect.DeepEqual(container.VolumeMounts, expectedVolumeMounts) {
		return false, fmt.Errorf("has an unexpected volume mounts: expected '%+v'; found: '%+v'", expectedVolumeMounts, &container.VolumeMounts)
	}

	return true, nil
}

func (m *beTheLumigoInjectorContainer) FailureMessage(actual interface{}) (message string) {
	return "is not the Lumigo injector container"
}

func (m *beTheLumigoInjectorContainer) NegatedFailureMessage(actual interface{}) (message string) {
	return "is the Lumigo injector container"
}

func BeTheLumigoInjectorVolume() types.GomegaMatcher {
	return &beTheLumigoInjectorVolume{}
}

type beTheLumigoInjectorVolume struct {
}

func (m *beTheLumigoInjectorVolume) Match(actual interface{}) (success bool, err error) {
	var volume corev1.Volume

	switch a := actual.(type) {
	case corev1.Volume:
		volume = a
	default:
		return false, fmt.Errorf("BeTheLumigoInjectorVolume matcher expects a *corev1.Volume; got:\n%s", format.Object(actual, 1))
	}

	if volume.Name != LumigoInjectorVolumeName {
		return false, fmt.Errorf("has an unexpected volume name: expected '%s'; found: '%s'", LumigoInjectorVolumeName, volume.Name)
	}

	if volume.VolumeSource.EmptyDir == nil {
		return false, fmt.Errorf("has an unexpected volume source: expected an EmptyDir; found: %+v", volume.VolumeSource)
	}

	return true, nil
}

func (m *beTheLumigoInjectorVolume) FailureMessage(actual interface{}) (message string) {
	return "is not the Lumigo injector volume"
}

func (m *beTheLumigoInjectorVolume) NegatedFailureMessage(actual interface{}) (message string) {
	return "is the Lumigo injector volume"
}
