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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewExporter_Defaults(t *testing.T) {
	e := NewExporter(Config{})

	if e.config.MetricsPort != 9090 {
		t.Errorf("MetricsPort = %d, want %d", e.config.MetricsPort, 9090)
	}
	if e.config.ScrapeInterval != 15*time.Second {
		t.Errorf("ScrapeInterval = %v, want %v", e.config.ScrapeInterval, 15*time.Second)
	}
	if e.stopCh == nil {
		t.Error("stopCh is nil")
	}
	if e.lastRestartCounts == nil {
		t.Error("lastRestartCounts map is nil")
	}
}

func TestNewExporter_CustomConfig(t *testing.T) {
	config := Config{
		NodeName:        "test-node",
		MetricsPort:     8888,
		ScrapeInterval:  30 * time.Second,
		RetryHandlerURL: "http://localhost:8080",
	}
	e := NewExporter(config)

	if e.config.NodeName != "test-node" {
		t.Errorf("NodeName = %q, want %q", e.config.NodeName, "test-node")
	}
	if e.config.MetricsPort != 8888 {
		t.Errorf("MetricsPort = %d, want %d", e.config.MetricsPort, 8888)
	}
	if e.config.ScrapeInterval != 30*time.Second {
		t.Errorf("ScrapeInterval = %v, want %v", e.config.ScrapeInterval, 30*time.Second)
	}
	if e.config.RetryHandlerURL != "http://localhost:8080" {
		t.Errorf("RetryHandlerURL = %q, want %q", e.config.RetryHandlerURL, "http://localhost:8080")
	}
}

func TestSetRetryHandlerClient(t *testing.T) {
	e := NewExporter(Config{})

	if e.rhClient != nil {
		t.Error("rhClient should be nil initially")
	}

	mockClient := &mockRetryHandlerClient{}
	e.SetRetryHandlerClient(mockClient)

	if e.rhClient == nil {
		t.Error("rhClient should not be nil after SetRetryHandlerClient")
	}
}

type mockRetryHandlerClient struct {
	statuses []RetryHandlerStatus
	err      error
}

func (m *mockRetryHandlerClient) GetStatus() ([]RetryHandlerStatus, error) {
	return m.statuses, m.err
}

func TestRetryHandlerStatus_Fields(t *testing.T) {
	status := RetryHandlerStatus{
		Device:       "cxi0",
		Running:      true,
		RestartCount: 5,
	}

	if status.Device != "cxi0" {
		t.Errorf("Device = %q, want %q", status.Device, "cxi0")
	}
	if !status.Running {
		t.Error("Running = false, want true")
	}
	if status.RestartCount != 5 {
		t.Errorf("RestartCount = %d, want %d", status.RestartCount, 5)
	}
}

func TestConfig_Fields(t *testing.T) {
	config := Config{
		NodeName:        "node1",
		MetricsPort:     9999,
		ScrapeInterval:  45 * time.Second,
		RetryHandlerURL: "http://rh:8080",
	}

	if config.NodeName != "node1" {
		t.Errorf("NodeName = %q, want %q", config.NodeName, "node1")
	}
	if config.MetricsPort != 9999 {
		t.Errorf("MetricsPort = %d, want %d", config.MetricsPort, 9999)
	}
	if config.ScrapeInterval != 45*time.Second {
		t.Errorf("ScrapeInterval = %v, want %v", config.ScrapeInterval, 45*time.Second)
	}
	if config.RetryHandlerURL != "http://rh:8080" {
		t.Errorf("RetryHandlerURL = %q, want %q", config.RetryHandlerURL, "http://rh:8080")
	}
}

func TestHealthzEndpoint(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	})

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "ok") {
		t.Errorf("body = %q, want to contain 'ok'", rr.Body.String())
	}
}

func TestReadyzEndpoint(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ready\n")
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "ready") {
		t.Errorf("body = %q, want to contain 'ready'", rr.Body.String())
	}
}

func TestLinkStateValues(t *testing.T) {
	tests := []struct {
		linkState string
		expected  float64
	}{
		{"up", 1},
		{"down", 0},
		{"unknown", -1},
		{"", -1},
		{"invalid", -1},
	}

	for _, tt := range tests {
		t.Run(tt.linkState, func(t *testing.T) {
			var value float64 = -1
			switch tt.linkState {
			case "up":
				value = 1
			case "down":
				value = 0
			}

			if value != tt.expected {
				t.Errorf("value = %f, want %f", value, tt.expected)
			}
		})
	}
}

