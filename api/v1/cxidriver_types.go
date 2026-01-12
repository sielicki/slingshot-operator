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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DriverSourceType defines how the driver is installed
// +kubebuilder:validation:Enum=dkms;prebuilt;preinstalled
type DriverSourceType string

const (
	DriverSourceDKMS         DriverSourceType = "dkms"
	DriverSourcePrebuilt     DriverSourceType = "prebuilt"
	DriverSourcePreinstalled DriverSourceType = "preinstalled"
)

// RetryHandlerMode defines how the retry handler is deployed
// +kubebuilder:validation:Enum=daemonset;sidecar;kernel;none
type RetryHandlerMode string

const (
	RetryHandlerModeDaemonSet RetryHandlerMode = "daemonset"
	RetryHandlerModeSidecar   RetryHandlerMode = "sidecar"
	RetryHandlerModeKernel    RetryHandlerMode = "kernel"
	RetryHandlerModeNone      RetryHandlerMode = "none"
)

// DeviceSharingMode defines how CXI devices are shared among pods
// +kubebuilder:validation:Enum=shared;exclusive
type DeviceSharingMode string

const (
	DeviceSharingModeShared    DeviceSharingMode = "shared"
	DeviceSharingModeExclusive DeviceSharingMode = "exclusive"
)

// DKMSSourceSpec defines DKMS-specific driver source configuration
type DKMSSourceSpec struct {
	// Tag is the release tag to use for all HPE Slingshot repos
	// e.g., "release/shs-12.0.2"
	// URLs for shs-cxi-driver, ss-sbl, and ss-link are auto-generated from this tag
	Tag string `json:"tag"`
}

// DriverSourceSpec defines where to get the driver from
type DriverSourceSpec struct {
	// Type specifies how the driver is installed
	// +kubebuilder:default=dkms
	Type DriverSourceType `json:"type,omitempty"`

	// Repository is the container image repository for driver source
	// +optional
	Repository string `json:"repository,omitempty"`

	// PrebuiltCache is the container image repository for pre-built modules
	// +optional
	PrebuiltCache string `json:"prebuiltCache,omitempty"`

	// DKMS contains DKMS-specific source configuration for HPE Slingshot drivers
	// When set, enables multi-repo builds with ss-sbl and ss-link dependencies
	// +optional
	DKMS *DKMSSourceSpec `json:"dkms,omitempty"`
}

