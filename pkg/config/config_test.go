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

package config

import (
	"os"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		expected     string
	}{
		{
			name:         "returns env value when set",
			key:          "TEST_GET_ENV_1",
			defaultValue: "default",
			envValue:     "custom",
			expected:     "custom",
		},
		{
			name:         "returns default when env not set",
			key:          "TEST_GET_ENV_2",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
		{
			name:         "empty default",
			key:          "TEST_GET_ENV_3",
			defaultValue: "",
			envValue:     "",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			got := GetEnv(tt.key, tt.defaultValue)
			if got != tt.expected {
				t.Errorf("GetEnv(%q, %q) = %q, want %q", tt.key, tt.defaultValue, got, tt.expected)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		envValue     string
		expected     int
	}{
		{
			name:         "returns int value when set",
			key:          "TEST_GET_ENV_INT_1",
			defaultValue: 10,
			envValue:     "42",
			expected:     42,
		},
		{
			name:         "returns default when env not set",
			key:          "TEST_GET_ENV_INT_2",
			defaultValue: 10,
			envValue:     "",
			expected:     10,
		},
		{
			name:         "returns default for invalid int",
			key:          "TEST_GET_ENV_INT_3",
			defaultValue: 10,
			envValue:     "not-a-number",
			expected:     10,
		},
		{
			name:         "negative number",
			key:          "TEST_GET_ENV_INT_4",
			defaultValue: 10,
			envValue:     "-5",
			expected:     -5,
		},
		{
			name:         "zero value",
			key:          "TEST_GET_ENV_INT_5",
			defaultValue: 10,
			envValue:     "0",
			expected:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			got := GetEnvInt(tt.key, tt.defaultValue)
			if got != tt.expected {
				t.Errorf("GetEnvInt(%q, %d) = %d, want %d", tt.key, tt.defaultValue, got, tt.expected)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		envValue     string
		expected     bool
	}{
		{
			name:         "true value",
			key:          "TEST_GET_ENV_BOOL_1",
			defaultValue: false,
			envValue:     "true",
			expected:     true,
		},
		{
			name:         "false value",
			key:          "TEST_GET_ENV_BOOL_2",
			defaultValue: true,
			envValue:     "false",
			expected:     false,
		},
		{
			name:         "1 is true",
			key:          "TEST_GET_ENV_BOOL_3",
			defaultValue: false,
			envValue:     "1",
			expected:     true,
		},
		{
			name:         "0 is false",
			key:          "TEST_GET_ENV_BOOL_4",
			defaultValue: true,
			envValue:     "0",
			expected:     false,
		},
		{
			name:         "returns default when env not set",
			key:          "TEST_GET_ENV_BOOL_5",
			defaultValue: true,
			envValue:     "",
			expected:     true,
		},
		{
			name:         "returns default for invalid bool",
			key:          "TEST_GET_ENV_BOOL_6",
			defaultValue: true,
			envValue:     "not-a-bool",
			expected:     true,
		},
		{
			name:         "TRUE uppercase",
			key:          "TEST_GET_ENV_BOOL_7",
			defaultValue: false,
			envValue:     "TRUE",
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			got := GetEnvBool(tt.key, tt.defaultValue)
			if got != tt.expected {
				t.Errorf("GetEnvBool(%q, %v) = %v, want %v", tt.key, tt.defaultValue, got, tt.expected)
			}
		})
	}
}

func TestGetEnvDuration(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue time.Duration
		envValue     string
		expected     time.Duration
	}{
		{
			name:         "seconds",
			key:          "TEST_GET_ENV_DUR_1",
			defaultValue: time.Second,
			envValue:     "30s",
			expected:     30 * time.Second,
		},
		{
			name:         "minutes",
			key:          "TEST_GET_ENV_DUR_2",
			defaultValue: time.Second,
			envValue:     "5m",
			expected:     5 * time.Minute,
		},
		{
			name:         "hours",
			key:          "TEST_GET_ENV_DUR_3",
			defaultValue: time.Second,
			envValue:     "2h",
			expected:     2 * time.Hour,
		},
		{
			name:         "milliseconds",
			key:          "TEST_GET_ENV_DUR_4",
			defaultValue: time.Second,
			envValue:     "500ms",
			expected:     500 * time.Millisecond,
		},
		{
			name:         "returns default when env not set",
			key:          "TEST_GET_ENV_DUR_5",
			defaultValue: 15 * time.Second,
			envValue:     "",
			expected:     15 * time.Second,
		},
		{
			name:         "returns default for invalid duration",
			key:          "TEST_GET_ENV_DUR_6",
			defaultValue: 15 * time.Second,
			envValue:     "not-a-duration",
			expected:     15 * time.Second,
		},
		{
			name:         "complex duration",
			key:          "TEST_GET_ENV_DUR_7",
			defaultValue: time.Second,
			envValue:     "1h30m",
			expected:     90 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			got := GetEnvDuration(tt.key, tt.defaultValue)
			if got != tt.expected {
				t.Errorf("GetEnvDuration(%q, %v) = %v, want %v", tt.key, tt.defaultValue, got, tt.expected)
			}
		})
	}
}

func TestGetEnvUint32(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue uint32
		envValue     string
		expected     uint32
	}{
		{
			name:         "decimal value",
			key:          "TEST_GET_ENV_UINT32_1",
			defaultValue: 100,
			envValue:     "42",
			expected:     42,
		},
		{
			name:         "hex value lowercase",
			key:          "TEST_GET_ENV_UINT32_2",
			defaultValue: 100,
			envValue:     "0xfffc0",
			expected:     0xfffc0,
		},
		{
			name:         "hex value uppercase prefix",
			key:          "TEST_GET_ENV_UINT32_3",
			defaultValue: 100,
			envValue:     "0XABCDE",
			expected:     0xABCDE,
		},
		{
			name:         "returns default when env not set",
			key:          "TEST_GET_ENV_UINT32_4",
			defaultValue: 100,
			envValue:     "",
			expected:     100,
		},
		{
			name:         "returns default for invalid value",
			key:          "TEST_GET_ENV_UINT32_5",
			defaultValue: 100,
			envValue:     "not-a-number",
			expected:     100,
		},
		{
			name:         "returns default for invalid hex",
			key:          "TEST_GET_ENV_UINT32_6",
			defaultValue: 100,
			envValue:     "0xGGGG",
			expected:     100,
		},
		{
			name:         "zero value",
			key:          "TEST_GET_ENV_UINT32_7",
			defaultValue: 100,
			envValue:     "0",
			expected:     0,
		},
		{
			name:         "hex zero",
			key:          "TEST_GET_ENV_UINT32_8",
			defaultValue: 100,
			envValue:     "0x0",
			expected:     0,
		},
		{
			name:         "max uint32",
			key:          "TEST_GET_ENV_UINT32_9",
			defaultValue: 100,
			envValue:     "0xFFFFFFFF",
			expected:     0xFFFFFFFF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			got := GetEnvUint32(tt.key, tt.defaultValue)
			if got != tt.expected {
				t.Errorf("GetEnvUint32(%q, %d) = %d (0x%x), want %d (0x%x)", tt.key, tt.defaultValue, got, got, tt.expected, tt.expected)
			}
		})
	}
}

