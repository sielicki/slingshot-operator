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

//nolint:dupl // similar test structure is intentional for different scenarios
package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	cxiv1 "github.com/sielicki/slingshot-operator/api/v1"
)

func TestCXIDriverValidator_Handle_ValidDriver(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			Source: cxiv1.DriverSourceSpec{
				Type: cxiv1.DriverSourcePreinstalled,
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("Expected valid driver to be allowed, got denied: %s", resp.Result.Message)
	}
}

func TestCXIDriverValidator_Handle_MissingVersion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "",
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("Expected missing version to be denied")
	}
}

func TestCXIDriverValidator_Handle_InvalidSemver(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	tests := []struct {
		name       string
		version    string
		shouldWarn bool
		shouldDeny bool
	}{
		{"valid semver", "1.0.0", false, false},
		{"valid semver with v", "v1.0.0", false, false},
		{"valid prerelease", "1.0.0-alpha", false, false},
		{"valid with build", "1.0.0+build", false, false},
		{"invalid format", "latest", true, false},
		{"missing patch", "1.0", true, false},
		{"just major", "1", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-driver",
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: tt.version,
				},
			}

			raw, _ := json.Marshal(driver)
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: raw,
					},
				},
			}

			resp := validator.Handle(context.Background(), req)

			if tt.shouldDeny && resp.Allowed {
				t.Errorf("Expected version %q to be denied", tt.version)
			}
			if !tt.shouldDeny && !resp.Allowed {
				t.Errorf("Expected version %q to be allowed, got: %s", tt.version, resp.Result.Message)
			}
			if tt.shouldWarn && len(resp.Warnings) == 0 {
				t.Errorf("Expected warning for version %q", tt.version)
			}
		})
	}
}

func TestCXIDriverValidator_Handle_DKMSMissingRepository(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			Source: cxiv1.DriverSourceSpec{
				Type:       cxiv1.DriverSourceDKMS,
				Repository: "",
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("Expected DKMS without repository to be denied")
	}
}

func TestCXIDriverValidator_Handle_PrebuiltMissingCache(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			Source: cxiv1.DriverSourceSpec{
				Type:          cxiv1.DriverSourcePrebuilt,
				PrebuiltCache: "",
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("Expected prebuilt without cache to be denied")
	}
}

func TestCXIDriverValidator_Handle_InvalidRetryHandlerMode(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			RetryHandler: cxiv1.RetryHandlerSpec{
				Enabled: true,
				Mode:    "invalid-mode",
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("Expected invalid retry handler mode to be denied")
	}
}

func TestCXIDriverValidator_Handle_InvalidDevicePluginSharingMode(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			DevicePlugin: cxiv1.DevicePluginSpec{
				Enabled:     true,
				SharingMode: "invalid-mode",
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("Expected invalid sharing mode to be denied")
	}
}

func TestCXIDriverValidator_Handle_InvalidHealthCheckTimeout(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			UpdateStrategy: cxiv1.UpdateStrategySpec{
				HealthCheckTimeout: "invalid-duration",
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("Expected invalid duration to be denied")
	}
}

func TestCXIDriverValidator_Handle_HighSharedCapacityWarning(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			DevicePlugin: cxiv1.DevicePluginSpec{
				Enabled:        true,
				SharingMode:    cxiv1.DeviceSharingModeShared,
				SharedCapacity: 2000,
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Error("Expected high capacity to be allowed with warning")
	}
	if len(resp.Warnings) == 0 {
		t.Error("Expected warning for high shared capacity")
	}
}

func TestCXIDriverValidator_Handle_KernelModeWarning(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	driver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			Source: cxiv1.DriverSourceSpec{
				Type: cxiv1.DriverSourcePreinstalled,
			},
			RetryHandler: cxiv1.RetryHandlerSpec{
				Enabled: true,
				Mode:    cxiv1.RetryHandlerModeKernel,
			},
		},
	}

	raw, _ := json.Marshal(driver)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Error("Expected kernel mode with preinstalled to be allowed with warning")
	}
	if len(resp.Warnings) == 0 {
		t.Error("Expected warning for kernel mode with preinstalled driver")
	}
}

func TestCXIDriverValidator_Handle_UpdateResourceNameChangeWarning(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	oldDriver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			DevicePlugin: cxiv1.DevicePluginSpec{
				Enabled:      true,
				ResourceName: "hpe.com/cxi",
			},
		},
	}

	newDriver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			DevicePlugin: cxiv1.DevicePluginSpec{
				Enabled:      true,
				ResourceName: "hpe.com/cxi-v2",
			},
		},
	}

	oldRaw, _ := json.Marshal(oldDriver)
	newRaw, _ := json.Marshal(newDriver)

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Object: runtime.RawExtension{
				Raw: newRaw,
			},
			OldObject: runtime.RawExtension{
				Raw: oldRaw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Error("Expected resource name change to be allowed with warning")
	}
	if len(resp.Warnings) == 0 {
		t.Error("Expected warning for resource name change")
	}
}

func TestCXIDriverValidator_Handle_UpdateModeChangeWarning(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	oldDriver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			RetryHandler: cxiv1.RetryHandlerSpec{
				Enabled: true,
				Mode:    cxiv1.RetryHandlerModeDaemonSet,
			},
		},
	}

	newDriver := &cxiv1.CXIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-driver",
		},
		Spec: cxiv1.CXIDriverSpec{
			Version: "1.0.0",
			RetryHandler: cxiv1.RetryHandlerSpec{
				Enabled: true,
				Mode:    cxiv1.RetryHandlerModeSidecar,
			},
		},
	}

	oldRaw, _ := json.Marshal(oldDriver)
	newRaw, _ := json.Marshal(newDriver)

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Object: runtime.RawExtension{
				Raw: newRaw,
			},
			OldObject: runtime.RawExtension{
				Raw: oldRaw,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Error("Expected mode change to be allowed with warning")
	}
	if len(resp.Warnings) == 0 {
		t.Error("Expected warning for retry handler mode change")
	}
}

func TestCXIDriverValidator_Handle_DecodeError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: []byte("invalid json"),
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("Expected decode error to deny request")
	}
	if resp.Result.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.Result.Code)
	}
}

func TestCXIDriverValidator_Handle_ValidRetryHandlerModes(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = cxiv1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := NewCXIDriverValidator(client, scheme)

	modes := []cxiv1.RetryHandlerMode{
		cxiv1.RetryHandlerModeDaemonSet,
		cxiv1.RetryHandlerModeSidecar,
		cxiv1.RetryHandlerModeKernel,
		cxiv1.RetryHandlerModeNone,
		"",
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-driver",
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					RetryHandler: cxiv1.RetryHandlerSpec{
						Enabled: true,
						Mode:    mode,
					},
				},
			}

			raw, _ := json.Marshal(driver)
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: raw,
					},
				},
			}

			resp := validator.Handle(context.Background(), req)
			if !resp.Allowed {
				t.Errorf("Expected mode %q to be allowed, got: %s", mode, resp.Result.Message)
			}
		})
	}
}
