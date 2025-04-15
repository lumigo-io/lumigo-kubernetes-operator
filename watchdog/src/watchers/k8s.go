package watchers

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/reporters"

	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type KubeWatcher struct {
	clientset *kubernetes.Clientset
	namespace string
	reporter  *reporters.KubeReporter
	config    *config.Config
}

func NewKubeWatcher(config *config.Config) (*KubeWatcher, error) {
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

	reporter := reporters.NewKubeReporter(config)

	return &KubeWatcher{
		clientset: clientset,
		namespace: config.NAMESPACE,
		reporter:  reporter,
		config:    config,
	}, nil
}

func (w *KubeWatcher) Watch() {
	watcher, err := w.clientset.CoreV1().Events(w.namespace).Watch(context.TODO(), v1.ListOptions{})
	if err != nil {
		log.Printf("Error starting watch: %s\n", err.Error())
		go w.Watch() // Start watch again in a new goroutine in case of error
		return
	}

	ch := watcher.ResultChan()

	log.Printf("Watching for namespace changes in %s...\n", w.namespace)
	for event := range ch {
		if w.config.LUMIGO_TOKEN != "" {
			e := event.Object.(*coreV1.Event)
			w.reporter.AddEvent(*e) // Pass event directly to reporters package
		} else {
			log.Printf("No token found, skipping event collection")
			time.Sleep(5 * time.Second)
		}
	}
}