func TestNewOperatorConfigFromEnv(t *testing.T) {
	t.Run("uses defaults when no env vars set", func(t *testing.T) {
		os.Unsetenv(EnvOperatorNamespace)
		os.Unsetenv("METRICS_BIND_ADDRESS")
		os.Unsetenv("HEALTH_PROBE_BIND_ADDRESS")
		os.Unsetenv("LEADER_ELECT")
		os.Unsetenv("SECURE_METRICS")
		os.Unsetenv("ENABLE_HTTP2")
		os.Unsetenv("ENABLE_SIDECAR_WEBHOOK")
		os.Unsetenv("RETRY_HANDLER_IMAGE")

		config := NewOperatorConfigFromEnv()

		if config.Namespace != DefaultNamespace {
			t.Errorf("Namespace = %q, want %q", config.Namespace, DefaultNamespace)
		}
		if config.MetricsBindAddress != DefaultMetricsBindAddress {
			t.Errorf("MetricsBindAddress = %q, want %q", config.MetricsBindAddress, DefaultMetricsBindAddress)
		}
		if config.HealthProbeBindAddress != DefaultHealthProbeBindAddress {
			t.Errorf("HealthProbeBindAddress = %q, want %q", config.HealthProbeBindAddress, DefaultHealthProbeBindAddress)
		}
		if config.LeaderElect != false {
			t.Error("LeaderElect should default to false")
		}
		if config.LeaderElectionID != DefaultLeaderElectionID {
			t.Errorf("LeaderElectionID = %q, want %q", config.LeaderElectionID, DefaultLeaderElectionID)
		}
		if config.SecureMetrics != true {
			t.Error("SecureMetrics should default to true")
		}
		if config.EnableHTTP2 != false {
			t.Error("EnableHTTP2 should default to false")
		}
		if config.EnableSidecarWebhook != false {
			t.Error("EnableSidecarWebhook should default to false")
		}
		if config.RetryHandlerImage != DefaultRetryHandlerImage {
			t.Errorf("RetryHandlerImage = %q, want %q", config.RetryHandlerImage, DefaultRetryHandlerImage)
		}
	})

	t.Run("uses env vars when set", func(t *testing.T) {
		os.Setenv(EnvOperatorNamespace, "custom-namespace")
		os.Setenv("METRICS_BIND_ADDRESS", ":9090")
		os.Setenv("HEALTH_PROBE_BIND_ADDRESS", ":9091")
		os.Setenv("LEADER_ELECT", "true")
		os.Setenv("SECURE_METRICS", "false")
		os.Setenv("ENABLE_HTTP2", "true")
		os.Setenv("ENABLE_SIDECAR_WEBHOOK", "true")
		os.Setenv("RETRY_HANDLER_IMAGE", "custom/retry:v1")

		defer func() {
			os.Unsetenv(EnvOperatorNamespace)
			os.Unsetenv("METRICS_BIND_ADDRESS")
			os.Unsetenv("HEALTH_PROBE_BIND_ADDRESS")
			os.Unsetenv("LEADER_ELECT")
			os.Unsetenv("SECURE_METRICS")
			os.Unsetenv("ENABLE_HTTP2")
			os.Unsetenv("ENABLE_SIDECAR_WEBHOOK")
			os.Unsetenv("RETRY_HANDLER_IMAGE")
		}()

		config := NewOperatorConfigFromEnv()

		if config.Namespace != "custom-namespace" {
			t.Errorf("Namespace = %q, want %q", config.Namespace, "custom-namespace")
		}
		if config.MetricsBindAddress != ":9090" {
			t.Errorf("MetricsBindAddress = %q, want %q", config.MetricsBindAddress, ":9090")
		}
		if config.HealthProbeBindAddress != ":9091" {
			t.Errorf("HealthProbeBindAddress = %q, want %q", config.HealthProbeBindAddress, ":9091")
		}
		if config.LeaderElect != true {
			t.Error("LeaderElect should be true")
		}
		if config.SecureMetrics != false {
			t.Error("SecureMetrics should be false")
		}
		if config.EnableHTTP2 != true {
			t.Error("EnableHTTP2 should be true")
		}
		if config.EnableSidecarWebhook != true {
			t.Error("EnableSidecarWebhook should be true")
		}
		if config.RetryHandlerImage != "custom/retry:v1" {
			t.Errorf("RetryHandlerImage = %q, want %q", config.RetryHandlerImage, "custom/retry:v1")
		}
	})
}

