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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	annotationInjectSidecar   = "slingshot.hpe.com/inject-retry-handler"
	annotationSidecarInjected = "slingshot.hpe.com/retry-handler-injected"
	labelManagedBy            = "slingshot.hpe.com/managed-by"

	defaultRetryHandlerImage = "ghcr.io/sielicki/slingshot-operator/retry-handler:latest"
	defaultCXIRHPath         = "/usr/bin/cxi_rh"
	sidecarContainerName     = "cxi-retry-handler"
)

type SidecarInjectorConfig struct {
	RetryHandlerImage string
	CXIRHPath         string
	ResourceName      string
	UseNativeSidecar  bool // Use K8s 1.28+ native sidecar (init container with restartPolicy: Always)
}

type SidecarInjector struct {
	Client  client.Client
	decoder admission.Decoder
	Config  SidecarInjectorConfig
}

func NewSidecarInjector(c client.Client, config SidecarInjectorConfig) *SidecarInjector {
	if config.RetryHandlerImage == "" {
		config.RetryHandlerImage = defaultRetryHandlerImage
	}
	if config.CXIRHPath == "" {
		config.CXIRHPath = defaultCXIRHPath
	}
	if config.ResourceName == "" {
		config.ResourceName = "hpe.com/cxi"
	}

	return &SidecarInjector{
		Client: c,
		Config: config,
	}
}

func (s *SidecarInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := s.decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !s.shouldInject(pod) {
		return admission.Allowed("no injection needed")
	}

	if s.alreadyInjected(pod) {
		return admission.Allowed("already injected")
	}

	injectedPod := s.injectSidecar(pod)

	marshaledPod, err := json.Marshal(injectedPod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (s *SidecarInjector) shouldInject(pod *corev1.Pod) bool {
	if val, ok := pod.Annotations[annotationInjectSidecar]; ok {
		return val == "true" || val == "enabled"
	}

	return s.requestsCXIDevice(pod)
}

func (s *SidecarInjector) requestsCXIDevice(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if s.containerRequestsCXI(&container) {
			return true
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if s.containerRequestsCXI(&container) {
			return true
		}
	}
	return false
}

func (s *SidecarInjector) containerRequestsCXI(container *corev1.Container) bool {
	for resourceName := range container.Resources.Limits {
		if strings.HasPrefix(string(resourceName), "hpe.com/cxi") {
			return true
		}
	}
	for resourceName := range container.Resources.Requests {
		if strings.HasPrefix(string(resourceName), "hpe.com/cxi") {
			return true
		}
	}
	return false
}

func (s *SidecarInjector) alreadyInjected(pod *corev1.Pod) bool {
	if val, ok := pod.Annotations[annotationSidecarInjected]; ok && val == "true" {
		return true
	}

	// Check regular containers
	for _, container := range pod.Spec.Containers {
		if container.Name == sidecarContainerName {
			return true
		}
	}

	// Check init containers (for native sidecar mode)
	for _, container := range pod.Spec.InitContainers {
		if container.Name == sidecarContainerName {
			return true
		}
	}

	return false
}

func (s *SidecarInjector) injectSidecar(pod *corev1.Pod) *corev1.Pod {
	injectedPod := pod.DeepCopy()

	if injectedPod.Annotations == nil {
		injectedPod.Annotations = make(map[string]string)
	}
	injectedPod.Annotations[annotationSidecarInjected] = "true"

	if injectedPod.Labels == nil {
		injectedPod.Labels = make(map[string]string)
	}
	injectedPod.Labels[labelManagedBy] = "slingshot-operator"

	devices := s.findCXIDevices(pod)

	sidecar := s.buildSidecarContainer(devices)

	if s.Config.UseNativeSidecar {
		// K8s 1.28+ native sidecar: init container with restartPolicy: Always
		// This starts before main containers, runs alongside them, and terminates after them
		restartAlways := corev1.ContainerRestartPolicyAlways
		sidecar.RestartPolicy = &restartAlways
		injectedPod.Spec.InitContainers = append(injectedPod.Spec.InitContainers, sidecar)
	} else {
		// Traditional sidecar: regular container
		injectedPod.Spec.Containers = append(injectedPod.Spec.Containers, sidecar)
	}

	return injectedPod
}

func (s *SidecarInjector) findCXIDevices(pod *corev1.Pod) []string {
	deviceSet := make(map[string]bool)

	for _, container := range pod.Spec.Containers {
		for resourceName := range container.Resources.Limits {
			if strings.HasPrefix(string(resourceName), "hpe.com/cxi") {
				deviceSet[string(resourceName)] = true
			}
		}
	}

	devices := make([]string, 0, len(deviceSet))
	for device := range deviceSet {
		devices = append(devices, device)
	}
	return devices
}

func (s *SidecarInjector) buildSidecarContainer(devices []string) corev1.Container {
	args := []string{
		"--mode=single",
		fmt.Sprintf("--cxi-rh-path=%s", s.Config.CXIRHPath),
	}

	if len(devices) > 0 {
		args = append(args, "--device=cxi0")
	}

	privileged := true
	runAsRoot := int64(0)

	return corev1.Container{
		Name:  sidecarContainerName,
		Image: s.Config.RetryHandlerImage,
		Args:  args,
		SecurityContext: &corev1.SecurityContext{
			Privileged: &privileged,
			RunAsUser:  &runAsRoot,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					"SYS_ADMIN",
					"IPC_LOCK",
				},
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "dev",
				MountPath: "/dev",
			},
		},
	}
}

func (s *SidecarInjector) InjectDecoder(d admission.Decoder) {
	s.decoder = d
}
