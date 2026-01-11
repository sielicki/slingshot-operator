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
	"context"
	"testing"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func TestNewCXIDevicePlugin_Defaults(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})

	if plugin.config.ResourceName != DefaultResourceName {
		t.Errorf("ResourceName = %q, want %q", plugin.config.ResourceName, DefaultResourceName)
	}
	if plugin.config.SharedCapacity != DefaultSharedCapacity {
		t.Errorf("SharedCapacity = %d, want %d", plugin.config.SharedCapacity, DefaultSharedCapacity)
	}
	if plugin.config.SharingMode != SharingModeShared {
		t.Errorf("SharingMode = %q, want %q", plugin.config.SharingMode, SharingModeShared)
	}
	if plugin.stopCh == nil {
		t.Error("stopCh is nil")
	}
	if plugin.health == nil {
		t.Error("health channel is nil")
	}
}

func TestNewCXIDevicePlugin_CustomConfig(t *testing.T) {
	config := Config{
		ResourceName:   "custom.io/cxi",
		SharedCapacity: 50,
		SharingMode:    SharingModeExclusive,
	}
	plugin := NewCXIDevicePlugin(config)

	if plugin.config.ResourceName != "custom.io/cxi" {
		t.Errorf("ResourceName = %q, want %q", plugin.config.ResourceName, "custom.io/cxi")
	}
	if plugin.config.SharedCapacity != 50 {
		t.Errorf("SharedCapacity = %d, want %d", plugin.config.SharedCapacity, 50)
	}
	if plugin.config.SharingMode != SharingModeExclusive {
		t.Errorf("SharingMode = %q, want %q", plugin.config.SharingMode, SharingModeExclusive)
	}
}

func TestGetDevicePluginOptions(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})
	ctx := context.Background()

	opts, err := plugin.GetDevicePluginOptions(ctx, &pluginapi.Empty{})
	if err != nil {
		t.Fatalf("GetDevicePluginOptions returned error: %v", err)
	}

	if opts.PreStartRequired {
		t.Error("PreStartRequired = true, want false")
	}
	if !opts.GetPreferredAllocationAvailable {
		t.Error("GetPreferredAllocationAvailable = false, want true")
	}
}

func TestPreStartContainer(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})
	ctx := context.Background()

	resp, err := plugin.PreStartContainer(ctx, &pluginapi.PreStartContainerRequest{})
	if err != nil {
		t.Fatalf("PreStartContainer returned error: %v", err)
	}
	if resp == nil {
		t.Error("response is nil")
	}
}

func TestBuildDeviceList_Exclusive_NoDevices(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = nil

	resp := plugin.buildDeviceList()

	if len(resp.Devices) != 0 {
		t.Errorf("got %d devices, want 0", len(resp.Devices))
	}
}

func TestBuildDeviceList_Exclusive_SingleDevice(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 1},
	}

	resp := plugin.buildDeviceList()

	if len(resp.Devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(resp.Devices))
	}

	dev := resp.Devices[0]
	if dev.ID != "cxi0" {
		t.Errorf("ID = %q, want %q", dev.ID, "cxi0")
	}
	if dev.Health != pluginapi.Healthy {
		t.Errorf("Health = %q, want %q", dev.Health, pluginapi.Healthy)
	}
	if dev.Topology == nil {
		t.Fatal("Topology is nil")
	}
	if len(dev.Topology.Nodes) != 1 {
		t.Fatalf("got %d NUMA nodes, want 1", len(dev.Topology.Nodes))
	}
	if dev.Topology.Nodes[0].ID != 1 {
		t.Errorf("NUMA node ID = %d, want 1", dev.Topology.Nodes[0].ID)
	}
}

func TestBuildDeviceList_Exclusive_MultipleDevices(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
		{Name: "cxi2", NUMANode: 0},
	}

	resp := plugin.buildDeviceList()

	if len(resp.Devices) != 3 {
		t.Fatalf("got %d devices, want 3", len(resp.Devices))
	}

	expectedIDs := []string{"cxi0", "cxi1", "cxi2"}
	for i, dev := range resp.Devices {
		if dev.ID != expectedIDs[i] {
			t.Errorf("device %d ID = %q, want %q", i, dev.ID, expectedIDs[i])
		}
	}
}

