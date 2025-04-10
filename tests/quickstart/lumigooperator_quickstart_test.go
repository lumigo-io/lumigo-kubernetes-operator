package kind

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
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
		Assess("Lumigo CRD is modified after an upgrade for a namespace provided in the quickstart settings", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if err := apimachinerywait.PollImmediateWithContext(ctx, 10*time.Second, 4*time.Minute, func(context.Context) (bool, error) {
				logger := testr.New(t)

				client := cfg.Client()
				r, err := resources.New(client.RESTConfig())
				if err != nil {
					t.Fatalf("Failed to create k8s client: %v", err)
				}
				operatorv1alpha1.AddToScheme(r.GetScheme())

				quickstartNamespaceWithExistingCrd := ctx.Value(internal.ContextQuickstartNamespaces).([]string)[0]
				_, err = internal.InstallOrUpgradeLumigoOperator(ctx, client, cfg.KubeconfigFile(), logger, []string{
					fmt.Sprintf("--set monitoredNamespaces[0].namespace=%s", quickstartNamespaceWithExistingCrd),
					fmt.Sprintf("--set monitoredNamespaces[0].loggingEnabled=%t", true),
					fmt.Sprintf("--set monitoredNamespaces[0].tracingEnabled=%t", true),
				})
				if err != nil {
					t.Fatalf("Failed to install or upgrade Lumigo operator: %v", err)
					return false, nil
				}

				lumigoes := &operatorv1alpha1.LumigoList{}
				if err := client.Resources(quickstartNamespaceWithExistingCrd).List(ctx, lumigoes); err != nil {
					t.Fatalf("Could not list Lumigo CRDs in namespace '%s': %v", quickstartNamespaceWithExistingCrd, err)
					return false, err
				}

				if len(lumigoes.Items) == 0 {
					t.Fatalf("No Lumigo CRDs found in the quickstart namespace '%s'", quickstartNamespaceWithExistingCrd)
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
		Assess("Lumigo CRDs are created for all namespaces when monitoredNamespaces=all was set during installation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			logger := testr.New(t)

			client := cfg.Client()
			r, err := resources.New(client.RESTConfig())
			if err != nil {
				t.Fatalf("Failed to create k8s client: %v", err)
			}
			operatorv1alpha1.AddToScheme(r.GetScheme())

			quickstartNamespaces, isOk := ctx.Value(internal.ContextQuickstartNamespaces).([]string)
			if !isOk {
				t.Fatalf("Failed to get quickstart namespaces from context")
			}

			lumigoNamespace, isOk := ctx.Value(internal.ContextKeyLumigoNamespace).(string)
			if !isOk {
				t.Fatalf("Failed to get Lumigo namespace from context")
			}

			_, err = internal.InstallOrUpgradeLumigoOperator(ctx, client, cfg.KubeconfigFile(), logger, []string{
				"--set monitoredNamespaces=all",
			})
			if err != nil {
				t.Fatalf("Failed to install or upgrade Lumigo operator: %v", err)
				return ctx
			}

			if err := apimachinerywait.PollImmediateWithContext(ctx, 10*time.Second, 4*time.Minute, func(context.Context) (bool, error) {
				for _, ns := range quickstartNamespaces {

					lumigoes := &operatorv1alpha1.LumigoList{}
					if err := client.Resources(ns).List(ctx, lumigoes); err != nil {
						t.Fatalf("Could not list Lumigo CRDs in namespace '%s': %v", ns, err)
						return false, err
					}

					if len(lumigoes.Items) == 0 {
						return false, nil
					}
				}

				for _, ignoredNs := range []string{lumigoNamespace, "kube-system"} {
					lumigoes := &operatorv1alpha1.LumigoList{}
					if err := client.Resources(ignoredNs).List(ctx, lumigoes); err != nil {
						t.Fatalf("Could not list Lumigo CRDs in namespace '%s': %v", ignoredNs, err)
						return false, err
					}

					if len(lumigoes.Items) > 0 {
						t.Fatalf("Found a Lumigo CRD in ignored namespace '%s'", ignoredNs)
						return false, nil
					}
				}
				return true, nil
			}); err != nil {
				t.Fatalf("Failed to find CRDs in one or more quickstart namespaces: %v", err)
			}

			return ctx
		}).
		Feature()

	testEnv.Test(t, testAppDeploymentFeature)
}
