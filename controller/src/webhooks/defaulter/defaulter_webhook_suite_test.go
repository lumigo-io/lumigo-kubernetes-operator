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

package defaulter

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
	"github.com/onsi/gomega/format"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"

	//+kubebuilder:scaffold:imports
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// start webhook server using Manager
	webhookInstallOptions := &testEnv.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme,
		Host:               webhookInstallOptions.LocalServingHost,
		Port:               webhookInstallOptions.LocalServingPort,
		CertDir:            webhookInstallOptions.LocalServingCertDir,
		LeaderElection:     false,
		MetricsBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&LumigoDefaulterWebhookHandler{
		LumigoOperatorVersion: "test",
		Log:                   ctrl.Log.WithName("defaulter-webhook").WithName("Lumigo"),
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

	Context("when creating the first Lumigo instance in the namespace", func() {

		It("it sets defaults for Tracing.Injection.* and Logging", func() {
			newLumigo := operatorv1alpha1.Lumigo{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Lumigo",
					APIVersion: lumigoApiVersion,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      "lumigo",
					Labels:    map[string]string{},
				},
				Spec: operatorv1alpha1.LumigoSpec{
					LumigoToken: operatorv1alpha1.Credentials{
						SecretRef: operatorv1alpha1.KubernetesSecretRef{
							Name: "doesnot",
							Key:  "exist",
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &newLumigo)).To(Succeed())

			Expect(newLumigo.Spec.Infrastructure.Enabled).To(&beBoolPointer{expectedValue: true})
			Expect(newLumigo.Spec.Infrastructure.KubeEvents.Enabled).To(&beBoolPointer{expectedValue: true})
			Expect(newLumigo.Spec.Tracing.Injection.Enabled).To(&beBoolPointer{expectedValue: true})
			Expect(newLumigo.Spec.Tracing.Injection.InjectLumigoIntoExistingResourcesOnCreation).To(&beBoolPointer{expectedValue: true})
			Expect(newLumigo.Spec.Tracing.Injection.RemoveLumigoFromResourcesOnDeletion).To(&beBoolPointer{expectedValue: true})
			Expect(newLumigo.Spec.Logging.Enabled).To(&beBoolPointer{expectedValue: false})
		})

		It("it rejects instances with blank .LumigoToken.Spec.LumigoToken.SecretRef.Name", func() {
			newLumigo := operatorv1alpha1.Lumigo{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Lumigo",
					APIVersion: lumigoApiVersion,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      "lumigo",
					Labels:    map[string]string{},
				},
				Spec: operatorv1alpha1.LumigoSpec{},
			}

			Expect(k8sClient.Create(ctx, &newLumigo)).To(MatchError("admission webhook \"lumigodefaulter.kb.io\" denied the request: no reference to a Lumigo token ('.Spec.LumigoToken.SecretRef.Name' is blank)"))
		})

		It("it rejects instances with blank .LumigoToken.Spec.LumigoToken.SecretRef.Key", func() {
			newLumigo := operatorv1alpha1.Lumigo{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Lumigo",
					APIVersion: lumigoApiVersion,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      "lumigo",
					Labels:    map[string]string{},
				},
				Spec: operatorv1alpha1.LumigoSpec{
					LumigoToken: operatorv1alpha1.Credentials{
						SecretRef: operatorv1alpha1.KubernetesSecretRef{
							Name: "lumigo-token",
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &newLumigo)).To(MatchError("admission webhook \"lumigodefaulter.kb.io\" denied the request: invalid reference to a Lumigo token ('.Spec.LumigoToken.SecretRef.Key' is blank)"))
		})

	})

	Context("with already one Lumigo instance in the namespace", func() {

		It("should prevent a second instance from being created", func() {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespaceName,
					Name:      "lumigo-credentials",
				},
				Data: map[string][]byte{
					"token": []byte("t_1234567890123456789AB"),
				},
			})).Should(Succeed())

			lumigoToken := operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "lumigo-credentials",
					Key:  "token",
				},
			}

			lumigo1 := newLumigo(namespaceName, "lumigo1", lumigoToken, true)
			Expect(k8sClient.Create(ctx, lumigo1)).Should(Succeed())

			By("adding a second Lumigo in the namespace", func() {
				lumigo2 := newLumigo(namespaceName, "lumigo2", lumigoToken, true)
				Expect(k8sClient.Create(ctx, lumigo2)).Should(MatchError(
					Equal(fmt.Sprintf("admission webhook \"lumigodefaulter.kb.io\" denied the request: There is already an instance of operator.lumigo.io/v1alpha1.Lumigo in the '%s' namespace", namespaceName)),
				))
			})
		})

	})

})

func newLumigo(namespace string, name string, lumigoToken operatorv1alpha1.Credentials, injectionEnabled bool) *operatorv1alpha1.Lumigo {
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
		},
	}
}

type beBoolPointer struct {
	expectedValue bool
}

func (m *beBoolPointer) Match(actual interface{}) (success bool, err error) {
	switch a := actual.(type) {
	case *bool:
		v := a
		if v == nil {
			return false, fmt.Errorf("the actual value is nil")
		}
		return *v == m.expectedValue, nil
	default:
		return false, fmt.Errorf("beBoolPointer matcher expects a *bool; got:\n%s", format.Object(actual, 1))
	}
}

func (m *beBoolPointer) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("is not a pointer to the boolean '%t'", m.expectedValue)
}

func (m *beBoolPointer) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("is a pointer to the boolean '%t'", m.expectedValue)
}