func TestBuildDeviceList_Exclusive_InvalidNUMA(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: -1},
	}

	resp := plugin.buildDeviceList()

	if len(resp.Devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(resp.Devices))
	}

	if resp.Devices[0].Topology != nil {
		t.Error("expected nil Topology for invalid NUMA node")
	}
}

func TestBuildDeviceList_Shared(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode:    SharingModeShared,
		SharedCapacity: 10,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
	}

	resp := plugin.buildDeviceList()

	if len(resp.Devices) != 10 {
		t.Fatalf("got %d devices, want 10", len(resp.Devices))
	}

	for i, dev := range resp.Devices {
		// Just verify the prefix since the number formatting might vary
		if len(dev.ID) < 7 || dev.ID[:7] != "shared-" {
			t.Errorf("device %d ID prefix = %q, want %q", i, dev.ID, "shared-*")
		}
		if dev.Health != pluginapi.Healthy {
			t.Errorf("device %d Health = %q, want %q", i, dev.Health, pluginapi.Healthy)
		}
	}
}

func TestBuildDeviceList_Shared_NUMADeduplication(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode:    SharingModeShared,
		SharedCapacity: 5,
	})
	// Multiple devices on same NUMA node
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 0},
		{Name: "cxi2", NUMANode: 1},
	}

	resp := plugin.buildDeviceList()

	// All shared devices should have deduplicated NUMA nodes
	for _, dev := range resp.Devices {
		if dev.Topology == nil {
			t.Fatal("Topology is nil")
		}
		if len(dev.Topology.Nodes) != 2 {
			t.Errorf("got %d NUMA nodes, want 2 (deduplicated)", len(dev.Topology.Nodes))
		}
	}
}

func TestBuildDeviceList_Shared_NoDevices(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode:    SharingModeShared,
		SharedCapacity: 5,
	})
	plugin.devices = nil

	resp := plugin.buildDeviceList()

	if len(resp.Devices) != 5 {
		t.Fatalf("got %d devices, want 5", len(resp.Devices))
	}

	// No topology hints without physical devices
	for i, dev := range resp.Devices {
		if dev.Topology != nil && len(dev.Topology.Nodes) > 0 {
			t.Errorf("device %d has topology hints without physical devices", i)
		}
	}
}

func TestGetPreferredAllocation_SingleContainer(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})
	ctx := context.Background()

	req := &pluginapi.PreferredAllocationRequest{
		ContainerRequests: []*pluginapi.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{"cxi0", "cxi1", "cxi2"},
				AllocationSize:     2,
			},
		},
	}

	resp, err := plugin.GetPreferredAllocation(ctx, req)
	if err != nil {
		t.Fatalf("GetPreferredAllocation returned error: %v", err)
	}

	if len(resp.ContainerResponses) != 1 {
		t.Fatalf("got %d container responses, want 1", len(resp.ContainerResponses))
	}

	if len(resp.ContainerResponses[0].DeviceIDs) != 2 {
		t.Errorf("got %d device IDs, want 2", len(resp.ContainerResponses[0].DeviceIDs))
	}
}

func TestGetPreferredAllocation_MultipleContainers(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})
	ctx := context.Background()

	req := &pluginapi.PreferredAllocationRequest{
		ContainerRequests: []*pluginapi.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{"cxi0", "cxi1"},
				AllocationSize:     1,
			},
			{
				AvailableDeviceIDs: []string{"cxi2", "cxi3"},
				AllocationSize:     2,
			},
		},
	}

	resp, err := plugin.GetPreferredAllocation(ctx, req)
	if err != nil {
		t.Fatalf("GetPreferredAllocation returned error: %v", err)
	}

	if len(resp.ContainerResponses) != 2 {
		t.Fatalf("got %d container responses, want 2", len(resp.ContainerResponses))
	}

	if len(resp.ContainerResponses[0].DeviceIDs) != 1 {
		t.Errorf("first container got %d device IDs, want 1", len(resp.ContainerResponses[0].DeviceIDs))
	}
	if len(resp.ContainerResponses[1].DeviceIDs) != 2 {
		t.Errorf("second container got %d device IDs, want 2", len(resp.ContainerResponses[1].DeviceIDs))
	}
}

