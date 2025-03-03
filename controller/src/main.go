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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/cache"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers"
	"github.com/lumigo-io/lumigo-kubernetes-operator/webhooks/defaulter"
	"github.com/lumigo-io/lumigo-kubernetes-operator/webhooks/injector"

	//+kubebuilder:scaffold:imports
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

type QuickstartSetting struct {
	Namespace      string `json:"namespace"`
	TracingEnabled *bool  `json:"tracingEnabled,omitempty"`
	LoggingEnabled *bool  `json:"loggingEnabled,omitempty"`
}

func (qs *QuickstartSetting) GetTracesEnabledOrDefault() *bool {
	if qs.TracingEnabled == nil {
		newTrue := true
		return &newTrue
	}
	return qs.TracingEnabled
}

func (qs *QuickstartSetting) GetLogsEnabledOrDefault() *bool {
	if qs.LoggingEnabled == nil {
		newTrue := true
		return &newTrue
	}
	return qs.LoggingEnabled
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var uninstall bool
	var quickstartSettings string
	var lumigoToken string
	var lumigoNamespace string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&uninstall, "uninstall", false,
		"Whether the execution of this manager is actually aimed at initiating the uninstallation procedure.")
	flag.StringVar(&quickstartSettings, "quickstart", "", "Quickstart settings")
	flag.StringVar(&lumigoToken, "lumigo-token", "", "Lumigo token for quickstart setup")
	flag.StringVar(&lumigoNamespace, "lumigo-namespace", "", "Lumigo namespace")

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	if uninstall {
		if err := uninstallHook(); err != nil {
			setupLog.Error(err, "Uninstallation hook failed")
			os.Exit(1)
		}
	}

	logger.Info("Got monitoredNamespace settings, entering quickstart mode", "settings", quickstartSettings)
	if quickstartSettings != "" {
		if lumigoToken == "" {
			logger.Error(fmt.Errorf("quickstart mode request, but Lumigo token was not provided"), "missing token")
			os.Exit(1)
		}
		if err := createQuickstartObjects(quickstartSettings, lumigoToken, lumigoNamespace); err != nil {
			logger.Error(err, "Failed to create quickstart objects")
			os.Exit(1)
		}
		os.Exit(0)
	}

	setupLog.Info("starting manager")
	if err := startManager(metricsAddr, probeAddr, enableLeaderElection); err != nil {
		logger.Error(err, "Manager failed")
		os.Exit(1)
	}
}

