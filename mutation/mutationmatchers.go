package mutation

import (
	"fmt"
	"reflect"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
)

func BeInstrumentedWithLumigo(lumigoOperatorVersion string, lumigoInjectorImage string, lumigoEndpointUrl string) types.GomegaMatcher {
	return &beInstrumentedWithLumigo{
		operatorVersion:   lumigoOperatorVersion,
		injectorImage:     lumigoInjectorImage,
		lumigoEndpointUrl: lumigoEndpointUrl,
	}
}

type beInstrumentedWithLumigo struct {
	operatorVersion   string
	injectorImage     string
	lumigoEndpointUrl string
}

func (m *beInstrumentedWithLumigo) Match(actual interface{}) (success bool, err error) {
	var container corev1.Container

	switch a := actual.(type) {
	case corev1.Container:
		container = a
	default:
		return false, fmt.Errorf("BeInstrumentedWithLumigo matcher expects a *corev1.Container; got:\n%s", format.Object(actual, 1))
	}

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
		return false, fmt.Errorf("the env var '%s' is not set on the container Env: %+v", LdPreloadEnvVarName, container.Env)
	}

	if !lumigoTracerTokenEnvVarFound {
		return false, fmt.Errorf("the env var '%s' is not set on the container Env", LumigoTracerTokenEnvVarName)
	}

	if !lumigoEndpointEnvVarFound {
		return false, fmt.Errorf("the env var '%s' is not set on the container Env", LumigoEndpointEnvVarName)
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
	return "is not instrumented with the Lumigo injector"
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