func TestGetPreferredAllocation_ExceedsAvailable(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})
	ctx := context.Background()

	req := &pluginapi.PreferredAllocationRequest{
		ContainerRequests: []*pluginapi.ContainerPreferredAllocationRequest{
			{
				AvailableDeviceIDs: []string{"cxi0"},
				AllocationSize:     5, // More than available
			},
		},
	}

	resp, err := plugin.GetPreferredAllocation(ctx, req)
	if err != nil {
		t.Fatalf("GetPreferredAllocation returned error: %v", err)
	}

	// Should return all available devices (1)
	if len(resp.ContainerResponses[0].DeviceIDs) != 1 {
		t.Errorf("got %d device IDs, want 1 (all available)", len(resp.ContainerResponses[0].DeviceIDs))
	}
}

func TestAllocate_Exclusive_SingleDevice(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", DevicePath: "/dev/cxi0", NUMANode: 0},
		{Name: "cxi1", DevicePath: "/dev/cxi1", NUMANode: 1},
	}
	ctx := context.Background()

	req := &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: []string{"cxi0"}},
		},
	}

	resp, err := plugin.Allocate(ctx, req)
	if err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	if len(resp.ContainerResponses) != 1 {
		t.Fatalf("got %d container responses, want 1", len(resp.ContainerResponses))
	}

	cr := resp.ContainerResponses[0]
	if len(cr.Devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(cr.Devices))
	}

	if cr.Devices[0].HostPath != "/dev/cxi0" {
		t.Errorf("HostPath = %q, want %q", cr.Devices[0].HostPath, "/dev/cxi0")
	}
	if cr.Devices[0].ContainerPath != "/dev/cxi0" {
		t.Errorf("ContainerPath = %q, want %q", cr.Devices[0].ContainerPath, "/dev/cxi0")
	}
	if cr.Devices[0].Permissions != "rw" {
		t.Errorf("Permissions = %q, want %q", cr.Devices[0].Permissions, "rw")
	}

	if cr.Envs["CXI_DEVICE_COUNT"] != "1" {
		t.Errorf("CXI_DEVICE_COUNT = %q, want %q", cr.Envs["CXI_DEVICE_COUNT"], "1")
	}
	if cr.Envs["CXI_DEVICE"] != "cxi0" {
		t.Errorf("CXI_DEVICE = %q, want %q", cr.Envs["CXI_DEVICE"], "cxi0")
	}
}

func TestAllocate_Exclusive_MultipleDevices(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", DevicePath: "/dev/cxi0"},
		{Name: "cxi1", DevicePath: "/dev/cxi1"},
	}
	ctx := context.Background()

	req := &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: []string{"cxi0", "cxi1"}},
		},
	}

	resp, err := plugin.Allocate(ctx, req)
	if err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	cr := resp.ContainerResponses[0]
	if len(cr.Devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(cr.Devices))
	}

	if cr.Envs["CXI_DEVICE_COUNT"] != "2" {
		t.Errorf("CXI_DEVICE_COUNT = %q, want %q", cr.Envs["CXI_DEVICE_COUNT"], "2")
	}
	// CXI_DEVICE should not be set for multiple devices
	if _, exists := cr.Envs["CXI_DEVICE"]; exists {
		t.Error("CXI_DEVICE should not be set for multiple devices")
	}
}

func TestAllocate_Exclusive_NonexistentDevice(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", DevicePath: "/dev/cxi0"},
	}
	ctx := context.Background()

	req := &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: []string{"cxi99"}}, // Non-existent
		},
	}

	resp, err := plugin.Allocate(ctx, req)
	if err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	cr := resp.ContainerResponses[0]
	if len(cr.Devices) != 0 {
		t.Errorf("got %d devices, want 0 for non-existent device", len(cr.Devices))
	}
}

func TestAllocate_Shared(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeShared,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", DevicePath: "/dev/cxi0"},
		{Name: "cxi1", DevicePath: "/dev/cxi1"},
	}
	ctx := context.Background()

	req := &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: []string{"shared-0"}},
		},
	}

	resp, err := plugin.Allocate(ctx, req)
	if err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	cr := resp.ContainerResponses[0]
	// In shared mode, all physical devices are mounted
	if len(cr.Devices) != 2 {
		t.Fatalf("got %d devices, want 2 (all physical devices)", len(cr.Devices))
	}

	if cr.Envs["CXI_DEVICE_COUNT"] != "2" {
		t.Errorf("CXI_DEVICE_COUNT = %q, want %q", cr.Envs["CXI_DEVICE_COUNT"], "2")
	}
}

