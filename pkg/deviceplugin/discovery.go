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

package deviceplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	sysClassCXI = "/sys/class/cxi"

	// DefaultSwitchIDMask extracts switch ID from NID.
	// Default mask 0xfffc0 uses bits 6-19 for switch ID (14 bits = 16384 switches)
	// and bits 0-5 for node within switch (6 bits = 64 nodes per switch).
	// This can be overridden via configuration to match the DFA algorithm deployed.
	DefaultSwitchIDMask = 0xfffc0

	// InvalidNID indicates NID was not available
	InvalidNID = 0xFFFFFFFF
)

// CXIDevice represents a discovered CXI device
type CXIDevice struct {
	Name       string // e.g., "cxi0"
	DevicePath string // e.g., "/dev/cxi0"
	PCIAddress string // e.g., "0000:41:00.0"
	NUMANode   int    // NUMA node ID
	NID        uint32 // Node ID (20-bit fabric address)
	LinkState  string // "up", "down", "unknown"
}

// SwitchID extracts the switch ID from NID using the provided mask.
// In Slingshot dragonfly topology, nodes on the same switch are in the same group.
func (d *CXIDevice) SwitchID(mask uint32) int {
	if d.NID == InvalidNID {
		return -1
	}
	return int(d.NID & mask)
}

// SwitchIDDefault extracts switch ID using the default mask.
func (d *CXIDevice) SwitchIDDefault() int {
	return d.SwitchID(DefaultSwitchIDMask)
}

// DiscoverDevices finds all CXI devices on the system
func DiscoverDevices() ([]*CXIDevice, error) {
	entries, err := os.ReadDir(sysClassCXI)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No CXI devices
		}
		return nil, fmt.Errorf("failed to read %s: %w", sysClassCXI, err)
	}

	devices := make([]*CXIDevice, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, "cxi") {
			continue
		}

		device, err := discoverDevice(name)
		if err != nil {
			fmt.Printf("Warning: failed to discover device %s: %v\n", name, err)
			continue
		}

		devices = append(devices, device)
	}

	return devices, nil
}

//nolint:unparam // error return for future error handling expansion
func discoverDevice(name string) (*CXIDevice, error) {
	devicePath := filepath.Join("/dev", name)
	sysPath := filepath.Join(sysClassCXI, name)

	device := &CXIDevice{
		Name:       name,
		DevicePath: devicePath,
		NUMANode:   -1,
		NID:        InvalidNID,
		LinkState:  "unknown",
	}

	deviceLink, err := os.Readlink(filepath.Join(sysPath, "device"))
	if err == nil {
		device.PCIAddress = filepath.Base(deviceLink)
	}

	numaPath := filepath.Join(sysPath, "device", "numa_node")
	if data, err := os.ReadFile(numaPath); err == nil {
		if numa, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			device.NUMANode = numa
		}
	}

	// Read NID from sysfs properties
	// The driver exposes NID as hex (e.g., "0x12345") or decimal
	nidPath := filepath.Join(sysPath, "device", "properties", "nid")
	if data, err := os.ReadFile(nidPath); err == nil {
		nidStr := strings.TrimSpace(string(data))
		if nid, err := parseNID(nidStr); err == nil {
			device.NID = nid
		}
	}

	// The driver exposes link state at device/properties/link
	// It returns "1" for link up, "0" for link down
	linkPath := filepath.Join(sysPath, "device", "properties", "link")
	if data, err := os.ReadFile(linkPath); err == nil {
		device.LinkState = strings.TrimSpace(string(data))
	}

	return device, nil
}

// parseNID parses NID from sysfs which may be hex (0x...) or decimal
func parseNID(s string) (uint32, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		nid, err := strconv.ParseUint(s[2:], 16, 32)
		return uint32(nid), err
	}
	nid, err := strconv.ParseUint(s, 10, 32)
	return uint32(nid), err
}

// WatchDevices monitors for device changes and returns updates via channel
func WatchDevices(stopCh <-chan struct{}) (<-chan []*CXIDevice, error) {
	updateCh := make(chan []*CXIDevice)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	go func() {
		defer close(updateCh)
		defer func() { _ = watcher.Close() }()

		devices, err := DiscoverDevices()
		if err != nil {
			fmt.Printf("Error discovering devices: %v\n", err)
			return
		}

		select {
		case updateCh <- devices:
		case <-stopCh:
			return
		}

		if _, err := os.Stat(sysClassCXI); os.IsNotExist(err) {
			fmt.Printf("Warning: %s does not exist, falling back to polling\n", sysClassCXI)
			pollDevices(stopCh, updateCh)
			return
		}

		if err := watcher.Add(sysClassCXI); err != nil {
			fmt.Printf("Warning: failed to watch %s, falling back to polling: %v\n", sysClassCXI, err)
			pollDevices(stopCh, updateCh)
			return
		}

		fmt.Printf("Watching %s for device changes\n", sysClassCXI)

		debounceTimer := time.NewTimer(0)
		if !debounceTimer.Stop() {
			<-debounceTimer.C
		}
		pendingUpdate := false

		for {
			select {
			case <-stopCh:
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&(fsnotify.Create|fsnotify.Remove) != 0 {
					if strings.HasPrefix(filepath.Base(event.Name), "cxi") {
						if !pendingUpdate {
							pendingUpdate = true
							debounceTimer.Reset(500 * time.Millisecond)
						}
					}
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Printf("fsnotify error: %v\n", err)

			case <-debounceTimer.C:
				pendingUpdate = false
				devices, err := DiscoverDevices()
				if err != nil {
					fmt.Printf("Error rediscovering devices: %v\n", err)
					continue
				}
				select {
				case updateCh <- devices:
				case <-stopCh:
					return
				}
			}
		}
	}()

	return updateCh, nil
}

func pollDevices(stopCh <-chan struct{}, updateCh chan<- []*CXIDevice) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var lastDeviceCount int

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			devices, err := DiscoverDevices()
			if err != nil {
				fmt.Printf("Error polling devices: %v\n", err)
				continue
			}

			if len(devices) != lastDeviceCount {
				lastDeviceCount = len(devices)
				select {
				case updateCh <- devices:
				case <-stopCh:
					return
				}
			}
		}
	}
}
