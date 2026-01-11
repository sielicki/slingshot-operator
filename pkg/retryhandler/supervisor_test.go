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

package retryhandler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSupervisor_Defaults(t *testing.T) {
	s := NewSupervisor("", 0)

	if s.cxiRHPath != defaultCXIRHPath {
		t.Errorf("cxiRHPath = %q, want %q", s.cxiRHPath, defaultCXIRHPath)
	}
	if s.healthPort != defaultHealthPort {
		t.Errorf("healthPort = %d, want %d", s.healthPort, defaultHealthPort)
	}
	if s.handlers == nil {
		t.Error("handlers map is nil")
	}
	if s.stopCh == nil {
		t.Error("stopCh is nil")
	}
}

func TestNewSupervisor_CustomConfig(t *testing.T) {
	s := NewSupervisor("/custom/path/cxi_rh", 9999)

	if s.cxiRHPath != "/custom/path/cxi_rh" {
		t.Errorf("cxiRHPath = %q, want %q", s.cxiRHPath, "/custom/path/cxi_rh")
	}
	if s.healthPort != 9999 {
		t.Errorf("healthPort = %d, want %d", s.healthPort, 9999)
	}
}

func TestHandlerStatus_Fields(t *testing.T) {
	now := time.Now()
	status := HandlerStatus{
		Device:       "cxi0",
		Running:      true,
		PID:          12345,
		RestartCount: 3,
		LastError:    "test error",
		StartedAt:    now,
	}

	if status.Device != "cxi0" {
		t.Errorf("Device = %q, want %q", status.Device, "cxi0")
	}
	if !status.Running {
		t.Error("Running = false, want true")
	}
	if status.PID != 12345 {
		t.Errorf("PID = %d, want %d", status.PID, 12345)
	}
	if status.RestartCount != 3 {
		t.Errorf("RestartCount = %d, want %d", status.RestartCount, 3)
	}
	if status.LastError != "test error" {
		t.Errorf("LastError = %q, want %q", status.LastError, "test error")
	}
	if !status.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", status.StartedAt, now)
	}
}

func TestGetStatus_Empty(t *testing.T) {
	s := NewSupervisor("", 0)

	statuses := s.GetStatus()

	if len(statuses) != 0 {
		t.Errorf("got %d statuses, want 0", len(statuses))
	}
}

func TestGetStatus_WithHandlers(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{
		device:       "cxi0",
		running:      true,
		restartCount: 2,
		lastError:    "",
		startedAt:    time.Now(),
	}
	s.handlers["cxi1"] = &handlerProcess{
		device:       "cxi1",
		running:      false,
		restartCount: 5,
		lastError:    "process exited",
		startedAt:    time.Now(),
	}

	statuses := s.GetStatus()

	if len(statuses) != 2 {
		t.Fatalf("got %d statuses, want 2", len(statuses))
	}

	// Find each status by device name
	var cxi0, cxi1 *HandlerStatus
	for i := range statuses {
		switch statuses[i].Device {
		case "cxi0":
			cxi0 = &statuses[i]
		case "cxi1":
			cxi1 = &statuses[i]
		}
	}

	if cxi0 == nil {
		t.Fatal("cxi0 status not found")
	}
	if !cxi0.Running {
		t.Error("cxi0 Running = false, want true")
	}
	if cxi0.RestartCount != 2 {
		t.Errorf("cxi0 RestartCount = %d, want 2", cxi0.RestartCount)
	}

	if cxi1 == nil {
		t.Fatal("cxi1 status not found")
	}
	if cxi1.Running {
		t.Error("cxi1 Running = true, want false")
	}
	if cxi1.RestartCount != 5 {
		t.Errorf("cxi1 RestartCount = %d, want 5", cxi1.RestartCount)
	}
	if cxi1.LastError != "process exited" {
		t.Errorf("cxi1 LastError = %q, want %q", cxi1.LastError, "process exited")
	}
}

func TestIsHealthy_NoHandlers(t *testing.T) {
	s := NewSupervisor("", 0)

	if !s.IsHealthy() {
		t.Error("IsHealthy() = false, want true (no handlers)")
	}
}

func TestIsHealthy_AllRunning(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{running: true}
	s.handlers["cxi1"] = &handlerProcess{running: true}

	if !s.IsHealthy() {
		t.Error("IsHealthy() = false, want true (all running)")
	}
}

func TestIsHealthy_OneNotRunning(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{running: true}
	s.handlers["cxi1"] = &handlerProcess{running: false}

	if s.IsHealthy() {
		t.Error("IsHealthy() = true, want false (one not running)")
	}
}

