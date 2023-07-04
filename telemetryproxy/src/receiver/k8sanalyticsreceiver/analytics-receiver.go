package k8sanalyticsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sanalyticsreceiver"

import (
	"context"
	"fmt"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/obsreport"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"os"
	"time"
)

var (
	ticker   *time.Ticker
	shutdown = false
)

type k8sanalyticsReceiver struct {
	kube     dynamic.Interface
	config   *Config
	consumer consumer.Logs
	obsrecv  *obsreport.Receiver
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Start(ctx context.Context, host component.Host) error {
	fmt.Println("k8sanalyticsReceiver start function " + k8sanalyticsRcvr.config.Namespace)
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
	customResourceList, err := k8sanalyticsRcvr.kube.Resource(gvr).Namespace(k8sanalyticsRcvr.config.Namespace).List(context.Background(), v1.ListOptions{})
	if err != nil {
		fmt.Printf("Failed to retrieve custom resource list: %v", err)
		os.Exit(1)
	}

	for _, n := range customResourceList.Items {
		fmt.Println("k8sanalyticsReceiver found resource: " + n.GetName())
	}

	//outputLogs := plog.NewLogs()
	//resourceLogs := outputLogs.ResourceLogs()
	//rl := resourceLogs.AppendEmpty()
	//resourceAttrs := rl.Resource().Attributes()
	//resourceAttrs.PutStr("k8s.resource.some_new_attr", "my_attr_value")
	//sl := rl.ScopeLogs().AppendEmpty()
	//logSlice := sl.LogRecords()
	//record := logSlice.AppendEmpty()
	//record.SetObservedTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	//record.Attributes().PutStr("k8s.resource.record_new_attr", "record_attr_value")
	//dest := record.Body()
	//dest.SetStr("body as string???")

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	resourceAttrs := rl.Resource().Attributes()
	resourceAttrs.PutStr("some_attre_1", "some_attre_1_val")
	lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	lr.Body().SetStr("body of message!")
	attrs := lr.Attributes()
	attrs.PutStr("lr_some_atribute", "lr_some_atribute_val")

	obsCtx := k8sanalyticsRcvr.obsrecv.StartLogsOp(ctx)
	err = k8sanalyticsRcvr.consumer.ConsumeLogs(obsCtx, ld)
	k8sanalyticsRcvr.obsrecv.EndLogsOp(obsCtx, "k8sanalytics", 1, err)

	if err != nil {
		fmt.Printf("Failed to retrieve custom resource list: %v", err)
		os.Exit(1)
	}
	runScheduler(k8sanalyticsRcvr.config.Namespace)
	return nil
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Shutdown(ctx context.Context) error {
	fmt.Println("k8sanalyticsReceiver shutdown function")
	shutdown = true
	if ticker != nil {
		fmt.Println("k8sanalyticsReceiver shutdown function - stopping ticker")
		ticker.Stop()

	}
	return nil
}

func runScheduler(namespace string) {
	duration := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))
	ticker = time.NewTicker(duration)

	go func() {
		<-ticker.C
		ticker.Stop()
		if shutdown {
			fmt.Println("scheduler running shutdown true in the first check "+namespace, time.Now())
			return
		}
		ticker = time.NewTicker(time.Minute)

		for {
			if shutdown {
				fmt.Println("scheduler running shutdown true "+namespace, time.Now())
				return
			}
			fmt.Println("scheduler running!!! "+namespace, time.Now())
			<-ticker.C
		}
	}()
}
