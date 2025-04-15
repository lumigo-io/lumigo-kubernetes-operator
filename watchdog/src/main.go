package main

import (
	"fmt"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/watchers"
)

func main() {
	config := config.LoadConfig()
	fmt.Printf("************************* %+v\n", config)
	KubeWatcher, _ := watchers.NewKubeWatcher(config)
	TelemetryWatcher := watchers.NewTelemetryWatcher(config)
	TopWatch, _ := watchers.NewTopWatcher(config)
	go KubeWatcher.Watch()
	go TelemetryWatcher.Watch()
	go TopWatch.Watch()

	select {} // Block forever
}
