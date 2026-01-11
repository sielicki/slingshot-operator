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

//nolint:goconst // test data strings are intentionally repeated
package webhook

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestShouldInject_WithAnnotation(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "annotation true",
			annotations: map[string]string{annotationInjectSidecar: "true"},
			want:        true,
		},
		{
			name:        "annotation enabled",
			annotations: map[string]string{annotationInjectSidecar: "enabled"},
			want:        true,
		},
		{
			name:        "annotation false",
			annotations: map[string]string{annotationInjectSidecar: "false"},
			want:        false,
		},
		{
			name:        "no annotation",
			annotations: map[string]string{},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			if got := injector.shouldInject(pod); got != tt.want {
				t.Errorf("shouldInject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldInject_WithCXIResource(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	tests := []struct {
		name      string
		resources corev1.ResourceRequirements
		want      bool
	}{
		{
			name: "has cxi limit",
			resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"hpe.com/cxi": resource.MustParse("1"),
				},
			},
			want: true,
		},
		{
			name: "has cxi request",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"hpe.com/cxi": resource.MustParse("1"),
				},
			},
			want: true,
		},
		{
			name: "no cxi resources",
			resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"cpu": resource.MustParse("1"),
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "test",
							Resources: tt.resources,
						},
					},
				},
			}
			if got := injector.shouldInject(pod); got != tt.want {
				t.Errorf("shouldInject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAlreadyInjected(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "has annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{annotationSidecarInjected: "true"},
				},
			},
			want: true,
		},
		{
			name: "has sidecar container",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: sidecarContainerName},
					},
				},
			},
			want: true,
		},
		{
			name: "not injected",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app"},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := injector.alreadyInjected(tt.pod); got != tt.want {
				t.Errorf("alreadyInjected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInjectSidecar(t *testing.T) {
	config := SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
		CXIRHPath:         "/test/cxi_rh",
	}
	injector := NewSidecarInjector(nil, config)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	injected := injector.injectSidecar(pod)

	if injected.Annotations[annotationSidecarInjected] != "true" {
		t.Error("expected injection annotation to be set")
	}

	if injected.Labels[labelManagedBy] != "slingshot-operator" {
		t.Error("expected managed-by label to be set")
	}

	if len(injected.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(injected.Spec.Containers))
	}

	var sidecar *corev1.Container
	for i := range injected.Spec.Containers {
		if injected.Spec.Containers[i].Name == sidecarContainerName {
			sidecar = &injected.Spec.Containers[i]
			break
		}
	}

	if sidecar == nil {
		t.Fatal("sidecar container not found")
	}

	if sidecar.Image != "test-image:v1" {
		t.Errorf("expected image test-image:v1, got %s", sidecar.Image)
	}

	if sidecar.SecurityContext == nil || !*sidecar.SecurityContext.Privileged {
		t.Error("sidecar should be privileged")
	}
}

func TestBuildSidecarContainer(t *testing.T) {
	config := SidecarInjectorConfig{
		RetryHandlerImage: "my-image:latest",
		CXIRHPath:         "/custom/path/cxi_rh",
	}
	injector := NewSidecarInjector(nil, config)

	container := injector.buildSidecarContainer([]string{"hpe.com/cxi"})

	if container.Name != sidecarContainerName {
		t.Errorf("expected name %s, got %s", sidecarContainerName, container.Name)
	}

	if container.Image != "my-image:latest" {
		t.Errorf("expected image my-image:latest, got %s", container.Image)
	}

	foundCXIRHPath := false
	for _, arg := range container.Args {
		if arg == "--cxi-rh-path=/custom/path/cxi_rh" {
			foundCXIRHPath = true
		}
	}
	if !foundCXIRHPath {
		t.Error("expected --cxi-rh-path arg")
	}

	if container.SecurityContext == nil {
		t.Fatal("security context should not be nil")
	}

	if !*container.SecurityContext.Privileged {
		t.Error("container should be privileged")
	}

	if container.SecurityContext.Capabilities == nil {
		t.Fatal("capabilities should not be nil")
	}

	foundSysAdmin := false
	for _, cap := range container.SecurityContext.Capabilities.Add {
		if cap == "SYS_ADMIN" {
			foundSysAdmin = true
		}
	}
	if !foundSysAdmin {
		t.Error("expected SYS_ADMIN capability")
	}
}

