package kind

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/exp/slices"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	operatorv1alpha1conditions "github.com/lumigo-io/lumigo-kubernetes-operator/controllers/conditions"
	"github.com/lumigo-io/lumigo-kubernetes-operator/tests/kubernetes-distros/kind/internal"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	apimachinerywait "k8s.io/apimachinery/pkg/util/wait"
)

var (
	DEFAULT_LUMIGO_TOKEN = "t_1234567890123456789AB"
)

// These tests assume:
// 1. A valid `kubectl` configuration available in the home directory of the user
// 2. A Lumigo operator installed into the Kubernetes cluster referenced by the
//    `kubectl` configuration

func TestLumigoOperatorLogsEventsAndObjects(t *testing.T) {
	testAppDeploymentFeature := features.New("TestApp").
		Setup(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
			client := config.Client()

			namespaceName := envconf.RandomName(ctx.Value(internal.ContextTestAppNamespacePrefix).(string), 12)
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

			lumigo := internal.NewLumigo(namespaceName, "lumigo", lumigoTokenName, lumigoTokenKey, true, true)

			r, err := resources.New(client.RESTConfig())
			if err != nil {
				t.Fail()
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

			deploymentName := "testdeployment"

			tr := true
			var g int64 = 5678

			var replicas int32 = 2
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      deploymentName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":  "myapp",
							"type": "deployment",
						},
					},
					Replicas: &replicas,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app":  "myapp",
								"type": "deployment",
							},
						},
						Spec: corev1.PodSpec{
							SecurityContext: &corev1.PodSecurityContext{
								RunAsUser:    &g,
								RunAsGroup:   &g,
								RunAsNonRoot: &tr,
								FSGroup:      &g,
							},
							Containers: []corev1.Container{
								{
									Name:  "myapp",
									Image: ctx.Value(internal.ContextTestAppPythonImageName).(string),
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("1Gi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("768Mi"),
										},
									},
								},
								{
									Name:    envconf.RandomName(ctx.Value(internal.ContextTestAppBusyboxIncludedContainerNamePrefix).(string), 12),
									Image:   "busybox:1.37.0",
									Command: []string{"sh", "-c", "while true; do echo $(date) - Hello from included busybox; sleep 5; done"},
								},
								{
									Name:    envconf.RandomName(ctx.Value(internal.ContextTestAppBusyboxExcludedContainerNamePrefix).(string), 12),
									Image:   "busybox:1.37.0",
									Command: []string{"sh", "-c", "while true; do echo $(date) - Hello from excluded busybox; sleep 5; done"},
								},
							},
						},
					},
				},
			}

			createAndDeleteTempDeployment(ctx, config, deployment, replicas)

			return ctx
		}).
		Assess("All events have rootOwnerReferences", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				logsBytes, err := os.ReadFile(logsPath)
				if err != nil {
					return false, err
				}

				if len(logsBytes) < 1 {
					return false, err
				}

				eventLogs := make([]plog.LogRecord, 0)

				/*
				 * Logs come in multiple lines, and two different scopes; we need to split by '\n'.
				 * bufio.NewScanner fails because our lines are "too long" (LOL).
				 */
				exportRequests := strings.Split(string(logsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := plogotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					if e, _, _, err := exportRequestToLogRecords(exportRequest); err != nil {
						t.Fatalf("Cannot extract logs from export request: %v", err)
					} else {
						eventLogs = append(eventLogs, e...)
					}
				}

				if len(eventLogs) < 1 {
					// No events received yet
					return false, nil
				}

				eventLogsMissingRootOwnerReferences := make([]eventWithRootOwnerReference, 0)
				for _, eventLog := range eventLogs {
					event := eventWithRootOwnerReference{}
					if err := json.Unmarshal([]byte(eventLog.Body().AsString()), &event); err != nil {
						t.Fatalf("Cannot extract root owner reference from event log: %v; %v", eventLog.Body().AsRaw(), err)
					}

					// We skip the Watch events
					if event.TypeMeta.APIVersion == "v1" && event.TypeMeta.Kind == "Event" && event.RootOwnerReference.APIVersion == "" {
						eventLogsMissingRootOwnerReferences = append(eventLogsMissingRootOwnerReferences, event)
					} else if event.InvolvedObject.APIVersion == "v1" && event.InvolvedObject.Kind == "Pod" {
						if !((event.RootOwnerReference.APIVersion == "apps/v1" && event.RootOwnerReference.Kind == "Deployment") ||
							(event.RootOwnerReference.APIVersion == "batch/v1" && event.RootOwnerReference.Kind == "CronJob")) {
							t.Fatalf("Wrong root-owner reference on pod event, points to neither deployment nor cronjob: %v", event.RootOwnerReference)
						}
					}
				}

				if len(eventLogsMissingRootOwnerReferences) > 0 {
					var buffer bytes.Buffer
					buffer.WriteString(fmt.Sprintf("Missing %d root-owner references out of %d events\n", len(eventLogsMissingRootOwnerReferences), len(eventLogs)))
					for _, event := range eventLogsMissingRootOwnerReferences {
						buffer.Write([]byte(fmt.Sprintf("* %s\n", event.UID)))
					}

					return false, fmt.Errorf(buffer.String())
				}

				t.Logf("All %d events have root-owner references", len(eventLogs))
				return true, nil
			}); err != nil {
				t.Fatalf("Failed to wait for logs: %v", err)
			}

			return ctx
		}).
		Assess("All resourceVersions referred to as in event involved objects are sent as objects in the 'lumigo-operator.k8s-objects' logs scope", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				logsBytes, err := os.ReadFile(logsPath)
				if err != nil {
					return false, err
				}

				if len(logsBytes) < 1 {
					return false, err
				}

				eventLogs := make([]plog.LogRecord, 0)
				objectLogs := make([]plog.LogRecord, 0)

				/*
				 * Logs come in multiple lines, and two different scopes; we need to split by '\n'.
				 * bufio.NewScanner fails because our lines are "too long" (LOL).
				 */
				exportRequests := strings.Split(string(logsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := plogotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					e, o, _, err := exportRequestToLogRecords(exportRequest)
					if err != nil {
						t.Fatalf("Cannot extract logs from export request: %v", err)
					}
					eventLogs = append(eventLogs, e...)
					objectLogs = append(objectLogs, o...)
				}

				if len(eventLogs) < 1 {
					// No events received yet
					return false, nil
				}

				// We will have repetitions in this one, when we get multiple events on
				// the same uid+resourceVersion combo
				objects, err := extractUnstructuredObjects(objectLogs)
				if err != nil {
					t.Fatalf("cannot extract unstructured objects from object logs: %v", err)
				}

				objectResourceVersions := make([]string, 0)
				for _, object := range objects {
					versionedObject := object
					if object.GetKind() == "" {
						// Likely we are looking at a Watch event
						raw := object.Object
						if nestedObject, found := raw["object"]; found {
							versionedObject = unstructured.Unstructured{
								Object: nestedObject.(map[string]interface{}),
							}
						}
					}

					objectResourceVersion := string(versionedObject.GetUID()) + "@" + versionedObject.GetResourceVersion()
					objectResourceVersions = append(objectResourceVersions, objectResourceVersion)
				}

				objectReferences, err := extractObjectReferences(eventLogs)
				if err != nil {
					t.Fatalf("cannot extract object references from event logs: %v", err)
				}

				missingRefs := make([]string, 0)
				for _, objectReference := range objectReferences {
					if objectReference.Kind != "" && objectReference.UID != "" && objectReference.ResourceVersion != "" {
						ref := fmt.Sprintf("%s@%s", objectReference.UID, objectReference.ResourceVersion)
						if !slices.Contains(objectResourceVersions, ref) {
							missingRefs = append(missingRefs, fmt.Sprintf("%s/%s@%s", objectReference.Kind, objectReference.UID, objectReference.ResourceVersion))
						}
					}
				}

				missingRefs = removeDuplicateStrings(missingRefs)

				if len(missingRefs) > 0 {
					var buffer bytes.Buffer
					buffer.WriteString(fmt.Sprintf("Missing %d references out of %d events\n", len(missingRefs), len(eventLogs)))
					for _, missingRef := range missingRefs {
						buffer.Write([]byte(fmt.Sprintf("* %s\n", missingRef)))
					}

					t.Errorf(buffer.String())
					return false, nil
				}

				t.Logf("Found all references out of %d events", len(eventLogs))
				return true, nil
			}); err != nil {
				t.Fatalf("Failed to wait for logs: %v", err)
			}

			return ctx
		}).
		Assess("All logs have the 'k8s.cluster.name' and 'k8s.cluster.uid' set correctly", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			kubeSystemNamespace := corev1.Namespace{}
			if err := c.Client().Resources().Get(ctx, "kube-system", "", &kubeSystemNamespace); err != nil {
				t.Fatal(fmt.Errorf("cannot retrieve the 'kube-system' namespace: %w", err))
			}
			expectedClusterUID := string(kubeSystemNamespace.UID)
			expectedClusterName := ctx.Value(internal.ContextKeyKubernetesClusterName).(string)

			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")

			logsBytes, err := os.ReadFile(logsPath)
			if err != nil {
				t.Fatal(err)
			}

			if len(logsBytes) < 1 {
				t.Fatalf("No log data found in '%s'", logsPath)
			}

			/*
			 * Logs come in multiple lines, and two different scopes; we need to split by '\n'.
			 * bufio.NewScanner fails because our lines are "too long" (LOL).
			 */
			exportRequests := strings.Split(string(logsBytes), "\n")
			for _, exportRequestJson := range exportRequests {
				exportRequest := plogotlp.NewExportRequest()
				exportRequest.UnmarshalJSON([]byte(exportRequestJson))

				l := exportRequest.Logs().ResourceLogs().Len()
				for i := 0; i < l; i++ {
					resourceLogs := exportRequest.Logs().ResourceLogs().At(i)
					resourceAttributes := resourceLogs.Resource().Attributes().AsRaw()

					if actualClusterName, found := resourceAttributes["k8s.cluster.name"]; !found {
						t.Fatalf("found logs without the 'k8s.cluster.name' resource attribute: %+v", resourceAttributes)
					} else if actualClusterName != expectedClusterName {
						t.Fatalf("wrong 'k8s.cluster.name' value found: '%s'; expected: '%s'; %+v", actualClusterName, expectedClusterName, resourceAttributes)
					}

					if actualClusterUID, found := resourceAttributes["k8s.cluster.uid"]; !found {
						// TODO: remove when logs collected from files have the cluster UID
						if resourceLogs.ScopeLogs().At(0).Scope().Name() != "lumigo-operator.log_file_collector" {
							t.Fatalf("found logs without the 'k8s.cluster.uid' resource attribute: %+v", resourceAttributes)
						}
					} else if actualClusterUID != expectedClusterUID {
						t.Fatalf("wrong 'k8s.cluster.uid' value found: '%s'; expected: '%s'; %+v", actualClusterUID, expectedClusterUID, resourceAttributes)
					}
				}
			}

			return ctx
		}).
		Assess("Usage heartbeat is sent for instrumented namespaces", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)

			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				logsBytes, err := os.ReadFile(logsPath)
				if err != nil {
					return false, err
				}

				if len(logsBytes) < 1 {
					return false, err
				}

				eventLogs := make([]plog.LogRecord, 0)

				/*
				 * Logs come in multiple lines, and two different scopes; we need to split by '\n'.
				 * bufio.NewScanner fails because our lines are "too long" (LOL).
				 */
				exportRequests := strings.Split(string(logsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := plogotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					if e, err := exportRequestLogRecords(exportRequest, filterHeartbeatLogRecords); err != nil {
						t.Fatalf("Cannot extract logs from export request: %v", err)
					} else {
						eventLogs = append(eventLogs, e...)
					}
				}

				if len(eventLogs) < 1 {
					// No events received yet
					t.Fatalf("No heartbeat logs were sent: %v", err)
					return false, nil
				}

				t.Logf("Found heartbeat logs: %d", len(eventLogs))
				return true, nil
			}); err != nil {
				t.Fatalf("Failed to wait for logs: %v", err)
			}

			return ctx
		}).
		Assess("Application logs are collected successfully and added k8s.* attributes", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)
			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				logsBytes, err := os.ReadFile(logsPath)
				if err != nil {
					return false, err
				}

				if len(logsBytes) < 1 {
					return false, err
				}

				applicationLogs := make([]plog.LogRecord, 0)

				/*
				 * Logs come in multiple lines, and two different scopes; we need to split by '\n'.
				 * bufio.NewScanner fails because our lines are "too long" (LOL).
				 */
				exportRequests := strings.Split(string(logsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := plogotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					if appLogs, err := exportRequestLogRecords(exportRequest, filterApplicationLogRecords); err != nil {
						t.Fatalf("Cannot extract logs from export request: %v", err)
					} else {
						applicationLogs = append(applicationLogs, appLogs...)
					}
				}

				if len(applicationLogs) < 1 {
					// No application logs received yet
					t.Fatalf("No application logs found in '%s'. \r\nMake sure the application has LUMIGO_ENABLE_LOGS=true and is emitting logs using a supported logger", logsPath)
					return false, nil
				}

				t.Logf("Found application logs: %d", len(applicationLogs))
				return true, nil
			}); err != nil {
				t.Fatalf("Failed to wait for application logs: %v", err)
			}

			return ctx
		}).
		Assess("Application logs are collected successfully via pod files and added k8s.* attributes", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)
			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				logsBytes, err := os.ReadFile(logsPath)
				if err != nil {
					return false, err
				}

				if len(logsBytes) < 1 {
					return false, err
				}

				applicationLogs := make([]plog.LogRecord, 0)

				/*
				 * Logs come in multiple lines, and different scopes; we need to split by '\n'.
				 * bufio.NewScanner fails because our lines are "too long" (LOL).
				 */
				exportRequests := strings.Split(string(logsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := plogotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					if appLogs, err := exportRequestLogRecords(exportRequest, filterPodLogRecords); err != nil {
						t.Fatalf("Cannot extract logs from export request: %v", err)
					} else {
						applicationLogs = append(applicationLogs, appLogs...)
					}
				}

				if len(applicationLogs) < 1 {
					// No application logs received yet
					t.Fatalf("No application logs found in '%s'. \r\nMake sure log files are emitted under /var/logs/pods/ in the cluster node", logsPath)
					return false, nil
				}

				foundApplicationLogRecord := false
				foundLogFromIncludedContainer := false
				for _, appLog := range applicationLogs {
					// Make sure the log came from log files and not the distro,
					// as the logs.json file is shared between several tests reporting to the same OTLP sink
					value, found := appLog.Attributes().Get("log.file.path")
					if found && strings.HasPrefix(value.AsString(), "/var/log/pods/") {
						if strings.Contains(value.AsString(), ctx.Value(internal.ContextTestAppBusyboxExcludedContainerNamePrefix).(string)) {
							t.Fatalf("Found a log file record from the excluded container: %s", value.AsString())
						}

						if strings.Contains(value.AsString(), ctx.Value(internal.ContextTestAppBusyboxIncludedContainerNamePrefix).(string)) {
							foundLogFromIncludedContainer = true
						}

						foundApplicationLogRecord = true
						t.Logf("Found application log: %s", appLog.Body().AsString())
						// Make sure the log body was parsed as JSON by the cluster-agent collector
						if appLog.Body().Type() == pcommon.ValueTypeMap {
							t.Logf("Found a JSON-object application log: %v", appLog.Body().Map())
							return true, nil
						}
					}
				}

				if !foundLogFromIncludedContainer {
					t.Fatalf("No logs found from the included container %s", ctx.Value(internal.ContextTestAppBusyboxIncludedContainerNamePrefix).(string))
				}

				if foundApplicationLogRecord {
					t.Fatalf("No application logs found with a JSON-object body")
				} else {
					t.Fatalf("No application logs found matching the 'log.file.path' attribute")
				}

				return false, nil
			}); err != nil {
				t.Fatalf("Failed to wait for application logs: %v", err)
			}

			return ctx
		}).Setup(
		func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
			quickstartNamespace := ctx.Value(internal.ContextQuickstartNamespace).(string)

			var replicas int32 = 1
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-quickstart-app",
					Namespace: quickstartNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-quickstart-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test-quickstart-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "quickstart-app",
									Image: ctx.Value(internal.ContextTestAppPythonImageName).(string),
								},
							},
						},
					},
				},
			}

			createAndDeleteTempDeployment(ctx, config, deployment, replicas)

			return ctx
		}).
		Assess("Application logs are collected from quickstart namespace", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)
			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")
			quickstartNamespace := ctx.Value(internal.ContextQuickstartNamespace).(string)

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				logsBytes, err := os.ReadFile(logsPath)
				if err != nil {
					return false, err
				}

				if len(logsBytes) < 1 {
					return false, err
				}

				applicationLogs := make([]plog.LogRecord, 0)

				/*
				 * Logs come in multiple lines, and two different scopes; we need to split by '\n'.
				 * bufio.NewScanner fails because our lines are "too long" (LOL).
				 */
				exportRequests := strings.Split(string(logsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := plogotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					if appLogs, err := exportRequestLogRecords(exportRequest, filterNamespaceAppLogRecords(quickstartNamespace)); err != nil {
						t.Fatalf("Cannot extract logs from export request: %v", err)
					} else {
						applicationLogs = append(applicationLogs, appLogs...)
					}
				}

				if len(applicationLogs) < 1 {
					// No application logs received yet
					t.Fatalf("No application logs found in '%s'. \r\nMake sure the application has LUMIGO_ENABLE_LOGS=true and is emitting logs using a supported logger", logsPath)
					return false, nil
				}

				for _, logLine := range applicationLogs {
					if strings.Contains(logLine.Body().AsString(), "something to say to the logs") {
						t.Logf("Found application logs: %d", len(applicationLogs))
						return true, nil
					}
				}

				t.Fatalf("No logs with expected body found in '%s'", logsPath)
				return false, nil
			}); err != nil {
				t.Fatalf("Failed to wait for application logs: %v", err)
			}

			return ctx
		}).
		Assess("Watchdog events are exported as logs", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			otlpSinkDataPath := ctx.Value(internal.ContextKeyOtlpSinkDataPath).(string)
			logsPath := filepath.Join(otlpSinkDataPath, "logs.json")
			foundWatchdogEvents := false

			if err := apimachinerywait.PollImmediateUntilWithContext(ctx, time.Second*5, func(context.Context) (bool, error) {
				logsBytes, err := os.ReadFile(logsPath)
				if err != nil {
					return false, err
				}

				if len(logsBytes) < 1 {
					return false, fmt.Errorf("No log data found in '%s'", logsPath)
				}

				exportRequests := strings.Split(string(logsBytes), "\n")
				for _, exportRequestJson := range exportRequests {
					exportRequest := plogotlp.NewExportRequest()
					exportRequest.UnmarshalJSON([]byte(exportRequestJson))

					_, _, watchdogEvents, err := exportRequestToLogRecords(exportRequest)
					if err != nil {
						t.Fatalf("Cannot extract logs from export request: %v", err)
					}

					if len(watchdogEvents) > 0 {
						foundWatchdogEvents = true
					} else {
						continue
					}

					for _, eventLog := range watchdogEvents {
						var eventData map[string]interface{}
						if err := json.Unmarshal([]byte(eventLog.Body().AsString()), &eventData); err != nil {
							t.Fatalf("Cannot parse event body: %v", err)
						}

						requiredFields := []string{
							"metadata",
							"lastTimestamp",
							"eventTime",
							"creationTimestamp",
							"type",
							"involvedObject",
							"rootOwnerReference",
							"reason",
							"message",
							"source",
						}

						for _, field := range requiredFields {
							if _, exists := eventData[field]; !exists {
								return false, fmt.Errorf("Missing required field in event: %s", field)
							}
						}

						involvedObject, ok := eventData["involvedObject"].(map[string]interface{})
						if !ok {
							t.Errorf("involvedObject is not a map, got: %T", eventData["involvedObject"])
						} else {
							requiredInvolvedObjectFields := []string{
								"kind",
								"namespace",
								"name",
								"uid",
								"apiVersion",
								"resourceVersion",
								"fieldPath",
							}
							for _, field := range requiredInvolvedObjectFields {
								if _, exists := involvedObject[field]; !exists {
									return false, fmt.Errorf("Missing required involvedObject field: %s", field)
								}
							}
						}

						rootOwnerRef, ok := eventData["rootOwnerReference"].(map[string]interface{})
						if !ok {
							t.Error("rootOwnerReference is not a map")
						} else {
							requiredRootOwnerRefFields := []string{
								"name",
								"kind",
								"uid",
							}
							for _, field := range requiredRootOwnerRefFields {
								if _, exists := rootOwnerRef[field]; !exists {
									return false, fmt.Errorf("Missing required rootOwnerReference field: %s", field)
								}
							}
						}

						source, ok := eventData["source"].(map[string]interface{})
						if !ok {
							t.Error("source is not a map")
						} else {
							requiredSourceFields := []string{
								"component",
								"host",
							}
							for _, field := range requiredSourceFields {
								if _, exists := source[field]; !exists {
									return false, fmt.Errorf("Missing required source field: %s", field)
								}
							}
						}

						// Verify severity mapping
						if eventData["type"] == "Normal" {
							if eventLog.SeverityNumber() != plog.SeverityNumberInfo {
								return false, fmt.Errorf("Expected Normal event to have Info severity, got %s", eventLog.SeverityNumber().String())
							}
						} else if eventData["type"] == "Warning" {
							if eventLog.SeverityNumber() != plog.SeverityNumberError {
								return false, fmt.Errorf("Expected Warning event to have Error severity, got %s", eventLog.SeverityNumber().String())
							}
						}
					}
				}

				if !foundWatchdogEvents {
					t.Log("No watchdog events found")
					return false, nil
				}

				return true, nil
			}); err != nil {
				t.Fatalf("Failed to verify event structure: %v", err)
			}

			timeoutCtx, cancel := context.WithTimeout(ctx, time.Minute*5)
			defer cancel()

			return timeoutCtx
		}).
		Feature()

	testEnv.Test(t, testAppDeploymentFeature)
}