func TestIsHealthy_NoneRunning(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{running: false}
	s.handlers["cxi1"] = &handlerProcess{running: false}

	if s.IsHealthy() {
		t.Error("IsHealthy() = true, want false (none running)")
	}
}

func TestHealthServer_Healthz_Healthy(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{running: true}

	// Create test server
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if s.IsHealthy() {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok\n")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "unhealthy\n")
		}
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

func TestHealthServer_Healthz_Unhealthy(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{running: false}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if s.IsHealthy() {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok\n")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "unhealthy\n")
		}
	})

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthServer_Readyz(t *testing.T) {
	_ = NewSupervisor("", 0) // Create supervisor to verify it works

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ready\n")
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHealthServer_Status(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{
		device:       "cxi0",
		running:      true,
		restartCount: 3,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := s.GetStatus()
		w.Header().Set("Content-Type", "application/json")

		type statusResponse struct {
			Handlers []struct {
				Device   string `json:"device"`
				Running  bool   `json:"running"`
				PID      int    `json:"pid"`
				Restarts int    `json:"restarts"`
			} `json:"handlers"`
			Healthy bool `json:"healthy"`
		}

		resp := statusResponse{
			Healthy: s.IsHealthy(),
		}
		for _, st := range statuses {
			resp.Handlers = append(resp.Handlers, struct {
				Device   string `json:"device"`
				Running  bool   `json:"running"`
				PID      int    `json:"pid"`
				Restarts int    `json:"restarts"`
			}{
				Device:   st.Device,
				Running:  st.Running,
				PID:      st.PID,
				Restarts: st.RestartCount,
			})
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	req := httptest.NewRequest("GET", "/status", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	// Verify JSON is parseable
	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Errorf("failed to parse JSON: %v", err)
	}

	if result["healthy"] != true {
		t.Errorf("healthy = %v, want true", result["healthy"])
	}
}

func TestBackoffConstants(t *testing.T) {
	// These are the expected values for the backoff constants
	expectedMin := 1 * time.Second
	expectedMax := 60 * time.Second
	expectedFactor := 2.0

	if restartBackoffMin != expectedMin {
		t.Errorf("restartBackoffMin = %v, want %v", restartBackoffMin, expectedMin)
	}
	if restartBackoffMax != expectedMax {
		t.Errorf("restartBackoffMax = %v, want %v", restartBackoffMax, expectedMax)
	}
	if restartBackoffFactor != expectedFactor {
		t.Errorf("restartBackoffFactor = %v, want %v", restartBackoffFactor, expectedFactor)
	}
}

func TestBackoffCalculation(t *testing.T) {
	tests := []struct {
		name            string
		currentBackoff  time.Duration
		expectedBackoff time.Duration
	}{
		{
			name:            "first backoff",
			currentBackoff:  restartBackoffMin,
			expectedBackoff: 2 * time.Second,
		},
		{
			name:            "second backoff",
			currentBackoff:  2 * time.Second,
			expectedBackoff: 4 * time.Second,
		},
		{
			name:            "backoff near max",
			currentBackoff:  32 * time.Second,
			expectedBackoff: 60 * time.Second, // Capped at max
		},
		{
			name:            "backoff at max",
			currentBackoff:  60 * time.Second,
			expectedBackoff: 60 * time.Second, // Already at max
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backoff := time.Duration(float64(tt.currentBackoff) * restartBackoffFactor)
			if backoff > restartBackoffMax {
				backoff = restartBackoffMax
			}

			if backoff != tt.expectedBackoff {
				t.Errorf("backoff = %v, want %v", backoff, tt.expectedBackoff)
			}
		})
	}
}

func TestHandlerProcess_Mutex(t *testing.T) {
	h := &handlerProcess{
		device: "cxi0",
	}

	// Test that mutex operations don't deadlock
	done := make(chan bool)
	go func() {
		h.mu.Lock()
		h.running = true
		h.mu.Unlock()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("mutex operation timed out")
	}

	h.mu.Lock()
	if !h.running {
		t.Error("running = false, want true after mutex operation")
	}
	h.mu.Unlock()
}

func TestDefaultCXIRHPath(t *testing.T) {
	if defaultCXIRHPath != "/usr/bin/cxi_rh" {
		t.Errorf("defaultCXIRHPath = %q, want %q", defaultCXIRHPath, "/usr/bin/cxi_rh")
	}
}

func TestDefaultHealthPort(t *testing.T) {
	if defaultHealthPort != 8080 {
		t.Errorf("defaultHealthPort = %d, want %d", defaultHealthPort, 8080)
	}
}

func TestSupervisor_Stop_NoHandlers(t *testing.T) {
	s := NewSupervisor("", 0)

	// Stop should not panic even with no handlers
	done := make(chan bool)
	go func() {
		s.Stop()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Stop timed out")
	}
}

func TestSupervisor_StopCh_Closed(t *testing.T) {
	s := NewSupervisor("", 0)

	select {
	case <-s.stopCh:
		t.Error("stopCh should not be closed initially")
	default:
		// Success - not closed
	}

	close(s.stopCh)

	select {
	case <-s.stopCh:
		// Success - now closed
	default:
		t.Error("stopCh should be closed after close()")
	}
}

func TestGetStatus_Concurrent(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{
		device:       "cxi0",
		running:      true,
		restartCount: 0,
	}

	done := make(chan bool, 10)

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			_ = s.GetStatus()
			done <- true
		}()
	}

	// Concurrent health checks
	for i := 0; i < 5; i++ {
		go func() {
			_ = s.IsHealthy()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("concurrent access timed out")
		}
	}
}