func startManager(metricsAddr string, probeAddr string, enableLeaderElection bool) error {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "1447aab8.lumigo.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	logger := ctrl.Log.WithName("controllers").WithName("Lumigo")

	lumigoOperatorVersion, isSet := os.LookupEnv("LUMIGO_OPERATOR_VERSION")
	if !isSet {
		lumigoOperatorVersion = "dev"
	}

	lumigoEndpoint, isSet := os.LookupEnv("TELEMETRY_PROXY_OTLP_SERVICE")
	if !isSet {
		return fmt.Errorf("unable to create controller: environment variable 'TELEMETRY_PROXY_OTLP_SERVICE' is not set")
	}

	telemetryProxyOtlpService := lumigoEndpoint + "/v1/traces" // TODO: Fix it when the distros use the Lumigo endpoint as root
	telemetryProxyOtlpLogsService := lumigoEndpoint + "/v1/logs"

	namespaceConfigurationsPath, isSet := os.LookupEnv("LUMIGO_NAMESPACE_CONFIGURATIONS")
	if !isSet {
		return fmt.Errorf("unable to create controller: environment variable 'LUMIGO_NAMESPACE_CONFIGURATIONS' is not set")
	}

	lumigoInjectorImage, isSet := os.LookupEnv("LUMIGO_INJECTOR_IMAGE")
	if !isSet {
		return fmt.Errorf("unable to create controller: environment variable 'LUMIGO_INJECTOR_IMAGE' is not set")
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("cannot create the clientset client for the controller")
	}

	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("cannot create the dynamic client for the controller")
	}

	if err = (&controllers.LumigoReconciler{
		Client:                           mgr.GetClient(),
		Clientset:                        clientset,
		DynamicClient:                    dynamicClient,
		EventRecorder:                    mgr.GetEventRecorderFor(fmt.Sprintf("lumigo-operator.v%s/controller", lumigoOperatorVersion)),
		Scheme:                           mgr.GetScheme(),
		LumigoOperatorVersion:            lumigoOperatorVersion,
		LumigoInjectorImage:              lumigoInjectorImage,
		TelemetryProxyOtlpServiceUrl:     telemetryProxyOtlpService,
		TelemetryProxyOtlpLogsServiceUrl: telemetryProxyOtlpLogsService,
		TelemetryProxyNamespaceConfigurationsPath: namespaceConfigurationsPath,
		Log: logger,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	if err = (&injector.LumigoInjectorWebhookHandler{
		EventRecorder:                    mgr.GetEventRecorderFor(fmt.Sprintf("lumigo-operator.v%s/injector-webhook", lumigoOperatorVersion)),
		LumigoOperatorVersion:            lumigoOperatorVersion,
		LumigoInjectorImage:              lumigoInjectorImage,
		TelemetryProxyOtlpServiceUrl:     telemetryProxyOtlpService,
		TelemetryProxyOtlpLogsServiceUrl: telemetryProxyOtlpLogsService,
		Log:                              logger,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create injector webhook: %w", err)
	}

	if err = (&defaulter.LumigoDefaulterWebhookHandler{
		LumigoOperatorVersion: lumigoOperatorVersion,
		Log:                   logger,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create defaulter webhook: %w", err)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}

func uninstallHook() error {
	logger := ctrl.Log.WithName("uninstaller").WithName("Lumigo")

	config := ctrl.GetConfigOrDie()
	s := runtime.NewScheme()
	operatorv1alpha1.AddToScheme(s)

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("cannot initialize client: %w", err)
	}

	Client, err := client.NewWithWatch(config, client.Options{
		Scheme: s,
	})

	if err != nil {
		return fmt.Errorf("cannot initialize client: %w", err)
	}

	ctx := context.TODO()
	lumigoes := &operatorv1alpha1.LumigoList{}
	if err := Client.List(ctx, lumigoes); err != nil {
		return fmt.Errorf("an error occurred while listing existing Lumigo resources: %w", err)
	}

	if len(lumigoes.Items) == 0 {
		logger.Info("No Lumigo resources to delete")
		return nil
	}

	logger.Info("Deleting all Lumigo resources", "lumigo-count", len(lumigoes.Items))

	lumigoesLeft := make([]operatorv1alpha1.Lumigo, len(lumigoes.Items))
	copy(lumigoesLeft, lumigoes.Items)

	resource := operatorv1alpha1.GroupVersion.WithResource("lumigoes")
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 0 /* TODO */, "" /* all namespaces */, nil)

	deletionCompletedChannel := make(chan error)
	stopInformerChannel := make(chan struct{})

	informer := factory.ForResource(resource).Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logger.Info(fmt.Sprintf("Unexpected 'add' event for Lumigo resources: %+v", obj))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			logger.Info(fmt.Sprintf("Unexpected 'update' event for Lumigo resources: %+v", newObj))
		},
		DeleteFunc: func(obj interface{}) {
			deletedLumigo := obj.(unstructured.Unstructured)
			for i, l := range lumigoesLeft {
				if l.Namespace == deletedLumigo.GetNamespace() && l.Name == deletedLumigo.GetName() {
					lumigoesLeft = append(lumigoesLeft[:i], lumigoesLeft[i+1:]...)
				}
			}

			if len(lumigoesLeft) == 0 {
				close(stopInformerChannel)
			}
		},
	})

	go func() {
		/*
		 * Informer.Run blocks until stopInformerChannel is closed,
		 * which we will close when there are no more Lumigo resources left.
		 */
		logger.Info("Informer started")
		informer.Run(stopInformerChannel)
		logger.Info("Informer stopped")
		deletionCompletedChannel <- nil
	}()

	for _, lumigo := range lumigoes.Items {
		if err := Client.Delete(ctx, &lumigo); err != nil {
			logger.Error(err, "An error occurred while deleting a Lumigo resource", "namespace", lumigo.Namespace, "name", lumigo.Name, "lumigo", lumigo)
		} else {
			logger.Info("Triggered Lumigo resource deletion", "namespace", lumigo.Namespace, "name", lumigo.Name)
		}
	}

	return <-deletionCompletedChannel
}

