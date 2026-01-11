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
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	kubeletSocket      = "/var/lib/kubelet/device-plugins/kubelet.sock"
	devicePluginPath   = "/var/lib/kubelet/device-plugins"
	socketNameTemplate = "cxi-%s.sock"

	// Default values
	DefaultResourceName   = "hpe.com/cxi"
	DefaultSharedCapacity = 100
)

// SharingMode defines how devices are shared
type SharingMode string

const (
	SharingModeShared    SharingMode = "shared"
	SharingModeExclusive SharingMode = "exclusive"
)

// Config holds device plugin configuration
type Config struct {
	ResourceName   string
	SharingMode    SharingMode
	SharedCapacity int32
	HealthPort     int
}

// CXIDevicePlugin implements the Kubernetes device plugin API
type CXIDevicePlugin struct {
	pluginapi.UnimplementedDevicePluginServer

	config       Config
	devices      []*CXIDevice
	devicesMu    sync.RWMutex
	server       *grpc.Server
	healthServer *http.Server
	socketPath   string
	stopCh       chan struct{}
	health       chan *pluginapi.Device
	ready        bool
	readyMu      sync.RWMutex
}

// NewCXIDevicePlugin creates a new device plugin instance
func NewCXIDevicePlugin(config Config) *CXIDevicePlugin {
	if config.ResourceName == "" {
		config.ResourceName = DefaultResourceName
	}
	if config.SharedCapacity == 0 {
		config.SharedCapacity = DefaultSharedCapacity
	}
	if config.SharingMode == "" {
		config.SharingMode = SharingModeShared
	}
	if config.HealthPort == 0 {
		config.HealthPort = 8082
	}

	return &CXIDevicePlugin{
		config:     config,
		socketPath: filepath.Join(devicePluginPath, fmt.Sprintf(socketNameTemplate, "plugin")),
		stopCh:     make(chan struct{}),
		health:     make(chan *pluginapi.Device),
	}
}

// Start starts the device plugin server
func (p *CXIDevicePlugin) Start() error {
	// Discover devices
	devices, err := DiscoverDevices()
	if err != nil {
		return fmt.Errorf("failed to discover devices: %w", err)
	}
	p.devices = devices

	fmt.Printf("Discovered %d CXI devices\n", len(devices))
	for _, d := range devices {
		fmt.Printf("  %s: PCI=%s NUMA=%d\n", d.Name, d.PCIAddress, d.NUMANode)
	}

	// Start health server
	go p.runHealthServer()

	// Remove old socket if exists
	if err := os.Remove(p.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old socket: %w", err)
	}

	// Create listener
	listener, err := net.Listen("unix", p.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Create gRPC server
	p.server = grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(p.server, p)

	// Start serving
	go func() {
		if err := p.server.Serve(listener); err != nil {
			fmt.Printf("gRPC server error: %v\n", err)
		}
	}()

	// Wait for server to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.waitForServer(ctx); err != nil {
		return fmt.Errorf("server failed to start: %w", err)
	}

	// Register with kubelet
	if err := p.register(); err != nil {
		return fmt.Errorf("failed to register with kubelet: %w", err)
	}

	p.setReady(true)
	fmt.Printf("Device plugin started and registered as %s\n", p.config.ResourceName)
	return nil
}

// Stop stops the device plugin
func (p *CXIDevicePlugin) Stop() {
	p.setReady(false)
	close(p.stopCh)
	if p.server != nil {
		p.server.Stop()
	}
	if p.healthServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p.healthServer.Shutdown(ctx)
	}
	os.Remove(p.socketPath)
}

func (p *CXIDevicePlugin) setReady(ready bool) {
	p.readyMu.Lock()
	defer p.readyMu.Unlock()
	p.ready = ready
}

func (p *CXIDevicePlugin) isReady() bool {
	p.readyMu.RLock()
	defer p.readyMu.RUnlock()
	return p.ready
}

func (p *CXIDevicePlugin) runHealthServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if p.isReady() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ready")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "not ready")
		}
	})

	p.healthServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", p.config.HealthPort),
		Handler: mux,
	}

	fmt.Printf("Health server listening on port %d\n", p.config.HealthPort)
	if err := p.healthServer.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Printf("Health server error: %v\n", err)
	}
}

