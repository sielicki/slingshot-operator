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

package driveragent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slingshotv1 "github.com/sielicki/slingshot-operator/api/v1"
	"github.com/sielicki/slingshot-operator/pkg/deviceplugin"
)

type DriverSource string

const (
	DriverSourceDKMS         DriverSource = "dkms"
	DriverSourcePrebuilt     DriverSource = "prebuilt"
	DriverSourcePreinstalled DriverSource = "preinstalled"
)

// Node label keys for topology awareness
const (
	// LabelCXIPresent indicates CXI devices are present on the node
	LabelCXIPresent = "slingshot.hpe.com/cxi-present"
	// LabelDeviceCount is the number of CXI devices on the node
	LabelDeviceCount = "slingshot.hpe.com/device-count"
	// LabelSwitchIDPrefix is the prefix for per-device switch ID labels
	// Full label: slingshot.hpe.com/<device>-switch-id (e.g., slingshot.hpe.com/cxi0-switch-id)
	LabelSwitchIDPrefix = "slingshot.hpe.com/"
	// LabelSwitchIDSuffix is the suffix for per-device switch ID labels
	LabelSwitchIDSuffix = "-switch-id"
)

type Config struct {
	NodeName        string
	Namespace       string
	DriverSource    DriverSource
	DKMSSourceURL   string // URL to driver source (GitHub zip or tarball) - legacy single-repo mode
	DKMSPkgName     string // DKMS package name (default: "cxi-driver")
	DKMSPkgVersion  string // DKMS package version (auto-detected from URL if empty)
	DKMSTag         string // HPE Slingshot release tag (e.g., "release/shs-12.0.2") - multi-repo mode
	PrebuiltURL     string
	HealthPort      int
	PollInterval    time.Duration
	SwitchIDMask    uint32 // Mask for extracting switch ID from NID (default: 0xfffc0)
	DisableLabeling bool   // Disable node labeling for topology awareness (default: false, labeling enabled)
}

// HPE Slingshot repository URL templates (derived from tag)
const (
	cxiDriverRepoURL = "https://github.com/HewlettPackard/shs-cxi-driver/archive/refs/tags/%s.tar.gz"
	sblRepoURL       = "https://github.com/HewlettPackard/ss-sbl/archive/refs/tags/%s.tar.gz"
	slRepoURL        = "https://github.com/HewlettPackard/ss-link/archive/refs/tags/%s.tar.gz"
)

type Agent struct {
	config      Config
	k8sClient   client.Client
	log         logr.Logger
	stopCh      chan struct{}
	wg          sync.WaitGroup
	mu          sync.RWMutex
	driverReady bool
	lastError   string
	devices     []*deviceplugin.CXIDevice
}

func NewAgent(config Config) *Agent {
	if config.HealthPort == 0 {
		config.HealthPort = 8081
	}
	if config.PollInterval == 0 {
		config.PollInterval = 30 * time.Second
	}
	if config.SwitchIDMask == 0 {
		config.SwitchIDMask = deviceplugin.DefaultSwitchIDMask
	}

	return &Agent{
		config: config,
		log:    log.Log.WithName("driver-agent"),
		stopCh: make(chan struct{}),
	}
}

func (a *Agent) Start(ctx context.Context) error {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(slingshotv1.AddToScheme(scheme))

	a.k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	a.log.Info("Driver agent starting", "node", a.config.NodeName, "driverSource", a.config.DriverSource)

	if err := a.ensureDriver(ctx); err != nil {
		a.setError(err)
		a.log.Error(err, "Driver setup failed")
	}

	a.wg.Add(1)
	go a.runHealthServer()

	a.wg.Add(1)
	go a.runDeviceWatcher(ctx)

	return nil
}

func (a *Agent) Stop() {
	close(a.stopCh)
	a.wg.Wait()
}

func (a *Agent) ensureDriver(ctx context.Context) error {
	switch a.config.DriverSource {
	case DriverSourceDKMS:
		return a.installDKMS(ctx)
	case DriverSourcePrebuilt:
		return a.installPrebuilt(ctx)
	case DriverSourcePreinstalled:
		return a.verifyPreinstalled(ctx)
	default:
		return fmt.Errorf("unknown driver source: %s", a.config.DriverSource)
	}
}

//nolint:unparam // ctx reserved for cancellation support
func (a *Agent) installDKMS(ctx context.Context) error {
	a.log.Info("Installing driver via DKMS")

	// Use multi-repo mode if DKMSTag is set
	if a.config.DKMSTag != "" {
		return a.installDKMSMultiRepo(ctx)
	}

	// Legacy single-repo mode
	return a.installDKMSLegacy(ctx)
}

