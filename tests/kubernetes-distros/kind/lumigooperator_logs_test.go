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

	"github.com/go-logr/logr/testr"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/tests/kubernetes-distros/kind/internal"
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

func TestLumigoOperatorEventsAndObjects(t *testing.T) {
	logger := testr.New(t)

	testAppDeploymentFeature := features.New("TestApp").
		Setup(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
			client := config.Client()

			namespaceName := envconf.RandomName("test-ns", 12)
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
				t.Fail()
			}
			operatorv1alpha1.AddToScheme(r.GetScheme())
			r.Create(ctx, lumigo)

			deploymentName := "testdeployment"
			testImage := "python"
			logOutput := "IT'S ALIIIIIIVE!"

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
									Name:    "myapp",
									Image:   testImage,
									Command: []string{"python", "-c", fmt.Sprintf("while True: print(\"%s\"); import time; time.sleep(5)", logOutput)},
								},
							},
						},
					},
				},
			}

			if err := client.Resources().Create(ctx, deployment); err != nil {
				t.Fatal(err)
			}

			// Wait until the deployment has all its pods running
			// See https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment
			wait.For(conditions.New(config.Client().Resources()).ResourceMatch(deployment, func(object k8s.Object) bool {
				d := object.(*appsv1.Deployment)
				return d.Status.AvailableReplicas == replicas && d.Status.ReadyReplicas == replicas
			}))

			logger.Info("Deployment is ready")

			// Tear it all down
			if err := client.Resources().Delete(ctx, deployment); err != nil {
				t.Fatal(err)
			}

			wait.For(conditions.New(config.Client().Resources()).ResourceDeleted(deployment))

			logger.Info("Deployment is deleted")

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

					if e, _, err := exportRequestToLogRecords(exportRequest); err != nil {
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

					e, o, err := exportRequestToLogRecords(exportRequest)
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
						t.Fatalf("found logs without the 'k8s.cluster.uid' resource attribute: %+v", resourceAttributes)
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

					if e, err := exportRequestToHeartbeatLogRecords(exportRequest); err != nil {
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

func exportRequestToLogRecords(exportRequest plogotlp.ExportRequest) ([]plog.LogRecord, []plog.LogRecord, error) {
	eventLogs := make([]plog.LogRecord, 0)
	objectLogs := make([]plog.LogRecord, 0)
	logs := exportRequest.Logs()

	l := logs.ResourceLogs().Len()
	for i := 0; i < l; i++ {
		e, o, err := resourceLogsToLogRecords(logs.ResourceLogs().At(i))
		if err != nil {
			return nil, nil, err
		}

		eventLogs = append(eventLogs, e...)
		objectLogs = append(objectLogs, o...)
	}

	return eventLogs, objectLogs, nil
}

func resourceLogsToLogRecords(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, []plog.LogRecord, error) {
	l := resourceLogs.ScopeLogs().Len()
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
	return eventLogRecords, objectLogRecords, nil
}

func scopeLogsToLogRecords(scopeLogs plog.ScopeLogs) []plog.LogRecord {
	l := scopeLogs.LogRecords().Len()
	logRecords := make([]plog.LogRecord, l)
	for i := 0; i < l; i++ {
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

func exportRequestToHeartbeatLogRecords(exportRequest plogotlp.ExportRequest) ([]plog.LogRecord, error) {
	eventLogs := make([]plog.LogRecord, 0)
	logs := exportRequest.Logs()

	l := logs.ResourceLogs().Len()
	for i := 0; i < l; i++ {
		e, err := resourceLogsToHeartbeatLogRecords(logs.ResourceLogs().At(i))
		if err != nil {
			return nil, err
		}

		eventLogs = append(eventLogs, e...)
	}

	return eventLogs, nil
}

func resourceLogsToHeartbeatLogRecords(resourceLogs plog.ResourceLogs) ([]plog.LogRecord, error) {
	l := resourceLogs.ScopeLogs().Len()
	heartbeatLogRecords := make([]plog.LogRecord, 0)
	for i := 0; i < l; i++ {
		scopeLogs := resourceLogs.ScopeLogs().At(i)
		scopeName := scopeLogs.Scope().Name()
		logRecords := scopeLogsToLogRecords(scopeLogs)

		switch scopeName {
		case "lumigo-operator.namespace_heartbeat":
			{
				heartbeatLogRecords = append(heartbeatLogRecords, logRecords...)
			}
		}
	}
	return heartbeatLogRecords, nil
}
