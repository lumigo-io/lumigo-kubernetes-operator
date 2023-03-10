package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	gintype "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	gtypes "github.com/onsi/gomega/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
)

var (
	ctx                     context.Context
	k8sClient               client.Client
	clientset               *kubernetes.Clientset
	lumigoToken             string
	lumigoNamespace         = "lumigo-system"
	defaultTimeout          = 10 * time.Second
	defaultInterval         = 100 * time.Millisecond
	keepNamespacesOnFailure bool
)

// These tests assume:
// 1. A valid `kubectl` configuration available in the home directory of the user
// 2. A Lumigo operator installed into the Kubernetes cluster referenced by the
//    `kubectl` configuration

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "E2E Test Suite")
}

var _ = BeforeSuite(func() {
	keepNamespacesOnFailureString, isSet := os.LookupEnv("KEEP_TEST_NAMESPACES_ON_FAILURE")
	keepNamespacesOnFailure = !isSet || (keepNamespacesOnFailureString == "false")

	ctx = context.TODO()
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("Looking up configurations from the environment", func() {
		if lumigoToken = os.Getenv("LUMIGO_TRACER_TOKEN"); lumigoToken == "" {
			lumigoToken = "t_1234567890123456789AB"
		}
		Expect(lumigoToken).NotTo(BeEmpty())
	})

	By("Setting up the Kubernetes client", func() {
		home := homedir.HomeDir()
		Expect(home).NotTo(BeEmpty())

		kubeconfig := filepath.Join(home, ".kube", "config")
		Expect(kubeconfig).To(BeARegularFile())

		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		Expect(err).NotTo(HaveOccurred())

		err = operatorv1alpha1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		k8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		clientset, err = kubernetes.NewForConfig(config)
		Expect(err).NotTo(HaveOccurred())
	})

})

