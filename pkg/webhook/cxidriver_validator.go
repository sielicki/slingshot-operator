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

package webhook

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	cxiv1 "github.com/sielicki/slingshot-operator/api/v1"
)

var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$`)

type CXIDriverValidator struct {
	client  client.Client
	decoder admission.Decoder
}

func NewCXIDriverValidator(c client.Client) *CXIDriverValidator {
	return &CXIDriverValidator{
		client: c,
	}
}

func (v *CXIDriverValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	driver := &cxiv1.CXIDriver{}

	if err := v.decoder.Decode(req, driver); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	var warnings []string
	var errs []string

	// Validate version format (semver)
	if driver.Spec.Version == "" {
		errs = append(errs, "spec.version is required")
	} else if !semverRegex.MatchString(driver.Spec.Version) {
		warnings = append(warnings,
			fmt.Sprintf("spec.version %q does not follow semver format (e.g., 1.8.3)", driver.Spec.Version))
	}

	// Validate driver source configuration
	switch driver.Spec.Source.Type {
	case cxiv1.DriverSourceDKMS:
		if driver.Spec.Source.Repository == "" {
			errs = append(errs, "spec.source.repository is required when source.type is 'dkms'")
		}
	case cxiv1.DriverSourcePrebuilt:
		if driver.Spec.Source.PrebuiltCache == "" {
			errs = append(errs, "spec.source.prebuiltCache is required when source.type is 'prebuilt'")
		}
	case cxiv1.DriverSourcePreinstalled, "":
		// No additional validation needed
	default:
		errs = append(errs,
			fmt.Sprintf("spec.source.type must be one of: dkms, prebuilt, preinstalled (got %q)", driver.Spec.Source.Type))
	}

	// Validate retry handler configuration
	if driver.Spec.RetryHandler.Enabled {
		switch driver.Spec.RetryHandler.Mode {
		case cxiv1.RetryHandlerModeDaemonSet, cxiv1.RetryHandlerModeSidecar,
			cxiv1.RetryHandlerModeKernel, cxiv1.RetryHandlerModeNone, "":
			// Valid modes
		default:
			errs = append(errs, fmt.Sprintf(
				"spec.retryHandler.mode must be one of: daemonset, sidecar, kernel, none (got %q)",
				driver.Spec.RetryHandler.Mode))
		}

		// Warn if sidecar mode is selected but no sidecar config
		if driver.Spec.RetryHandler.Mode == cxiv1.RetryHandlerModeSidecar && driver.Spec.RetryHandler.Sidecar == nil {
			warnings = append(warnings, "spec.retryHandler.sidecar is not configured; using defaults")
		}
	}

	// Validate device plugin configuration
	if driver.Spec.DevicePlugin.Enabled {
		switch driver.Spec.DevicePlugin.SharingMode {
		case cxiv1.DeviceSharingModeShared, cxiv1.DeviceSharingModeExclusive, "":
			// Valid modes
		default:
			errs = append(errs, fmt.Sprintf(
				"spec.devicePlugin.sharingMode must be one of: shared, exclusive (got %q)",
				driver.Spec.DevicePlugin.SharingMode))
		}

		sharedMode := driver.Spec.DevicePlugin.SharingMode == cxiv1.DeviceSharingModeShared
		if driver.Spec.DevicePlugin.SharedCapacity < 1 && sharedMode {
			errs = append(errs, "spec.devicePlugin.sharedCapacity must be at least 1 when sharingMode is 'shared'")
		}

		if driver.Spec.DevicePlugin.SharedCapacity > 1000 {
			warnings = append(warnings, fmt.Sprintf(
				"spec.devicePlugin.sharedCapacity=%d is very high; consider if this is intentional",
				driver.Spec.DevicePlugin.SharedCapacity))
		}
	}

	// Validate update strategy
	if driver.Spec.UpdateStrategy.HealthCheckTimeout != "" {
		if _, err := time.ParseDuration(driver.Spec.UpdateStrategy.HealthCheckTimeout); err != nil {
			errs = append(errs, fmt.Sprintf("spec.updateStrategy.healthCheckTimeout is not a valid duration: %v", err))
		}
	}

	// Check for conflicting configurations
	kernelMode := driver.Spec.RetryHandler.Mode == cxiv1.RetryHandlerModeKernel
	preinstalled := driver.Spec.Source.Type == cxiv1.DriverSourcePreinstalled
	if kernelMode && preinstalled {
		warnings = append(warnings,
			"kernel retry handler mode requires driver to be built with CONFIG_CXI_RETRY_HANDLER=y")
	}

	// Validate on UPDATE: check for breaking changes
	if req.Operation == admissionv1.Update {
		oldDriver := &cxiv1.CXIDriver{}
		if err := v.decoder.DecodeRaw(req.OldObject, oldDriver); err == nil {
			// Warn about device plugin resource name changes
			if oldDriver.Spec.DevicePlugin.ResourceName != "" &&
				driver.Spec.DevicePlugin.ResourceName != oldDriver.Spec.DevicePlugin.ResourceName {
				warnings = append(warnings, fmt.Sprintf(
					"changing devicePlugin.resourceName from %q to %q will require workloads to be updated",
					oldDriver.Spec.DevicePlugin.ResourceName, driver.Spec.DevicePlugin.ResourceName))
			}

			// Warn about mode changes
			if oldDriver.Spec.RetryHandler.Mode != "" &&
				driver.Spec.RetryHandler.Mode != oldDriver.Spec.RetryHandler.Mode {
				warnings = append(warnings, fmt.Sprintf(
					"changing retryHandler.mode from %q to %q may disrupt running workloads",
					oldDriver.Spec.RetryHandler.Mode, driver.Spec.RetryHandler.Mode))
			}
		}
	}

	if len(errs) > 0 {
		return admission.Denied(fmt.Sprintf("validation failed: %v", errs))
	}

	if len(warnings) > 0 {
		return admission.Allowed("").WithWarnings(warnings...)
	}

	return admission.Allowed("")
}

func (v *CXIDriverValidator) InjectDecoder(d admission.Decoder) error {
	v.decoder = d
	return nil
}
