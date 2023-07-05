package lumigooperatorheartbeatreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/lumigooperatorheartbeatreceiver"

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

type lumigooperatorheartbeatReceiver struct {
	kube     dynamic.Interface
	config   *Config
	consumer consumer.Logs
	obsrecv  *obsreport.Receiver
	ticker   *time.Ticker
	ctx      context.Context
}

func (lumigooperatorheartbeatRcvr *lumigooperatorheartbeatReceiver) Start(ctx context.Context, host component.Host) error {
	fmt.Println("lumigooperatorheartbeatReceiver start function " + lumigooperatorheartbeatRcvr.config.Namespace)
	lumigooperatorheartbeatRcvr.ctx = ctx
	lumigooperatorheartbeatRcvr.SendUsage()
	lumigooperatorheartbeatRcvr.RunScheduler()
	return nil
}

func (lumigooperatorheartbeatRcvr *lumigooperatorheartbeatReceiver) Shutdown(ctx context.Context) error {
	fmt.Println("lumigooperatorheartbeatReceiver shutdown function")
	if lumigooperatorheartbeatRcvr.ticker != nil {
		fmt.Println("lumigooperatorheartbeatReceiver shutdown function - stopping ticker")
		lumigooperatorheartbeatRcvr.ticker.Stop()
	}
	return nil
}

func (lumigooperatorheartbeatRcvr *lumigooperatorheartbeatReceiver) RunScheduler() {
	duration := time.Until(time.Now().Truncate(time.Hour).Add(time.Hour))
	lumigooperatorheartbeatRcvr.ticker = time.NewTicker(duration)

	go func() {
		<-lumigooperatorheartbeatRcvr.ticker.C
		lumigooperatorheartbeatRcvr.ticker.Stop()
		lumigooperatorheartbeatRcvr.ticker.Reset(time.Hour)
		for {
			fmt.Println("scheduler running "+lumigooperatorheartbeatRcvr.config.Namespace, time.Now())
			lumigooperatorheartbeatRcvr.SendUsage()
			<-lumigooperatorheartbeatRcvr.ticker.C
		}
	}()
}

func (lumigooperatorheartbeatRcvr *lumigooperatorheartbeatReceiver) SendUsage() {
	gvr := schema.GroupVersionResource{
		Group:    "operator.lumigo.io",
		Version:  "v1alpha1",
		Resource: "lumigoes",
	}
	customResourceList, err := lumigooperatorheartbeatRcvr.kube.Resource(gvr).Namespace(lumigooperatorheartbeatRcvr.config.Namespace).List(context.Background(), v1.ListOptions{})
	if err != nil {
		fmt.Printf("Failed to retrieve custom resource list: %v", err)
		return
	}

	for _, n := range customResourceList.Items {
		fmt.Println("lumigooperatorheartbeatReceiver found lumigo resource in namespace: " + lumigooperatorheartbeatRcvr.config.Namespace)
		lumigoResource, err := lumigooperatorheartbeatRcvr.kube.Resource(gvr).Namespace(lumigooperatorheartbeatRcvr.config.Namespace).Get(context.Background(), n.GetName(), v1.GetOptions{})
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
		fmt.Println("lumigooperatorheartbeatReceiver sending lumigo status: " + lumigoStatusBody)

		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		sl := rl.ScopeLogs().AppendEmpty()
		lr := sl.LogRecords().AppendEmpty()
		lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
		lr.Body().SetStr(lumigoStatusBody)
		resourceAttrs := rl.Resource().Attributes()
		resourceAttrs.PutStr("cluster_id", lumigooperatorheartbeatRcvr.getClusterUid())

		obsCtx := lumigooperatorheartbeatRcvr.obsrecv.StartLogsOp(lumigooperatorheartbeatRcvr.ctx)
		err = lumigooperatorheartbeatRcvr.consumer.ConsumeLogs(obsCtx, ld)
		lumigooperatorheartbeatRcvr.obsrecv.EndLogsOp(obsCtx, "lumigooperatorheartbeat", 1, err)
	}
}

func (lumigooperatorheartbeatRcvr *lumigooperatorheartbeatReceiver) getClusterUid() string {
	gvr := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	}
	namespaceObj, err := lumigooperatorheartbeatRcvr.kube.Resource(gvr).Namespace("").Get(context.Background(), "kube-system", v1.GetOptions{})
	if err != nil {
		fmt.Printf("Failed to load Namespace: %v", err)
		return ""
	}

	return string(namespaceObj.GetUID())
}
