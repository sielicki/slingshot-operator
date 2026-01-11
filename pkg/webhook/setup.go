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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// WebhookConfig holds configuration for all webhooks
type WebhookConfig struct {
	SidecarInjector        SidecarInjectorConfig
	EnableSidecarInjector  bool
	EnableCXIDriverWebhook bool
}

// SetupWithManager registers all webhooks with the manager
func SetupWithManager(mgr manager.Manager, config WebhookConfig) error {
	// Register sidecar injector (mutating webhook for pods)
	if config.EnableSidecarInjector {
		injector := NewSidecarInjector(mgr.GetClient(), config.SidecarInjector)
		mgr.GetWebhookServer().Register("/mutate-v1-pod", &webhook.Admission{
			Handler: injector,
		})
	}

	// Register CXIDriver validator (validating webhook)
	if config.EnableCXIDriverWebhook {
		validator := NewCXIDriverValidator(mgr.GetClient())
		mgr.GetWebhookServer().Register("/validate-cxi-hpe-com-v1-cxidriver", &webhook.Admission{
			Handler: validator,
		})
	}

	return nil
}

// SetupSidecarInjector registers only the sidecar injector webhook (legacy function)
func SetupSidecarInjector(mgr manager.Manager, config SidecarInjectorConfig) error {
	return SetupWithManager(mgr, WebhookConfig{
		SidecarInjector:       config,
		EnableSidecarInjector: true,
	})
}