var _ = Context("End-to-end tests", func() {

	Context("the Lumigo operator", func() {

		var namespaceName string

		BeforeEach(func() {
			// Validate the operator is in good health
			componentIsManagerRequirement, err := labels.NewRequirement(
				"app.kubernetes.io/component", selection.Equals, []string{"manager"},
			)
			Expect(err).NotTo(HaveOccurred())

			partOfLumigoRequirement, err := labels.NewRequirement(
				"app.kubernetes.io/part-of", selection.Equals, []string{"lumigo"},
			)
			Expect(err).NotTo(HaveOccurred())

			By("Validating health of controller", func() {
				Eventually(func(g Gomega) {
					deployments := &appsv1.DeploymentList{}

					g.Expect(k8sClient.List(ctx, deployments, &client.ListOptions{
						Namespace:     lumigoNamespace,
						LabelSelector: labels.NewSelector().Add(*componentIsManagerRequirement, *partOfLumigoRequirement),
					})).To(Succeed())

					g.Expect(deployments.Items).To(HaveLen(1))

					lumigoControllerDeployment := deployments.Items[0]

					g.Expect(lumigoControllerDeployment).Should(BeAvailable())
				}, defaultTimeout, defaultInterval).Should(Succeed())
			})

			By("Preparing test namespace", func() {
				namespaceName = fmt.Sprintf("test%s", uuid.New())

				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespaceName,
					},
				}

				Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())
			})

		})

		AfterEach(func() {
			if CurrentSpecReport().State == gintype.SpecStatePassed || !keepNamespacesOnFailure {
				By("Cleaning up test namespace", func() {
					namespace := &corev1.Namespace{
						ObjectMeta: metav1.ObjectMeta{
							Name: namespaceName,
						},
					}

					Expect(k8sClient.Delete(ctx, namespace)).Should(Succeed())

					Eventually(func() bool {
						err := k8sClient.Get(context.Background(), types.NamespacedName{
							Name: namespace.Name,
						}, namespace)

						return err != nil && apierrors.IsNotFound(err)
					}, 1*time.Minute, defaultInterval).Should(BeTrue())
				})
			}
		})

		It("trace a Python job created after the Lumigo resource is created", func() {
			lumigoTokenName := "lumigo-credentials"
			lumigoTokenKey := "token"
			testImage := "python"

			logOutput := "IT'S ALIIIIIIVE!"

			By("Creating the LumigoToken secret", func() {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      lumigoTokenName,
					},
					StringData: map[string]string{
						lumigoTokenKey: lumigoToken,
					},
				})).Should(Succeed())
			})

			By("Creating the Lumigo instance", func() {
				lumigo1 := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
					SecretRef: operatorv1alpha1.KubernetesSecretRef{
						Name: lumigoTokenName,
						Key:  lumigoTokenKey,
					},
				}, true)
				Expect(k8sClient.Create(ctx, lumigo1)).Should(Succeed())

				lumigoNamespacedName := types.NamespacedName{
					Namespace: namespaceName,
					Name:      lumigo1.Name,
				}
				Eventually(func(g Gomega) {
					lumigo := &operatorv1alpha1.Lumigo{}
					g.Expect(k8sClient.Get(ctx, lumigoNamespacedName, lumigo)).To(Succeed())

					activityCondition := &operatorv1alpha1.LumigoCondition{}
					g.Expect(lumigo.Status.Conditions).Should(
						ContainElement(
							MatchesLumigoCondition(
								&operatorv1alpha1.LumigoCondition{
									Type:   "Active",
									Status: "True",
								},
							),
							activityCondition,
						),
					)
				}, defaultTimeout, defaultInterval).Should(Succeed())
			})

			jobName := "testjob"
			By("Creating the job", func() {
				var completions int32 = 1
				job := &batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      jobName,
					},
					Spec: batchv1.JobSpec{
						Completions: &completions,
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app":  "myapp",
									"type": "job",
								},
							},
							Spec: corev1.PodSpec{
								RestartPolicy: "Never",
								Containers: []corev1.Container{
									{
										Name:    "myapp",
										Image:   testImage,
										Command: []string{"python", "-c", fmt.Sprintf("print(\"%s\")", logOutput)},
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, job)).Should(Succeed())
			})

			By("Checking wether the job is injected", func() {
				appReq, err := labels.NewRequirement("app", selection.Equals, []string{"myapp"})
				Expect(err).NotTo(HaveOccurred())

				typeReq, err := labels.NewRequirement("type", selection.Equals, []string{"job"})
				Expect(err).NotTo(HaveOccurred())

				jobPodSelector := labels.NewSelector()
				jobPodSelector.Add(*appReq)
				jobPodSelector.Add(*typeReq)

				Eventually(func(g Gomega) {
					pods := &corev1.PodList{}
					g.Expect(k8sClient.List(ctx, pods, &client.ListOptions{
						Namespace:     namespaceName,
						LabelSelector: jobPodSelector,
					})).To(Succeed())
					g.Expect(pods.Items).To(HaveLen(1))

					request := clientset.CoreV1().Pods(namespaceName).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{})
					podLogs, err := request.Stream(ctx)
					g.Expect(err).NotTo(HaveOccurred())

					defer podLogs.Close()

					buf := new(bytes.Buffer)
					_, err = io.Copy(buf, podLogs)
					g.Expect(err).NotTo(HaveOccurred())

					logs := buf.String()
					g.Expect(logs).To(ContainSubstring("Loading the Lumigo OpenTelemetry distribution"))
					g.Expect(logs).To(ContainSubstring(logOutput))
				},
					// Relax timeout, this image will need to be pulled remotely
					1*time.Minute,
					defaultInterval).Should(Succeed())
			})

			By("Checking that the job has the added-instrumentation Lumigo event", func() {
				Eventually(func(g Gomega) {
					job := &batchv1.Job{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespaceName,
							Name:      jobName,
						},
					}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
					g.Expect(job).To(HaveLumigoEvent(operatorv1alpha1.LumigoEventReasonAddedInstrumentation))
				}, defaultTimeout, defaultInterval).Should(Succeed())
			})
		})

		It("trace a Python deployment with tight security settings that is created after the Lumigo resource is created", func() {
			lumigoTokenName := "lumigo-credentials"
			lumigoTokenKey := "token"
			testImage := "python"

			logOutput := "IT'S ALIIIIIIVE!"

			By("Creating the LumigoToken secret", func() {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      lumigoTokenName,
					},
					StringData: map[string]string{
						lumigoTokenKey: lumigoToken,
					},
				})).Should(Succeed())
			})

			By("Creating the Lumigo instance", func() {
				lumigo1 := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
					SecretRef: operatorv1alpha1.KubernetesSecretRef{
						Name: lumigoTokenName,
						Key:  lumigoTokenKey,
					},
				}, true)
				Expect(k8sClient.Create(ctx, lumigo1)).Should(Succeed())

				lumigoNamespacedName := types.NamespacedName{
					Namespace: namespaceName,
					Name:      lumigo1.Name,
				}
				Eventually(func(g Gomega) {
					lumigo := &operatorv1alpha1.Lumigo{}
					g.Expect(k8sClient.Get(ctx, lumigoNamespacedName, lumigo)).To(Succeed())

					activityCondition := &operatorv1alpha1.LumigoCondition{}
					g.Expect(lumigo.Status.Conditions).Should(
						ContainElement(
							MatchesLumigoCondition(
								&operatorv1alpha1.LumigoCondition{
									Type:   "Active",
									Status: "True",
								},
							),
							activityCondition,
						),
					)
				}, defaultTimeout, defaultInterval).Should(Succeed())
			})

			deploymentName := "testdeployment"

			By("Creating the deployment", func() {
				t := true
				var g int64 = 5678

				var replicas int32 = 1
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
									RunAsNonRoot: &t,
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
				Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())
			})

			By("Checking that the deployment is injected", func() {
				appReq, err := labels.NewRequirement("app", selection.Equals, []string{"myapp"})
				Expect(err).NotTo(HaveOccurred())

				typeReq, err := labels.NewRequirement("type", selection.Equals, []string{"deployment"})
				Expect(err).NotTo(HaveOccurred())

				deploymentPodSelector := labels.NewSelector()
				deploymentPodSelector.Add(*appReq)
				deploymentPodSelector.Add(*typeReq)

				Eventually(func(g Gomega) {
					pods := &corev1.PodList{}
					g.Expect(k8sClient.List(ctx, pods, &client.ListOptions{
						Namespace:     namespaceName,
						LabelSelector: deploymentPodSelector,
					})).To(Succeed())
					g.Expect(pods.Items).To(HaveLen(1))

					request := clientset.CoreV1().Pods(namespaceName).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{})
					podLogs, err := request.Stream(ctx)
					g.Expect(err).NotTo(HaveOccurred())

					defer podLogs.Close()

					buf := new(bytes.Buffer)
					_, err = io.Copy(buf, podLogs)
					g.Expect(err).NotTo(HaveOccurred())

					logs := buf.String()
					g.Expect(logs).To(ContainSubstring("Loading the Lumigo OpenTelemetry distribution"))
					// g.Expect(logs).To(ContainSubstring(logOutput))
				},
					// Relax timeout, this image will need to be pulled remotely
					1*time.Minute,
					defaultInterval).Should(Succeed())
			})

			By("Checking that the deployment has the added-instrumentation Lumigo event", func() {
				Eventually(func(g Gomega) {
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespaceName,
							Name:      deploymentName,
						},
					}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
					g.Expect(deployment).To(HaveLumigoEvent(operatorv1alpha1.LumigoEventReasonAddedInstrumentation))
				}, defaultTimeout, defaultInterval).Should(Succeed())
			})
		})

		It("trace a Python statefulset created after the Lumigo resource is created", func() {
			lumigoTokenName := "lumigo-credentials"
			lumigoTokenKey := "token"
			testImage := "python"

			logOutput := "IT'S ALIIIIIIVE!"

			By("Creating the LumigoToken secret", func() {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      lumigoTokenName,
					},
					StringData: map[string]string{
						lumigoTokenKey: lumigoToken,
					},
				})).Should(Succeed())
			})

			By("Creating the Lumigo instance", func() {
				lumigo1 := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
					SecretRef: operatorv1alpha1.KubernetesSecretRef{
						Name: lumigoTokenName,
						Key:  lumigoTokenKey,
					},
				}, true)
				Expect(k8sClient.Create(ctx, lumigo1)).Should(Succeed())

				lumigoNamespacedName := types.NamespacedName{
					Namespace: namespaceName,
					Name:      lumigo1.Name,
				}
				Eventually(func(g Gomega) {
					lumigo := &operatorv1alpha1.Lumigo{}
					g.Expect(k8sClient.Get(ctx, lumigoNamespacedName, lumigo)).To(Succeed())

					activityCondition := &operatorv1alpha1.LumigoCondition{}
					g.Expect(lumigo.Status.Conditions).Should(
						ContainElement(
							MatchesLumigoCondition(
								&operatorv1alpha1.LumigoCondition{
									Type:   "Active",
									Status: "True",
								},
							),
							activityCondition,
						),
					)
				}, defaultTimeout, defaultInterval).Should(Succeed())
			})

			statefulSetName := "teststatefulset"

			By("Creating the statefulset", func() {
				var replicas int32 = 1
				statefulset := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      statefulSetName,
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: &replicas,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app":  "myapp",
								"type": "statefulset",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app":  "myapp",
									"type": "statefulset",
								},
							},
							Spec: corev1.PodSpec{
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
				Expect(k8sClient.Create(ctx, statefulset)).Should(Succeed())
			})

			By("Checking that the statefulset is injected", func() {
				appReq, err := labels.NewRequirement("app", selection.Equals, []string{"myapp"})
				Expect(err).NotTo(HaveOccurred())

				typeReq, err := labels.NewRequirement("type", selection.Equals, []string{"deployment"})
				Expect(err).NotTo(HaveOccurred())

				statefulsetPodSelector := labels.NewSelector()
				statefulsetPodSelector.Add(*appReq)
				statefulsetPodSelector.Add(*typeReq)

				Eventually(func(g Gomega) {
					pods := &corev1.PodList{}
					g.Expect(k8sClient.List(ctx, pods, &client.ListOptions{
						Namespace:     namespaceName,
						LabelSelector: statefulsetPodSelector,
					})).To(Succeed())
					g.Expect(pods.Items).To(HaveLen(1))

					request := clientset.CoreV1().Pods(namespaceName).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{})
					podLogs, err := request.Stream(ctx)
					g.Expect(err).NotTo(HaveOccurred())

					defer podLogs.Close()

					buf := new(bytes.Buffer)
					_, err = io.Copy(buf, podLogs)
					g.Expect(err).NotTo(HaveOccurred())

					logs := buf.String()
					g.Expect(logs).To(ContainSubstring("Loading the Lumigo OpenTelemetry distribution"))
					// g.Expect(logs).To(ContainSubstring(logOutput))
				},
					// Relax timeout, this image will need to be pulled remotely
					1*time.Minute,
					defaultInterval).Should(Succeed())

				By("Checking that the statefulset has the added-instrumentation Lumigo event", func() {
					Eventually(func(g Gomega) {
						statefulSet := &appsv1.StatefulSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespaceName,
								Name:      statefulSetName,
							},
						}
						g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), statefulSet)).To(Succeed())
						g.Expect(statefulSet).To(HaveLumigoEvent(operatorv1alpha1.LumigoEventReasonAddedInstrumentation))
					}, defaultTimeout, defaultInterval).Should(Succeed())
				})
			})
		})

	})

})