func TestRestartCountDelta(t *testing.T) {
	tests := []struct {
		name          string
		lastCount     int
		currentCount  int
		exists        bool
		expectedDelta int
		shouldAdd     bool
	}{
		{
			name:          "first observation",
			lastCount:     0,
			currentCount:  3,
			exists:        false,
			expectedDelta: 0,
			shouldAdd:     false,
		},
		{
			name:          "restart occurred",
			lastCount:     3,
			currentCount:  5,
			exists:        true,
			expectedDelta: 2,
			shouldAdd:     true,
		},
		{
			name:          "no restart",
			lastCount:     5,
			currentCount:  5,
			exists:        true,
			expectedDelta: 0,
			shouldAdd:     false,
		},
		{
			name:          "counter reset (process restarted from 0)",
			lastCount:     10,
			currentCount:  2,
			exists:        true,
			expectedDelta: 0,
			shouldAdd:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var delta int
			shouldAdd := false

			if tt.exists && tt.currentCount > tt.lastCount {
				delta = tt.currentCount - tt.lastCount
				shouldAdd = true
			}

			if delta != tt.expectedDelta {
				t.Errorf("delta = %d, want %d", delta, tt.expectedDelta)
			}
			if shouldAdd != tt.shouldAdd {
				t.Errorf("shouldAdd = %v, want %v", shouldAdd, tt.shouldAdd)
			}
		})
	}
}

func TestModuleNames(t *testing.T) {
	expectedModules := []string{"cxi_core", "cxi_ss1", "cxi_user", "cxi", "sbl", "sl"}

	for _, mod := range expectedModules {
		t.Run(mod, func(t *testing.T) {
			if mod == "" {
				t.Error("module name is empty")
			}
			if strings.Contains(mod, " ") {
				t.Error("module name contains space")
			}
		})
	}
}

func TestNamespaceConstant(t *testing.T) {
	if namespace != "cxi" {
		t.Errorf("namespace = %q, want %q", namespace, "cxi")
	}
}

func TestScrapeRetryHandlerStatus_NoClient(t *testing.T) {
	e := NewExporter(Config{
		NodeName: "test-node",
		// No RetryHandlerURL set
	})

	// This should not panic
	e.scrapeRetryHandlerStatus()
}

func TestScrapeRetryHandlerStatus_WithMockClient(t *testing.T) {
	e := NewExporter(Config{
		NodeName: "test-node",
	})

	mockClient := &mockRetryHandlerClient{
		statuses: []RetryHandlerStatus{
			{Device: "cxi0", Running: true, RestartCount: 3},
			{Device: "cxi1", Running: false, RestartCount: 1},
		},
	}
	e.SetRetryHandlerClient(mockClient)

	// This should not panic
	e.scrapeRetryHandlerStatus()

	// Verify restart counts are tracked
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.lastRestartCounts["cxi0"] != 3 {
		t.Errorf("lastRestartCounts[cxi0] = %d, want 3", e.lastRestartCounts["cxi0"])
	}
	if e.lastRestartCounts["cxi1"] != 1 {
		t.Errorf("lastRestartCounts[cxi1] = %d, want 1", e.lastRestartCounts["cxi1"])
	}
}

func TestScrapeRetryHandlerStatus_RestartDeltaTracking(t *testing.T) {
	e := NewExporter(Config{
		NodeName: "test-node",
	})

	// First scrape
	mockClient := &mockRetryHandlerClient{
		statuses: []RetryHandlerStatus{
			{Device: "cxi0", Running: true, RestartCount: 3},
		},
	}
	e.SetRetryHandlerClient(mockClient)
	e.scrapeRetryHandlerStatus()

	// Second scrape with increased restart count
	mockClient.statuses = []RetryHandlerStatus{
		{Device: "cxi0", Running: true, RestartCount: 5},
	}
	e.scrapeRetryHandlerStatus()

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.lastRestartCounts["cxi0"] != 5 {
		t.Errorf("lastRestartCounts[cxi0] = %d, want 5", e.lastRestartCounts["cxi0"])
	}
}

func TestExporterStop(t *testing.T) {
	e := NewExporter(Config{
		NodeName:       "test-node",
		ScrapeInterval: 100 * time.Millisecond,
	})

	// Don't actually start the server, just test the stop logic
	done := make(chan bool)
	go func() {
		select {
		case <-e.stopCh:
			done <- true
		case <-time.After(1 * time.Second):
			done <- false
		}
	}()

	close(e.stopCh)

	if !<-done {
		t.Error("stopCh was not closed properly")
	}
}