func TestFindCXIDevices(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app1",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
				{
					Name: "app2",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi-exclusive": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	devices := injector.findCXIDevices(pod)

	if len(devices) != 2 {
		t.Errorf("expected 2 devices, got %d", len(devices))
	}
}

func TestDefaultConfig(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	if injector.Config.RetryHandlerImage != defaultRetryHandlerImage {
		t.Errorf("expected default image %s, got %s", defaultRetryHandlerImage, injector.Config.RetryHandlerImage)
	}

	if injector.Config.CXIRHPath != defaultCXIRHPath {
		t.Errorf("expected default path %s, got %s", defaultCXIRHPath, injector.Config.CXIRHPath)
	}

	if injector.Config.ResourceName != "hpe.com/cxi" {
		t.Errorf("expected default resource hpe.com/cxi, got %s", injector.Config.ResourceName)
	}
}

func TestInjectSidecar_NativeSidecar(t *testing.T) {
	config := SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
		CXIRHPath:         "/test/cxi_rh",
		UseNativeSidecar:  true,
	}
	injector := NewSidecarInjector(nil, config)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	injected := injector.injectSidecar(pod)

	// Native sidecar should be added as init container
	if len(injected.Spec.InitContainers) != 1 {
		t.Fatalf("expected 1 init container for native sidecar, got %d", len(injected.Spec.InitContainers))
	}

	initContainer := injected.Spec.InitContainers[0]
	if initContainer.Name != sidecarContainerName {
		t.Errorf("init container name = %q, want %q", initContainer.Name, sidecarContainerName)
	}

	// Check restart policy is set
	if initContainer.RestartPolicy == nil {
		t.Error("RestartPolicy should be set for native sidecar")
	} else if *initContainer.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Errorf("RestartPolicy = %v, want Always", *initContainer.RestartPolicy)
	}
}

func TestInjectSidecar_RegularSidecar(t *testing.T) {
	config := SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
		CXIRHPath:         "/test/cxi_rh",
		UseNativeSidecar:  false,
	}
	injector := NewSidecarInjector(nil, config)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	injected := injector.injectSidecar(pod)

	// Regular sidecar should be added as regular container
	if len(injected.Spec.InitContainers) != 0 {
		t.Errorf("expected 0 init containers, got %d", len(injected.Spec.InitContainers))
	}
	if len(injected.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(injected.Spec.Containers))
	}
}

func TestInjectSidecar_PreservesExistingAnnotations(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Annotations: map[string]string{
				"existing-annotation": "value",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	injected := injector.injectSidecar(pod)

	if injected.Annotations["existing-annotation"] != "value" {
		t.Error("existing annotation was not preserved")
	}
	if injected.Annotations[annotationSidecarInjected] != "true" {
		t.Error("injection annotation was not set")
	}
}

func TestInjectSidecar_PreservesExistingLabels(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Labels: map[string]string{
				"existing-label": "value",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	injected := injector.injectSidecar(pod)

	if injected.Labels["existing-label"] != "value" {
		t.Error("existing label was not preserved")
	}
}

func TestInjectSidecar_NilAnnotationsAndLabels(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Annotations: nil,
			Labels:      nil,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	// Should not panic
	injected := injector.injectSidecar(pod)

	if injected.Annotations == nil {
		t.Error("annotations should be initialized")
	}
	if injected.Labels == nil {
		t.Error("labels should be initialized")
	}
}

func TestShouldInject_MultipleContainers(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app1",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cpu": resource.MustParse("1"),
						},
					},
				},
				{
					Name: "app2",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	if !injector.shouldInject(pod) {
		t.Error("shouldInject() = false, want true when one container has CXI resource")
	}
}