// installDKMSMultiRepo builds the CXI driver with its dependencies (SBL, SL)
func (a *Agent) installDKMSMultiRepo(ctx context.Context) error {
	tag := a.config.DKMSTag

	a.log.Info("Building HPE Slingshot driver stack", "tag", tag)

	// 1. Download and build SBL (Slingshot Base Link)
	sblURL := fmt.Sprintf(sblRepoURL, tag)
	a.log.Info("Building SBL dependency", "url", sblURL)
	if err := a.buildDependency("sbl", sblURL); err != nil {
		return fmt.Errorf("building SBL: %w", err)
	}

	// 2. Download and build SL (Slingshot 2 Link)
	slURL := fmt.Sprintf(slRepoURL, tag)
	a.log.Info("Building SL dependency", "url", slURL)
	if err := a.buildDependency("sl", slURL); err != nil {
		return fmt.Errorf("building SL: %w", err)
	}

	// 3. Download and build CXI driver with dependency symvers
	cxiURL := fmt.Sprintf(cxiDriverRepoURL, tag)
	a.log.Info("Building CXI driver", "url", cxiURL)
	if err := a.buildCXIDriver(cxiURL); err != nil {
		return fmt.Errorf("building CXI driver: %w", err)
	}

	// Load the modules
	if err := loadModule("cxi_ss1"); err != nil {
		return fmt.Errorf("failed to load cxi_ss1: %w", err)
	}

	if err := loadModule("cxi_user"); err != nil {
		a.log.V(1).Info("cxi_user not loaded (may be auto-loaded by udev)", "error", err)
	}

	a.setDriverReady(true)
	a.log.Info("DKMS multi-repo driver installation complete", "tag", tag)
	return nil
}

// installDKMSLegacy is the original single-URL DKMS installation
//
//nolint:unparam // ctx reserved for cancellation support
func (a *Agent) installDKMSLegacy(ctx context.Context) error {
	if a.config.DKMSSourceURL == "" {
		return fmt.Errorf("DKMS source URL not configured (set DKMSTag or DKMSSourceURL)")
	}

	sourcePath := "/tmp/cxi-driver"

	// Download and extract the source archive (supports .tar.gz, .tgz, .zip)
	if err := downloadAndExtract(a.config.DKMSSourceURL, sourcePath); err != nil {
		return fmt.Errorf("failed to download driver source: %w", err)
	}

	// Determine package name and version
	pkgName := a.config.DKMSPkgName
	if pkgName == "" {
		pkgName = "cxi-driver"
	}

	pkgVersion := a.config.DKMSPkgVersion
	if pkgVersion == "" {
		pkgVersion = extractVersionFromURL(a.config.DKMSSourceURL)
	}

	a.log.Info("Preparing DKMS source", "package", pkgName, "version", pkgVersion)

	// Prepare the source directory (flatten GitHub structure, generate dkms.conf)
	if err := prepareDKMSSource(sourcePath, pkgName, pkgVersion); err != nil {
		return fmt.Errorf("failed to prepare DKMS source: %w", err)
	}

	if err := runDKMSAdd(sourcePath); err != nil {
		return fmt.Errorf("dkms add failed: %w", err)
	}

	if err := runDKMSBuild(pkgName); err != nil {
		return fmt.Errorf("dkms build failed: %w", err)
	}

	if err := runDKMSInstall(pkgName); err != nil {
		return fmt.Errorf("dkms install failed: %w", err)
	}

	// cxi_ss1 is the main driver module (cxi_core.c is compiled into it)
	// Loading cxi_ss1 triggers udev rules that auto-load cxi_user and cxi_eth
	if err := loadModule("cxi_ss1"); err != nil {
		return fmt.Errorf("failed to load cxi_ss1: %w", err)
	}

	// cxi_user provides userspace interface - may be auto-loaded by udev
	if err := loadModule("cxi_user"); err != nil {
		a.log.V(1).Info("cxi_user not loaded (may be auto-loaded by udev)", "error", err)
	}

	a.setDriverReady(true)
	a.log.Info("DKMS driver installation complete", "package", pkgName, "version", pkgVersion)
	return nil
}

