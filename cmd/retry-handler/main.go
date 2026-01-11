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

	"github.com/sielicki/slingshot-operator/pkg/retryhandler"
)

func main() {
	var mode string
	var device string
	var cxiRHPath string
	var healthPort int

	flag.StringVar(&mode, "mode", "supervisor",
		"Operating mode: 'supervisor' (manage all devices) or 'single' (single device)")
	flag.StringVar(&device, "device", "",
		"Device to manage (required for single mode, e.g., 'cxi0')")
	flag.StringVar(&cxiRHPath, "cxi-rh-path", "/usr/bin/cxi_rh",
		"Path to cxi_rh binary")
	flag.IntVar(&healthPort, "health-port", 8080,
		"Port for health check endpoint")
	flag.Parse()

	fmt.Println("CXI Retry Handler starting...")
	fmt.Printf("  Mode: %s\n", mode)
	fmt.Printf("  cxi_rh path: %s\n", cxiRHPath)
	fmt.Printf("  Health port: %d\n", healthPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch mode {
	case "supervisor":
		runSupervisorMode(ctx, cxiRHPath, healthPort)
	case "single":
		if device == "" {
			fmt.Println("Error: --device is required for single mode")
			os.Exit(1)
		}
		runSingleMode(ctx, device, cxiRHPath)
	default:
		fmt.Printf("Error: unknown mode %q\n", mode)
		os.Exit(1)
	}
}

func runSupervisorMode(ctx context.Context, cxiRHPath string, healthPort int) {
	supervisor := retryhandler.NewSupervisor(cxiRHPath, healthPort)

	if err := supervisor.Start(ctx); err != nil {
		fmt.Printf("Failed to start supervisor: %v\n", err)
		os.Exit(1)
	}

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	fmt.Printf("Received signal %s, shutting down...\n", sig)

	supervisor.Stop()
	fmt.Println("Supervisor stopped")
}

//nolint:unparam // ctx reserved for cancellation support
func runSingleMode(ctx context.Context, device, cxiRHPath string) {
	fmt.Printf("Running single-device mode for %s\n", device)

	// In single mode, we just exec the cxi_rh directly
	// This is used for sidecar injection where we want minimal overhead

	devicePath := fmt.Sprintf("/dev/%s", device)

	// Check if device exists
	if _, err := os.Stat(devicePath); os.IsNotExist(err) {
		fmt.Printf("Error: device %s not found\n", devicePath)
		os.Exit(1)
	}

	// Check if cxi_rh exists
	if _, err := os.Stat(cxiRHPath); os.IsNotExist(err) {
		fmt.Printf("Error: cxi_rh not found at %s\n", cxiRHPath)
		os.Exit(1)
	}

	fmt.Printf("Executing: %s -d %s\n", cxiRHPath, devicePath)

	// Replace this process with cxi_rh
	err := syscall.Exec(cxiRHPath, []string{cxiRHPath, "-d", devicePath}, os.Environ())
	if err != nil {
		fmt.Printf("Failed to exec cxi_rh: %v\n", err)
		os.Exit(1)
	}
}
