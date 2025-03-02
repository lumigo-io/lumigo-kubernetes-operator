package kind

import (
	"context"
	"fmt"
	"testing"
	"time"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/tests/quickstart/internal"
	"k8s.io/apimachinery/pkg/runtime"
	apimachinerywait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

// These tests assume:
// 1. A valid `kubectl` configuration available in the home directory of the user
// 2. A Lumigo operator installed into the Kubernetes cluster referenced by the
//    `kubectl` configuration

func TestLumigoOperatorQuickstart(t *testing.T) {
	// Add scheme registration before creating the feature
	scheme := runtime.NewScheme()
	if err := operatorv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add Lumigo scheme: %v", err)
	}

	testAppDeploymentFeature := features.New("TestApp").
		Assess("Lumigo CRD is modified after an upgrade for a namespace mentioned in the quickstart settings", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if err := apimachinerywait.PollImmediateWithContext(ctx, 10*time.Second, 4*time.Minute, func(context.Context) (bool, error) {
				client := cfg.Client()
				r, err := resources.New(client.RESTConfig())
				if err != nil {
					t.Fatalf("Failed to create k8s client: %v", err)
				}
				operatorv1alpha1.AddToScheme(r.GetScheme())

				lumigoes := &operatorv1alpha1.LumigoList{}
				quickstartNamespace := ctx.Value(internal.ContextQuickstartNamespace).(string)
				if err := client.Resources(quickstartNamespace).List(ctx, lumigoes); err != nil {
					t.Fatalf("Could not list Lumigo CRDs in namespace '%s': %v", quickstartNamespace, err)
					return false, err
				}

				if len(lumigoes.Items) == 0 {
					t.Fatalf("No Lumigo CRDs found in the quickstart namespace '%s'", quickstartNamespace)
					return false, nil
				}

				lumigo := lumigoes.Items[0]
				if lumigo.Spec.Tracing.Enabled == nil {
					return false, fmt.Errorf("Spec.Tracing.Enabled in the Lumigo CRD found in the quickstart namespace is not set")
				}
				if *lumigo.Spec.Tracing.Enabled != true {
					return false, fmt.Errorf("Value of Spec.Tracing.Enabled from the Lumigo CRD found in the quickstart namespace is set to false, expected: true")
				}
				if lumigo.Spec.Logging.Enabled == nil {
					return false, fmt.Errorf("Spec.Logging.Enabled in the Lumigo CRD found in the quickstart namespace is not set")
				}
				if *lumigo.Spec.Logging.Enabled != true {
					return false, fmt.Errorf("Value of Spec.Logging.Enabled from the Lumigo CRD found in the quickstart namespace is set to false, expected: true")
				}

				return true, nil
			}); err != nil {
				t.Fatalf("Failed to match correct CRD spec stetting for CRD created during operator installation: %v", err)
			}

			return ctx
		}).
		Feature()

	testEnv.Test(t, testAppDeploymentFeature)
}