//nolint:unparam // ctx reserved for cancellation support
func (a *Agent) installPrebuilt(ctx context.Context) error {
	a.log.Info("Installing prebuilt driver")

	if a.config.PrebuiltURL == "" {
		return fmt.Errorf("prebuilt URL not configured")
	}

	kernelVersion, err := getKernelVersion()
	if err != nil {
		return fmt.Errorf("failed to get kernel version: %w", err)
	}

	moduleURL := fmt.Sprintf("%s/%s/cxi.ko", a.config.PrebuiltURL, kernelVersion)
	modulePath := fmt.Sprintf("/lib/modules/%s/extra/cxi/cxi.ko", kernelVersion)

	if err := downloadFile(moduleURL, modulePath); err != nil {
		return fmt.Errorf("failed to download prebuilt module: %w", err)
	}

	if err := runDepmod(); err != nil {
		return fmt.Errorf("depmod failed: %w", err)
	}

	if err := loadModule("cxi"); err != nil {
		return fmt.Errorf("failed to load cxi module: %w", err)
	}

	a.setDriverReady(true)
	a.log.Info("Prebuilt driver installation complete")
	return nil
}

//nolint:unparam // ctx reserved for cancellation support
func (a *Agent) verifyPreinstalled(ctx context.Context) error {
	a.log.Info("Verifying preinstalled driver")

	// cxi_ss1 is the main driver module (cxi_core.c is compiled into it)
	if !isModuleLoaded("cxi_ss1") {
		if err := loadModule("cxi_ss1"); err != nil {
			return fmt.Errorf("cxi_ss1 module not found or loadable: %w", err)
		}
	}

	a.setDriverReady(true)
	a.log.Info("Preinstalled driver verified")
	return nil
}

func (a *Agent) runDeviceWatcher(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.PollInterval)
	defer ticker.Stop()

	a.updateDevices(ctx)

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.updateDevices(ctx)
		}
	}
}

func (a *Agent) updateDevices(ctx context.Context) {
	devices, err := deviceplugin.DiscoverDevices()
	if err != nil {
		a.setError(err)
		a.log.Error(err, "Failed to discover devices")
		return
	}

	a.mu.Lock()
	a.devices = devices
	a.mu.Unlock()

	for _, dev := range devices {
		if err := a.updateCXIDeviceStatus(ctx, dev); err != nil {
			a.log.Error(err, "Failed to update CXIDevice status", "device", dev.Name)
		}
	}

	if !a.config.DisableLabeling {
		if err := a.updateNodeLabels(ctx, devices); err != nil {
			a.log.Error(err, "Failed to update node labels")
		}
	}
}

func (a *Agent) updateCXIDeviceStatus(ctx context.Context, dev *deviceplugin.CXIDevice) error {
	deviceName := fmt.Sprintf("%s-%s", a.config.NodeName, dev.Name)

	linkState := slingshotv1.LinkStateUnknown
	switch dev.LinkState {
	case "1", "up": // Driver returns "1" for link up
		linkState = slingshotv1.LinkStateUp
	case "0", "down": // Driver returns "0" for link down
		linkState = slingshotv1.LinkStateDown
	}

	var healthStatus slingshotv1.HealthStatus
	switch linkState {
	case slingshotv1.LinkStateDown:
		healthStatus = slingshotv1.HealthStatusUnhealthy
	case slingshotv1.LinkStateUnknown:
		healthStatus = slingshotv1.HealthStatusUnknown
	default:
		healthStatus = slingshotv1.HealthStatusHealthy
	}

	now := metav1.Now()

	existing := &slingshotv1.CXIDevice{}
	err := a.k8sClient.Get(ctx, client.ObjectKey{Name: deviceName}, existing)

	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get CXIDevice: %w", err)
	}

	if apierrors.IsNotFound(err) {
		cxiDevice := &slingshotv1.CXIDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name: deviceName,
				Labels: map[string]string{
					"slingshot.hpe.com/node":   a.config.NodeName,
					"slingshot.hpe.com/device": dev.Name,
				},
			},
		}

		if err := a.k8sClient.Create(ctx, cxiDevice); err != nil {
			return fmt.Errorf("failed to create CXIDevice: %w", err)
		}

		existing = cxiDevice
		a.log.Info("Created CXIDevice", "name", deviceName)
	}

	// Compute switch ID from NID
	switchID := int32(-1)
	if dev.NID != deviceplugin.InvalidNID {
		switchID = int32(dev.SwitchID(a.config.SwitchIDMask))
	}

	existing.Status = slingshotv1.CXIDeviceStatus{
		Node:          a.config.NodeName,
		Device:        dev.Name,
		Type:          slingshotv1.CXIDeviceTypePF,
		PCIAddress:    dev.PCIAddress,
		NUMANode:      int32(dev.NUMANode),
		NID:           dev.NID,
		SwitchID:      switchID,
		LinkState:     linkState,
		DriverVersion: strings.TrimSpace(getDriverVersion()),
		Health: slingshotv1.DeviceHealth{
			Status:    healthStatus,
			LastCheck: &now,
		},
	}

	if err := a.k8sClient.Status().Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update CXIDevice status: %w", err)
	}

	a.log.V(1).Info("Updated CXIDevice",
		"name", deviceName, "pci", dev.PCIAddress, "numa", dev.NUMANode, "link", dev.LinkState)

	return nil
}

