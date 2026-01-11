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

	"github.com/sielicki/slingshot-operator/pkg/metrics"
)

func main() {
	var nodeName string
	var metricsPort int
	var scrapeInterval time.Duration
	var retryHandlerURL string

	flag.StringVar(&nodeName, "node-name", os.Getenv("NODE_NAME"),
		"Name of the node (defaults to NODE_NAME env var)")
	flag.IntVar(&metricsPort, "metrics-port", 9090,
		"Port for Prometheus metrics endpoint")
	flag.DurationVar(&scrapeInterval, "scrape-interval", 15*time.Second,
		"Interval between metric scrapes")
	flag.StringVar(&retryHandlerURL, "retry-handler-url", "http://localhost:8080",
		"URL of the retry handler status endpoint")
	flag.Parse()

	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			fmt.Printf("Error: could not determine node name: %v\n", err)
			os.Exit(1)
		}
		nodeName = hostname
	}

	fmt.Println("CXI Metrics Exporter starting...")
	fmt.Printf("  Node: %s\n", nodeName)
	fmt.Printf("  Metrics port: %d\n", metricsPort)
	fmt.Printf("  Scrape interval: %s\n", scrapeInterval)
	fmt.Printf("  Retry handler URL: %s\n", retryHandlerURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	exporter := metrics.NewExporter(metrics.Config{
		NodeName:        nodeName,
		MetricsPort:     metricsPort,
		ScrapeInterval:  scrapeInterval,
		RetryHandlerURL: retryHandlerURL,
	})

	if err := exporter.Start(ctx); err != nil {
		fmt.Printf("Failed to start exporter: %v\n", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	fmt.Printf("Received signal %s, shutting down...\n", sig)

	exporter.Stop()
	fmt.Println("Metrics exporter stopped")
}