type eventWithRootOwnerReference struct {
	corev1.Event
	RootOwnerReference corev1.ObjectReference `json:"rootOwnerReference,omitempty"`
}

func extractObjectReferences(eventLogs []plog.LogRecord) ([]corev1.ObjectReference, error) {
	objectReferences := make([]corev1.ObjectReference, len(eventLogs))

	for i, eventLog := range eventLogs {
		event := corev1.Event{}
		if err := json.Unmarshal([]byte(eventLog.Body().AsString()), &event); err != nil {
			return nil, err
		}
		objectReferences[i] = event.InvolvedObject
	}

	return objectReferences, nil
}

func extractUnstructuredObjects(objectLogs []plog.LogRecord) ([]unstructured.Unstructured, error) {
	objects := make([]unstructured.Unstructured, len(objectLogs))

	for i, objectLog := range objectLogs {
		u := &unstructured.Unstructured{Object: map[string]interface{}{}}
		if err := json.Unmarshal([]byte(objectLog.Body().AsString()), &u.Object); err != nil {
			return nil, err
		}
		objects[i] = *u
	}

	return objects, nil
}

func exportRequestToLogRecords(exportRequest plogotlp.ExportRequest) ([]plog.LogRecord, []plog.LogRecord, []plog.LogRecord, error) {
	eventLogs := make([]plog.LogRecord, 0)
	objectLogs := make([]plog.LogRecord, 0)
	watchdogEventLogs := make([]plog.LogRecord, 0)
	logs := exportRequest.Logs()

	l := logs.ResourceLogs().Len()
	for i := 0; i < l; i++ {
		e, o, w, err := resourceLogsToLogRecords(logs.ResourceLogs().At(i))
		if err != nil {
			return nil, nil, nil, err
		}

		eventLogs = append(eventLogs, e...)
		objectLogs = append(objectLogs, o...)
		watchdogEventLogs = append(watchdogEventLogs, w...)
	}

	return eventLogs, objectLogs, watchdogEventLogs, nil
}

