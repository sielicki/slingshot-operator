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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cxiv1 "github.com/sielicki/slingshot-operator/api/v1"
)

// reconcileUntilDone reconciles repeatedly until no requeue is requested
func reconcileUntilDone(ctx context.Context, r *CXIDriverReconciler, req reconcile.Request) error {
	for i := 0; i < 10; i++ {
		result, err := r.Reconcile(ctx, req)
		if err != nil {
			return err
		}
		if !result.Requeue {
			return nil
		}
	}
	return nil
}

var _ = Describe("CXIDriver Controller", func() {
	const namespace = "default"

	// Clean up singleton ConfigMap after each test to ensure test isolation
	AfterEach(func() {
		ctx := context.Background()
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      singletonConfigMapName,
				Namespace: namespace,
			},
		}
		_ = k8sClient.Delete(ctx, cm)
	})

	Context("When creating a CXIDriver", func() {
		ctx := context.Background()

		It("should create device plugin daemonset", func() {
			driverName := "test-device-plugin"
			typeNamespacedName := types.NamespacedName{
				Name:      driverName,
				Namespace: namespace,
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      driverName,
					Namespace: namespace,
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					Source: cxiv1.DriverSourceSpec{
						Type: cxiv1.DriverSourcePreinstalled,
					},
					DevicePlugin: cxiv1.DevicePluginSpec{
						Enabled:     true,
						SharingMode: cxiv1.DeviceSharingModeShared,
					},
					RetryHandler: cxiv1.RetryHandlerSpec{
						Mode: cxiv1.RetryHandlerModeNone,
					},
				},
			}

			Expect(k8sClient.Create(ctx, driver)).To(Succeed())

			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := reconcileUntilDone(ctx, controllerReconciler, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ds := &appsv1.DaemonSet{}
			dsName := types.NamespacedName{
				Name:      driverName + "-device-plugin",
				Namespace: namespace,
			}
			Expect(k8sClient.Get(ctx, dsName, ds)).To(Succeed())

			Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Containers[0].Name).To(Equal("device-plugin"))

			Expect(k8sClient.Delete(ctx, driver)).To(Succeed())
		})

		It("should create retry handler daemonset when mode is daemonset", func() {
			driverName := "test-retry-handler"
			typeNamespacedName := types.NamespacedName{
				Name:      driverName,
				Namespace: namespace,
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      driverName,
					Namespace: namespace,
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					Source: cxiv1.DriverSourceSpec{
						Type: cxiv1.DriverSourcePreinstalled,
					},
					DevicePlugin: cxiv1.DevicePluginSpec{
						Enabled:     true,
						SharingMode: cxiv1.DeviceSharingModeShared,
					},
					RetryHandler: cxiv1.RetryHandlerSpec{
						Enabled: true,
						Mode:    cxiv1.RetryHandlerModeDaemonSet,
					},
				},
			}

			Expect(k8sClient.Create(ctx, driver)).To(Succeed())

			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := reconcileUntilDone(ctx, controllerReconciler, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ds := &appsv1.DaemonSet{}
			dsName := types.NamespacedName{
				Name:      driverName + "-retry-handler",
				Namespace: namespace,
			}
			Expect(k8sClient.Get(ctx, dsName, ds)).To(Succeed())

			Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Containers[0].Name).To(Equal("retry-handler"))

			Expect(k8sClient.Delete(ctx, driver)).To(Succeed())
		})

		It("should not create retry handler daemonset when mode is none", func() {
			driverName := "test-no-retry-handler"
			typeNamespacedName := types.NamespacedName{
				Name:      driverName,
				Namespace: namespace,
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      driverName,
					Namespace: namespace,
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					Source: cxiv1.DriverSourceSpec{
						Type: cxiv1.DriverSourcePreinstalled,
					},
					DevicePlugin: cxiv1.DevicePluginSpec{
						Enabled:     true,
						SharingMode: cxiv1.DeviceSharingModeShared,
					},
					RetryHandler: cxiv1.RetryHandlerSpec{
						Enabled: false,
						Mode:    cxiv1.RetryHandlerModeNone,
					},
				},
			}

			Expect(k8sClient.Create(ctx, driver)).To(Succeed())

			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := reconcileUntilDone(ctx, controllerReconciler, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ds := &appsv1.DaemonSet{}
			dsName := types.NamespacedName{
				Name:      driverName + "-retry-handler",
				Namespace: namespace,
			}
			err = k8sClient.Get(ctx, dsName, ds)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			Expect(k8sClient.Delete(ctx, driver)).To(Succeed())
		})

		It("should apply node selector to daemonsets", func() {
			driverName := "test-node-selector"
			typeNamespacedName := types.NamespacedName{
				Name:      driverName,
				Namespace: namespace,
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      driverName,
					Namespace: namespace,
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					Source: cxiv1.DriverSourceSpec{
						Type: cxiv1.DriverSourcePreinstalled,
					},
					DevicePlugin: cxiv1.DevicePluginSpec{
						Enabled:     true,
						SharingMode: cxiv1.DeviceSharingModeShared,
					},
					NodeSelector: map[string]string{
						"node-type": "compute",
					},
				},
			}

			Expect(k8sClient.Create(ctx, driver)).To(Succeed())

			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := reconcileUntilDone(ctx, controllerReconciler, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ds := &appsv1.DaemonSet{}
			dsName := types.NamespacedName{
				Name:      driverName + "-device-plugin",
				Namespace: namespace,
			}
			Expect(k8sClient.Get(ctx, dsName, ds)).To(Succeed())

			Expect(ds.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("node-type", "compute"))

			Expect(k8sClient.Delete(ctx, driver)).To(Succeed())
		})

		It("should create metrics exporter daemonset when enabled", func() {
			driverName := "test-metrics-exporter"
			typeNamespacedName := types.NamespacedName{
				Name:      driverName,
				Namespace: namespace,
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      driverName,
					Namespace: namespace,
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					Source: cxiv1.DriverSourceSpec{
						Type: cxiv1.DriverSourcePreinstalled,
					},
					DevicePlugin: cxiv1.DevicePluginSpec{
						Enabled:     true,
						SharingMode: cxiv1.DeviceSharingModeShared,
					},
					MetricsExporter: cxiv1.MetricsExporterSpec{
						Enabled: true,
						Port:    9090,
					},
				},
			}

			Expect(k8sClient.Create(ctx, driver)).To(Succeed())

			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := reconcileUntilDone(ctx, controllerReconciler, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ds := &appsv1.DaemonSet{}
			dsName := types.NamespacedName{
				Name:      driverName + "-metrics-exporter",
				Namespace: namespace,
			}
			Expect(k8sClient.Get(ctx, dsName, ds)).To(Succeed())

			Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Containers[0].Name).To(Equal("metrics-exporter"))

			Expect(k8sClient.Delete(ctx, driver)).To(Succeed())
		})
	})

	Context("When deleting a CXIDriver", func() {
		ctx := context.Background()

		It("should handle not found gracefully", func() {
			typeNamespacedName := types.NamespacedName{
				Name:      "nonexistent",
				Namespace: namespace,
			}

			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
		})
	})

	Context("Singleton ConfigMap Management", func() {
		ctx := context.Background()

		It("should return empty string when no active driver is set", func() {
			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			active, err := controllerReconciler.getActiveCXIDriver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(active).To(Equal(""))
		})

		It("should set and get active driver via ConfigMap", func() {
			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := controllerReconciler.setActiveCXIDriver(ctx, "my-driver")
			Expect(err).NotTo(HaveOccurred())

			active, err := controllerReconciler.getActiveCXIDriver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(active).To(Equal("my-driver"))

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      singletonConfigMapName,
				Namespace: namespace,
			}, cm)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data[singletonActiveKey]).To(Equal("my-driver"))
		})

		It("should clear active driver when set to empty string", func() {
			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := controllerReconciler.setActiveCXIDriver(ctx, "my-driver")
			Expect(err).NotTo(HaveOccurred())

			err = controllerReconciler.setActiveCXIDriver(ctx, "")
			Expect(err).NotTo(HaveOccurred())

			active, err := controllerReconciler.getActiveCXIDriver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(active).To(Equal(""))
		})

		It("should auto-assign first driver as active", func() {
			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			isActive, err := controllerReconciler.isActiveCXIDriver(ctx, "first-driver")
			Expect(err).NotTo(HaveOccurred())
			Expect(isActive).To(BeTrue())

			active, err := controllerReconciler.getActiveCXIDriver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(active).To(Equal("first-driver"))
		})

		It("should reject second driver when first is active", func() {
			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "first-driver",
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
				},
			}
			Expect(k8sClient.Create(ctx, driver)).To(Succeed())

			err := controllerReconciler.setActiveCXIDriver(ctx, "first-driver")
			Expect(err).NotTo(HaveOccurred())

			isActive, err := controllerReconciler.isActiveCXIDriver(ctx, "second-driver")
			Expect(err).NotTo(HaveOccurred())
			Expect(isActive).To(BeFalse())

			Expect(k8sClient.Delete(ctx, driver)).To(Succeed())
		})

		It("should reassign to new driver if active driver is deleted", func() {
			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := controllerReconciler.setActiveCXIDriver(ctx, "deleted-driver")
			Expect(err).NotTo(HaveOccurred())

			isActive, err := controllerReconciler.isActiveCXIDriver(ctx, "new-driver")
			Expect(err).NotTo(HaveOccurred())
			Expect(isActive).To(BeTrue())

			active, err := controllerReconciler.getActiveCXIDriver(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(active).To(Equal("new-driver"))
		})

		It("should return true for same driver that is active", func() {
			controllerReconciler := &CXIDriverReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Namespace: namespace,
			}

			err := controllerReconciler.setActiveCXIDriver(ctx, "my-driver")
			Expect(err).NotTo(HaveOccurred())

			isActive, err := controllerReconciler.isActiveCXIDriver(ctx, "my-driver")
			Expect(err).NotTo(HaveOccurred())
			Expect(isActive).To(BeTrue())
		})
	})

	Context("DaemonSet Building", func() {
		It("should build driver agent DaemonSet with correct structure", func() {
			scheme := runtime.NewScheme()
			_ = cxiv1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			reconciler := &CXIDriverReconciler{
				Scheme:    scheme,
				Namespace: "test-namespace",
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-driver",
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					Source: cxiv1.DriverSourceSpec{
						Type: cxiv1.DriverSourcePreinstalled,
					},
					NodeSelector: map[string]string{
						"node-type": "compute",
					},
				},
			}

			ds := reconciler.buildDriverAgentDaemonSet(driver)

			Expect(ds.Name).To(Equal("test-driver-driver-agent"))
			Expect(ds.Namespace).To(Equal("test-namespace"))
			Expect(ds.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("node-type", "compute"))
			Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Containers[0].Name).To(Equal("driver-agent"))

			// Check health probes are set
			Expect(ds.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())
			Expect(ds.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())

			// Check resource limits are set
			Expect(ds.Spec.Template.Spec.Containers[0].Resources.Limits).NotTo(BeEmpty())
			Expect(ds.Spec.Template.Spec.Containers[0].Resources.Requests).NotTo(BeEmpty())
		})

		It("should build device plugin DaemonSet with correct structure", func() {
			scheme := runtime.NewScheme()
			_ = cxiv1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			reconciler := &CXIDriverReconciler{
				Scheme:    scheme,
				Namespace: "test-namespace",
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-driver",
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					DevicePlugin: cxiv1.DevicePluginSpec{
						Enabled:        true,
						SharingMode:    cxiv1.DeviceSharingModeShared,
						SharedCapacity: 50,
						ResourceName:   "hpe.com/cxi",
					},
				},
			}

			ds := reconciler.buildDevicePluginDaemonSet(driver)

			Expect(ds.Name).To(Equal("test-driver-device-plugin"))
			Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Containers[0].Name).To(Equal("device-plugin"))

			// Check environment variables
			envMap := make(map[string]string)
			for _, env := range ds.Spec.Template.Spec.Containers[0].Env {
				envMap[env.Name] = env.Value
			}
			Expect(envMap["RESOURCE_NAME"]).To(Equal("hpe.com/cxi"))
			Expect(envMap["SHARING_MODE"]).To(Equal("shared"))
			Expect(envMap["SHARED_CAPACITY"]).To(Equal("50"))

			// Check health probes
			Expect(ds.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())
			Expect(ds.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		})

		It("should build retry handler DaemonSet with correct structure", func() {
			scheme := runtime.NewScheme()
			_ = cxiv1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			reconciler := &CXIDriverReconciler{
				Scheme:    scheme,
				Namespace: "test-namespace",
			}

			driver := &cxiv1.CXIDriver{
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

			ds := reconciler.buildRetryHandlerDaemonSet(driver)

			Expect(ds.Name).To(Equal("test-driver-retry-handler"))
			Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Containers[0].Name).To(Equal("retry-handler"))
		})

		It("should build metrics exporter with correct retry handler URL for daemonset mode", func() {
			scheme := runtime.NewScheme()
			_ = cxiv1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			reconciler := &CXIDriverReconciler{
				Scheme:    scheme,
				Namespace: "test-namespace",
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-driver",
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					RetryHandler: cxiv1.RetryHandlerSpec{
						Enabled: true,
						Mode:    cxiv1.RetryHandlerModeDaemonSet,
					},
					MetricsExporter: cxiv1.MetricsExporterSpec{
						Enabled: true,
					},
				},
			}

			ds := reconciler.buildMetricsExporterDaemonSet(driver)

			found := false
			for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
				if arg == "--retry-handler-url=http://localhost:8080" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "metrics exporter should have localhost retry handler URL in daemonset mode")
		})

		It("should build metrics exporter with empty retry handler URL for sidecar mode", func() {
			scheme := runtime.NewScheme()
			_ = cxiv1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			reconciler := &CXIDriverReconciler{
				Scheme:    scheme,
				Namespace: "test-namespace",
			}

			driver := &cxiv1.CXIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-driver",
				},
				Spec: cxiv1.CXIDriverSpec{
					Version: "1.0.0",
					RetryHandler: cxiv1.RetryHandlerSpec{
						Enabled: true,
						Mode:    cxiv1.RetryHandlerModeSidecar,
					},
					MetricsExporter: cxiv1.MetricsExporterSpec{
						Enabled: true,
					},
				},
			}

			ds := reconciler.buildMetricsExporterDaemonSet(driver)

			found := false
			for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
				if arg == "--retry-handler-url=" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "metrics exporter should have empty retry handler URL in sidecar mode")
		})
	})

	Context("Constants", func() {
		It("should have correct condition type constants", func() {
			Expect(conditionTypeAvailable).To(Equal("Available"))
			Expect(conditionTypeProgressing).To(Equal("Progressing"))
			Expect(conditionTypeDegraded).To(Equal("Degraded"))
			Expect(conditionTypeDriverAgentReady).To(Equal("DriverAgentReady"))
			Expect(conditionTypeDevicePluginReady).To(Equal("DevicePluginReady"))
			Expect(conditionTypeRetryHandlerReady).To(Equal("RetryHandlerReady"))
			Expect(conditionTypeMetricsExporterReady).To(Equal("MetricsExporterReady"))
		})

		It("should have correct label constants", func() {
			Expect(labelManagedBy).To(Equal("app.kubernetes.io/managed-by"))
			Expect(labelComponent).To(Equal("app.kubernetes.io/component"))
			Expect(labelInstance).To(Equal("app.kubernetes.io/instance"))
		})

		It("should have correct component constants", func() {
			Expect(componentDriverAgent).To(Equal("driver-agent"))
			Expect(componentDevicePlugin).To(Equal("device-plugin"))
			Expect(componentRetryHandler).To(Equal("retry-handler"))
			Expect(componentMetricsExporter).To(Equal("metrics-exporter"))
		})

		It("should have correct singleton ConfigMap constants", func() {
			Expect(singletonConfigMapName).To(Equal("cxidriver-singleton"))
			Expect(singletonActiveKey).To(Equal("active-driver"))
		})
	})
})
