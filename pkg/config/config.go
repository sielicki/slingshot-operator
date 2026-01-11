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
	"strconv"
	"time"
)

const (
	// EnvOperatorNamespace is the environment variable for the operator namespace
	EnvOperatorNamespace = "OPERATOR_NAMESPACE"
	// EnvNodeName is the environment variable for the node name
	EnvNodeName = "NODE_NAME"
	// EnvNamespace is the environment variable for the namespace (used by agents)
	EnvNamespace = "NAMESPACE"
)

const (
	// DefaultNamespace is the default namespace for the operator
	DefaultNamespace = "slingshot-system"
	// DefaultMetricsBindAddress is the default metrics bind address
	DefaultMetricsBindAddress = ":8443"
	// DefaultHealthProbeBindAddress is the default health probe bind address
	DefaultHealthProbeBindAddress = ":8081"
	// DefaultLeaderElectionID is the leader election ID
	DefaultLeaderElectionID = "bf7f9b28.hpe.com"
)

const (
	// DefaultDriverAgentImage is the default image for the driver agent
	DefaultDriverAgentImage = "ghcr.io/sielicki/slingshot-operator/driver-agent:latest"
	// DefaultDevicePluginImage is the default image for the device plugin
	DefaultDevicePluginImage = "ghcr.io/sielicki/slingshot-operator/device-plugin:latest"
	// DefaultRetryHandlerImage is the default image for the retry handler
	DefaultRetryHandlerImage = "ghcr.io/sielicki/slingshot-operator/retry-handler:latest"
	// DefaultMetricsExporterImage is the default image for the metrics exporter
	DefaultMetricsExporterImage = "ghcr.io/sielicki/slingshot-operator/metrics-exporter:latest"
)

const (
	// DefaultDevicePluginResourceName is the default Kubernetes resource name
	DefaultDevicePluginResourceName = "hpe.com/cxi"
	// DefaultDevicePluginSharedCapacity is the default shared capacity
	DefaultDevicePluginSharedCapacity = 100
	// DefaultSwitchIDMask extracts switch ID from NID for topology awareness.
	// Default mask 0xfffc0 uses bits 6-19 for switch ID.
	// This matches the default in cxi_rh.conf.
	DefaultSwitchIDMask = 0xfffc0
)

const (
	// DefaultMetricsPort is the default port for the metrics exporter
	DefaultMetricsPort = 9090
	// DefaultHealthPort is the default port for health checks
	DefaultHealthPort = 8081
	// DefaultRetryHandlerHealthPort is the default port for retry handler health checks
	DefaultRetryHandlerHealthPort = 8080
)

const (
	// DefaultScrapeInterval is the default metrics scrape interval
	DefaultScrapeInterval = 15 * time.Second
	// DefaultPollInterval is the default device polling interval
	DefaultPollInterval = 30 * time.Second
)

const (
	// DefaultCXIRHPath is the default path to the cxi_rh binary
	DefaultCXIRHPath = "/usr/bin/cxi_rh"
)

// GetEnv returns the value of an environment variable or a default value
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvInt returns the value of an environment variable as an int or a default value
func GetEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// GetEnvBool returns the value of an environment variable as a bool or a default value
func GetEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// GetEnvDuration returns the value of an environment variable as a duration or a default value
func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// GetEnvUint32 returns the value of an environment variable as uint32 or a default value.
// Supports both decimal and hex (0x prefix) formats.
func GetEnvUint32(key string, defaultValue uint32) uint32 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var parsed uint64
	var err error
	if len(value) > 2 && (value[:2] == "0x" || value[:2] == "0X") {
		parsed, err = strconv.ParseUint(value[2:], 16, 32)
	} else {
		parsed, err = strconv.ParseUint(value, 10, 32)
	}
	if err != nil {
		return defaultValue
	}
	return uint32(parsed)
}

// OperatorConfig holds configuration for the main operator
type OperatorConfig struct {
	Namespace              string
	MetricsBindAddress     string
	HealthProbeBindAddress string
	LeaderElect            bool
	LeaderElectionID       string
	SecureMetrics          bool
	EnableHTTP2            bool
	EnableSidecarWebhook   bool
	RetryHandlerImage      string
}

// NewOperatorConfigFromEnv creates an OperatorConfig from environment variables
func NewOperatorConfigFromEnv() OperatorConfig {
	return OperatorConfig{
		Namespace:              GetEnv(EnvOperatorNamespace, DefaultNamespace),
		MetricsBindAddress:     GetEnv("METRICS_BIND_ADDRESS", DefaultMetricsBindAddress),
		HealthProbeBindAddress: GetEnv("HEALTH_PROBE_BIND_ADDRESS", DefaultHealthProbeBindAddress),
		LeaderElect:            GetEnvBool("LEADER_ELECT", false),
		LeaderElectionID:       DefaultLeaderElectionID,
		SecureMetrics:          GetEnvBool("SECURE_METRICS", true),
		EnableHTTP2:            GetEnvBool("ENABLE_HTTP2", false),
		EnableSidecarWebhook:   GetEnvBool("ENABLE_SIDECAR_WEBHOOK", false),
		RetryHandlerImage:      GetEnv("RETRY_HANDLER_IMAGE", DefaultRetryHandlerImage),
	}
}
