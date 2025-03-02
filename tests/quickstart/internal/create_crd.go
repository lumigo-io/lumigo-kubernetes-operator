package internal

import (
	"context"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

func CreateCRD(namespace string, tracingEnabled *bool, loggingEnabled *bool) func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		client := cfg.Client()
		r, err := resources.New(client.RESTConfig())
		if err != nil {
			return ctx, err
		}
		operatorv1alpha1.AddToScheme(r.GetScheme())

		secretName := "lumigo-credentials"
		tokenKeyName := "token"

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			StringData: map[string]string{
				tokenKeyName: ctx.Value(ContextKeyLumigoToken).(string),
			},
		}
		err = r.Create(ctx, secret)
		if err != nil {
			return ctx, err
		}

		lumigo := &operatorv1alpha1.Lumigo{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "lumigo",
				Labels:    map[string]string{},
			},
			Spec: operatorv1alpha1.LumigoSpec{
				LumigoToken: operatorv1alpha1.Credentials{
					SecretRef: operatorv1alpha1.KubernetesSecretRef{
						Name: secretName,
						Key:  tokenKeyName,
					},
				},
				Tracing: operatorv1alpha1.TracingSpec{
					Injection: operatorv1alpha1.InjectionSpec{
						Enabled: tracingEnabled,
					},
				},
				Logging: operatorv1alpha1.LoggingSpec{
					Enabled: loggingEnabled,
				},
			},
		}

		err = r.Create(ctx, lumigo)
		if err != nil {
			return ctx, err
		}

		return ctx, nil
	}

}
