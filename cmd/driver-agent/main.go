/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sielicki/slingshot-operator/pkg/driveragent"
)

func main() {
	var nodeName string
	var namespace string
	var driverSource string
	var dkmsSourceURL string
	var prebuiltURL string
	var healthPort int
	var pollInterval time.Duration

	flag.StringVar(&nodeName, "node-name", os.Getenv("NODE_NAME"),
		"The name of this node (defaults to NODE_NAME env var)")
	flag.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"),
		"The namespace for CXIDevice resources (defaults to NAMESPACE env var)")
	flag.StringVar(&driverSource, "driver-source", "preinstalled",
		"Driver source: 'dkms', 'prebuilt', or 'preinstalled'")
	flag.StringVar(&dkmsSourceURL, "dkms-source-url", "",
		"URL to download DKMS source tarball (required for dkms mode)")
	flag.StringVar(&prebuiltURL, "prebuilt-url", "",
		"Base URL for prebuilt kernel modules (required for prebuilt mode)")
	flag.IntVar(&healthPort, "health-port", 8081,
		"Port for health check endpoint")
	flag.DurationVar(&pollInterval, "poll-interval", 30*time.Second,
		"Interval for device polling")
	flag.Parse()

	if nodeName == "" {
		fmt.Println("Error: --node-name is required (or set NODE_NAME env var)")
		os.Exit(1)
	}

	if namespace == "" {
		namespace = "slingshot-system"
	}

	fmt.Println("CXI Driver Agent starting...")
	fmt.Printf("  Node: %s\n", nodeName)
	fmt.Printf("  Namespace: %s\n", namespace)
	fmt.Printf("  Driver source: %s\n", driverSource)
	fmt.Printf("  Health port: %d\n", healthPort)
	fmt.Printf("  Poll interval: %s\n", pollInterval)

	config := driveragent.Config{
		NodeName:      nodeName,
		Namespace:     namespace,
		DriverSource:  driveragent.DriverSource(driverSource),
		DKMSSourceURL: dkmsSourceURL,
		PrebuiltURL:   prebuiltURL,
		HealthPort:    healthPort,
		PollInterval:  pollInterval,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := driveragent.NewAgent(config)

	if err := agent.Start(ctx); err != nil {
		fmt.Printf("Failed to start driver agent: %v\n", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	fmt.Printf("Received signal %s, shutting down...\n", sig)

	agent.Stop()
	fmt.Println("Driver agent stopped")
}
