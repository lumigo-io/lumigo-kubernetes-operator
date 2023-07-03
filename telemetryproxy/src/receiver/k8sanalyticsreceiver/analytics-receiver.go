package k8sanalyticsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sanalyticsreceiver"

import (
	"context"
	"fmt"
	"go.opentelemetry.io/collector/component"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"os"
)

type k8sanalyticsReceiver struct {
	kube   dynamic.Interface
	config *Config
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Start(ctx context.Context, host component.Host) error {
	fmt.Println("k8sanalyticsReceiver start function")
	//namespaces, err := k8sanalyticsRcvr.kube.CoreV1().Namespaces().List(context.Background(), v1.ListOptions{})
	//if err != nil {
	//	err = fmt.Errorf("error getting pods: %v\n", err)
	//}
	//for _, n := range namespaces.Items {
	//	fmt.Println("k8sanalyticsReceiver found namespace" + n.Name)
	//}
	//pods, err := k8sanalyticsRcvr.kube.CoreV1().Pods("").List(context.Background(), v1.ListOptions{})
	//if err != nil {
	//	err = fmt.Errorf("error getting pods: %v\n", err)
	//}
	//
	//for _, n := range pods.Items {
	//	fmt.Println("k8sanalyticsReceiver found pods: " + n.Name)
	//}

	gvr := schema.GroupVersionResource{
		Group:    "operator.lumigo.io",
		Version:  "v1alpha1",
		Resource: "lumigoes",
	}

	// Retrieve the list of custom resources
	customResourceList, err := k8sanalyticsRcvr.kube.Resource(gvr).Namespace("").List(context.Background(), v1.ListOptions{})
	if err != nil {
		fmt.Printf("Failed to retrieve custom resource list: %v", err)
		os.Exit(1)
	}

	for _, n := range customResourceList.Items {
		fmt.Println("k8sanalyticsReceiver found resource: " + n.GetName())
	}
	return nil
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Shutdown(ctx context.Context) error {
	fmt.Println("k8sanalyticsReceiver shutdown function")
	return nil
}
