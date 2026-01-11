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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CXIDeviceType indicates the type of CXI device
// +kubebuilder:validation:Enum=pf;vf
type CXIDeviceType string

const (
	CXIDeviceTypePF CXIDeviceType = "pf"
	CXIDeviceTypeVF CXIDeviceType = "vf"
)

// LinkState indicates the link state of the device
// +kubebuilder:validation:Enum=up;down;unknown
type LinkState string

const (
	LinkStateUp      LinkState = "up"
	LinkStateDown    LinkState = "down"
	LinkStateUnknown LinkState = "unknown"
)

// HealthStatus indicates the health of the device
// +kubebuilder:validation:Enum=Healthy;Degraded;Unhealthy;Unknown
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "Healthy"
	HealthStatusDegraded  HealthStatus = "Degraded"
	HealthStatusUnhealthy HealthStatus = "Unhealthy"
	HealthStatusUnknown   HealthStatus = "Unknown"
)

// RetryHandlerStats contains retry handler statistics
type RetryHandlerStats struct {
	// TotalRetries is the total number of retries performed
	TotalRetries int64 `json:"totalRetries,omitempty"`

	// TimeoutRetries is the number of timeout-triggered retries
	TimeoutRetries int64 `json:"timeoutRetries,omitempty"`

	// NackRetries is the number of NACK-triggered retries
	NackRetries int64 `json:"nackRetries,omitempty"`

	// PendingRetries is the current number of pending retries
	PendingRetries int32 `json:"pendingRetries,omitempty"`
}

// RetryHandlerStatus contains retry handler status for this device
type RetryHandlerStatus struct {
	// Enabled indicates if retry handling is active for this device
	Enabled bool `json:"enabled,omitempty"`

	// Policy is the active retry policy name
	// +optional
	Policy string `json:"policy,omitempty"`

	// Stats contains retry statistics
	// +optional
	Stats RetryHandlerStats `json:"stats,omitempty"`
}

// DeviceHealth contains health information for the device
type DeviceHealth struct {
	// Status is the overall health status
	Status HealthStatus `json:"status,omitempty"`

	// LastCheck is the timestamp of the last health check
	// +optional
	LastCheck *metav1.Time `json:"lastCheck,omitempty"`

	// Message provides additional context about the health status
	// +optional
	Message string `json:"message,omitempty"`
}

// CXIDeviceSpec defines the desired state of CXIDevice.
// This is intentionally minimal as CXIDevice is primarily a status resource.
type CXIDeviceSpec struct {
	// No user-configurable fields - CXIDevice is auto-managed by the operator
}

// CXIDeviceStatus defines the observed state of CXIDevice
type CXIDeviceStatus struct {
	// Node is the name of the node where this device exists
	Node string `json:"node,omitempty"`

	// Device is the device name (e.g., "cxi0")
	Device string `json:"device,omitempty"`

	// Type indicates if this is a physical function (pf) or virtual function (vf)
	// +kubebuilder:default=pf
	Type CXIDeviceType `json:"type,omitempty"`

	// PCIAddress is the PCI address of the device (e.g., "0000:41:00.0")
	PCIAddress string `json:"pciAddress,omitempty"`

	// NUMANode is the NUMA node the device is attached to
	NUMANode int32 `json:"numaNode,omitempty"`

	// NID is the Node ID (20-bit fabric address) for this device
	// +optional
	NID uint32 `json:"nid,omitempty"`

	// SwitchID is the switch ID derived from NID for topology awareness
	// This identifies which switch in the dragonfly topology this device connects to
	// +optional
	SwitchID int32 `json:"switchID,omitempty"`

	// LinkSpeed is the link speed (e.g., "200Gbps")
	// +optional
	LinkSpeed string `json:"linkSpeed,omitempty"`

	// LinkState is the current link state
	LinkState LinkState `json:"linkState,omitempty"`

	// DriverVersion is the loaded driver version
	// +optional
	DriverVersion string `json:"driverVersion,omitempty"`

	// CassiniGeneration is the hardware generation (1, 2, or 3)
	// +optional
	CassiniGeneration int32 `json:"cassiniGeneration,omitempty"`

	// RetryHandler contains retry handler status for this device
	// +optional
	RetryHandler RetryHandlerStatus `json:"retryHandler,omitempty"`

	// Health contains health information
	// +optional
	Health DeviceHealth `json:"health,omitempty"`

	// Conditions represent the current state of the CXIDevice
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Node",type="string",JSONPath=".status.node"
// +kubebuilder:printcolumn:name="Device",type="string",JSONPath=".status.device"
// +kubebuilder:printcolumn:name="NUMA",type="integer",JSONPath=".status.numaNode"
// +kubebuilder:printcolumn:name="Switch",type="integer",JSONPath=".status.switchID"
// +kubebuilder:printcolumn:name="Link",type="string",JSONPath=".status.linkState"
// +kubebuilder:printcolumn:name="Health",type="string",JSONPath=".status.health.status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CXIDevice is the Schema for the cxidevices API.
// It represents a discovered CXI device on a node and is auto-managed by the operator.
type CXIDevice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CXIDeviceSpec   `json:"spec,omitempty"`
	Status CXIDeviceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CXIDeviceList contains a list of CXIDevice
type CXIDeviceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CXIDevice `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CXIDevice{}, &CXIDeviceList{})
}