func BeAvailable() gtypes.GomegaMatcher {
	return &beAvailable{}
}

type beAvailable struct{}

func (m *beAvailable) Match(actual interface{}) (success bool, err error) {
	switch a := actual.(type) {
	case appsv1.Deployment:
		var actualDeployment appsv1.Deployment = a

		for _, condition := range actualDeployment.Status.Conditions {
			if condition.Type == "Available" {
				return condition.Status == "True", nil
			}
		}

		return false, nil
	default:
		return false, fmt.Errorf("MatchesLumigoCondition matcher expects a appsv1.Deployment; got:\n%s", format.Object(actual, 1))
	}
}

func (m *beAvailable) FailureMessage(actual interface{}) (message string) {
	return "have the 'Available' condition with status 'True'"
}

func (m *beAvailable) NegatedFailureMessage(actual interface{}) (message string) {
	return "not have the 'Available' condition with status 'True'"
}

func MatchesLumigoCondition(condition *operatorv1alpha1.LumigoCondition) gtypes.GomegaMatcher {
	return &matchesLumigoCondition{
		condition: condition,
	}
}

type matchesLumigoCondition struct {
	condition *operatorv1alpha1.LumigoCondition
}

func (m *matchesLumigoCondition) Match(actual interface{}) (success bool, err error) {
	var actualCondition operatorv1alpha1.LumigoCondition

	switch a := actual.(type) {
	case operatorv1alpha1.LumigoCondition:
		actualCondition = a
	default:
		return false, fmt.Errorf("MatchesLumigoCondition matcher expects a operatorv1alpha1.LumigoCondition; got:\n%s", format.Object(actual, 1))
	}

	if m.condition.Status != "" && m.condition.Status != actualCondition.Status {
		return false, fmt.Errorf("has a different Status: expected '%s', got '%s'", m.condition.Status, actualCondition.Status)
	}

	if m.condition.Type != "" && m.condition.Type != actualCondition.Type {
		return false, fmt.Errorf("has a different Type: expected '%s', got '%s'", m.condition.Type, actualCondition.Type)
	}

	if m.condition.Message != "" && m.condition.Message != actualCondition.Message {
		return false, fmt.Errorf("has a different Message: expected '%s', got '%s'", m.condition.Message, actualCondition.Message)
	}

	return true, nil
}

