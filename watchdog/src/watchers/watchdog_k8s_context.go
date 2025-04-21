package watchers

import (
	"context"
	"path/filepath"

	"github.com/go-logr/logr"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type watchdogContext struct {
	CloudProvider     string
	ClusterUid        string
	NamespaceUID      string
	ClusterK8sVersion string
	Clientset         *kubernetes.Clientset
	K8sConfig         *rest.Config
	OtelResource      *resource.Resource
	Logger            logr.Logger
}

func getK8SVersion(clientset *kubernetes.Clientset) string {
	version, err := clientset.ServerVersion()
	if err != nil {
		return "unknown"
	}

	return version.String()
}

func getK8SCloudProvider(ctx context.Context, clientset *kubernetes.Clientset) string {
	nodeList, err := clientset.CoreV1().Nodes().List(ctx, v1.ListOptions{})
	if err != nil {
		return "unknown"
	}

	provider := nodeList.Items[0].Spec.ProviderID
	if provider == "" {
		return "unknown"
	}

	return provider
}

func getClusterUid(ctx context.Context, clientset *kubernetes.Clientset) (string, error) {
	ns, err := clientset.CoreV1().Namespaces().Get(ctx, "kube-system", v1.GetOptions{})
	if err != nil {
		return "", err
	}

	return string(ns.GetUID()), nil
}

func getK8sConfig() (*rest.Config, error) {
	k8sConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	if err != nil {
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}
	return k8sConfig, nil
}

func getK8sClient(k8sConfig *rest.Config) (*kubernetes.Clientset, error) {
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func getNamespaceUid(ctx context.Context, clientset *kubernetes.Clientset, namespace string) (string, error) {
	ns, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, v1.GetOptions{})
	if err != nil {
		return "", err
	}
	return string(ns.GetUID()), nil
}

func NewWatchdogK8sContext(ctx context.Context, config *config.Config) (*watchdogContext, error) {
	k8sConfig, err := getK8sConfig()

	if err != nil {
		return nil, err
	}
	clientset, err := getK8sClient(k8sConfig)
	if err != nil {
		return nil, err
	}

	clusterUID, err := getClusterUid(ctx, clientset)
	if err != nil {
		return nil, err
	}

	namespaceUID, err := getNamespaceUid(ctx, clientset, config.LUMIGO_OPERATOR_NAMESPACE)
	if err != nil {
		return nil, err
	}

	cloudProvider := getK8SCloudProvider(ctx, clientset)
	if err != nil {
		return nil, err
	}

	clusterK8sVersion := getK8SVersion(clientset)

	logger := zap.New().WithName("watchdog")

	otelResource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("lumigo-kubernetes-operator-watchdog"),
		attribute.String("lumigo.k8s_operator.version", config.LUMIGO_OPERATOR_VERSION),
		attribute.String("k8s.cluster.version", clusterK8sVersion),
		attribute.String("k8s.namespace.name", config.LUMIGO_OPERATOR_NAMESPACE),
		attribute.String("k8s.cluster.uid", clusterUID),
		attribute.String("k8s.cluster.name", config.ClusterName),
		attribute.String("k8s.namespace.uid", namespaceUID),
	)

	return &watchdogContext{
		K8sConfig:         k8sConfig,
		Clientset:         clientset,
		CloudProvider:     cloudProvider,
		ClusterUid:        clusterUID,
		NamespaceUID:      namespaceUID,
		ClusterK8sVersion: clusterK8sVersion,
		OtelResource:      otelResource,
		Logger:            logger,
	}, nil
}