func TestAllocate_MultipleContainers(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", DevicePath: "/dev/cxi0"},
		{Name: "cxi1", DevicePath: "/dev/cxi1"},
	}
	ctx := context.Background()

	req := &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: []string{"cxi0"}},
			{DevicesIds: []string{"cxi1"}},
		},
	}

	resp, err := plugin.Allocate(ctx, req)
	if err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	if len(resp.ContainerResponses) != 2 {
		t.Fatalf("got %d container responses, want 2", len(resp.ContainerResponses))
	}

	if resp.ContainerResponses[0].Devices[0].HostPath != "/dev/cxi0" {
		t.Errorf("first container HostPath = %q, want %q", resp.ContainerResponses[0].Devices[0].HostPath, "/dev/cxi0")
	}
	if resp.ContainerResponses[1].Devices[0].HostPath != "/dev/cxi1" {
		t.Errorf("second container HostPath = %q, want %q", resp.ContainerResponses[1].Devices[0].HostPath, "/dev/cxi1")
	}
}

func TestSharingMode_Constants(t *testing.T) {
	if SharingModeShared != "shared" {
		t.Errorf("SharingModeShared = %q, want %q", SharingModeShared, "shared")
	}
	if SharingModeExclusive != "exclusive" {
		t.Errorf("SharingModeExclusive = %q, want %q", SharingModeExclusive, "exclusive")
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultResourceName != "hpe.com/cxi" {
		t.Errorf("DefaultResourceName = %q, want %q", DefaultResourceName, "hpe.com/cxi")
	}
	if DefaultSharedCapacity != 100 {
		t.Errorf("DefaultSharedCapacity = %d, want %d", DefaultSharedCapacity, 100)
	}
}

func TestSelectPreferredDevices_SharedMode(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeShared,
	})

	available := []string{"shared-0", "shared-1", "shared-2", "shared-3"}
	result := plugin.selectPreferredDevices(available, nil, 2)

	if len(result) != 2 {
		t.Fatalf("got %d devices, want 2", len(result))
	}
	if result[0] != "shared-0" || result[1] != "shared-1" {
		t.Errorf("got %v, want [shared-0 shared-1]", result)
	}
}

func TestSelectPreferredDevices_SharedMode_ExceedsAvailable(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeShared,
	})

	available := []string{"shared-0", "shared-1"}
	result := plugin.selectPreferredDevices(available, nil, 5)

	if len(result) != 2 {
		t.Fatalf("got %d devices, want 2 (all available)", len(result))
	}
}

func TestSelectPreferredDevices_ZeroCount(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
	}

	result := plugin.selectPreferredDevices([]string{"cxi0"}, nil, 0)

	if result != nil {
		t.Errorf("got %v, want nil for zero count", result)
	}
}

func TestSelectPreferredDevices_NegativeCount(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})

	result := plugin.selectPreferredDevices([]string{"cxi0"}, nil, -1)

	if result != nil {
		t.Errorf("got %v, want nil for negative count", result)
	}
}

func TestSelectPreferredDevices_MustIncludeAlwaysPresent(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
		{Name: "cxi2", NUMANode: 0},
	}

	available := []string{"cxi0", "cxi1", "cxi2"}
	mustInclude := []string{"cxi1"}
	result := plugin.selectPreferredDevices(available, mustInclude, 2)

	if len(result) != 2 {
		t.Fatalf("got %d devices, want 2", len(result))
	}

	found := false
	for _, id := range result {
		if id == "cxi1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("must-include device cxi1 not in result: %v", result)
	}
}

func TestSelectPreferredDevices_MustIncludeExceedsCount(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
		{Name: "cxi2", NUMANode: 0},
	}

	available := []string{"cxi0", "cxi1", "cxi2"}
	mustInclude := []string{"cxi0", "cxi1", "cxi2"}
	result := plugin.selectPreferredDevices(available, mustInclude, 2)

	if len(result) != 2 {
		t.Fatalf("got %d devices, want 2 (capped at count)", len(result))
	}
	if result[0] != "cxi0" || result[1] != "cxi1" {
		t.Errorf("got %v, want first 2 must-include devices", result)
	}
}

