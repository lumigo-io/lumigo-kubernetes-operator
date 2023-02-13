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
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type Mutator struct {
	Log            *logr.Logger
	LumigoEndpoint string
	LumigoToken    *operatorv1alpha1.Credentials
}

const targetDirectoryPath = "/target"
const lumigoInjectorVolumeMountPoint = "/opt/lumigo"

func (m *Mutator) Mutate(podSpec *corev1.PodSpec) error {
	lumigoInjectorImage, isLumigoInjectorImageSetInEnv := os.LookupEnv("LUMIGO_INJECTOR_IMAGE")
	if !isLumigoInjectorImageSetInEnv {
		return fmt.Errorf("unknown 'lumigo-injector' image: environment variable 'LUMIGO_INJECTOR_IMAGE' is not set")
	}

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
		Image: lumigoInjectorImage,
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
		m.Log.Info("Mutating container", "container-name", container.Name)

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
						Name: m.LumigoToken.SecretRef.Name,
					},
					Key:      m.LumigoToken.SecretRef.Key,
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
			Value: m.LumigoEndpoint,
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

	marshalledPodSpec, err := json.Marshal(podSpec)
	if err != nil {
		return err
	}
	m.Log.Info("PodSpec mutated", "mutated-podspec", marshalledPodSpec)

	return nil
}

func newTrue() *bool {
	b := true
	return &b
}