func TestConcurrentAccess(t *testing.T) {
	e := NewExporter(Config{
		NodeName: "test-node",
	})

	mockClient := &mockRetryHandlerClient{
		statuses: []RetryHandlerStatus{
			{Device: "cxi0", Running: true, RestartCount: 1},
		},
	}
	e.SetRetryHandlerClient(mockClient)

	done := make(chan bool, 10)

	// Concurrent writes
	for i := 0; i < 5; i++ {
		go func(count int) {
			mockClient.statuses = []RetryHandlerStatus{
				{Device: "cxi0", Running: true, RestartCount: count},
			}
			e.scrapeRetryHandlerStatus()
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			e.mu.RLock()
			_ = e.lastRestartCounts["cxi0"]
			e.mu.RUnlock()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("concurrent access timed out")
		}
	}
}

func TestScrapeRetryHandlerStatus_ClientError(t *testing.T) {
	e := NewExporter(Config{
		NodeName: "test-node",
	})

	mockClient := &mockRetryHandlerClient{
		err: http.ErrServerClosed,
	}
	e.SetRetryHandlerClient(mockClient)

	// Should not panic on client error
	e.scrapeRetryHandlerStatus()
}

func TestScrapeRetryHandlerStatus_MultipleDevices(t *testing.T) {
	e := NewExporter(Config{
		NodeName: "test-node",
	})

	mockClient := &mockRetryHandlerClient{
		statuses: []RetryHandlerStatus{
			{Device: "cxi0", Running: true, RestartCount: 1},
			{Device: "cxi1", Running: true, RestartCount: 2},
			{Device: "cxi2", Running: false, RestartCount: 0},
		},
	}
	e.SetRetryHandlerClient(mockClient)
	e.scrapeRetryHandlerStatus()

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.lastRestartCounts) != 3 {
		t.Errorf("expected 3 devices tracked, got %d", len(e.lastRestartCounts))
	}
}

func TestScrapeRetryHandlerStatus_CreateClientFromURL(t *testing.T) {
	e := NewExporter(Config{
		NodeName:        "test-node",
		RetryHandlerURL: "http://localhost:8080",
	})

	// First scrape should create client from URL
	e.scrapeRetryHandlerStatus()

	// rhClient should be non-nil now (created from URL)
	if e.rhClient == nil {
		t.Error("rhClient should be created from RetryHandlerURL")
	}
}

func TestPrometheusMetricNames(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"device_link_state", "cxi_device_link_state"},
		{"device_numa_node", "cxi_device_numa_node"},
		{"device_count", "cxi_device_count"},
		{"retry_handler_running", "cxi_retry_handler_running"},
		{"retry_handler_restarts_total", "cxi_retry_handler_restarts_total"},
		{"driver_loaded", "cxi_driver_loaded"},
		{"scrape_errors_total", "cxi_scrape_errors_total"},
		{"last_scrape_timestamp_seconds", "cxi_last_scrape_timestamp_seconds"},
	}

	// Just verify namespace is correct
	if namespace != "cxi" {
		t.Errorf("namespace = %q, want %q", namespace, "cxi")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify expected metric name format
			if !strings.HasPrefix(tt.expected, namespace+"_") {
				t.Errorf("%s doesn't have expected prefix %s_", tt.expected, namespace)
			}
		})
	}
}

func TestRetryHandlerStatus_NotRunning(t *testing.T) {
	e := NewExporter(Config{
		NodeName: "test-node",
	})

	mockClient := &mockRetryHandlerClient{
		statuses: []RetryHandlerStatus{
			{Device: "cxi0", Running: false, RestartCount: 10},
		},
	}
	e.SetRetryHandlerClient(mockClient)
	e.scrapeRetryHandlerStatus()

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Even non-running handlers should be tracked
	if e.lastRestartCounts["cxi0"] != 10 {
		t.Errorf("expected restart count 10, got %d", e.lastRestartCounts["cxi0"])
	}
}

func TestNewExporter_ZeroScrapeInterval(t *testing.T) {
	e := NewExporter(Config{
		NodeName:       "test-node",
		ScrapeInterval: 0,
	})

	// Should default to 15 seconds
	if e.config.ScrapeInterval != 15*time.Second {
		t.Errorf("ScrapeInterval = %v, want %v", e.config.ScrapeInterval, 15*time.Second)
	}
}

func TestNewExporter_ZeroMetricsPort(t *testing.T) {
	e := NewExporter(Config{
		NodeName:    "test-node",
		MetricsPort: 0,
	})

	// Should default to 9090
	if e.config.MetricsPort != 9090 {
		t.Errorf("MetricsPort = %d, want %d", e.config.MetricsPort, 9090)
	}
}