func TestSelectPreferredDevices_PrefersSameNUMA(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
		{Name: "cxi2", NUMANode: 0},
		{Name: "cxi3", NUMANode: 1},
	}

	available := []string{"cxi0", "cxi1", "cxi2", "cxi3"}
	result := plugin.selectPreferredDevices(available, nil, 2)

	if len(result) != 2 {
		t.Fatalf("got %d devices, want 2", len(result))
	}

	numaMap := plugin.buildNUMAMap()
	numa0 := numaMap[result[0]]
	numa1 := numaMap[result[1]]

	if numa0 != numa1 {
		t.Errorf("selected devices on different NUMA nodes: %s (NUMA %d), %s (NUMA %d)",
			result[0], numa0, result[1], numa1)
	}
}

func TestSelectPreferredDevices_MustIncludeDeterminesNUMA(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
		{Name: "cxi2", NUMANode: 0},
		{Name: "cxi3", NUMANode: 1},
	}

	available := []string{"cxi0", "cxi1", "cxi2", "cxi3"}
	mustInclude := []string{"cxi1"} // NUMA 1
	result := plugin.selectPreferredDevices(available, mustInclude, 2)

	if len(result) != 2 {
		t.Fatalf("got %d devices, want 2", len(result))
	}

	if result[0] != "cxi1" {
		t.Errorf("first device should be must-include cxi1, got %s", result[0])
	}

	numaMap := plugin.buildNUMAMap()
	if numaMap[result[1]] != 1 {
		t.Errorf("second device should be on NUMA 1 (same as must-include), got NUMA %d", numaMap[result[1]])
	}
}

func TestSelectPreferredDevices_FallsBackToDifferentNUMA(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
		{Name: "cxi2", NUMANode: 1},
	}

	available := []string{"cxi0", "cxi1", "cxi2"}
	result := plugin.selectPreferredDevices(available, nil, 3)

	if len(result) != 3 {
		t.Fatalf("got %d devices, want 3", len(result))
	}

	resultSet := make(map[string]bool)
	for _, id := range result {
		resultSet[id] = true
	}

	for _, expected := range []string{"cxi0", "cxi1", "cxi2"} {
		if !resultSet[expected] {
			t.Errorf("missing device %s in result: %v", expected, result)
		}
	}
}

func TestSelectPreferredDevices_EmptyAvailable(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{}

	result := plugin.selectPreferredDevices(nil, nil, 2)

	if len(result) != 0 {
		t.Errorf("got %d devices, want 0 for empty available", len(result))
	}
}

func TestSelectPreferredDevices_UnknownDeviceInAvailable(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		SharingMode: SharingModeExclusive,
	})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
	}

	available := []string{"cxi0", "unknown"}
	result := plugin.selectPreferredDevices(available, nil, 2)

	if len(result) != 2 {
		t.Fatalf("got %d devices, want 2", len(result))
	}
}

func TestBuildNUMAMap(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})
	plugin.devices = []*CXIDevice{
		{Name: "cxi0", NUMANode: 0},
		{Name: "cxi1", NUMANode: 1},
		{Name: "cxi2", NUMANode: -1},
	}

	numaMap := plugin.buildNUMAMap()

	if len(numaMap) != 3 {
		t.Fatalf("got %d entries, want 3", len(numaMap))
	}
	if numaMap["cxi0"] != 0 {
		t.Errorf("cxi0 NUMA = %d, want 0", numaMap["cxi0"])
	}
	if numaMap["cxi1"] != 1 {
		t.Errorf("cxi1 NUMA = %d, want 1", numaMap["cxi1"])
	}
	if numaMap["cxi2"] != -1 {
		t.Errorf("cxi2 NUMA = %d, want -1", numaMap["cxi2"])
	}
}

func TestBuildNUMAMap_Empty(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})
	plugin.devices = nil

	numaMap := plugin.buildNUMAMap()

	if len(numaMap) != 0 {
		t.Errorf("got %d entries, want 0 for no devices", len(numaMap))
	}
}

func TestCXIDevice_SwitchID(t *testing.T) {
	tests := []struct {
		name     string
		nid      uint32
		mask     uint32
		expected int
	}{
		{
			name:     "default mask extracts bits 6-19",
			nid:      0x12345,
			mask:     DefaultSwitchIDMask,
			expected: 0x12345 & DefaultSwitchIDMask,
		},
		{
			name:     "node bits are masked out",
			nid:      0x1234F, // last nibble is node bits
			mask:     DefaultSwitchIDMask,
			expected: 0x12340, // node bits (0xF) masked out
		},
		{
			name:     "custom mask",
			nid:      0xABCDE,
			mask:     0xFF000,
			expected: 0xAB000,
		},
		{
			name:     "zero NID",
			nid:      0,
			mask:     DefaultSwitchIDMask,
			expected: 0,
		},
		{
			name:     "max 20-bit NID",
			nid:      0xFFFFF,
			mask:     DefaultSwitchIDMask,
			expected: 0xFFFC0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := &CXIDevice{NID: tt.nid}
			got := device.SwitchID(tt.mask)
			if got != tt.expected {
				t.Errorf("SwitchID(%#x) = %#x, want %#x", tt.mask, got, tt.expected)
			}
		})
	}
}