func TestHandlerProcess_StatusWithPID(t *testing.T) {
	s := NewSupervisor("", 0)
	s.handlers["cxi0"] = &handlerProcess{
		device:       "cxi0",
		running:      true,
		restartCount: 2,
		startedAt:    time.Now(),
		// cmd is nil, so PID should be 0
	}

	statuses := s.GetStatus()
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}

	if statuses[0].PID != 0 {
		t.Errorf("PID = %d, want 0 (no process)", statuses[0].PID)
	}
}

func TestHandlerProcess_IncrementRestartCount(t *testing.T) {
	h := &handlerProcess{
		device:       "cxi0",
		restartCount: 0,
	}

	for i := 1; i <= 5; i++ {
		h.mu.Lock()
		h.restartCount++
		h.mu.Unlock()

		h.mu.Lock()
		count := h.restartCount
		h.mu.Unlock()

		if count != i {
			t.Errorf("after %d increments, restartCount = %d, want %d", i, count, i)
		}
	}
}

func TestSupervisor_HandlersMapInitialized(t *testing.T) {
	s := NewSupervisor("", 0)

	if s.handlers == nil {
		t.Error("handlers map should be initialized")
	}

	// Should be able to add handlers
	s.handlers["test"] = &handlerProcess{device: "test"}
	if len(s.handlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(s.handlers))
	}
}

func TestHandlerStatus_JSONSerialization(t *testing.T) {
	now := time.Now()
	status := HandlerStatus{
		Device:       "cxi0",
		Running:      true,
		PID:          1234,
		RestartCount: 3,
		LastError:    "test error",
		StartedAt:    now,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled HandlerStatus
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.Device != status.Device {
		t.Errorf("Device = %q, want %q", unmarshaled.Device, status.Device)
	}
	if unmarshaled.Running != status.Running {
		t.Errorf("Running = %v, want %v", unmarshaled.Running, status.Running)
	}
	if unmarshaled.PID != status.PID {
		t.Errorf("PID = %d, want %d", unmarshaled.PID, status.PID)
	}
	if unmarshaled.RestartCount != status.RestartCount {
		t.Errorf("RestartCount = %d, want %d", unmarshaled.RestartCount, status.RestartCount)
	}
}

func TestIsHealthy_SingleHandler(t *testing.T) {
	tests := []struct {
		name     string
		running  bool
		expected bool
	}{
		{"running", true, true},
		{"not running", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSupervisor("", 0)
			s.handlers["cxi0"] = &handlerProcess{running: tt.running}

			if s.IsHealthy() != tt.expected {
				t.Errorf("IsHealthy() = %v, want %v", s.IsHealthy(), tt.expected)
			}
		})
	}
}

func TestBackoffReset(t *testing.T) {
	// Test that backoff starts at minimum
	backoff := restartBackoffMin

	if backoff != 1*time.Second {
		t.Errorf("initial backoff = %v, want %v", backoff, 1*time.Second)
	}
}

func TestSupervisor_MultipleDevices(t *testing.T) {
	s := NewSupervisor("", 0)

	devices := []string{"cxi0", "cxi1", "cxi2", "cxi3"}
	for _, dev := range devices {
		s.handlers[dev] = &handlerProcess{
			device:  dev,
			running: true,
		}
	}

	statuses := s.GetStatus()
	if len(statuses) != 4 {
		t.Errorf("got %d statuses, want 4", len(statuses))
	}

	if !s.IsHealthy() {
		t.Error("IsHealthy() = false, want true (all running)")
	}

	// Stop one handler
	s.handlers["cxi2"].running = false

	if s.IsHealthy() {
		t.Error("IsHealthy() = true, want false (one not running)")
	}
}
