package lumigooperatorheartbeatreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/lumigooperatorheartbeatreceiver"

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	"go.uber.org/zap"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	lumigoGVR = schema.GroupVersionResource{
		Group:    "operator.lumigo.io",
		Version:  "v1alpha1",
		Resource: "lumigoes",
	}
	namespaceGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	}
)

type lumigooperatorheartbeatReceiver struct {
	kube     dynamic.Interface
	config   *Config
	consumer consumer.Logs
	obsrecv  *receiverhelper.ObsReport
	ticker   *time.Ticker
	logger   *zap.Logger
}

func (r *lumigooperatorheartbeatReceiver) Start(ctx context.Context, host component.Host) error {
	r.SendUsage(context.TODO())
	r.RunScheduler()
	return nil
}

func (r *lumigooperatorheartbeatReceiver) Shutdown(ctx context.Context) error {
	if r.ticker != nil {
		r.ticker.Stop()
	}
	return nil
}

func (r *lumigooperatorheartbeatReceiver) RunScheduler() {
	duration := time.Until(time.Now().Truncate(time.Hour).Add(time.Hour))
	r.ticker = time.NewTicker(duration)

	go func() {
		<-r.ticker.C
		r.ticker.Reset(time.Hour)
		for {
			r.logger.Debug("Scheduler running")
			if err := r.SendUsage(context.Background()); err != nil {
				r.logger.Error("Failed to send heartbeat", zap.Error(err))
			}
			<-r.ticker.C
		}
	}()
}

func (r *lumigooperatorheartbeatReceiver) SendUsage(ctx context.Context) error {
	customResourceList, err := r.kube.Resource(lumigoGVR).Namespace(r.config.Namespace).List(ctx, v1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to retrieve Lumigo resources: %w", err)
	}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("lumigo-operator.heartbeat")

	for _, n := range customResourceList.Items {
		lumigoResource, err := r.kube.Resource(lumigoGVR).Namespace(r.config.Namespace).Get(ctx, n.GetName(), v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("cannot retrieve the '%s' Lumigo resource: %w", n.GetName(), err)
		}

		jsonRaw, err := json.Marshal(lumigoResource.UnstructuredContent())
		if err != nil {
			return fmt.Errorf("cannot marshal the '%s' Lumigo resource into JSON: %w", n.GetName(), err)
		}

		lr := sl.LogRecords().AppendEmpty()
		lr.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
		lr.Body().SetStr(string(jsonRaw))
	}

	opCtx := r.obsrecv.StartLogsOp(context.Background())
	err = r.consumer.ConsumeLogs(opCtx, ld)
	r.obsrecv.EndLogsOp(opCtx, "lumigooperatorheartbeat", 1, err)
	r.logger.Debug("Lumigo heartbeat sent")

	return nil
}