// RetryHandlerDaemonSetSpec defines DaemonSet-specific retry handler options
type RetryHandlerDaemonSetSpec struct {
	// Resources defines resource requirements for the retry handler DaemonSet
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector for the retry handler DaemonSet
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for the retry handler DaemonSet
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// RetryHandlerSidecarSpec defines sidecar-specific retry handler options
type RetryHandlerSidecarSpec struct {
	// Resources defines resource requirements for the sidecar container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// RetryHandlerSpec defines retry handler configuration
type RetryHandlerSpec struct {
	// Enabled specifies whether the retry handler is enabled
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Mode specifies the retry handler deployment mode
	// +kubebuilder:default=daemonset
	Mode RetryHandlerMode `json:"mode,omitempty"`

	// Image is the container image for the retry handler
	// +optional
	Image string `json:"image,omitempty"`

	// DaemonSet contains DaemonSet-specific configuration
	// +optional
	DaemonSet *RetryHandlerDaemonSetSpec `json:"daemonset,omitempty"`

	// Sidecar contains sidecar-specific configuration
	// +optional
	Sidecar *RetryHandlerSidecarSpec `json:"sidecar,omitempty"`
}

// DevicePluginSpec defines device plugin configuration
type DevicePluginSpec struct {
	// Enabled specifies whether the device plugin is enabled
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// ResourceName is the extended resource name (e.g., "hpe.com/cxi")
	// +kubebuilder:default="hpe.com/cxi"
	ResourceName string `json:"resourceName,omitempty"`

	// Image is the container image for the device plugin
	// +optional
	Image string `json:"image,omitempty"`

	// SharingMode defines how devices are shared among pods
	// +kubebuilder:default=shared
	SharingMode DeviceSharingMode `json:"sharingMode,omitempty"`

	// SharedCapacity is the max pods per node when sharingMode is "shared"
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	SharedCapacity int32 `json:"sharedCapacity,omitempty"`
}

// MetricsExporterSpec defines metrics exporter configuration
type MetricsExporterSpec struct {
	// Enabled specifies whether the metrics exporter is enabled
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Image is the container image for the metrics exporter
	// +optional
	Image string `json:"image,omitempty"`

	// Port is the port for Prometheus metrics endpoint
	// +kubebuilder:default=9090
	Port int32 `json:"port,omitempty"`

	// Resources defines resource requirements for the metrics exporter
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// UpdateStrategySpec defines how driver updates are rolled out
type UpdateStrategySpec struct {
	// Type specifies the update strategy type
	// +kubebuilder:default=RollingUpdate
	// +kubebuilder:validation:Enum=RollingUpdate;OnDelete
	Type string `json:"type,omitempty"`

	// MaxUnavailable is the maximum number of nodes that can be unavailable during update
	// +kubebuilder:default="10%"
	MaxUnavailable string `json:"maxUnavailable,omitempty"`

	// RollbackOnFailure enables automatic rollback if update fails health checks
	// +kubebuilder:default=true
	RollbackOnFailure bool `json:"rollbackOnFailure,omitempty"`

	// HealthCheckTimeout is the duration to wait for health checks after update
	// +kubebuilder:default="5m"
	HealthCheckTimeout string `json:"healthCheckTimeout,omitempty"`
}

// CXIDriverSpec defines the desired state of CXIDriver
type CXIDriverSpec struct {
	// Version is the driver version to install
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// Source defines where to get the driver from
	// +optional
	Source DriverSourceSpec `json:"source,omitempty"`

	// RetryHandler defines retry handler configuration
	// +optional
	RetryHandler RetryHandlerSpec `json:"retryHandler,omitempty"`

	// DevicePlugin defines device plugin configuration
	// +optional
	DevicePlugin DevicePluginSpec `json:"devicePlugin,omitempty"`

	// MetricsExporter defines metrics exporter configuration
	// +optional
	MetricsExporter MetricsExporterSpec `json:"metricsExporter,omitempty"`

	// NodeSelector selects which nodes the operator manages
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// UpdateStrategy defines how driver updates are rolled out
	// +optional
	UpdateStrategy UpdateStrategySpec `json:"updateStrategy,omitempty"`
}

// CXIDriverStatus defines the observed state of CXIDriver
type CXIDriverStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Ready is the number of nodes with driver ready
	Ready int32 `json:"ready,omitempty"`

	// Updating is the number of nodes currently updating
	Updating int32 `json:"updating,omitempty"`

	// Failed is the number of nodes in failed state
	Failed int32 `json:"failed,omitempty"`

	// DriverVersion is the currently deployed driver version
	// +optional
	DriverVersion string `json:"driverVersion,omitempty"`

	// RetryHandlerMode is the currently active retry handler mode
	// +optional
	RetryHandlerMode RetryHandlerMode `json:"retryHandlerMode,omitempty"`

	// WebhookReady indicates if the sidecar injection webhook is ready (when mode=sidecar)
	WebhookReady bool `json:"webhookReady,omitempty"`

	// Conditions represent the current state of the CXIDriver resource
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Updating",type="integer",JSONPath=".status.updating"
// +kubebuilder:printcolumn:name="Failed",type="integer",JSONPath=".status.failed"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CXIDriver is the Schema for the cxidrivers API.
// It defines the cluster-wide configuration for CXI driver management.
type CXIDriver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CXIDriverSpec   `json:"spec,omitempty"`
	Status CXIDriverStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CXIDriverList contains a list of CXIDriver
type CXIDriverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CXIDriver `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CXIDriver{}, &CXIDriverList{})
}
