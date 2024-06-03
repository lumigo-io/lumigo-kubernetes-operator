package internal

import (
	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewLumigo(namespace, name, lumigoTokenSecretName, lumigoTokenSecretKey string, injectionEnabled bool, enableLogs bool) *operatorv1alpha1.Lumigo {
	return &operatorv1alpha1.Lumigo{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{},
		},
		Spec: operatorv1alpha1.LumigoSpec{
			LumigoToken: operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: lumigoTokenSecretName,
					Key:  lumigoTokenSecretKey,
				},
			},
			Tracing: operatorv1alpha1.TracingSpec{
				Injection: operatorv1alpha1.InjectionSpec{
					Enabled: &injectionEnabled,
				},
			},
			Logging: operatorv1alpha1.LoggingSpec{
				Enabled: &enableLogs,
			},
		},
	}
}