func TestShouldInject_InitContainerWithCXI(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name: "init",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
			Containers: []corev1.Container{
				{Name: "app"},
			},
		},
	}

	// Init containers with CXI resources should also trigger injection
	if !injector.shouldInject(pod) {
		t.Error("shouldInject() = false, want true when init container has CXI resource")
	}
}

func TestFindCXIDevices_Deduplication(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app1",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
				{
					Name: "app2",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("2"), // Same resource name
						},
					},
				},
			},
		},
	}

	devices := injector.findCXIDevices(pod)

	// Should deduplicate to 1 device type
	if len(devices) != 1 {
		t.Errorf("expected 1 device after deduplication, got %d", len(devices))
	}
}

func TestFindCXIDevices_NoDevices(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	devices := injector.findCXIDevices(pod)

	if len(devices) != 0 {
		t.Errorf("expected 0 devices, got %d", len(devices))
	}
}

func TestBuildSidecarContainer_VolumeMounts(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	container := injector.buildSidecarContainer([]string{"hpe.com/cxi"})

	foundDevMount := false
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "/dev" {
			foundDevMount = true
			break
		}
	}

	if !foundDevMount {
		t.Error("expected /dev volume mount")
	}
}

func TestBuildSidecarContainer_ResourceLimits(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	container := injector.buildSidecarContainer([]string{"hpe.com/cxi"})

	// Check resource limits are set
	cpuLimit := container.Resources.Limits[corev1.ResourceCPU]
	if cpuLimit.IsZero() {
		t.Error("CPU limit should be set")
	}

	memLimit := container.Resources.Limits[corev1.ResourceMemory]
	if memLimit.IsZero() {
		t.Error("Memory limit should be set")
	}
}

func TestBuildSidecarContainer_Capabilities(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	container := injector.buildSidecarContainer([]string{"hpe.com/cxi"})

	if container.SecurityContext == nil || container.SecurityContext.Capabilities == nil {
		t.Fatal("capabilities should be set")
	}

	caps := container.SecurityContext.Capabilities.Add
	expectedCaps := map[corev1.Capability]bool{
		"IPC_LOCK":  false,
		"SYS_ADMIN": false,
	}

	for _, cap := range caps {
		expectedCaps[cap] = true
	}

	for cap, found := range expectedCaps {
		if !found {
			t.Errorf("expected capability %s not found", cap)
		}
	}
}

func TestAlreadyInjected_FalseAnnotationValue(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{annotationSidecarInjected: "false"},
		},
	}

	// "false" annotation value means NOT injected (annotation must equal "true")
	if injector.alreadyInjected(pod) {
		t.Error("alreadyInjected() = true, want false when annotation value is 'false'")
	}
}

func TestSidecarContainerName(t *testing.T) {
	if sidecarContainerName != "cxi-retry-handler" {
		t.Errorf("sidecarContainerName = %q, want %q", sidecarContainerName, "cxi-retry-handler")
	}
}

func TestAnnotationConstants(t *testing.T) {
	if annotationInjectSidecar != "slingshot.hpe.com/inject-retry-handler" {
		t.Errorf("annotationInjectSidecar = %q", annotationInjectSidecar)
	}
	if annotationSidecarInjected != "slingshot.hpe.com/retry-handler-injected" {
		t.Errorf("annotationSidecarInjected = %q", annotationSidecarInjected)
	}
}

