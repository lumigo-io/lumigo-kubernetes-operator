package main

import (
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/watchers"
)

func main() {
	config := config.LoadConfig()
	KubeWatcher, _ := watchers.NewKubeWatcher(config)
	TelemetryWatcher := watchers.NewTelemetryWatcher(config)
	TopWatch, _ := watchers.NewTopWatcher(config)
	go KubeWatcher.Watch()
	go TelemetryWatcher.Watch()
	go TopWatch.Watch()

	select {} // Block forever

}