func TestConstants(t *testing.T) {
	t.Run("environment variable names", func(t *testing.T) {
		if EnvOperatorNamespace != "OPERATOR_NAMESPACE" {
			t.Errorf("EnvOperatorNamespace = %q", EnvOperatorNamespace)
		}
		if EnvNodeName != "NODE_NAME" {
			t.Errorf("EnvNodeName = %q", EnvNodeName)
		}
		if EnvNamespace != "NAMESPACE" {
			t.Errorf("EnvNamespace = %q", EnvNamespace)
		}
	})

	t.Run("default operator settings", func(t *testing.T) {
		if DefaultNamespace != "slingshot-system" {
			t.Errorf("DefaultNamespace = %q", DefaultNamespace)
		}
		if DefaultMetricsBindAddress != ":8443" {
			t.Errorf("DefaultMetricsBindAddress = %q", DefaultMetricsBindAddress)
		}
		if DefaultHealthProbeBindAddress != ":8081" {
			t.Errorf("DefaultHealthProbeBindAddress = %q", DefaultHealthProbeBindAddress)
		}
	})

	t.Run("default images", func(t *testing.T) {
		if DefaultDriverAgentImage == "" {
			t.Error("DefaultDriverAgentImage is empty")
		}
		if DefaultDevicePluginImage == "" {
			t.Error("DefaultDevicePluginImage is empty")
		}
		if DefaultRetryHandlerImage == "" {
			t.Error("DefaultRetryHandlerImage is empty")
		}
		if DefaultMetricsExporterImage == "" {
			t.Error("DefaultMetricsExporterImage is empty")
		}
	})

	t.Run("default device plugin settings", func(t *testing.T) {
		if DefaultDevicePluginResourceName != "hpe.com/cxi" {
			t.Errorf("DefaultDevicePluginResourceName = %q", DefaultDevicePluginResourceName)
		}
		if DefaultDevicePluginSharedCapacity != 100 {
			t.Errorf("DefaultDevicePluginSharedCapacity = %d", DefaultDevicePluginSharedCapacity)
		}
		if DefaultSwitchIDMask != 0xfffc0 {
			t.Errorf("DefaultSwitchIDMask = 0x%x", DefaultSwitchIDMask)
		}
	})

	t.Run("default ports", func(t *testing.T) {
		if DefaultMetricsPort != 9090 {
			t.Errorf("DefaultMetricsPort = %d", DefaultMetricsPort)
		}
		if DefaultHealthPort != 8081 {
			t.Errorf("DefaultHealthPort = %d", DefaultHealthPort)
		}
		if DefaultRetryHandlerHealthPort != 8080 {
			t.Errorf("DefaultRetryHandlerHealthPort = %d", DefaultRetryHandlerHealthPort)
		}
	})

	t.Run("default intervals", func(t *testing.T) {
		if DefaultScrapeInterval != 15*time.Second {
			t.Errorf("DefaultScrapeInterval = %v", DefaultScrapeInterval)
		}
		if DefaultPollInterval != 30*time.Second {
			t.Errorf("DefaultPollInterval = %v", DefaultPollInterval)
		}
	})

	t.Run("default paths", func(t *testing.T) {
		if DefaultCXIRHPath != "/usr/bin/cxi_rh" {
			t.Errorf("DefaultCXIRHPath = %q", DefaultCXIRHPath)
		}
	})
}
