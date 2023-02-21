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
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorv1alpha1 "github.com/lumigo-io/lumigo-kubernetes-operator/api/v1alpha1"
	"github.com/lumigo-io/lumigo-kubernetes-operator/controllers"
	"github.com/lumigo-io/lumigo-kubernetes-operator/webhooks/injector"
	"github.com/lumigo-io/lumigo-kubernetes-operator/webhooks/validator"
	//+kubebuilder:scaffold:imports
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

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	logger := ctrl.Log.WithName("controllers").WithName("Lumigo")

	lumigoOperatorVersion, isSet := os.LookupEnv("LUMIGO_OPERATOR_VERSION")
	if !isSet {
		lumigoOperatorVersion = "dev"
	}

	lumigoEndpoint, isSet := os.LookupEnv("TELEMETRY_PROXY_OTLP_SERVICE")
	if !isSet {
		setupLog.Error(err, "unable to create controller: environment variable 'TELEMETRY_PROXY_OTLP_SERVICE' is not set", "controller", "Lumigo")
		os.Exit(1)
	}

	telemetryProxyOtlpService := lumigoEndpoint + "/v1/traces" // TODO: Fix it when the distros use the Lumigo endpoint as root

	lumigoInjectorImage, isSet := os.LookupEnv("LUMIGO_INJECTOR_IMAGE")
	if !isSet {
		setupLog.Error(err, "unable to create controller: environment variable 'LUMIGO_INJECTOR_IMAGE' is not set", "controller", "Lumigo")
		os.Exit(1)
	}

	if err = (&controllers.LumigoReconciler{
		Client:                       mgr.GetClient(),
		Scheme:                       mgr.GetScheme(),
		LumigoOperatorVersion:        lumigoOperatorVersion,
		LumigoInjectorImage:          lumigoInjectorImage,
		TelemetryProxyOtlpServiceUrl: telemetryProxyOtlpService,
		Log:                          logger,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Lumigo")
		os.Exit(1)
	}

	if err = (&injector.LumigoInjectorWebhookHandler{
		LumigoOperatorVersion:        lumigoOperatorVersion,
		LumigoInjectorImage:          lumigoInjectorImage,
		TelemetryProxyOtlpServiceUrl: telemetryProxyOtlpService,
		Log:                          logger,
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create injector webhook", "webhook", "lumigo-injector")
		os.Exit(1)
	}

	if err = (&validator.LumigoValidatorWebhookHandler{
		LumigoOperatorVersion: lumigoOperatorVersion,
		Log:                   logger,
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create validator-webhook", "webhook", "lumigo-validator")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