func resourceLogsToLogRecords(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, []plog.LogRecord, []plog.LogRecord, error) {
	l := resourceLogs.ScopeLogs().Len()

	watchdogEventLogRecords := make([]plog.LogRecord, 0)
	serviceName, hasServiceName := resourceLogs.Resource().Attributes().Get("service.name")
	if hasServiceName && serviceName.AsString() == "lumigo-kubernetes-operator-watchdog" {
		for i := 0; i < l; i++ {
			scopeLogs := resourceLogs.ScopeLogs().At(i)
			watchdogEventLogRecords = append(watchdogEventLogRecords, scopeLogsToLogRecords(scopeLogs)...)
		}
		return nil, nil, watchdogEventLogRecords, nil
	}

	eventLogRecords := make([]plog.LogRecord, 0)
	objectLogRecords := make([]plog.LogRecord, 0)
	for i := 0; i < l; i++ {
		scopeLogs := resourceLogs.ScopeLogs().At(i)
		scopeName := scopeLogs.Scope().Name()
		logRecords := scopeLogsToLogRecords(scopeLogs)

		switch scopeName {
		case "lumigo-operator.k8s-events":
			{
				eventLogRecords = append(eventLogRecords, logRecords...)
			}
		case "lumigo-operator.k8s-objects":
			{
				objectLogRecords = append(objectLogRecords, logRecords...)
			}
		}
	}
	return eventLogRecords, objectLogRecords, nil, nil
}

