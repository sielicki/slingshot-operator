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

package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/sielicki/slingshot-operator/pkg/deviceplugin"
)

const (
	namespace = "cxi"
)

var (
	deviceLinkState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "device_link_state",
			Help:      "Link state of CXI device (1=up, 0=down, -1=unknown)",
		},
		[]string{"device", "node", "pci_address"},
	)

	deviceNUMANode = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "device_numa_node",
			Help:      "NUMA node of CXI device",
		},
		[]string{"device", "node", "pci_address"},
	)

	deviceCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "device_count",
			Help:      "Number of CXI devices on the node",
		},
		[]string{"node"},
	)

	retryHandlerRunning = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "retry_handler_running",
			Help:      "Whether retry handler is running for device (1=running, 0=stopped)",
		},
		[]string{"device", "node"},
	)

	retryHandlerRestarts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "retry_handler_restarts_total",
			Help:      "Total number of retry handler restarts",
		},
		[]string{"device", "node"},
	)

	driverLoaded = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "driver_loaded",
			Help:      "Whether CXI driver is loaded (1=loaded, 0=not loaded)",
		},
		[]string{"node", "module"},
	)

	scrapeErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scrape_errors_total",
			Help:      "Total number of scrape errors",
		},
		[]string{"node", "type"},
	)

	lastScrapeTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scrape_timestamp_seconds",
			Help:      "Unix timestamp of last successful scrape",
		},
		[]string{"node"},
	)
)

func init() {
	prometheus.MustRegister(
		deviceLinkState,
		deviceNUMANode,
		deviceCount,
		retryHandlerRunning,
		retryHandlerRestarts,
		driverLoaded,
		scrapeErrors,
		lastScrapeTime,
	)
}

type RetryHandlerStatus struct {
	Device       string
	Running      bool
	RestartCount int
}

type RetryHandlerClient interface {
	GetStatus() ([]RetryHandlerStatus, error)
}

type Config struct {
	NodeName           string
	MetricsPort        int
	ScrapeInterval     time.Duration
	RetryHandlerURL    string
}

type Exporter struct {
	config   Config
	log      logr.Logger
	stopCh   chan struct{}
	wg       sync.WaitGroup
	rhClient RetryHandlerClient

	mu                   sync.RWMutex
	lastRestartCounts    map[string]int
}

func NewExporter(config Config) *Exporter {
	if config.MetricsPort == 0 {
		config.MetricsPort = 9090
	}
	if config.ScrapeInterval == 0 {
		config.ScrapeInterval = 15 * time.Second
	}

	return &Exporter{
		config:            config,
		log:               log.Log.WithName("metrics-exporter"),
		stopCh:            make(chan struct{}),
		lastRestartCounts: make(map[string]int),
	}
}

func (e *Exporter) SetRetryHandlerClient(client RetryHandlerClient) {
	e.rhClient = client
}

func (e *Exporter) Start(ctx context.Context) error {
	e.log.Info("Starting metrics exporter", "node", e.config.NodeName, "port", e.config.MetricsPort)

	e.wg.Add(1)
	go e.runScraper(ctx)

	e.wg.Add(1)
	go e.runMetricsServer()

	return nil
}

func (e *Exporter) Stop() {
	close(e.stopCh)
	e.wg.Wait()
}

func (e *Exporter) runScraper(ctx context.Context) {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.ScrapeInterval)
	defer ticker.Stop()

	e.scrape(ctx)

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.scrape(ctx)
		}
	}
}

func (e *Exporter) scrape(ctx context.Context) {
	e.scrapeDevices()
	e.scrapeDriverStatus()
	e.scrapeRetryHandlerStatus()
	lastScrapeTime.WithLabelValues(e.config.NodeName).SetToCurrentTime()
}

func (e *Exporter) scrapeDevices() {
	devices, err := deviceplugin.DiscoverDevices()
	if err != nil {
		e.log.Error(err, "Failed to discover devices")
		scrapeErrors.WithLabelValues(e.config.NodeName, "device_discovery").Inc()
		return
	}

	deviceCount.WithLabelValues(e.config.NodeName).Set(float64(len(devices)))

	for _, dev := range devices {
		linkStateValue := float64(-1)
		switch dev.LinkState {
		case "1", "up": // Driver returns "1" for link up
			linkStateValue = 1
		case "0", "down": // Driver returns "0" for link down
			linkStateValue = 0
		}

		deviceLinkState.WithLabelValues(dev.Name, e.config.NodeName, dev.PCIAddress).Set(linkStateValue)
		deviceNUMANode.WithLabelValues(dev.Name, e.config.NodeName, dev.PCIAddress).Set(float64(dev.NUMANode))
	}
}

func (e *Exporter) scrapeDriverStatus() {
	// cxi_ss1 is the main driver module (cxi_core.c is compiled into it)
	// cxi_user provides userspace interface
	// cxi_eth provides ethernet support
	// sbl (slingshot-base-link) and sl (slingshot-link) are dependencies
	modules := []string{"cxi_ss1", "cxi_user", "cxi_eth", "sbl", "sl"}

	for _, module := range modules {
		loaded := float64(0)
		if isModuleLoaded(module) {
			loaded = 1
		}
		driverLoaded.WithLabelValues(e.config.NodeName, module).Set(loaded)
	}
}

func (e *Exporter) scrapeRetryHandlerStatus() {
	if e.rhClient == nil {
		if e.config.RetryHandlerURL != "" {
			e.rhClient = &httpRetryHandlerClient{url: e.config.RetryHandlerURL}
		} else {
			return
		}
	}

	statuses, err := e.rhClient.GetStatus()
	if err != nil {
		e.log.V(1).Info("Failed to get retry handler status", "error", err)
		scrapeErrors.WithLabelValues(e.config.NodeName, "retry_handler").Inc()
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, status := range statuses {
		running := float64(0)
		if status.Running {
			running = 1
		}
		retryHandlerRunning.WithLabelValues(status.Device, e.config.NodeName).Set(running)

		lastCount, exists := e.lastRestartCounts[status.Device]
		if exists && status.RestartCount > lastCount {
			retryHandlerRestarts.WithLabelValues(status.Device, e.config.NodeName).Add(float64(status.RestartCount - lastCount))
		}
		e.lastRestartCounts[status.Device] = status.RestartCount
	}
}

func (e *Exporter) runMetricsServer() {
	defer e.wg.Done()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ready")
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", e.config.MetricsPort),
		Handler: mux,
	}

	go func() {
		<-e.stopCh
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	e.log.Info("Metrics server listening", "port", e.config.MetricsPort)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		e.log.Error(err, "Metrics server error")
	}
}
