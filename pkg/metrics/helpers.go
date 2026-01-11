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
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func isModuleLoaded(name string) bool {
	file, err := os.Open("/proc/modules")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

type httpRetryHandlerClient struct {
	url    string
	client *http.Client
}

type retryHandlerStatusResponse struct {
	Handlers []struct {
		Device   string `json:"device"`
		Running  bool   `json:"running"`
		PID      int    `json:"pid"`
		Restarts int    `json:"restarts"`
	} `json:"handlers"`
	Healthy bool `json:"healthy"`
}

func (c *httpRetryHandlerClient) GetStatus() ([]RetryHandlerStatus, error) {
	if c.client == nil {
		c.client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := c.client.Get(c.url + "/status")
	if err != nil {
		return nil, fmt.Errorf("failed to get retry handler status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var statusResp retryHandlerStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var statuses []RetryHandlerStatus
	for _, h := range statusResp.Handlers {
		statuses = append(statuses, RetryHandlerStatus{
			Device:       h.Device,
			Running:      h.Running,
			RestartCount: h.Restarts,
		})
	}

	return statuses, nil
}