func scopeLogsToLogRecords(scopeLogs plog.ScopeLogs) []plog.LogRecord {
	scopeLogsLen := scopeLogs.LogRecords().Len()
	logRecords := make([]plog.LogRecord, scopeLogsLen)
	for i := 0; i < scopeLogsLen; i++ {
		logRecords[i] = scopeLogs.LogRecords().At(i)
	}
	return logRecords
}

// https://stackoverflow.com/a/71864796/6188451
func removeDuplicateStrings(s []string) []string {
	if len(s) < 1 {
		return s
	}

	sort.Strings(s)
	prev := 1
	for curr := 1; curr < len(s); curr++ {
		if s[curr-1] != s[curr] {
			s[prev] = s[curr]
			prev++
		}
	}

	return s[:prev]
}

type LogRecordFilter func(plog.ResourceLogs) ([]plog.LogRecord, error)

func exportRequestLogRecords(exportRequest plogotlp.ExportRequest, filter LogRecordFilter) ([]plog.LogRecord, error) {
	allLogRecords := make([]plog.LogRecord, 0)
	logs := exportRequest.Logs()

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		resourceLogRecords, err := filter(logs.ResourceLogs().At(i))
		if err != nil {
			return nil, err
		}

		allLogRecords = append(allLogRecords, resourceLogRecords...)
	}

	return allLogRecords, nil
}

