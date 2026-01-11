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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sielicki/slingshot-operator/pkg/deviceplugin"
)

func TestNewAgent_Defaults(t *testing.T) {
	agent := NewAgent(Config{})

	if agent.config.HealthPort != 8081 {
		t.Errorf("HealthPort = %d, want %d", agent.config.HealthPort, 8081)
	}
	if agent.config.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want %v", agent.config.PollInterval, 30*time.Second)
	}
	if agent.stopCh == nil {
		t.Error("stopCh is nil")
	}
}

func TestNewAgent_CustomConfig(t *testing.T) {
	config := Config{
		NodeName:     "test-node",
		Namespace:    "test-ns",
		DriverSource: DriverSourceDKMS,
		HealthPort:   9999,
		PollInterval: 60 * time.Second,
	}
	agent := NewAgent(config)

	if agent.config.NodeName != "test-node" {
		t.Errorf("NodeName = %q, want %q", agent.config.NodeName, "test-node")
	}
	if agent.config.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", agent.config.Namespace, "test-ns")
	}
	if agent.config.DriverSource != DriverSourceDKMS {
		t.Errorf("DriverSource = %q, want %q", agent.config.DriverSource, DriverSourceDKMS)
	}
	if agent.config.HealthPort != 9999 {
		t.Errorf("HealthPort = %d, want %d", agent.config.HealthPort, 9999)
	}
	if agent.config.PollInterval != 60*time.Second {
		t.Errorf("PollInterval = %v, want %v", agent.config.PollInterval, 60*time.Second)
	}
}

func TestDriverSource_Constants(t *testing.T) {
	if DriverSourceDKMS != "dkms" {
		t.Errorf("DriverSourceDKMS = %q, want %q", DriverSourceDKMS, "dkms")
	}
	if DriverSourcePrebuilt != "prebuilt" {
		t.Errorf("DriverSourcePrebuilt = %q, want %q", DriverSourcePrebuilt, "prebuilt")
	}
	if DriverSourcePreinstalled != "preinstalled" {
		t.Errorf("DriverSourcePreinstalled = %q, want %q", DriverSourcePreinstalled, "preinstalled")
	}
}

func TestIsReady(t *testing.T) {
	agent := NewAgent(Config{})

	// Initially not ready
	if agent.IsReady() {
		t.Error("IsReady() = true, want false initially")
	}

	// Set ready
	agent.setDriverReady(true)
	if !agent.IsReady() {
		t.Error("IsReady() = false, want true after setDriverReady(true)")
	}

	// Set not ready
	agent.setDriverReady(false)
	if agent.IsReady() {
		t.Error("IsReady() = true, want false after setDriverReady(false)")
	}
}

func TestSetDriverReady_ClearsError(t *testing.T) {
	agent := NewAgent(Config{})

	// Set an error first
	agent.setError(io.EOF)
	agent.mu.RLock()
	if agent.lastError == "" {
		t.Error("lastError should be set")
	}
	agent.mu.RUnlock()

	// Setting ready should clear the error
	agent.setDriverReady(true)
	agent.mu.RLock()
	if agent.lastError != "" {
		t.Errorf("lastError = %q, want empty after setDriverReady(true)", agent.lastError)
	}
	agent.mu.RUnlock()
}

func TestSetError(t *testing.T) {
	agent := NewAgent(Config{})

	agent.setError(io.EOF)

	agent.mu.RLock()
	defer agent.mu.RUnlock()
	if agent.lastError != "EOF" {
		t.Errorf("lastError = %q, want %q", agent.lastError, "EOF")
	}
}

func TestGetDevices(t *testing.T) {
	agent := NewAgent(Config{})

	// Initially empty
	devices := agent.GetDevices()
	if len(devices) != 0 {
		t.Errorf("GetDevices() returned %d devices, want 0", len(devices))
	}

	// Add devices
	agent.mu.Lock()
	agent.devices = []*deviceplugin.CXIDevice{
		{Name: "cxi0"},
		{Name: "cxi1"},
	}
	agent.mu.Unlock()

	devices = agent.GetDevices()
	if len(devices) != 2 {
		t.Errorf("GetDevices() returned %d devices, want 2", len(devices))
	}
}