func TestCXIDevice_SwitchID_InvalidNID(t *testing.T) {
	device := &CXIDevice{NID: InvalidNID}
	got := device.SwitchID(DefaultSwitchIDMask)
	if got != -1 {
		t.Errorf("SwitchID with InvalidNID = %d, want -1", got)
	}
}

func TestCXIDevice_SwitchIDDefault(t *testing.T) {
	device := &CXIDevice{NID: 0x12345}
	got := device.SwitchIDDefault()
	expected := int(0x12345 & DefaultSwitchIDMask)
	if got != expected {
		t.Errorf("SwitchIDDefault() = %#x, want %#x", got, expected)
	}
}

func TestParseNID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint32
		wantErr  bool
	}{
		{
			name:     "hex with 0x prefix",
			input:    "0x12345",
			expected: 0x12345,
		},
		{
			name:     "hex with 0X prefix",
			input:    "0X12345",
			expected: 0x12345,
		},
		{
			name:     "decimal",
			input:    "74565",
			expected: 74565,
		},
		{
			name:     "zero",
			input:    "0",
			expected: 0,
		},
		{
			name:     "hex zero",
			input:    "0x0",
			expected: 0,
		},
		{
			name:     "with whitespace",
			input:    "  0x12345  ",
			expected: 0x12345,
		},
		{
			name:     "max 20-bit value",
			input:    "0xFFFFF",
			expected: 0xFFFFF,
		},
		{
			name:    "invalid hex",
			input:   "0xGGGGG",
			wantErr: true,
		},
		{
			name:    "invalid decimal",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNID(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseNID(%q) expected error, got %d", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("parseNID(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.expected {
				t.Errorf("parseNID(%q) = %#x, want %#x", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDefaultSwitchIDMask(t *testing.T) {
	// Verify the mask matches the CXI driver default (0xfffc0)
	if DefaultSwitchIDMask != 0xfffc0 {
		t.Errorf("DefaultSwitchIDMask = %#x, want %#x", DefaultSwitchIDMask, 0xfffc0)
	}

	// Verify bit layout: bits 6-19 for switch ID (14 bits)
	// bits 0-5 for node within switch (6 bits = 64 nodes)
	nodeBits := 6
	switchBits := 14

	expectedMask := uint32(((1 << switchBits) - 1) << nodeBits)
	if DefaultSwitchIDMask != expectedMask {
		t.Errorf("DefaultSwitchIDMask bit layout incorrect: got %#x, want %#x", DefaultSwitchIDMask, expectedMask)
	}
}

func TestInvalidNID(t *testing.T) {
	if InvalidNID != 0xFFFFFFFF {
		t.Errorf("InvalidNID = %#x, want %#x", InvalidNID, 0xFFFFFFFF)
	}
}

func TestHealthServer_SetReady(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})

	if plugin.isReady() {
		t.Error("plugin should not be ready initially")
	}

	plugin.setReady(true)
	if !plugin.isReady() {
		t.Error("plugin should be ready after setReady(true)")
	}

	plugin.setReady(false)
	if plugin.isReady() {
		t.Error("plugin should not be ready after setReady(false)")
	}
}

func TestHealthServer_ConcurrentAccess(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			plugin.setReady(true)
			plugin.setReady(false)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = plugin.isReady()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestNewCXIDevicePlugin_HealthPortDefault(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{})

	if plugin.config.HealthPort != 8082 {
		t.Errorf("HealthPort = %d, want 8082", plugin.config.HealthPort)
	}
}

func TestNewCXIDevicePlugin_HealthPortCustom(t *testing.T) {
	plugin := NewCXIDevicePlugin(Config{
		HealthPort: 9999,
	})

	if plugin.config.HealthPort != 9999 {
		t.Errorf("HealthPort = %d, want 9999", plugin.config.HealthPort)
	}
}
