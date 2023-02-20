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

package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg             *rest.Config
	k8sClient       client.Client
	testEnv         *envtest.Environment
	ctx             context.Context
	cancel          context.CancelFunc
	defaultTimeout  = 20 * time.Second
	defaultInterval = 100 * time.Millisecond
)

var lumigoApiVersion = fmt.Sprintf("%s/%s", operatorv1alpha1.GroupVersion.Group, operatorv1alpha1.GroupVersion.Version)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = operatorv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Start controller
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	if err := (&LumigoReconciler{
		Client:                       mgr.GetClient(),
		Scheme:                       mgr.GetScheme(),
		Log:                          ctrl.Log.WithName("controllers").WithName("Lumigo"),
		LumigoOperatorVersion:        "test",
		LumigoInjectorImage:          "localhost:5000/lumigo-injector:latest",
		TelemetryProxyOtlpServiceUrl: "http://localhost:4318",
	}).SetupWithManager(mgr); err != nil {
		Expect(err).ToNot(HaveOccurred())
	}

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	go func() {
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	// TODO Fix? https://github.com/kubernetes-sigs/controller-runtime/issues/1571
	fmt.Fprint(GinkgoWriter, err)
	// Expect(err).NotTo(HaveOccurred())
})

var _ = Context("Lumigo controller", func() {

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

	Context("with one Lumigo instance", func() {

		It("has an error if the referenced secret does not exist", func() {
			lumigo := newLumigo(namespaceName, "lumigo", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "lumigo-credentials",
					Key:  "token",
				},
			}, true)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			By("the Lumigo instance goes in an erroneous state", func() {
				Eventually(func() bool {
					return hasErrorCondition(lumigo, fmt.Sprintf("the Lumigo token is not valid: cannot retrieve secret '%s/lumigo-credentials'", namespaceName))
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("the Lumigo instance recovers when the secret is created", func() {
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

				Eventually(func() bool {
					return hasActiveCondition(lumigo, corev1.ConditionTrue)
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("Test completed")
		})

		It("has an error if the referenced secret does not have the expected key", func() {
			expectedTokenKey := "token"
			wrongTokenKey := "NOTTOKEN"
			lumigo := newLumigo(namespaceName, "lumigo", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "lumigo-credentials",
					Key:  expectedTokenKey,
				},
			}, true)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			By("the Lumigo instance goes in an erroneous state", func() {
				Eventually(func() bool {
					return hasErrorCondition(lumigo, fmt.Sprintf("the Lumigo token is not valid: cannot retrieve secret '%s/lumigo-credentials'", namespaceName))
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("the Lumigo instance recovers when the secret is created with the wrong key", func() {
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
						wrongTokenKey: []byte("t_1234567890123456789AB"),
					},
				})).Should(Succeed())

				Eventually(func() bool {
					return hasErrorCondition(lumigo, fmt.Sprintf("the Lumigo token is not valid: the secret '%s/%s' does not have the key '%s'", namespaceName, "lumigo-credentials", expectedTokenKey))
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("the Lumigo instance recovers when the secret is updated with the right key", func() {
				updatedSecret := &corev1.Secret{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Secret",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      "lumigo-credentials",
					},
					Data: map[string][]byte{
						expectedTokenKey: []byte("t_1234567890123456789AB"),
					},
				}

				Expect(k8sClient.Update(ctx, updatedSecret)).Should(Succeed())

				Eventually(func() bool {
					return hasActiveCondition(lumigo, corev1.ConditionTrue)
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("Test completed")
		})

		It("has an error if the referenced secret has an invalid token", func() {
			expectedTokenKey := "token"

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
					expectedTokenKey: []byte("abcd"),
				},
			})).Should(Succeed())

			lumigo := newLumigo(namespaceName, "lumigo", operatorv1alpha1.Credentials{
				SecretRef: operatorv1alpha1.KubernetesSecretRef{
					Name: "lumigo-credentials",
					Key:  expectedTokenKey,
				},
			}, true)
			Expect(k8sClient.Create(ctx, lumigo)).Should(Succeed())

			By("the Lumigo instance goes in an erroneous state", func() {
				Eventually(func() bool {
					return hasErrorCondition(lumigo, fmt.Sprintf(
						"the Lumigo token is not valid: the value of the field '%s' of the secret '%s/%s' does not match the expected structure of Lumigo tokens: "+
							"it should be `t_` followed by of 21 alphanumeric characters; see https://docs.lumigo.io/docs/lumigo-tokens "+
							"for instructions on how to retrieve your Lumigo token", expectedTokenKey, namespaceName, "lumigo-credentials"))
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("the Lumigo instance recovers when the secret is updated with the a valid token", func() {
				updatedSecret := &corev1.Secret{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Secret",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceName,
						Name:      "lumigo-credentials",
					},
					Data: map[string][]byte{
						expectedTokenKey: []byte("t_1234567890123456789AB"),
					},
				}

				Expect(k8sClient.Update(ctx, updatedSecret)).Should(Succeed())

				Eventually(func() bool {
					return hasActiveCondition(lumigo, corev1.ConditionTrue)
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("Test completed")
		})

	})

	Context("with two Lumigo instances in the namespace", func() {

		It("should set both instances as not active and with an error", func() {
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

			lumigo2 := newLumigo(namespaceName, "lumigo2", lumigoToken, true)

			By("adding a second Lumigo in the namespace", func() {
				Expect(k8sClient.Create(ctx, lumigo2)).Should(Succeed())

				By("checking the status of the original lumigo resource")
				Eventually(func() bool {
					return hasErrorCondition(lumigo1, "multiple Lumigo instances in this namespace")
				}, defaultTimeout, defaultInterval).Should(BeTrue())

				By("checking the status of the new lumigo resource")
				Eventually(func() bool {
					return hasErrorCondition(lumigo2, "multiple Lumigo instances in this namespace")
				}, defaultTimeout, defaultInterval).Should(BeTrue())
			})

			By("the first Lumigo instance recovers when the second is deleted", func() {
				Expect(k8sClient.Delete(ctx, lumigo2)).Should(Succeed())

				Eventually(func() bool {
					return hasActiveCondition(lumigo1, corev1.ConditionTrue)
				}, 15*time.Second, defaultInterval).Should(BeTrue())
			})

			By("Test completed")
		})

	})

})

func hasErrorCondition(lumigo *operatorv1alpha1.Lumigo, message string) bool {
	updatedLumigo := &operatorv1alpha1.Lumigo{}
	if err := k8sClient.Get(context.Background(), *toNamespacedName(lumigo), updatedLumigo); err != nil {
		fmt.Fprint(GinkgoWriter, err)
		return false
	}

	if errorCondition := findErrorCondition(updatedLumigo); errorCondition != nil {
		return errorCondition.Status == corev1.ConditionTrue && errorCondition.Message == message
	}

	return false
}

func toNamespacedName(lumigo *operatorv1alpha1.Lumigo) *client.ObjectKey {
	return &client.ObjectKey{
		Namespace: lumigo.Namespace,
		Name:      lumigo.Name,
	}
}

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

func hasActiveCondition(lumigo *operatorv1alpha1.Lumigo, conditionStatus corev1.ConditionStatus) bool {
	updatedLumigo := &operatorv1alpha1.Lumigo{}
	if err := k8sClient.Get(context.Background(), *toNamespacedName(lumigo), updatedLumigo); err != nil {
		fmt.Fprint(GinkgoWriter, err)
		return false
	}

	activeCondition := findConditionByType(updatedLumigo, operatorv1alpha1.LumigoConditionTypeActive)

	return activeCondition != nil && activeCondition.Status == conditionStatus
}

func findErrorCondition(lumigo *operatorv1alpha1.Lumigo) *operatorv1alpha1.LumigoCondition {
	return findConditionByType(lumigo, operatorv1alpha1.LumigoConditionTypeError)
}

func findConditionByType(lumigo *operatorv1alpha1.Lumigo, conditionType operatorv1alpha1.LumigoConditionType) *operatorv1alpha1.LumigoCondition {
	for _, condition := range lumigo.Status.Conditions {
		if condition.Type == conditionType {
			return &condition
		}
	}

	return nil
}