func createAndDeleteTempDeployment(ctx context.Context, config *envconf.Config, deployment *appsv1.Deployment, expectedReplicas int32) error {
	client := config.Client()
	if err := client.Resources().Create(ctx, deployment); err != nil {
		return err
	}

	// Wait until the deployment has all its pods running
	// See https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment
	wait.For(conditions.New(config.Client().Resources()).ResourceMatch(deployment, func(object k8s.Object) bool {
		d := object.(*appsv1.Deployment)
		return d.Status.AvailableReplicas == expectedReplicas && d.Status.ReadyReplicas == expectedReplicas
	}))

	fmt.Printf("Deployment %s/%s is ready\n", deployment.Namespace, deployment.Name)

	if err := client.Resources().Delete(ctx, deployment); err != nil {
		return err
	}

	wait.For(conditions.New(config.Client().Resources()).ResourceDeleted(deployment))

	fmt.Printf("Deployment %s/%s is deleted\n", deployment.Namespace, deployment.Name)

	return nil
}

func filterApplicationLogRecords(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, error) {
	return resourceLogsToScopedLogRecords(resourceLogs, "opentelemetry.sdk._logs._internal", "*", "*")
}

func filterHeartbeatLogRecords(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, error) {
	return resourceLogsToScopedLogRecords(resourceLogs, "lumigo-operator.namespace_heartbeat", "*", "*")
}

