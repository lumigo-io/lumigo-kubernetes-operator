package k8sanalyticsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sanalyticsreceiver"

import (
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/obsreport"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"time"
)

type k8sanalyticsReceiver struct {
	kube     dynamic.Interface
	config   *Config
	consumer consumer.Logs
	obsrecv  *obsreport.Receiver
	ticker   *time.Ticker
	ctx      context.Context
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Start(ctx context.Context, host component.Host) error {
	fmt.Println("k8sanalyticsReceiver start function " + k8sanalyticsRcvr.config.Namespace)
	k8sanalyticsRcvr.ctx = ctx
	k8sanalyticsRcvr.SendUsage()
	k8sanalyticsRcvr.RunScheduler()
	return nil
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) Shutdown(ctx context.Context) error {
	fmt.Println("k8sanalyticsReceiver shutdown function")
	if k8sanalyticsRcvr.ticker != nil {
		fmt.Println("k8sanalyticsReceiver shutdown function - stopping ticker")
		k8sanalyticsRcvr.ticker.Stop()
	}
	return nil
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) RunScheduler() {
	duration := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))
	k8sanalyticsRcvr.ticker = time.NewTicker(duration)

	go func() {
		<-k8sanalyticsRcvr.ticker.C
		k8sanalyticsRcvr.ticker.Stop()
		k8sanalyticsRcvr.ticker.Reset(time.Minute)
		for {
			fmt.Println("scheduler running "+k8sanalyticsRcvr.config.Namespace, time.Now())
			k8sanalyticsRcvr.SendUsage()
			<-k8sanalyticsRcvr.ticker.C
		}
	}()
}

func (k8sanalyticsRcvr *k8sanalyticsReceiver) SendUsage() {
	gvr := schema.GroupVersionResource{
		Group:    "operator.lumigo.io",
		Version:  "v1alpha1",
		Resource: "lumigoes",
	}
	customResourceList, err := k8sanalyticsRcvr.kube.Resource(gvr).Namespace(k8sanalyticsRcvr.config.Namespace).List(context.Background(), v1.ListOptions{})
	if err != nil {
		fmt.Printf("Failed to retrieve custom resource list: %v", err)
		return
	}

	for _, n := range customResourceList.Items {
		fmt.Println("k8sanalyticsReceiver found lumigo resource in namespace: " + k8sanalyticsRcvr.config.Namespace)
		lumigoResource, err := k8sanalyticsRcvr.kube.Resource(gvr).Namespace(k8sanalyticsRcvr.config.Namespace).Get(context.Background(), n.GetName(), v1.GetOptions{})
		if err != nil {
			fmt.Printf("Failed to retrieve custom resource list: %v", err)
			return
		}
		jsonStr, err := json.Marshal(lumigoResource.UnstructuredContent())
		if err != nil {
			fmt.Printf("Error creating json from resource description: %s", err.Error())
			return
		}
		lumigoStatusBody := string(jsonStr)
		fmt.Println("k8sanalyticsReceiver sending lumigo status: " + lumigoStatusBody)

		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		sl := rl.ScopeLogs().AppendEmpty()
		lr := sl.LogRecords().AppendEmpty()
		lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
		lr.Body().SetStr(lumigoStatusBody)

		obsCtx := k8sanalyticsRcvr.obsrecv.StartLogsOp(k8sanalyticsRcvr.ctx)
		err = k8sanalyticsRcvr.consumer.ConsumeLogs(obsCtx, ld)
		k8sanalyticsRcvr.obsrecv.EndLogsOp(obsCtx, "k8sanalytics", 1, err)
	}
}