func (m *matchesLumigoCondition) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("does not match the LumigoCondition %+v", m.condition)
}

func (m *matchesLumigoCondition) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("matches the LumigoCondition %+v", m.condition)
}

func MatchesDeploymentCondition(condition *appsv1.DeploymentCondition) gtypes.GomegaMatcher {
	return &matchesDeploymentConditionMatcher{
		condition: condition,
	}
}

type matchesDeploymentConditionMatcher struct {
	condition *appsv1.DeploymentCondition
}

func (m *matchesDeploymentConditionMatcher) Match(actual interface{}) (success bool, err error) {
	var actualCondition appsv1.DeploymentCondition

	switch a := actual.(type) {
	case appsv1.DeploymentCondition:
		actualCondition = a
	default:
		return false, fmt.Errorf("MatchesDeploymentCondition matcher expects a appsv1.DeploymentCondition; got:\n%s", format.Object(actual, 1))
	}

	if m.condition.Status != "" && m.condition.Status != actualCondition.Status {
		return false, fmt.Errorf("has a different Status: expected '%s', got '%s'", m.condition.Status, actualCondition.Status)
	}

	if m.condition.Type != "" && m.condition.Type != actualCondition.Type {
		return false, fmt.Errorf("has a different Type: expected '%s', got '%s'", m.condition.Type, actualCondition.Type)
	}

	if m.condition.Message != "" && m.condition.Message != actualCondition.Message {
		return false, fmt.Errorf("has a different Message: expected '%s', got '%s'", m.condition.Message, actualCondition.Message)
	}

	if m.condition.Reason != "" && m.condition.Reason != actualCondition.Reason {
		return false, fmt.Errorf("has a different Reason: expected '%s', got '%s'", m.condition.Reason, actualCondition.Reason)
	}

	return true, nil
}

