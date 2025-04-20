package main

import (
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/config"
	"github.com/lumigo-io/lumigo-kubernetes-operator/watchdog/watchers"
)

func main() {
	config := config.LoadConfig()
	kubeWatcher, err := watchers.NewKubeWatcher(config)
	if err != nil {
		stdlog.Fatalf("Failed to create KubeWatcher: %v", err)
	}

	topWatcher, err := watchers.NewTopWatcher(config)
	if err != nil {
		stdlog.Fatalf("Failed to create TopWatcher: %v", err)
	}

	// Create a signal channel to handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start watchers
	go kubeWatcher.Watch()
	go topWatcher.Watch()

	// Wait for termination signal
	<-sigChan
	stdlog.Println("Watchdog received termination signal, shutting down")
}