func createQuickstartObjects(quickstartSettings string, lumigoToken string, lumigoNamespace string) error {
	var settings []QuickstartSetting

	logger := ctrl.Log.WithName("quickstart")

	config := ctrl.GetConfigOrDie()
	s := runtime.NewScheme()
	operatorv1alpha1.AddToScheme(s)
	corev1.AddToScheme(s)

	ctx := context.Background()

	client, err := runtimeclient.New(config, runtimeclient.Options{Scheme: s})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	if quickstartSettings == "all" {
		settings, err = getAllNamespacesQuickstartSettings(ctx, client, lumigoNamespace)
		if err != nil {
			return fmt.Errorf("failed to get all namespaces: %w", err)
		}
	} else {
		err = json.Unmarshal([]byte(quickstartSettings), &settings)
		if err != nil {
			return err
		}
	}

	quickstartSecretName := "lumigo-credentials"
	quickstartSecretKey := "token"

	for _, setting := range settings {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      quickstartSecretName,
				Namespace: setting.Namespace,
			},
			StringData: map[string]string{
				quickstartSecretKey: lumigoToken,
			},
		}
		if err := client.Create(ctx, secret); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				logger.Info("Lumigo secret already exists, skipping creation during quickstart", "namespace", setting.Namespace, "secret", quickstartSecretName)
			} else {
				logger.Error(err, "Failed to create Lumigo secret during quickstart", "namespace", setting.Namespace, "secret", quickstartSecretName)
				continue
			}
		}

		var createErr error
		lumigo := &operatorv1alpha1.Lumigo{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lumigo",
				Namespace: setting.Namespace,
			},
			Spec: operatorv1alpha1.LumigoSpec{
				LumigoToken: operatorv1alpha1.Credentials{
					SecretRef: operatorv1alpha1.KubernetesSecretRef{
						Name: quickstartSecretName,
						Key:  quickstartSecretKey,
					},
				},
				Tracing: operatorv1alpha1.TracingSpec{
					Enabled: setting.GetTracesEnabledOrDefault(),
				},
				Logging: operatorv1alpha1.LoggingSpec{
					Enabled: setting.GetLogsEnabledOrDefault(),
				},
			},
		}

		for attempts := 0; attempts < 20; attempts++ {
			if err := client.Create(ctx, lumigo); err != nil {
				lumigoAlreadyExistsInNamespace := strings.Contains(err.Error(), "There is already an instance of operator.lumigo.io/v1alpha1.Lumigo")
				// checking that Lumigo CRD already exists in the namespace cannot be done via k8serrors.IsAlreadyExists(err),
				// as the admission webhook rejects it before the IsAlreadyExists gets a chance to be thrown,
				// Therefore, we check for the error message returned from the admission webhook
				if lumigoAlreadyExistsInNamespace {
					logger.Info("Lumigo resource already exists in namespace, updating values", "namespace", setting.Namespace)
					if client.Get(ctx, runtimeclient.ObjectKey{Namespace: setting.Namespace, Name: "lumigo"}, lumigo) != nil {
						logger.Error(err, "could not read existing Lumigo resource", "namespace", setting.Namespace)
						break
					} else {
						newFalse := false
						newTrue := true
						// If both the existing CRD are missing an explicit value, we set it to the default value
						// the operator given to each setting when reading a CRD - which is `true` for tracing and `false` for logging
						updateIfNil(&lumigo.Spec.Tracing.Enabled, setting.TracingEnabled, &newTrue)
						updateIfNil(&lumigo.Spec.Logging.Enabled, setting.LoggingEnabled, &newFalse)
						client.Update(ctx, lumigo)
					}
				} else {
					logger.Error(err, "Failed to create Lumigo CRD during quickstart, controller is probably not ready yet. Retrying...", "namespace", setting.Namespace, "attempt", attempts+1)
					createErr = err
					time.Sleep(5 * time.Second)
					continue
				}
			}
			createErr = nil
			break
		}

		if createErr == nil {
			logger.Info("Lumigo CRD successfully created", "namespace", setting.Namespace)
		} else {
			return fmt.Errorf("failed to create Lumigo CRD in namespace %s after multiple attempts: %w", setting.Namespace, createErr)
		}
	}

	return nil
}

func updateIfNil(target **bool, source *bool, defaultValue *bool) {
	if *target == nil {
		if source == nil {
			*target = defaultValue
		} else {
			*target = source
		}
	} else if source != nil {
		*target = source
	}
}

func getAllNamespacesQuickstartSettings(ctx context.Context, client runtimeclient.Client, lumigoNamespace string) ([]QuickstartSetting, error) {
	ignoredNamespaces := []string{
		lumigoNamespace,
		"kube-system",
		"kube-public",
		"kube-node-lease",
	}
	namespaceList := &corev1.NamespaceList{}
	if err := client.List(ctx, namespaceList); err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	settings := []QuickstartSetting{}
	for _, namespace := range namespaceList.Items {
		if !slices.Contains(ignoredNamespaces, namespace.Name) {
			settings = append(settings, QuickstartSetting{
				Namespace: namespace.Name,
			})
		}
	}

	return settings, nil
}
