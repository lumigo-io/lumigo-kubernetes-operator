package kind

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	apimachinerywait "k8s.io/apimachinery/pkg/util/wait"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	operatorv1alpha1conditions "github.com/lumigo-io/lumigo-kubernetes-operator/controllers/conditions"
	"github.com/lumigo-io/lumigo-kubernetes-operator/tests/kubernetes-distros/kind/internal"
)

func TestLumigoOperatorTraces(t *testing.T) {
	logger := testr.New(t)

	appServiceName := "test-js-app"
	loadGeneratorServiceName := "test-js-load-generator"

	deploymentName := "app"
	cronJobName := "load-generator"
	namespaceName := envconf.RandomName("test-ns", 12)

	testAppDeploymentFeature := features.New("TestApp").
		Setup(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
			testJsAppClientImage := ctx.Value(internal.ContextTestAppJsClientImageName).(string)
			testJsAppServerImage := ctx.Value(internal.ContextTestAppJsServerImageName).(string)

			client := config.Client()

			if err := client.Resources().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}); err != nil {
				t.Fatal(err)
			}

			lumigoToken := ctx.Value(internal.ContextKeyLumigoToken).(string)

			lumigoTokenName := "lumigo-credentials"
			lumigoTokenKey := "token"

			secret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      lumigoTokenName,
				},
				StringData: map[string]string{
					lumigoTokenKey: lumigoToken,
				},
			}

			if err := client.Resources().Create(ctx, &secret); err != nil {
				t.Fatal(err)
			}

			lumigo := internal.NewLumigo(namespaceName, "lumigo", lumigoTokenName, lumigoTokenKey, true)

			r, err := resources.New(client.RESTConfig())
			if err != nil {
				t.Fatal(err)
			}
			operatorv1alpha1.AddToScheme(r.GetScheme())
			r.Create(ctx, lumigo)

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*1, func(context.Context) (bool, error) {
				currentLumigo := &operatorv1alpha1.Lumigo{}

				if err := r.Get(ctx, lumigo.Name, lumigo.Namespace, currentLumigo); err != nil {
					return false, err
				}

				return operatorv1alpha1conditions.IsActive(currentLumigo), err
			}); err != nil {
				t.Fatal(err)
			}

			var replicas int32 = 2
			deploymentLabels := map[string]string{
				"app":  deploymentName,
				"type": "deployment",
			}
			deploymentPort := 8080
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      deploymentName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: deploymentLabels,
					},
					Replicas: &replicas,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: deploymentLabels,
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "server",
									Image: testJsAppServerImage,
									Env: []corev1.EnvVar{
										{
											Name:  "SERVER_PORT",
											Value: fmt.Sprintf("%d", deploymentPort),
										},
										{
											Name:  "OTEL_SERVICE_NAME",
											Value: appServiceName,
										},
										// Dump spans to pod logs for easier debugging
										{
											Name:  "LUMIGO_DEBUG_SPANDUMP",
											Value: "console:log",
										},
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "app",
											Protocol:      corev1.ProtocolTCP,
											ContainerPort: int32(deploymentPort),
										},
									},
								},
							},
						},
					},
				},
			}

			if err := client.Resources().Create(ctx, deployment); err != nil {
				t.Fatal(err)
			}

			servicePort := int32(8080)
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      "app",
				},
				Spec: corev1.ServiceSpec{
					Selector: deploymentLabels,
					Ports: []corev1.ServicePort{
						{
							Name:       "app",
							Protocol:   corev1.ProtocolTCP,
							Port:       servicePort,
							TargetPort: intstr.FromInt(deploymentPort),
						},
					},
				},
			}

			if err := client.Resources().Create(ctx, service); err != nil {
				t.Fatal(err)
			}

			// Wait until the deployment has all its pods running
			// See https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment
			wait.For(conditions.New(config.Client().Resources()).ResourceMatch(deployment, func(object k8s.Object) bool {
				d := object.(*appsv1.Deployment)
				return d.Status.AvailableReplicas == replicas && d.Status.ReadyReplicas == replicas
			}))

			logger.Info("Deployment is ready")

			cronJob := &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronJobName,
					Namespace: namespaceName,
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "* * * * *", // Every minute
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"app":  cronJobName,
										"type": "cronjob",
									},
								},
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									Containers: []corev1.Container{
										{
											Name:  "client",
											Image: testJsAppClientImage,
											Env: []corev1.EnvVar{
												{
													Name:  "TARGET_URL",
													Value: fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", service.Name, service.Namespace, servicePort),
												},
												{
													Name:  "OTEL_SERVICE_NAME",
													Value: loadGeneratorServiceName,
												},
												// Dump spans to pod logs for easier debugging
												{
													Name:  "LUMIGO_DEBUG_SPANDUMP",
													Value: "console:log",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}

			if err := client.Resources().Create(ctx, cronJob); err != nil {
				t.Fatal(err)
			}

			logger.Info("CronJob is created")

			return ctx
		}).
		Assess("CronJob traces have the 'k8s.cronjob.id' resource attribute set", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			tracesPath := filepath.Join(otlpSinkDataPath, "traces.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(
				ctx,
				time.Second*5,
				wrapWithLogging(t, "CronJob traces have the 'k8s.cronjob.id' resource attribute set", func(context.Context) (bool, error) {
					traceBytes, err := os.ReadFile(tracesPath)
					if err != nil {
						return false, err
					}

					if len(traceBytes) < 1 {
						return false, nil
					}

					client := c.Client()
					loadGeneratorCronJob := &batchv1.CronJob{}
					if err := client.Resources().Get(ctx, cronJobName, namespaceName, loadGeneratorCronJob); err != nil {
						t.Fatal(err)
					}

					spansFound := false
					scanner := bufio.NewScanner(bytes.NewBuffer(traceBytes))
					for scanner.Scan() {
						exportRequest := ptraceotlp.NewExportRequest()
						exportRequest.UnmarshalJSON(scanner.Bytes())

						if exportRequest.Traces().SpanCount() < 1 {
							continue
						}

						l := exportRequest.Traces().ResourceSpans().Len()
						for i := 0; i < l; i++ {
							resourceSpans := exportRequest.Traces().ResourceSpans().At(i)

							resourceAttributes := resourceSpans.Resource().Attributes().AsRaw()

							serviceName, isOk := resourceAttributes["service.name"].(string)

							if !isOk || serviceName != loadGeneratorServiceName {
								continue
							}

							spansFound = true

							cronJobUid := resourceAttributes["k8s.cronjob.uid"]
							if cronJobUid == nil {
								return false, fmt.Errorf("found load-generator spans without the \"%s\" resource attribute: %+v", "k8s.cronjob.uid", resourceAttributes)
							}

							if types.UID(cronJobUid.(string)) != loadGeneratorCronJob.UID {
								return false, fmt.Errorf("unexpected cronjob UID found in load-generator resource attributes: found \"%s\", expected \"%s\"; all resource attributes: %+v", cronJobUid, loadGeneratorCronJob.UID, resourceAttributes)
							}
						}
					}

					if err := scanner.Err(); err != nil {
						return false, err
					}

					return spansFound, nil
				})); err != nil {
				t.Fatalf("Failed to wait for traces: %v", err)
			}

			return ctx
		}).
		Assess("Deployment traces have the 'k8s.deployment.uid' resource attribute set", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			tracesPath := filepath.Join(otlpSinkDataPath, "traces.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(
				ctx,
				time.Second*5,
				wrapWithLogging(t, "Deployment spans have the 'k8s.deployment.uid' resource attribute set", func(context.Context) (bool, error) {
					traceBytes, err := os.ReadFile(tracesPath)
					if err != nil {
						return false, err
					}

					if len(traceBytes) < 1 {
						return false, nil
					}

					client := c.Client()
					appDeployment := &appsv1.Deployment{}
					if err := client.Resources().Get(ctx, deploymentName, namespaceName, appDeployment); err != nil {
						t.Fatal(err)
					}

					spansFound := false
					scanner := bufio.NewScanner(bytes.NewBuffer(traceBytes))
					for scanner.Scan() {
						exportRequest := ptraceotlp.NewExportRequest()
						exportRequest.UnmarshalJSON(scanner.Bytes())

						if exportRequest.Traces().SpanCount() < 1 {
							continue
						}

						l := exportRequest.Traces().ResourceSpans().Len()
						for i := 0; i < l; i++ {
							resourceSpans := exportRequest.Traces().ResourceSpans().At(i)

							resourceAttributes := resourceSpans.Resource().Attributes().AsRaw()

							if resourceAttributes["service.name"] != appServiceName {
								continue
							}

							spansFound = true

							deploymentUid := resourceAttributes["k8s.deployment.uid"]
							if deploymentUid == nil {
								return false, fmt.Errorf("found deployment spans without the \"%s\" resource attribute: %+v", "k8s.deployment.uid", resourceAttributes)
							}

							if types.UID(deploymentUid.(string)) != appDeployment.UID {
								return false, fmt.Errorf("unexpected deployment UID found in load-generator resource attributes: found \"%s\", expected \"%s\"; all resource attributes: %+v", deploymentUid, appDeployment.UID, resourceAttributes)
							}
						}
					}

					return spansFound, nil
				})); err != nil {
				t.Fatalf("Failed to wait for traces: %v", err)
			}

			return ctx
		}).
		Assess("All traces have the 'k8s.cluster.uid' set to the UID of the 'kube-system' namespace", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			kubeSystemNamespace := corev1.Namespace{}
			if err := c.Client().Resources().Get(ctx, "kube-system", "", &kubeSystemNamespace); err != nil {
				t.Fatal(fmt.Errorf("cannot retrieve the 'kube-system' namespace: %w", err))
			}

			expectedClusterUID := string(kubeSystemNamespace.UID)

			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			tracesPath := filepath.Join(otlpSinkDataPath, "traces.json")

			traceBytes, err := os.ReadFile(tracesPath)
			if err != nil {
				t.Fatal(err)
			}

			if len(traceBytes) < 1 {
				t.Fatalf("No trace data found in '%s'", tracesPath)
			}

			scanner := bufio.NewScanner(bytes.NewBuffer(traceBytes))
			for scanner.Scan() {
				exportRequest := ptraceotlp.NewExportRequest()
				exportRequest.UnmarshalJSON(scanner.Bytes())

				if exportRequest.Traces().SpanCount() < 1 {
					continue
				}

				l := exportRequest.Traces().ResourceSpans().Len()
				for i := 0; i < l; i++ {
					resourceSpans := exportRequest.Traces().ResourceSpans().At(i)

					resourceAttributes := resourceSpans.Resource().Attributes().AsRaw()

					if actualClusterUID, found := resourceAttributes["k8s.cluster.uid"]; !found {
						t.Fatalf("found spans without the 'k8s.cluster.uid' resource attribute: %+v", resourceAttributes)
					} else if actualClusterUID != expectedClusterUID {
						t.Fatalf("wrong 'k8s.cluster.uid' value found: '%s'; expected: '%s'; %+v", actualClusterUID, expectedClusterUID, resourceAttributes)
					}
				}
			}

			return ctx
		}).
		Assess("All traces have the 'k8s.provider.id' taken from describe node", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {

			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			tracesPath := filepath.Join(otlpSinkDataPath, "traces.json")

			traceBytes, err := os.ReadFile(tracesPath)
			if err != nil {
				t.Fatal(err)
			}

			if len(traceBytes) < 1 {
				t.Fatalf("No trace data found in '%s'", tracesPath)
			}

			scanner := bufio.NewScanner(bytes.NewBuffer(traceBytes))
			for scanner.Scan() {
				exportRequest := ptraceotlp.NewExportRequest()
				exportRequest.UnmarshalJSON(scanner.Bytes())

				if exportRequest.Traces().SpanCount() < 1 {
					continue
				}

				l := exportRequest.Traces().ResourceSpans().Len()
				for i := 0; i < l; i++ {
					resourceSpans := exportRequest.Traces().ResourceSpans().At(i)

					resourceAttributes := resourceSpans.Resource().Attributes().AsRaw()

					if actualClusterUID, found := resourceAttributes["k8s.provider.id"]; !found {
						t.Fatalf("found spans without the 'k8s.provider.id' resource attribute: %+v", resourceAttributes)
					} else {
						actualClusterUIDstr, ok := actualClusterUID.(string)
						if !ok || !strings.HasPrefix(actualClusterUIDstr, "kind://") {
							t.Fatalf("wrong 'k8s.provider.id' value found: '%s'; %+v", actualClusterUID, resourceAttributes)
						}
					}
				}
			}

			return ctx
		}).
		Feature()

	testEnv.Test(t, testAppDeploymentFeature)
}

func wrapWithLogging(t *testing.T, description string, test func(context.Context) (bool, error)) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		isOk, err := test(ctx)

		if !isOk {
			if err != nil {
				t.Logf("test function '%s' failed: %v", description, err)
			}
		}

		return isOk, err
	}
}
