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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sielicki/slingshot-operator/pkg/deviceplugin"
)

func main() {
	var resourceName string
	var sharingMode string
	var sharedCapacity int

	flag.StringVar(&resourceName, "resource-name", deviceplugin.DefaultResourceName,
		"The resource name to register with kubelet")
	flag.StringVar(&sharingMode, "sharing-mode", "shared",
		"Device sharing mode: 'shared' or 'exclusive'")
	flag.IntVar(&sharedCapacity, "shared-capacity", deviceplugin.DefaultSharedCapacity,
		"Maximum number of pods that can share devices (only used in shared mode)")
	flag.Parse()

	fmt.Println("CXI Device Plugin starting...")
	fmt.Printf("  Resource name: %s\n", resourceName)
	fmt.Printf("  Sharing mode: %s\n", sharingMode)
	if sharingMode == "shared" {
		fmt.Printf("  Shared capacity: %d\n", sharedCapacity)
	}

	config := deviceplugin.Config{
		ResourceName:   resourceName,
		SharingMode:    deviceplugin.SharingMode(sharingMode),
		SharedCapacity: int32(sharedCapacity),
	}

	plugin := deviceplugin.NewCXIDevicePlugin(config)

	if err := plugin.Start(); err != nil {
		fmt.Printf("Failed to start device plugin: %v\n", err)
		os.Exit(1)
	}

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	fmt.Printf("Received signal %s, shutting down...\n", sig)

	plugin.Stop()
	fmt.Println("Device plugin stopped")
}
