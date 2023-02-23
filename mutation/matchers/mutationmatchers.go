package matchers

import (
	"fmt"
	"reflect"

	"github.com/lumigo-io/lumigo-kubernetes-operator/mutation"
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
		case mutation.LdPreloadEnvVarName:
			if envVar.Value != mutation.LdPreloadEnvVarValue {
				return false, fmt.Errorf("unexpected value for '%s' env var: expected '%s', found '%s'", mutation.LdPreloadEnvVarName, mutation.LdPreloadEnvVarValue, envVar.Value)
			}
			ldPreloadEnvVarFound = true

		case mutation.LumigoTracerTokenEnvVarName:
			if envVar.ValueFrom == nil {
				return false, fmt.Errorf("unexpected value for '%s' env var: expected value from secret, found %+v", mutation.LumigoTracerTokenEnvVarName, envVar.Value)
			}
			lumigoTracerTokenEnvVarFound = true

		case mutation.LumigoEndpointEnvVarName:
			if envVar.Value != m.lumigoEndpointUrl {
				return false, fmt.Errorf("unexpected value for '%s' env var: expected '%s', found '%s'", mutation.LumigoEndpointEnvVarName, m.lumigoEndpointUrl, envVar.Value)
			}
			lumigoEndpointEnvVarFound = true
		}
	}

	if !ldPreloadEnvVarFound {
		return false, fmt.Errorf("the env var '%s' is not set on the container Env: %+v", mutation.LdPreloadEnvVarName, container.Env)
	}

	if !lumigoTracerTokenEnvVarFound {
		return false, fmt.Errorf("the env var '%s' is not set on the container Env", mutation.LumigoTracerTokenEnvVarName)
	}

	if !lumigoEndpointEnvVarFound {
		return false, fmt.Errorf("the env var '%s' is not set on the container Env", mutation.LumigoEndpointEnvVarName)
	}

	volumeMountFound := false
	for _, volumeMount := range container.VolumeMounts {
		if volumeMount.Name == mutation.LumigoInjectorVolumeName {
			volumeMountFound = true

			if !volumeMount.ReadOnly {
				return false, fmt.Errorf("unexpected 'ReadOnly' value found on '%s' volume mount: expected '%t', found '%t'", volumeMount.Name, true, volumeMount.ReadOnly)
			}

			if volumeMount.MountPath != mutation.LumigoInjectorVolumeMountPoint {
				return false, fmt.Errorf("unexpected 'MountPath' value found on '%s' volume mount: expected '%s', found '%s'", volumeMount.Name, mutation.LumigoInjectorVolumeMountPoint, volumeMount.MountPath)
			}
		}
	}

	if !volumeMountFound {
		return false, fmt.Errorf("no '%s' volume mount found", mutation.LumigoInjectorVolumeName)
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

	if container.Name != mutation.LumigoInjectorContainerName {
		return false, fmt.Errorf("has an unexpected container name: expected '%s'; found: '%s'", mutation.LumigoInjectorContainerName, container.Name)
	}

	if container.Image != m.lumigoInjectorImage {
		return false, fmt.Errorf("has an unexpected container image: expected '%s'; found: '%s'", m.lumigoInjectorImage, container.Image)
	}

	expectedEnvironment := []corev1.EnvVar{
		{
			Name:  "TARGET_DIRECTORY",
			Value: mutation.TargetDirectoryPath,
		},
	}
	if !reflect.DeepEqual(container.Env, expectedEnvironment) {
		return false, fmt.Errorf("has an unexpected container environment: expected '%+v'; found: '%+v'", expectedEnvironment, &container.Env)
	}

	expectedVolumeMounts := []corev1.VolumeMount{
		{
			Name:      mutation.LumigoInjectorVolumeName,
			ReadOnly:  false,
			MountPath: mutation.TargetDirectoryPath,
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