func filterPodLogRecords(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, error) {
	return resourceLogsToScopedLogRecords(resourceLogs, "lumigo-operator.log_file_collector", "*", "*")
}

func filterNamespaceAppLogRecords(namespaceName string) LogRecordFilter {
	return func(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, error) {
		return resourceLogsToScopedLogRecords(resourceLogs, "opentelemetry.sdk._logs._internal", namespaceName, "*")
	}
}

func filterWatchdogLogRecords(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, error) {
	return resourceLogsToScopedLogRecords(resourceLogs, "lumigo-operator.k8s-events", "*", "lumigo-kubernetes-operator-watchdog")
}

func resourceLogsToScopedLogRecords(resourceLogs plog.ResourceLogs, filteredScopeName string, filteredNamespaceName string, filteredServiceName string) ([]plog.LogRecord, error) {
	l := resourceLogs.ScopeLogs().Len()
	filteredScopeLogRecords := make([]plog.LogRecord, 0)

	for i := 0; i < l; i++ {
		scopeLogs := resourceLogs.ScopeLogs().At(i)
		scopeName := scopeLogs.Scope().Name()
		namespaceName, hasNamespaceName := resourceLogs.Resource().Attributes().Get("k8s.namespace.name")
		serviceName, hasServiceName := resourceLogs.Resource().Attributes().Get("service.name")
		logRecords := scopeLogsToLogRecords(scopeLogs)

		scopeMatches := filteredScopeName == "*" || (scopeName == filteredScopeName)
		namespaceMatches := filteredNamespaceName == "*" || (hasNamespaceName && (namespaceName.AsString() == filteredNamespaceName))
		serviceMatches := filteredServiceName == "*" || (hasServiceName && (serviceName.AsString() == filteredServiceName))

		if scopeMatches && namespaceMatches && serviceMatches {
			filteredScopeLogRecords = append(filteredScopeLogRecords, logRecords...)
		}
	}

	return filteredScopeLogRecords, nil
}
