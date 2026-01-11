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
	"testing"
)

func TestWebhookConfig_Defaults(t *testing.T) {
	config := WebhookConfig{}

	if config.EnableSidecarInjector {
		t.Error("EnableSidecarInjector should default to false")
	}
	if config.EnableCXIDriverWebhook {
		t.Error("EnableCXIDriverWebhook should default to false")
	}
}

func TestWebhookConfig_SidecarInjectorOnly(t *testing.T) {
	config := WebhookConfig{
		EnableSidecarInjector: true,
		SidecarInjector: SidecarInjectorConfig{
			RetryHandlerImage: "test-image:latest",
		},
	}

	if !config.EnableSidecarInjector {
		t.Error("EnableSidecarInjector should be true")
	}
	if config.EnableCXIDriverWebhook {
		t.Error("EnableCXIDriverWebhook should be false")
	}
	if config.SidecarInjector.RetryHandlerImage != "test-image:latest" {
		t.Errorf("RetryHandlerImage = %q, want %q", config.SidecarInjector.RetryHandlerImage, "test-image:latest")
	}
}

func TestWebhookConfig_CXIDriverWebhookOnly(t *testing.T) {
	config := WebhookConfig{
		EnableCXIDriverWebhook: true,
	}

	if config.EnableSidecarInjector {
		t.Error("EnableSidecarInjector should be false")
	}
	if !config.EnableCXIDriverWebhook {
		t.Error("EnableCXIDriverWebhook should be true")
	}
}

func TestWebhookConfig_BothWebhooksEnabled(t *testing.T) {
	config := WebhookConfig{
		EnableSidecarInjector:  true,
		EnableCXIDriverWebhook: true,
		SidecarInjector: SidecarInjectorConfig{
			RetryHandlerImage: "retry-handler:v1",
		},
	}

	if !config.EnableSidecarInjector {
		t.Error("EnableSidecarInjector should be true")
	}
	if !config.EnableCXIDriverWebhook {
		t.Error("EnableCXIDriverWebhook should be true")
	}
}

func TestSidecarInjectorConfig_EmptyImage(t *testing.T) {
	config := SidecarInjectorConfig{
		RetryHandlerImage: "",
	}

	if config.RetryHandlerImage != "" {
		t.Errorf("RetryHandlerImage should be empty, got %q", config.RetryHandlerImage)
	}
}

func TestSidecarInjectorConfig_CustomImage(t *testing.T) {
	config := SidecarInjectorConfig{
		RetryHandlerImage: "ghcr.io/custom/retry-handler:v2.0.0",
	}

	if config.RetryHandlerImage != "ghcr.io/custom/retry-handler:v2.0.0" {
		t.Errorf("RetryHandlerImage = %q, want %q", config.RetryHandlerImage, "ghcr.io/custom/retry-handler:v2.0.0")
	}
}