// updateNodeLabels sets topology-related labels on the node for scheduler awareness.
// Labels include per-device switch IDs derived from NID, device presence, and device count.
func (a *Agent) updateNodeLabels(ctx context.Context, devices []*deviceplugin.CXIDevice) error {
	node := &corev1.Node{}
	if err := a.k8sClient.Get(ctx, client.ObjectKey{Name: a.config.NodeName}, node); err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	labelsChanged := false

	// Set device presence label
	cxiPresent := "false"
	if len(devices) > 0 {
		cxiPresent = "true"
	}
	if node.Labels[LabelCXIPresent] != cxiPresent {
		node.Labels[LabelCXIPresent] = cxiPresent
		labelsChanged = true
	}

	// Set device count label
	deviceCount := strconv.Itoa(len(devices))
	if node.Labels[LabelDeviceCount] != deviceCount {
		node.Labels[LabelDeviceCount] = deviceCount
		labelsChanged = true
	}

	// Track which device labels we're setting this round
	currentDeviceLabels := make(map[string]bool)

	// Set per-device switch ID labels
	for _, dev := range devices {
		labelKey := LabelSwitchIDPrefix + dev.Name + LabelSwitchIDSuffix
		currentDeviceLabels[labelKey] = true

		if dev.NID != deviceplugin.InvalidNID {
			sid := dev.SwitchID(a.config.SwitchIDMask)
			if sid >= 0 {
				switchIDStr := strconv.Itoa(sid)
				if node.Labels[labelKey] != switchIDStr {
					node.Labels[labelKey] = switchIDStr
					labelsChanged = true
				}
			}
		} else {
			// Remove label if NID is invalid
			if _, exists := node.Labels[labelKey]; exists {
				delete(node.Labels, labelKey)
				labelsChanged = true
			}
		}
	}

	// Remove stale device labels (devices that no longer exist)
	for key := range node.Labels {
		if strings.HasPrefix(key, LabelSwitchIDPrefix) && strings.HasSuffix(key, LabelSwitchIDSuffix) {
			if !currentDeviceLabels[key] {
				delete(node.Labels, key)
				labelsChanged = true
			}
		}
	}

	if !labelsChanged {
		return nil
	}

	if err := a.k8sClient.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node labels: %w", err)
	}

	a.log.Info("Updated node labels",
		"node", a.config.NodeName,
		LabelCXIPresent, cxiPresent,
		LabelDeviceCount, deviceCount,
		"deviceCount", len(devices))

	return nil
}

func (a *Agent) setDriverReady(ready bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.driverReady = ready
	if ready {
		a.lastError = ""
	}
}

func (a *Agent) setError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err == nil {
		a.lastError = ""
		return
	}
	a.lastError = err.Error()
}

func (a *Agent) IsReady() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.driverReady
}

func (a *Agent) GetDevices() []*deviceplugin.CXIDevice {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.devices
}

func (a *Agent) runHealthServer() {
	defer a.wg.Done()

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if a.IsReady() {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "ready")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			a.mu.RLock()
			lastErr := a.lastError
			a.mu.RUnlock()
			_, _ = fmt.Fprintf(w, "not ready: %s\n", lastErr)
		}
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.RLock()
		devices := a.devices
		ready := a.driverReady
		lastErr := a.lastError
		a.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "{\n  \"node\": %q,\n  \"driverReady\": %v,\n", a.config.NodeName, ready)
		if lastErr != "" {
			_, _ = fmt.Fprintf(w, "  \"lastError\": %q,\n", lastErr)
		}
		_, _ = fmt.Fprintf(w, "  \"devices\": [\n")
		for i, dev := range devices {
			_, _ = fmt.Fprintf(w, "    {\"name\": %q, \"pci\": %q, \"numa\": %d, \"link\": %q}",
				dev.Name, dev.PCIAddress, dev.NUMANode, dev.LinkState)
			if i < len(devices)-1 {
				_, _ = fmt.Fprint(w, ",")
			}
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintln(w, "  ]\n}")
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.HealthPort),
		Handler: mux,
	}

	go func() {
		<-a.stopCh
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	a.log.Info("Health server listening", "port", a.config.HealthPort)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		a.log.Error(err, "Health server error")
	}
}

func getDriverVersion() string {
	// cxi_ss1 is the main driver module
	data, err := os.ReadFile("/sys/module/cxi_ss1/version")
	if err != nil {
		return "unknown"
	}
	return string(data)
}