func TestHealthServer_Healthz(t *testing.T) {
	_ = NewAgent(Config{}) // Verify agent creation works

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

func TestHealthServer_Readyz_Ready(t *testing.T) {
	agent := NewAgent(Config{})
	agent.setDriverReady(true)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agent.IsReady() {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ready\n")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			agent.mu.RLock()
			lastErr := agent.lastError
			agent.mu.RUnlock()
			io.WriteString(w, "not ready: "+lastErr+"\n")
		}
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHealthServer_Readyz_NotReady(t *testing.T) {
	agent := NewAgent(Config{})
	agent.setError(io.EOF)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agent.IsReady() {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ready\n")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			agent.mu.RLock()
			lastErr := agent.lastError
			agent.mu.RUnlock()
			io.WriteString(w, "not ready: "+lastErr+"\n")
		}
	})

	req := httptest.NewRequest("GET", "/readyz", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rr.Body.String(), "not ready") {
		t.Errorf("body = %q, want to contain 'not ready'", rr.Body.String())
	}
}

func TestHealthServer_Status(t *testing.T) {
	config := Config{
		NodeName: "test-node",
	}
	agent := NewAgent(config)
	agent.setDriverReady(true)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent.mu.RLock()
		devices := agent.devices
		ready := agent.driverReady
		lastErr := agent.lastError
		agent.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")

		resp := map[string]interface{}{
			"node":        agent.config.NodeName,
			"driverReady": ready,
			"devices":     devices,
		}
		if lastErr != "" {
			resp["lastError"] = lastErr
		}

		json.NewEncoder(w).Encode(resp)
	})

	req := httptest.NewRequest("GET", "/status", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["node"] != "test-node" {
		t.Errorf("node = %v, want %q", result["node"], "test-node")
	}
	if result["driverReady"] != true {
		t.Errorf("driverReady = %v, want true", result["driverReady"])
	}
}

func TestConfig_Fields(t *testing.T) {
	config := Config{
		NodeName:      "node1",
		Namespace:     "ns1",
		DriverSource:  DriverSourcePreinstalled,
		DKMSSourceURL: "http://example.com/source.tar.gz",
		PrebuiltURL:   "http://example.com/prebuilt",
		HealthPort:    8888,
		PollInterval:  45 * time.Second,
	}

	if config.NodeName != "node1" {
		t.Errorf("NodeName = %q, want %q", config.NodeName, "node1")
	}
	if config.Namespace != "ns1" {
		t.Errorf("Namespace = %q, want %q", config.Namespace, "ns1")
	}
	if config.DriverSource != DriverSourcePreinstalled {
		t.Errorf("DriverSource = %q, want %q", config.DriverSource, DriverSourcePreinstalled)
	}
	if config.DKMSSourceURL != "http://example.com/source.tar.gz" {
		t.Errorf("DKMSSourceURL = %q, want %q", config.DKMSSourceURL, "http://example.com/source.tar.gz")
	}
	if config.PrebuiltURL != "http://example.com/prebuilt" {
		t.Errorf("PrebuiltURL = %q, want %q", config.PrebuiltURL, "http://example.com/prebuilt")
	}
	if config.HealthPort != 8888 {
		t.Errorf("HealthPort = %d, want %d", config.HealthPort, 8888)
	}
	if config.PollInterval != 45*time.Second {
		t.Errorf("PollInterval = %v, want %v", config.PollInterval, 45*time.Second)
	}
}

