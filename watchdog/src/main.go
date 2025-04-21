package main

import (
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/watchers"
	"golang.org/x/net/context"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	config := config.LoadConfig()
	watchdogContext, err := watchers.NewWatchdogK8sContext(ctx, config)

	if err != nil {
		stdlog.Fatalf("Failed to create K8s context: %v", err)
	}

	kubeWatcher, err := watchers.NewKubeWatcher(ctx, config, watchdogContext)
	if err != nil {
		stdlog.Fatalf("Failed to create KubeWatcher: %v", err)
	}

	topWatcher, err := watchers.NewTopWatcher(ctx, config, watchdogContext)
	if err != nil {
		stdlog.Fatalf("Failed to create TopWatcher: %v", err)
	}

	// Create a signal channel to handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go kubeWatcher.Watch(ctx)
	go topWatcher.Watch(ctx)

	// Wait for termination signal
	<-sigChan
	cancel()
	stdlog.Println("Watchdog received termination signal, shutting down")
}
