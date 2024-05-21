package watchers

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/reporters"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

type TopWatcher struct {
	clientset     *kubernetes.Clientset
	metricsClient *metricsv.Clientset
	namespace     string
	reporter      *reporters.MetricsReporter
	interval      time.Duration
}

func NewTopWatcher(config *config.Config) (*TopWatcher, error) {
	k8sConfig, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	if err != nil {
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}
	metricsClient, err := metricsv.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}

	return &TopWatcher{
		clientset:     clientset,
		metricsClient: metricsClient,
		reporter:      reporters.NewMetricsReporter(config),
		namespace:     config.NAMESPACE,
		interval:      time.Duration(config.TOP_INTERVAL),
	}, nil
}

func (w *TopWatcher) Watch() {
	for {
		var buffer bytes.Buffer

		podMetricsList, err := w.metricsClient.MetricsV1beta1().PodMetricses(w.namespace).List(context.TODO(), v1.ListOptions{})
		if err != nil {
			fmt.Printf("Error getting pod metrics: %v\n", err)
			return
		}
		version := w.GetK8SVersion()
		cloudProvider := w.GetK8SCloudProvider()

		for _, podMetrics := range podMetricsList.Items {
			for _, container := range podMetrics.Containers {
				cpuUsage := container.Usage.Cpu().MilliValue()
				memUsage := container.Usage.Memory().Value()

				buffer.WriteString(fmt.Sprintf("kube_pod_container_resource_usage_cpu{namespace=\"%s\", pod=\"%s\", container=\"%s\", version=\"%s\", cloudProvider=\"%s\"} %d\n",
					w.namespace, podMetrics.Name, container.Name, version, cloudProvider, cpuUsage))
				buffer.WriteString(fmt.Sprintf("kube_pod_container_resource_usage_memory{namespace=\"%s\", pod=\"%s\", container=\"%s\", version=\"%s\", cloudProvider=\"%s\"} %d\n",
					w.namespace, podMetrics.Name, container.Name, version, cloudProvider, memUsage))
			}
		}

		metrics := buffer.String()
		w.reporter.Report(metrics)

		time.Sleep(w.interval * time.Second)
	}
}

func (w *TopWatcher) GetK8SVersion() string {
	version, err := w.clientset.ServerVersion()
if err != nil {
		return "unknown"
	}

	return version.String()
}

func (w *TopWatcher) GetK8SCloudProvider() string {
	cloudProvider, err := w.clientset.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return "unknown"
	}

	provider := cloudProvider.Items[0].Spec.ProviderID
	if provider == "" {
		return "unknown"
	}

	return provider
}