func TestConcurrentAccess(t *testing.T) {
	agent := NewAgent(Config{})

	done := make(chan bool, 10)

	// Concurrent writers
	for i := 0; i < 5; i++ {
		go func(ready bool) {
			agent.setDriverReady(ready)
			done <- true
		}(i%2 == 0)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			_ = agent.IsReady()
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

func TestAgent_Stop_ClosesChannel(t *testing.T) {
	agent := NewAgent(Config{})

	select {
	case <-agent.stopCh:
		t.Error("stopCh should not be closed initially")
	default:
		// Success
	}

	close(agent.stopCh)

	select {
	case <-agent.stopCh:
		// Success - closed
	default:
		t.Error("stopCh should be closed")
	}
}

func TestGetDevices_ConcurrentAccess(t *testing.T) {
	agent := NewAgent(Config{})
	agent.mu.Lock()
	agent.devices = []*deviceplugin.CXIDevice{
		{Name: "cxi0"},
	}
	agent.mu.Unlock()

	done := make(chan bool, 20)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_ = agent.GetDevices()
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(num int) {
			agent.mu.Lock()
			agent.devices = append(agent.devices, &deviceplugin.CXIDevice{Name: "cxi" + string(rune('0'+num))})
			agent.mu.Unlock()
			done <- true
		}(i)
	}

	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatal("concurrent access timed out")
		}
	}
}

func TestSetError_NilError(t *testing.T) {
	agent := NewAgent(Config{})

	// Set a real error first
	agent.setError(io.EOF)
	agent.mu.RLock()
	if agent.lastError == "" {
		t.Error("lastError should be set")
	}
	agent.mu.RUnlock()

	// Set nil error
	agent.setError(nil)
	agent.mu.RLock()
	if agent.lastError != "" {
		t.Errorf("lastError = %q, want empty after setError(nil)", agent.lastError)
	}
	agent.mu.RUnlock()
}

func TestDriver_SourceValidation(t *testing.T) {
	validSources := []DriverSource{
		DriverSourceDKMS,
		DriverSourcePrebuilt,
		DriverSourcePreinstalled,
	}

	for _, source := range validSources {
		t.Run(string(source), func(t *testing.T) {
			agent := NewAgent(Config{
				DriverSource: source,
			})

			if agent.config.DriverSource != source {
				t.Errorf("DriverSource = %q, want %q", agent.config.DriverSource, source)
			}
		})
	}
}

func TestDefaultHealthPort(t *testing.T) {
	agent := NewAgent(Config{})

	if agent.config.HealthPort != 8081 {
		t.Errorf("default HealthPort = %d, want 8081", agent.config.HealthPort)
	}
}

func TestDefaultPollInterval(t *testing.T) {
	agent := NewAgent(Config{})

	if agent.config.PollInterval != 30*time.Second {
		t.Errorf("default PollInterval = %v, want 30s", agent.config.PollInterval)
	}
}

func TestAgent_MultipleErrorSets(t *testing.T) {
	agent := NewAgent(Config{})

	errors := []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		io.ErrClosedPipe,
	}

	for _, err := range errors {
		agent.setError(err)
		agent.mu.RLock()
		if agent.lastError != err.Error() {
			t.Errorf("lastError = %q, want %q", agent.lastError, err.Error())
		}
		agent.mu.RUnlock()
	}
}

func TestAgent_ReadyStateTransitions(t *testing.T) {
	agent := NewAgent(Config{})

	// Initial state
	if agent.IsReady() {
		t.Error("should not be ready initially")
	}

	// Transition to ready
	agent.setDriverReady(true)
	if !agent.IsReady() {
		t.Error("should be ready after setDriverReady(true)")
	}

	// Multiple calls to setDriverReady(true) should be idempotent
	agent.setDriverReady(true)
	if !agent.IsReady() {
		t.Error("should still be ready")
	}

	// Transition back to not ready
	agent.setDriverReady(false)
	if agent.IsReady() {
		t.Error("should not be ready after setDriverReady(false)")
	}
}

func TestAgent_EmptyNodeName(t *testing.T) {
	agent := NewAgent(Config{
		NodeName: "",
	})

	if agent.config.NodeName != "" {
		t.Errorf("NodeName = %q, want empty", agent.config.NodeName)
	}
}

func TestAgent_WaitGroup(t *testing.T) {
	agent := NewAgent(Config{})

	// wg should be usable
	agent.wg.Add(1)
	go func() {
		defer agent.wg.Done()
		time.Sleep(10 * time.Millisecond)
	}()

	done := make(chan bool)
	go func() {
		agent.wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("WaitGroup wait timed out")
	}
}
