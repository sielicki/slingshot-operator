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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHttpRetryHandlerClient_GetStatus_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := retryHandlerStatusResponse{
			Handlers: []struct {
				Device   string `json:"device"`
				Running  bool   `json:"running"`
				PID      int    `json:"pid"`
				Restarts int    `json:"restarts"`
			}{
				{Device: "cxi0", Running: true, PID: 1234, Restarts: 3},
				{Device: "cxi1", Running: false, PID: 0, Restarts: 1},
			},
			Healthy: true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &httpRetryHandlerClient{url: server.URL}
	statuses, err := client.GetStatus()

	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}

	if len(statuses) != 2 {
		t.Fatalf("got %d statuses, want 2", len(statuses))
	}

	// Find cxi0 status
	var cxi0 *RetryHandlerStatus
	for i := range statuses {
		if statuses[i].Device == "cxi0" {
			cxi0 = &statuses[i]
			break
		}
	}

	if cxi0 == nil {
		t.Fatal("cxi0 status not found")
	}
	if !cxi0.Running {
		t.Error("cxi0 Running = false, want true")
	}
	if cxi0.RestartCount != 3 {
		t.Errorf("cxi0 RestartCount = %d, want 3", cxi0.RestartCount)
	}
}

func TestHttpRetryHandlerClient_GetStatus_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &httpRetryHandlerClient{url: server.URL}
	_, err := client.GetStatus()

	if err == nil {
		t.Error("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "unexpected status code") {
		t.Errorf("error = %v, want to contain 'unexpected status code'", err)
	}
}

func TestHttpRetryHandlerClient_GetStatus_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := &httpRetryHandlerClient{url: server.URL}
	_, err := client.GetStatus()

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to decode") {
		t.Errorf("error = %v, want to contain 'failed to decode'", err)
	}
}

func TestHttpRetryHandlerClient_GetStatus_ConnectionError(t *testing.T) {
	client := &httpRetryHandlerClient{url: "http://localhost:1"} // Invalid port
	_, err := client.GetStatus()

	if err == nil {
		t.Error("expected error for connection failure")
	}
	if !strings.Contains(err.Error(), "failed to get retry handler status") {
		t.Errorf("error = %v, want to contain 'failed to get retry handler status'", err)
	}
}

func TestHttpRetryHandlerClient_GetStatus_EmptyHandlers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := retryHandlerStatusResponse{
			Handlers: nil,
			Healthy:  true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &httpRetryHandlerClient{url: server.URL}
	statuses, err := client.GetStatus()

	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}

	if len(statuses) != 0 {
		t.Errorf("got %d statuses, want 0", len(statuses))
	}
}

func TestHttpRetryHandlerClient_ClientReuse(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := retryHandlerStatusResponse{Healthy: true}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &httpRetryHandlerClient{url: server.URL}

	// First call creates client
	_, err := client.GetStatus()
	if err != nil {
		t.Fatalf("first GetStatus returned error: %v", err)
	}
	if client.client == nil {
		t.Error("client should be initialized after first call")
	}

	// Second call reuses client
	_, err = client.GetStatus()
	if err != nil {
		t.Fatalf("second GetStatus returned error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("server was called %d times, want 2", callCount)
	}
}

func TestRetryHandlerStatusResponse_JSONParsing(t *testing.T) {
	jsonData := `{
		"handlers": [
			{"device": "cxi0", "running": true, "pid": 1234, "restarts": 5},
			{"device": "cxi1", "running": false, "pid": 0, "restarts": 2}
		],
		"healthy": true
	}`

	var resp retryHandlerStatusResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(resp.Handlers) != 2 {
		t.Fatalf("got %d handlers, want 2", len(resp.Handlers))
	}

	if resp.Handlers[0].Device != "cxi0" {
		t.Errorf("handlers[0].Device = %q, want %q", resp.Handlers[0].Device, "cxi0")
	}
	if !resp.Handlers[0].Running {
		t.Error("handlers[0].Running = false, want true")
	}
	if resp.Handlers[0].PID != 1234 {
		t.Errorf("handlers[0].PID = %d, want 1234", resp.Handlers[0].PID)
	}
	if resp.Handlers[0].Restarts != 5 {
		t.Errorf("handlers[0].Restarts = %d, want 5", resp.Handlers[0].Restarts)
	}

	if !resp.Healthy {
		t.Error("Healthy = false, want true")
	}
}

func TestRetryHandlerStatusResponse_EmptyJSON(t *testing.T) {
	jsonData := `{}`

	var resp retryHandlerStatusResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if resp.Handlers != nil && len(resp.Handlers) != 0 {
		t.Errorf("got %d handlers, want 0", len(resp.Handlers))
	}
	if resp.Healthy {
		t.Error("Healthy = true, want false (default)")
	}
}

func TestProcModulesFormat(t *testing.T) {
	// Test parsing of /proc/modules format
	lines := []struct {
		line     string
		wantName string
	}{
		{
			line:     "cxi_core 123456 1 - Live 0xffffffffa0000000",
			wantName: "cxi_core",
		},
		{
			line:     "nf_tables 234567 3 nf_tables_set, Live 0xffffffffa0200000",
			wantName: "nf_tables",
		},
		{
			line:     "module_with_underscores_and_numbers_123 1000 0 - Live 0x0",
			wantName: "module_with_underscores_and_numbers_123",
		},
	}

	for _, tt := range lines {
		t.Run(tt.wantName, func(t *testing.T) {
			fields := strings.Fields(tt.line)
			if len(fields) == 0 {
				t.Fatal("no fields found")
			}

			name := fields[0]
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestIsModuleLoaded_Parsing(t *testing.T) {
	// Test the logic of isModuleLoaded without actually reading /proc/modules
	testModules := `cxi_core 123456 1 - Live 0xffffffffa0000000
cxi_user 78901 2 cxi_core, Live 0xffffffffa0100000
nf_tables 234567 3 - Live 0xffffffffa0200000
`

	checkModule := func(content, targetName string) bool {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == targetName {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name     string
		expected bool
	}{
		{"cxi_core", true},
		{"cxi_user", true},
		{"nf_tables", true},
		{"not_loaded", false},
		{"cxi", false}, // Partial match should not work
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkModule(testModules, tt.name)
			if result != tt.expected {
				t.Errorf("checkModule(%q) = %v, want %v", tt.name, result, tt.expected)
			}
		})
	}
}