func TestContainerRequestsCXI(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	tests := []struct {
		name      string
		resources corev1.ResourceRequirements
		want      bool
	}{
		{
			name: "standard cxi limit",
			resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"hpe.com/cxi": resource.MustParse("1"),
				},
			},
			want: true,
		},
		{
			name: "cxi in requests",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"hpe.com/cxi": resource.MustParse("1"),
				},
			},
			want: true,
		},
		{
			name: "exclusive cxi",
			resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"hpe.com/cxi-exclusive": resource.MustParse("1"),
				},
			},
			want: true,
		},
		{
			name: "custom cxi resource",
			resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"hpe.com/cxi-custom": resource.MustParse("1"),
				},
			},
			want: true,
		},
		{
			name: "non-cxi hpe resource",
			resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"hpe.com/other": resource.MustParse("1"),
				},
			},
			want: false,
		},
		{
			name: "cpu only",
			resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"cpu": resource.MustParse("1"),
				},
			},
			want: false,
		},
		{
			name:      "empty resources",
			resources: corev1.ResourceRequirements{},
			want:      false,
		},
		{
			name: "nil limits and requests",
			resources: corev1.ResourceRequirements{
				Limits:   nil,
				Requests: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &corev1.Container{
				Name:      "test",
				Resources: tt.resources,
			}
			if got := injector.containerRequestsCXI(container); got != tt.want {
				t.Errorf("containerRequestsCXI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldInject_ExplicitDisable(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotationInjectSidecar: "false",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	// Pod has CXI resources but explicitly disabled injection
	if injector.shouldInject(pod) {
		t.Error("shouldInject() = true, want false when explicitly disabled")
	}
}

func TestBuildSidecarContainer_MultipleDevices(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	devices := []string{"hpe.com/cxi", "hpe.com/cxi-exclusive"}
	container := injector.buildSidecarContainer(devices)

	// Current implementation adds a single hardcoded --device=cxi0 when devices are present
	// NOTE: This may be a limitation - future implementation might add per-device args
	deviceArgCount := 0
	for _, arg := range container.Args {
		if len(arg) > 9 && arg[:9] == "--device=" {
			deviceArgCount++
		}
	}

	if deviceArgCount != 1 {
		t.Errorf("expected 1 device arg (hardcoded cxi0), got %d", deviceArgCount)
	}
}

func TestBuildSidecarContainer_EmptyDevices(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	container := injector.buildSidecarContainer([]string{})

	// Should still create container, just without device args
	if container.Name != sidecarContainerName {
		t.Errorf("container name = %q, want %q", container.Name, sidecarContainerName)
	}
}

func TestInjectSidecar_SidecarHasDevVolumeMount(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	injected := injector.injectSidecar(pod)

	var sidecar *corev1.Container
	for i := range injected.Spec.Containers {
		if injected.Spec.Containers[i].Name == sidecarContainerName {
			sidecar = &injected.Spec.Containers[i]
			break
		}
	}

	if sidecar == nil {
		t.Fatal("sidecar container not found")
	}

	foundDevMount := false
	for _, mount := range sidecar.VolumeMounts {
		if mount.Name == "dev" && mount.MountPath == "/dev" {
			foundDevMount = true
			break
		}
	}

	if !foundDevMount {
		t.Error("sidecar should have /dev volume mount")
	}

	// NOTE: Current implementation adds volume mount but doesn't add the volume itself
	// The pod would need a "dev" volume to be added for this to work at runtime
	// This is a potential bug to fix - volume should be added if not present
}

func TestInjectSidecar_PreservesExistingVolumes(t *testing.T) {
	injector := NewSidecarInjector(nil, SidecarInjectorConfig{
		RetryHandlerImage: "test-image:v1",
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "dev",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/dev",
						},
					},
				},
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "my-config",
							},
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hpe.com/cxi": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	injected := injector.injectSidecar(pod)

	// Existing volumes should be preserved
	if len(injected.Spec.Volumes) != 2 {
		t.Errorf("expected 2 volumes preserved, got %d", len(injected.Spec.Volumes))
	}

	foundDev := false
	foundConfig := false
	for _, vol := range injected.Spec.Volumes {
		if vol.Name == "dev" {
			foundDev = true
		}
		if vol.Name == "config" {
			foundConfig = true
		}
	}

	if !foundDev {
		t.Error("dev volume not preserved")
	}
	if !foundConfig {
		t.Error("config volume not preserved")
	}
}
