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

package injector

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/mutation"
	"github.com/lumigo-io/lumigo-kubernetes-operator/webhooks/defaulter"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"

	//+kubebuilder:scaffold:imports
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

var lumigoApiVersion = fmt.Sprintf("%s/%s", operatorv1alpha1.GroupVersion.Group, operatorv1alpha1.GroupVersion.Version)
var lumigoOperatorVersion = "2b1e6b60ca871edee1d8f543c400f0b24663349144b78c79cfa006efaad6176a" // Unrealistically long, but we need to ensure we don't set label values too long
var lumigoInjectorImage = "localhost:5000/lumigo-autotrace:test"
var telemetryProxyOtlpServiceUrl = "lumigo-telemetry-proxy.lumigo-system.svc.cluster.local"
var telemetryProxyOtlpLogsServiceUrl = telemetryProxyOtlpServiceUrl

var statusActive = operatorv1alpha1.LumigoStatus{
	Conditions: []operatorv1alpha1.LumigoCondition{
		{
			Type:               operatorv1alpha1.LumigoConditionTypeError,
			Status:             corev1.ConditionFalse,
			Message:            "♫ Everything is awesome ♪",
			LastUpdateTime:     metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
		{
			Type:               operatorv1alpha1.LumigoConditionTypeActive,
			Status:             corev1.ConditionTrue,
			Message:            "♪ Everything is cool ♫",
			LastUpdateTime:     metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
	},
	InstrumentedResources: make([]corev1.ObjectReference, 0),
}

var statusErroneous = operatorv1alpha1.LumigoStatus{
	Conditions: []operatorv1alpha1.LumigoCondition{
		{
			Type:               operatorv1alpha1.LumigoConditionTypeError,
			Status:             corev1.ConditionTrue,
			Message:            "♫ Cuz you had a bad day ♪",
			LastUpdateTime:     metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
		{
			Type:               operatorv1alpha1.LumigoConditionTypeActive,
			Status:             corev1.ConditionFalse,
			Message:            "♪ You're taking one down ♫",
			LastUpdateTime:     metav1.NewTime(time.Now()),
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
	},
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Validator Webhook Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "..", "..", "config", "webhooks")},
		},
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := runtime.NewScheme()
	err = operatorv1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = admissionv1beta1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = corev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = appsv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	err = batchv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// start webhook server using Manager
	webhookInstallOptions := &testEnv.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    webhookInstallOptions.LocalServingHost,
			Port:    webhookInstallOptions.LocalServingPort,
			CertDir: webhookInstallOptions.LocalServingCertDir,
		}),
		LeaderElection: false,
		Metrics:        server.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	// We need the defaulter webhook as well to be able to create Lumigo objects
	// To remove this dependency, we would need to split config/webhooks in two
	// folders, one per webhook
	err = (&defaulter.LumigoDefaulterWebhookHandler{
		LumigoOperatorVersion: lumigoOperatorVersion,
		Log:                   ctrl.Log.WithName("defaulter-webhook").WithName("Lumigo"),
	}).SetupWebhookWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&LumigoInjectorWebhookHandler{
		EventRecorder:                    mgr.GetEventRecorderFor(fmt.Sprintf("lumigo-operator.v%s", lumigoOperatorVersion)),
		LumigoOperatorVersion:            lumigoOperatorVersion,
		LumigoInjectorImage:              lumigoInjectorImage,
		TelemetryProxyOtlpServiceUrl:     telemetryProxyOtlpServiceUrl,
		TelemetryProxyOtlpLogsServiceUrl: telemetryProxyOtlpLogsServiceUrl,
		Log:                              ctrl.Log.WithName("injector-webhook").WithName("Lumigo"),
	}).SetupWebhookWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&operatorv1alpha1.Lumigo{}).SetupWebhookWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:webhook

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// wait for the webhook server to get ready
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}).Should(Succeed())

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Context("Lumigo defaulter webhook", func() {

	var namespaceName string

	BeforeEach(func() {
		namespaceName = fmt.Sprintf("test%s", uuid.New())

		namespace := &corev1.Namespace{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Namespace",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}

		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())
	})

	AfterEach(func() {
		By("clean up test namespace", func() {
			namespace := &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Namespace",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}

			Expect(k8sClient.Delete(ctx, namespace)).Should(Succeed())

			// TODO Deleting test namespace: at the time of writing this comment, it hangs
			// Eventually(func() bool {
			// 	err := k8sClient.Get(context.Background(), types.NamespacedName{
			// 		Name: namespace.Name,
			// 	}, namespace)

			// 	return err != nil && errors.IsNotFound(err)
			// }, timeout, interval).Should(BeTrue())
		})
	})

	Context("with no Lumigo instance in the namespace", func() {

		It("should not inject", func() {
			name := "test-deployment"

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespaceName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"deployment": name,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"deployment": name,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "myapp",
									Image: "busybox",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(deploymentAfter.ObjectMeta.Labels).To(BeEmpty())
			Expect(deploymentAfter.Spec.Template.ObjectMeta.Labels).To(BeEquivalentTo(map[string]string{
				"deployment": name,
			}))
			Expect(deploymentAfter.Spec.Template.Spec.InitContainers).To(BeEmpty())
			Expect(deploymentAfter.Spec.Template.Spec.Volumes).To(BeEmpty())
			Expect(deploymentAfter.Spec.Template.Spec.Containers).To(HaveLen(1))
		})

		It("should inject a deployment having the lumigo.auto-trace label set to true", func() {
			name := "test-opt-in-deployment"

			deployment := newDeployment(namespaceName, name, true)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(deploymentAfter).To(mutation.BeInstrumentedWithLumigo(lumigoOperatorVersion, lumigoInjectorImage, telemetryProxyOtlpServiceUrl, false))
		})

		It("should inject after the lumigo.auto-trace label is changed from false to true", func() {
			name := "test-deployment-label-change"

			// Create deployment with auto-trace label set to false
			deployment := newDeployment(namespaceName, name, false)
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			// Verify the deployment is not instrumented
			deploymentBefore := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentBefore); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(deploymentBefore).NotTo(mutation.BeInstrumentedWithLumigo(lumigoOperatorVersion, lumigoInjectorImage, telemetryProxyOtlpServiceUrl, false))

			// Now update the deployment to set auto-trace label to true
			deploymentBefore.ObjectMeta.Labels[mutation.LumigoAutoTraceLabelKey] = "true"
			Expect(k8sClient.Update(ctx, deploymentBefore)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify the deployment is now instrumented
			Expect(deploymentAfter).To(mutation.BeInstrumentedWithLumigo(lumigoOperatorVersion, lumigoInjectorImage, telemetryProxyOtlpServiceUrl, false))
		})
	})

	Context("with one inactive Lumigo instance in the namespace", func() {

		It("should not inject", func() {
			lumigo := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "DoesNot",
					Key:  "Exist",
				},
			}, true, true)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			lumigo.Status = statusErroneous
			k8sClient.Status().Update(ctx, lumigo)

			name := "test-deployment"

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespaceName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"deployment": name,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"deployment": name,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "myapp",
									Image: "busybox",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(deploymentAfter.ObjectMeta.Labels).To(BeEmpty())
			Expect(deploymentAfter.Spec.Template.ObjectMeta.Labels).To(BeEquivalentTo(map[string]string{
				"deployment": name,
			}))
			Expect(deploymentAfter.Spec.Template.Spec.InitContainers).To(BeEmpty())
			Expect(deploymentAfter.Spec.Template.Spec.Volumes).To(BeEmpty())
			Expect(deploymentAfter.Spec.Template.Spec.Containers).To(HaveLen(1))
		})

	})

	Context("with one active Lumigo instance in the namespace", func() {

		It("should inject a minimal deployment", func() {
			lumigo := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "lumigosecret",
					Key:  "token",
				},
			}, true, true)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			lumigo.Status = statusActive
			k8sClient.Status().Update(ctx, lumigo)

			name := "test-deployment"

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespaceName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"deployment": name,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"deployment": name,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "myapp",
									Image: "busybox",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(deploymentAfter).To(mutation.BeInstrumentedWithLumigo(lumigoOperatorVersion, lumigoInjectorImage, telemetryProxyOtlpServiceUrl, true))
		})

		It("should inject a deployment with containers running not as root", func() {
			lumigo := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "lumigosecret",
					Key:  "token",
				},
			}, true, false)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			lumigo.Status = statusActive
			k8sClient.Status().Update(ctx, lumigo)

			name := "test-deployment"

			f := false
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespaceName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"deployment": name,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"deployment": name,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "myapp",
									Image: "busybox",
								},
							},
							SecurityContext: &corev1.PodSecurityContext{
								RunAsNonRoot: &f,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(deploymentAfter).To(mutation.BeInstrumentedWithLumigo(lumigoOperatorVersion, lumigoInjectorImage, telemetryProxyOtlpServiceUrl, false))
			Expect(deploymentAfter.Spec.Template.Spec.InitContainers[0].SecurityContext.RunAsNonRoot).To(Equal(&f))
		})

		It("should inject a deployment with containers with FSGroup set", func() {
			lumigo := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "lumigosecret",
					Key:  "token",
				},
			}, true, false)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			lumigo.Status = statusActive
			k8sClient.Status().Update(ctx, lumigo)

			name := "test-deployment"

			var group int64 = 4321
			t := true
			f := false

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespaceName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"deployment": name,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"deployment": name,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "myapp",
									Image: "busybox",
									SecurityContext: &corev1.SecurityContext{
										RunAsNonRoot:             &f,
										RunAsGroup:               &group,
										RunAsUser:                &group,
										Privileged:               &f,
										AllowPrivilegeEscalation: &f,
										ReadOnlyRootFilesystem:   &t,
									},
								},
							},
							SecurityContext: &corev1.PodSecurityContext{
								FSGroup: &group,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(deploymentAfter).To(mutation.BeInstrumentedWithLumigo(lumigoOperatorVersion, lumigoInjectorImage, telemetryProxyOtlpServiceUrl, false))
			Expect(deploymentAfter.Spec.Template.Spec.InitContainers[0].SecurityContext.RunAsGroup).To(Equal(&group))
		})

		It("should ignore the settings related to the lumigo.enable-traces and lumigo.enable-logs labels", func() {
			lumigo := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: DefaultLumigoSecretName,
					Key:  DefaultLumigoSecretKey,
				},
			}, true, true)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			lumigo.Status = statusActive
			k8sClient.Status().Update(ctx, lumigo)

			name := "test-deployment-label-override"

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespaceName,
					Labels: map[string]string{
						mutation.LumigoAutoTraceLabelKey:              "true",
						mutation.LumigoAutoTraceTracesEnabledLabelKey: "false",
						mutation.LumigoAutoTraceLogsEnabledLabelKey:   "false",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"deployment": name,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"deployment": name,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "myapp",
									Image: "busybox",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			deploymentAfter := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespaceName,
				Name:      name,
			}, deploymentAfter); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			// The deployment should be instrumented despite the trace/logs labels being set to false
			Expect(deploymentAfter).To(mutation.BeInstrumentedWithLumigo(lumigoOperatorVersion, lumigoInjectorImage, telemetryProxyOtlpServiceUrl, true))

			// Verify the original labels are preserved
			Expect(deploymentAfter.ObjectMeta.Labels).To(HaveKeyWithValue(mutation.LumigoAutoTraceTracesEnabledLabelKey, "false"))
			Expect(deploymentAfter.ObjectMeta.Labels).To(HaveKeyWithValue(mutation.LumigoAutoTraceLogsEnabledLabelKey, "false"))
		})
	})

	It("should not inject a deployment with the lumigo.auto-trace label set to false", func() {
		lumigo := newLumigo(namespaceName, "lumigo1", operatorv1alpha1.Credentials{
			SecretRef: operatorv1alpha1.KubernetesSecretRef{
				Name: "doesnot",
				Key:  "exist",
			},
		}, true, true)
		Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

		lumigo.Status = statusActive
		k8sClient.Status().Update(ctx, lumigo)

		name := "test-deployment"

		deployment := newDeployment(namespaceName, name, false)
		Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

		deploymentAfter := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespaceName,
			Name:      name,
		}, deploymentAfter); err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(deploymentAfter.ObjectMeta.Labels).To(BeEquivalentTo(map[string]string{
			mutation.LumigoAutoTraceLabelKey: "false",
		}))
		Expect(deploymentAfter.Spec.Template.ObjectMeta.Labels).To(BeEquivalentTo(map[string]string{
			"deployment": name,
		}))
		Expect(deploymentAfter.Spec.Template.Spec.InitContainers).To(BeEmpty())
		Expect(deploymentAfter.Spec.Template.Spec.Volumes).To(BeEmpty())
		Expect(deploymentAfter.Spec.Template.Spec.Containers).To(HaveLen(1))
	})
})

func newLumigo(namespace string, name string, lumigoToken operatorv1alpha1.Credentials, injectionEnabled bool, loggingEnabled bool) *operatorv1alpha1.Lumigo {
	return &operatorv1alpha1.Lumigo{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Lumigo",
			APIVersion: lumigoApiVersion,
		},
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
			Logging: operatorv1alpha1.LoggingSpec{
				Enabled: &loggingEnabled,
			},
		},
	}
}

func newDeployment(namespaceName, name string, autoTraceEnabled bool) *appsv1.Deployment {
	autoTraceValue := "false"
	if autoTraceEnabled {
		autoTraceValue = "true"
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespaceName,
			Labels: map[string]string{
				mutation.LumigoAutoTraceLabelKey: autoTraceValue,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"deployment": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"deployment": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "myapp",
							Image: "busybox",
						},
					},
				},
			},
		},
	}

	return deployment
}