func (p *CXIDevicePlugin) waitForServer(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			conn, err := grpc.NewClient(
				"unix://"+p.socketPath,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err == nil {
				conn.Close()
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (p *CXIDevicePlugin) register() error {
	conn, err := grpc.NewClient(
		"unix://"+kubeletSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to kubelet: %w", err)
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	req := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     filepath.Base(p.socketPath),
		ResourceName: p.config.ResourceName,
		Options: &pluginapi.DevicePluginOptions{
			PreStartRequired:                false,
			GetPreferredAllocationAvailable: true,
		},
	}

	_, err = client.Register(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}

	return nil
}

// GetDevicePluginOptions returns options for the device plugin
func (p *CXIDevicePlugin) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired:                false,
		GetPreferredAllocationAvailable: true,
	}, nil
}

// ListAndWatch returns a stream of device updates
func (p *CXIDevicePlugin) ListAndWatch(empty *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	fmt.Println("ListAndWatch called")

	// Send initial device list
	response := p.buildDeviceList()
	if err := stream.Send(response); err != nil {
		return err
	}

	// Watch for changes
	for {
		select {
		case <-p.stopCh:
			return nil
		case d := <-p.health:
			// Health update for a device
			response := p.buildDeviceList()
			for _, dev := range response.Devices {
				if dev.ID == d.ID {
					dev.Health = d.Health
				}
			}
			if err := stream.Send(response); err != nil {
				return err
			}
		}
	}
}

func (p *CXIDevicePlugin) buildDeviceList() *pluginapi.ListAndWatchResponse {
	p.devicesMu.RLock()
	defer p.devicesMu.RUnlock()

	var devices []*pluginapi.Device

	switch p.config.SharingMode {
	case SharingModeExclusive:
		// One resource per physical device
		for _, cxiDev := range p.devices {
			dev := &pluginapi.Device{
				ID:     cxiDev.Name,
				Health: pluginapi.Healthy,
			}

			// Add NUMA topology hints
			if cxiDev.NUMANode >= 0 {
				dev.Topology = &pluginapi.TopologyInfo{
					Nodes: []*pluginapi.NUMANode{
						{ID: int64(cxiDev.NUMANode)},
					},
				}
			}

			devices = append(devices, dev)
		}

	case SharingModeShared:
		// SharedCapacity virtual devices that share physical devices
		for i := int32(0); i < p.config.SharedCapacity; i++ {
			dev := &pluginapi.Device{
				ID:     fmt.Sprintf("shared-%d", i),
				Health: pluginapi.Healthy,
			}

			// Add topology hints from all physical devices
			// (scheduler will use these to prefer NUMA-local placement)
			if len(p.devices) > 0 {
				var nodes []*pluginapi.NUMANode
				seen := make(map[int]bool)
				for _, cxiDev := range p.devices {
					if cxiDev.NUMANode >= 0 && !seen[cxiDev.NUMANode] {
						nodes = append(nodes, &pluginapi.NUMANode{ID: int64(cxiDev.NUMANode)})
						seen[cxiDev.NUMANode] = true
					}
				}
				if len(nodes) > 0 {
					dev.Topology = &pluginapi.TopologyInfo{Nodes: nodes}
				}
			}

			devices = append(devices, dev)
		}
	}

	return &pluginapi.ListAndWatchResponse{Devices: devices}
}

// GetPreferredAllocation returns preferred device allocation for topology awareness.
// It tries to allocate devices from the same NUMA node when possible.
func (p *CXIDevicePlugin) GetPreferredAllocation(ctx context.Context, req *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	response := &pluginapi.PreferredAllocationResponse{}

	for _, containerReq := range req.ContainerRequests {
		preferredIDs := p.selectPreferredDevices(
			containerReq.AvailableDeviceIDs,
			containerReq.MustIncludeDeviceIDs,
			int(containerReq.AllocationSize),
		)

		response.ContainerResponses = append(response.ContainerResponses,
			&pluginapi.ContainerPreferredAllocationResponse{
				DeviceIDs: preferredIDs,
			})
	}

	return response, nil
}

// selectPreferredDevices selects devices preferring NUMA-local allocation.
// It ensures mustInclude devices are always in the result, then fills remaining
// slots with devices on the same NUMA node when possible.
func (p *CXIDevicePlugin) selectPreferredDevices(available, mustInclude []string, count int) []string {
	if count <= 0 {
		return nil
	}

	// For shared mode, device IDs don't map to physical devices, just take first N
	if p.config.SharingMode == SharingModeShared {
		if len(available) <= count {
			return available
		}
		return available[:count]
	}

	// Build device ID to NUMA node map
	numaMap := p.buildNUMAMap()

	// Start with must-include devices
	result := make([]string, 0, count)
	selected := make(map[string]bool)

	for _, id := range mustInclude {
		if len(result) >= count {
			break
		}
		result = append(result, id)
		selected[id] = true
	}

	if len(result) >= count {
		return result
	}

	// Determine preferred NUMA node from must-include devices
	preferredNUMA := -1
	for _, id := range mustInclude {
		if numa, ok := numaMap[id]; ok && numa >= 0 {
			preferredNUMA = numa
			break
		}
	}

	// If no preferred NUMA from must-include, use the NUMA of the first available device
	if preferredNUMA < 0 {
		for _, id := range available {
			if numa, ok := numaMap[id]; ok && numa >= 0 {
				preferredNUMA = numa
				break
			}
		}
	}

	// Separate available devices into same-NUMA and different-NUMA
	var sameNUMA, differentNUMA []string
	for _, id := range available {
		if selected[id] {
			continue
		}
		numa := numaMap[id]
		if numa == preferredNUMA {
			sameNUMA = append(sameNUMA, id)
		} else {
			differentNUMA = append(differentNUMA, id)
		}
	}

	// Fill remaining slots: prefer same-NUMA devices first
	for _, id := range sameNUMA {
		if len(result) >= count {
			break
		}
		result = append(result, id)
	}

	// If still need more, use different-NUMA devices
	for _, id := range differentNUMA {
		if len(result) >= count {
			break
		}
		result = append(result, id)
	}

	return result
}

// buildNUMAMap creates a map of device ID to NUMA node
func (p *CXIDevicePlugin) buildNUMAMap() map[string]int {
	p.devicesMu.RLock()
	defer p.devicesMu.RUnlock()

	numaMap := make(map[string]int, len(p.devices))
	for _, dev := range p.devices {
		numaMap[dev.Name] = dev.NUMANode
	}
	return numaMap
}

// Allocate handles device allocation requests
func (p *CXIDevicePlugin) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	fmt.Printf("Allocate called with %d container requests\n", len(req.ContainerRequests))

	response := &pluginapi.AllocateResponse{}

	for _, containerReq := range req.ContainerRequests {
		containerResp := &pluginapi.ContainerAllocateResponse{}

		// Determine which physical devices to expose
		var devicesToMount []*CXIDevice

		switch p.config.SharingMode {
		case SharingModeExclusive:
			// Map requested device IDs to physical devices
			for _, id := range containerReq.DevicesIds {
				for _, dev := range p.devices {
					if dev.Name == id {
						devicesToMount = append(devicesToMount, dev)
						break
					}
				}
			}

		case SharingModeShared:
			// In shared mode, all pods get access to all devices
			devicesToMount = p.devices
		}

		// Add device mounts
		for _, dev := range devicesToMount {
			containerResp.Devices = append(containerResp.Devices, &pluginapi.DeviceSpec{
				HostPath:      dev.DevicePath,
				ContainerPath: dev.DevicePath,
				Permissions:   "rw",
			})
		}

		// Set environment variables
		if len(devicesToMount) > 0 {
			containerResp.Envs = map[string]string{
				"CXI_DEVICE_COUNT": fmt.Sprintf("%d", len(devicesToMount)),
			}

			// For exclusive mode, set the specific device
			if p.config.SharingMode == SharingModeExclusive && len(devicesToMount) == 1 {
				containerResp.Envs["CXI_DEVICE"] = devicesToMount[0].Name
			}
		}

		response.ContainerResponses = append(response.ContainerResponses, containerResp)
	}

	return response, nil
}

// PreStartContainer is called before container starts (not used)
func (p *CXIDevicePlugin) PreStartContainer(ctx context.Context, req *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}