func (m *matchesDeploymentConditionMatcher) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("does not match the DeploymentCondition %+v", m.condition)
}

func (m *matchesDeploymentConditionMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("matches the DeploymentCondition %+v", m.condition)
}

func HaveLumigoEvent(reason operatorv1alpha1.LumigoEventReason) gtypes.GomegaMatcher {
	return &haveLumigoEventMatcher{
		reason: reason,
	}
}

type haveLumigoEventMatcher struct {
	reason operatorv1alpha1.LumigoEventReason
}

func (m *haveLumigoEventMatcher) Match(actual interface{}) (success bool, err error) {
	var namespace string
	var name string

	switch a := actual.(type) {
	case *appsv1.DaemonSet:
		namespace = a.Namespace
		name = a.Name
	case *appsv1.Deployment:
		namespace = a.Namespace
		name = a.Name
	case *appsv1.ReplicaSet:
		namespace = a.Namespace
		name = a.Name
	case *appsv1.StatefulSet:
		namespace = a.Namespace
		name = a.Name
	case *batchv1.CronJob:
		namespace = a.Namespace
		name = a.Name
	case *batchv1.Job:
		namespace = a.Namespace
		name = a.Name
	default:
		return false, fmt.Errorf("HasLumigoEvent matcher expects one of *appsv1.DaemonSet, *appsv1.Deployment, *appsv1.ReplicaSet, *appsv1.StatefulSet, *batchv1.CronJob or *batchv1.Job; got:\n%s", format.Object(actual, 1))
	}

	eventList, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	for _, event := range eventList.Items {
		if event.InvolvedObject.Name == name && event.InvolvedObject.Namespace == namespace && event.Reason == string(m.reason) {
			return true, nil
		}
	}

	return false, nil
}

func (m *haveLumigoEventMatcher) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("does not have an event with reason '%s'", m.reason)
}

func (m *haveLumigoEventMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("has an event with reason '%s'", m.reason)
}

func newLumigo(namespace string, name string, lumigoToken operatorv1alpha1.Credentials, injectionEnabled bool) *operatorv1alpha1.Lumigo {
	return &operatorv1alpha1.Lumigo{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{},
		},
		Spec: operatorv1alpha1.LumigoSpec{
			LumigoToken: lumigoToken,
			Tracing: operatorv1alpha1.TracingSpec{
				Injection: operatorv1alpha1.InjectionSpec{
					Enabled: &injectionEnabled,
				},
			},
		},
	}
}
