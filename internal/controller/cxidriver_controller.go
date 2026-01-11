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
	"fmt"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cxiv1 "github.com/sielicki/slingshot-operator/api/v1"
	"github.com/sielicki/slingshot-operator/pkg/config"
)

const (
	cxiDriverFinalizer = "cxi.hpe.com/finalizer"

	conditionTypeAvailable   = "Available"
	conditionTypeProgressing = "Progressing"
	conditionTypeDegraded    = "Degraded"

	conditionTypeDriverAgentReady     = "DriverAgentReady"
	conditionTypeDevicePluginReady    = "DevicePluginReady"
	conditionTypeRetryHandlerReady    = "RetryHandlerReady"
	conditionTypeMetricsExporterReady = "MetricsExporterReady"

	labelManagedBy = "app.kubernetes.io/managed-by"
	labelComponent = "app.kubernetes.io/component"
	labelInstance  = "app.kubernetes.io/instance"

	componentDriverAgent     = "driver-agent"
	componentDevicePlugin    = "device-plugin"
	componentRetryHandler    = "retry-handler"
	componentMetricsExporter = "metrics-exporter"

	singletonConfigMapName = "cxidriver-singleton"
	singletonActiveKey     = "active-driver"
)

// CXIDriverReconciler reconciles a CXIDriver object
type CXIDriverReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string // Namespace where managed resources are deployed
}

// +kubebuilder:rbac:groups=cxi.hpe.com,resources=cxidrivers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cxi.hpe.com,resources=cxidrivers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cxi.hpe.com,resources=cxidrivers/finalizers,verbs=update
// +kubebuilder:rbac:groups=cxi.hpe.com,resources=cxidevices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cxi.hpe.com,resources=cxidevices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

func (r *CXIDriverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the CXIDriver instance
	cxiDriver := &cxiv1.CXIDriver{}
	if err := r.Get(ctx, req.NamespacedName, cxiDriver); err != nil {
		if apierrors.IsNotFound(err) {
			// Clear active driver if it was deleted
			active, _ := r.getActiveCXIDriver(ctx)
			if active == req.Name {
				if err := r.setActiveCXIDriver(ctx, ""); err != nil {
					log.Error(err, "Failed to clear active CXIDriver")
				}
			}
			log.Info("CXIDriver resource not found, likely deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get CXIDriver")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Handle finalizer for cleanup
	if cxiDriver.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(cxiDriver, cxiDriverFinalizer) {
			if err := r.cleanupResources(ctx, cxiDriver); err != nil {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			controllerutil.RemoveFinalizer(cxiDriver, cxiDriverFinalizer)
			if err := r.Update(ctx, cxiDriver); err != nil {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			// Clear active driver on deletion
			active, _ := r.getActiveCXIDriver(ctx)
			if active == cxiDriver.Name {
				if err := r.setActiveCXIDriver(ctx, ""); err != nil {
					log.Error(err, "Failed to clear active CXIDriver on deletion")
				}
			}
		}
		return ctrl.Result{}, nil
	}

	// Singleton enforcement: only one CXIDriver can be active at a time
	isActive, err := r.isActiveCXIDriver(ctx, cxiDriver.Name)
	if err != nil {
		log.Error(err, "Failed to check singleton status")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if !isActive {
		active, _ := r.getActiveCXIDriver(ctx)
		log.Info("Ignoring CXIDriver because another is already active",
			"active", active, "ignored", cxiDriver.Name)
		meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
			Type:    conditionTypeDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  "AnotherCXIDriverActive",
			Message: fmt.Sprintf("CXIDriver %q is already active; this resource is ignored", active),
		})
		meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
			Type:    conditionTypeAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  "Ignored",
			Message: "Another CXIDriver is already managing this cluster",
		})
		return r.updateStatusWithRetry(ctx, req, cxiDriver)
	}

	log.V(1).Info("CXIDriver is active singleton", "name", cxiDriver.Name)

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cxiDriver, cxiDriverFinalizer) {
		controllerutil.AddFinalizer(cxiDriver, cxiDriverFinalizer)
		if err := r.Update(ctx, cxiDriver); err != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Set progressing condition
	meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
		Type:    conditionTypeProgressing,
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciling",
		Message: "Reconciling CXIDriver resources",
	})

	// Reconcile driver agent DaemonSet
	if err := r.reconcileDriverAgent(ctx, cxiDriver); err != nil {
		log.Error(err, "Failed to reconcile driver agent")
		r.setComponentCondition(cxiDriver, conditionTypeDriverAgentReady, false, "Failed", err.Error())
		r.setDegradedCondition(cxiDriver, "DriverAgentFailed", err.Error())
		_, _ = r.updateStatusWithRetry(ctx, req, cxiDriver)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	r.setComponentCondition(cxiDriver, conditionTypeDriverAgentReady, true, "Ready", "Driver agent DaemonSet is ready")

	// Reconcile device plugin DaemonSet
	if cxiDriver.Spec.DevicePlugin.Enabled {
		if err := r.reconcileDevicePlugin(ctx, cxiDriver); err != nil {
			log.Error(err, "Failed to reconcile device plugin")
			r.setComponentCondition(cxiDriver, conditionTypeDevicePluginReady, false, "Failed", err.Error())
			r.setDegradedCondition(cxiDriver, "DevicePluginFailed", err.Error())
			_, _ = r.updateStatusWithRetry(ctx, req, cxiDriver)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		r.setComponentCondition(cxiDriver, conditionTypeDevicePluginReady, true, "Ready", "Device plugin DaemonSet is ready")
	} else {
		meta.RemoveStatusCondition(&cxiDriver.Status.Conditions, conditionTypeDevicePluginReady)
	}

	// Reconcile retry handler based on mode
	if cxiDriver.Spec.RetryHandler.Enabled {
		if err := r.reconcileRetryHandler(ctx, cxiDriver); err != nil {
			log.Error(err, "Failed to reconcile retry handler")
			r.setComponentCondition(cxiDriver, conditionTypeRetryHandlerReady, false, "Failed", err.Error())
			r.setDegradedCondition(cxiDriver, "RetryHandlerFailed", err.Error())
			_, _ = r.updateStatusWithRetry(ctx, req, cxiDriver)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		r.setComponentCondition(cxiDriver, conditionTypeRetryHandlerReady, true, "Ready", "Retry handler is ready")
	} else {
		meta.RemoveStatusCondition(&cxiDriver.Status.Conditions, conditionTypeRetryHandlerReady)
	}

	// Reconcile metrics exporter DaemonSet
	if cxiDriver.Spec.MetricsExporter.Enabled {
		if err := r.reconcileMetricsExporter(ctx, cxiDriver); err != nil {
			log.Error(err, "Failed to reconcile metrics exporter")
			r.setComponentCondition(cxiDriver, conditionTypeMetricsExporterReady, false, "Failed", err.Error())
			r.setDegradedCondition(cxiDriver, "MetricsExporterFailed", err.Error())
			_, _ = r.updateStatusWithRetry(ctx, req, cxiDriver)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		r.setComponentCondition(cxiDriver, conditionTypeMetricsExporterReady, true, "Ready", "Metrics exporter DaemonSet is ready")
	} else {
		meta.RemoveStatusCondition(&cxiDriver.Status.Conditions, conditionTypeMetricsExporterReady)
	}

	// Update status with ObservedGeneration
	if err := r.updateStatus(ctx, cxiDriver); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	log.Info("Successfully reconciled CXIDriver", "version", cxiDriver.Spec.Version)
	return ctrl.Result{}, nil
}

func (r *CXIDriverReconciler) reconcileDriverAgent(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	log := logf.FromContext(ctx)
	ds := r.buildDriverAgentDaemonSet(cxiDriver)

	if err := controllerutil.SetControllerReference(cxiDriver, ds, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	existing := &appsv1.DaemonSet{}
	err := r.Get(ctx, client.ObjectKeyFromObject(ds), existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating driver agent DaemonSet", "name", ds.Name)
		if err := r.Create(ctx, ds); err != nil {
			return fmt.Errorf("creating DaemonSet %s: %w", ds.Name, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting DaemonSet %s: %w", ds.Name, err)
	}

	// Update if needed
	existing.Spec = ds.Spec
	log.Info("Updating driver agent DaemonSet", "name", ds.Name)
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating DaemonSet %s: %w", ds.Name, err)
	}
	return nil
}

func (r *CXIDriverReconciler) reconcileDevicePlugin(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	log := logf.FromContext(ctx)
	ds := r.buildDevicePluginDaemonSet(cxiDriver)

	if err := controllerutil.SetControllerReference(cxiDriver, ds, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	existing := &appsv1.DaemonSet{}
	err := r.Get(ctx, client.ObjectKeyFromObject(ds), existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating device plugin DaemonSet", "name", ds.Name)
		if err := r.Create(ctx, ds); err != nil {
			return fmt.Errorf("creating DaemonSet %s: %w", ds.Name, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting DaemonSet %s: %w", ds.Name, err)
	}

	existing.Spec = ds.Spec
	log.Info("Updating device plugin DaemonSet", "name", ds.Name)
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating DaemonSet %s: %w", ds.Name, err)
	}
	return nil
}

func (r *CXIDriverReconciler) reconcileRetryHandler(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	log := logf.FromContext(ctx)

	switch cxiDriver.Spec.RetryHandler.Mode {
	case cxiv1.RetryHandlerModeDaemonSet, "":
		ds := r.buildRetryHandlerDaemonSet(cxiDriver)
		if err := controllerutil.SetControllerReference(cxiDriver, ds, r.Scheme); err != nil {
			return fmt.Errorf("setting controller reference: %w", err)
		}

		existing := &appsv1.DaemonSet{}
		err := r.Get(ctx, client.ObjectKeyFromObject(ds), existing)
		if apierrors.IsNotFound(err) {
			log.Info("Creating retry handler DaemonSet", "name", ds.Name)
			if err := r.Create(ctx, ds); err != nil {
				return fmt.Errorf("creating DaemonSet %s: %w", ds.Name, err)
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("getting DaemonSet %s: %w", ds.Name, err)
		}

		existing.Spec = ds.Spec
		log.Info("Updating retry handler DaemonSet", "name", ds.Name)
		if err := r.Update(ctx, existing); err != nil {
			return fmt.Errorf("updating DaemonSet %s: %w", ds.Name, err)
		}
		return nil

	case cxiv1.RetryHandlerModeSidecar:
		if err := r.reconcileSidecarWebhook(ctx, cxiDriver); err != nil {
			return fmt.Errorf("reconciling sidecar webhook: %w", err)
		}
		return nil

	case cxiv1.RetryHandlerModeKernel:
		// Kernel mode - no userspace retry handler needed
		log.Info("Kernel retry handler mode - no DaemonSet needed")
		return nil

	case cxiv1.RetryHandlerModeNone:
		log.Info("Retry handler disabled")
		return nil

	default:
		return fmt.Errorf("unknown retry handler mode: %s", cxiDriver.Spec.RetryHandler.Mode)
	}
}

func (r *CXIDriverReconciler) buildDriverAgentDaemonSet(cxiDriver *cxiv1.CXIDriver) *appsv1.DaemonSet {
	labels := map[string]string{
		labelManagedBy: "slingshot-operator",
		labelComponent: componentDriverAgent,
		labelInstance:  cxiDriver.Name,
	}

	privileged := true
	hostPID := true

	image := cxiDriver.Spec.Source.Repository
	if image == "" {
		image = config.DefaultDriverAgentImage
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-driver-agent", cxiDriver.Name),
			Namespace: r.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					HostPID:      hostPID,
					NodeSelector: cxiDriver.Spec.NodeSelector,
					Containers: []corev1.Container{
						{
							Name:  "driver-agent",
							Image: image,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							Env: []corev1.EnvVar{
								{
									Name:  "CXI_DRIVER_VERSION",
									Value: cxiDriver.Spec.Version,
								},
								{
									Name:  "DRIVER_SOURCE_TYPE",
									Value: string(cxiDriver.Spec.Source.Type),
								},
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "spec.nodeName",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "health",
									ContainerPort: 8081,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8081),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       30,
								TimeoutSeconds:      5,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(8081),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "host-modules",
									MountPath: "/lib/modules",
									ReadOnly:  true,
								},
								{
									Name:      "host-usr-src",
									MountPath: "/usr/src",
									ReadOnly:  true,
								},
								{
									Name:      "dev",
									MountPath: "/dev",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "host-modules",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/lib/modules",
								},
							},
						},
						{
							Name: "host-usr-src",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/usr/src",
								},
							},
						},
						{
							Name: "dev",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/dev",
								},
							},
						},
					},
				},
			},
		},
	}

	return ds
}

func (r *CXIDriverReconciler) buildDevicePluginDaemonSet(cxiDriver *cxiv1.CXIDriver) *appsv1.DaemonSet {
	labels := map[string]string{
		labelManagedBy: "slingshot-operator",
		labelComponent: componentDevicePlugin,
		labelInstance:  cxiDriver.Name,
	}

	privileged := true

	image := cxiDriver.Spec.DevicePlugin.Image
	if image == "" {
		image = config.DefaultDevicePluginImage
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-device-plugin", cxiDriver.Name),
			Namespace: r.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					NodeSelector: cxiDriver.Spec.NodeSelector,
					Containers: []corev1.Container{
						{
							Name:  "device-plugin",
							Image: image,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							Env: []corev1.EnvVar{
								{
									Name:  "RESOURCE_NAME",
									Value: cxiDriver.Spec.DevicePlugin.ResourceName,
								},
								{
									Name:  "SHARING_MODE",
									Value: string(cxiDriver.Spec.DevicePlugin.SharingMode),
								},
								{
									Name:  "SHARED_CAPACITY",
									Value: fmt.Sprintf("%d", cxiDriver.Spec.DevicePlugin.SharedCapacity),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "health",
									ContainerPort: 8082,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8082),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       30,
								TimeoutSeconds:      5,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(8082),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "device-plugin",
									MountPath: "/var/lib/kubelet/device-plugins",
								},
								{
									Name:      "sys",
									MountPath: "/sys",
									ReadOnly:  true,
								},
								{
									Name:      "dev",
									MountPath: "/dev",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "device-plugin",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/lib/kubelet/device-plugins",
								},
							},
						},
						{
							Name: "sys",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/sys",
								},
							},
						},
						{
							Name: "dev",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/dev",
								},
							},
						},
					},
				},
			},
		},
	}

	return ds
}

func (r *CXIDriverReconciler) buildRetryHandlerDaemonSet(cxiDriver *cxiv1.CXIDriver) *appsv1.DaemonSet {
	labels := map[string]string{
		labelManagedBy: "slingshot-operator",
		labelComponent: componentRetryHandler,
		labelInstance:  cxiDriver.Name,
	}

	privileged := true

	image := cxiDriver.Spec.RetryHandler.Image
	if image == "" {
		image = config.DefaultRetryHandlerImage
	}

	resources := corev1.ResourceRequirements{}
	if cxiDriver.Spec.RetryHandler.DaemonSet != nil {
		resources = cxiDriver.Spec.RetryHandler.DaemonSet.Resources
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-retry-handler", cxiDriver.Name),
			Namespace: r.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					NodeSelector: cxiDriver.Spec.NodeSelector,
					Containers: []corev1.Container{
						{
							Name:  "retry-handler",
							Image: image,
							Args:  []string{"--mode=supervisor", "--discover=auto"},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							Resources: resources,
							Ports: []corev1.ContainerPort{
								{
									Name:          "health",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       30,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dev",
									MountPath: "/dev",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "dev",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/dev",
								},
							},
						},
					},
				},
			},
		},
	}

	return ds
}

func (r *CXIDriverReconciler) reconcileMetricsExporter(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	log := logf.FromContext(ctx)
	ds := r.buildMetricsExporterDaemonSet(cxiDriver)

	if err := controllerutil.SetControllerReference(cxiDriver, ds, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	existing := &appsv1.DaemonSet{}
	err := r.Get(ctx, client.ObjectKeyFromObject(ds), existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating metrics exporter DaemonSet", "name", ds.Name)
		if err := r.Create(ctx, ds); err != nil {
			return fmt.Errorf("creating DaemonSet %s: %w", ds.Name, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting DaemonSet %s: %w", ds.Name, err)
	}

	existing.Spec = ds.Spec
	log.Info("Updating metrics exporter DaemonSet", "name", ds.Name)
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating DaemonSet %s: %w", ds.Name, err)
	}
	return nil
}

func (r *CXIDriverReconciler) buildMetricsExporterDaemonSet(cxiDriver *cxiv1.CXIDriver) *appsv1.DaemonSet {
	labels := map[string]string{
		labelManagedBy: "slingshot-operator",
		labelComponent: componentMetricsExporter,
		labelInstance:  cxiDriver.Name,
	}

	image := cxiDriver.Spec.MetricsExporter.Image
	if image == "" {
		image = config.DefaultMetricsExporterImage
	}

	port := cxiDriver.Spec.MetricsExporter.Port
	if port == 0 {
		port = config.DefaultMetricsPort
	}

	resources := cxiDriver.Spec.MetricsExporter.Resources

	// Build args based on retry handler mode
	args := []string{
		fmt.Sprintf("--metrics-port=%d", port),
	}

	// Only add retry handler URL if retry handler is in daemonset mode
	// In sidecar mode, the retry handler runs per-pod, not per-node, so we can't query it
	// In kernel/none mode, there's no userspace retry handler to query
	if cxiDriver.Spec.RetryHandler.Enabled {
		switch cxiDriver.Spec.RetryHandler.Mode {
		case cxiv1.RetryHandlerModeDaemonSet, "":
			// DaemonSet mode: retry handler runs on same node, reachable via localhost
			args = append(args, "--retry-handler-url=http://localhost:8080")
		case cxiv1.RetryHandlerModeSidecar:
			// Sidecar mode: retry handler runs per-pod, not queryable from metrics exporter
			args = append(args, "--retry-handler-url=")
		case cxiv1.RetryHandlerModeKernel, cxiv1.RetryHandlerModeNone:
			// No userspace retry handler
			args = append(args, "--retry-handler-url=")
		}
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-metrics-exporter", cxiDriver.Name),
			Namespace: r.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/port":   fmt.Sprintf("%d", port),
						"prometheus.io/path":   "/metrics",
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector: cxiDriver.Spec.NodeSelector,
					Containers: []corev1.Container{
						{
							Name:      "metrics-exporter",
							Image:     image,
							Resources: resources,
							Args:      args,
							Env: []corev1.EnvVar{
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "spec.nodeName",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "metrics",
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt32(port),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       30,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt32(port),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "sys",
									MountPath: "/sys",
									ReadOnly:  true,
								},
								{
									Name:      "proc",
									MountPath: "/proc",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "sys",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/sys",
								},
							},
						},
						{
							Name: "proc",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/proc",
								},
							},
						},
					},
				},
			},
		},
	}

	return ds
}

func (r *CXIDriverReconciler) updateStatus(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	// Count ready/updating/failed nodes by checking DaemonSet status
	var ready, updating, failed int32

	// Check driver agent DaemonSet
	driverAgentDS := &appsv1.DaemonSet{}
	dsName := fmt.Sprintf("%s-driver-agent", cxiDriver.Name)
	if err := r.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: dsName}, driverAgentDS); err == nil {
		ready = driverAgentDS.Status.NumberReady
		updating = driverAgentDS.Status.DesiredNumberScheduled - driverAgentDS.Status.NumberReady
		failed = driverAgentDS.Status.NumberUnavailable
	}

	cxiDriver.Status.Ready = ready
	cxiDriver.Status.Updating = updating
	cxiDriver.Status.Failed = failed
	cxiDriver.Status.DriverVersion = cxiDriver.Spec.Version
	cxiDriver.Status.RetryHandlerMode = cxiDriver.Spec.RetryHandler.Mode
	cxiDriver.Status.ObservedGeneration = cxiDriver.Generation

	// Set available condition
	if ready > 0 && failed == 0 {
		meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
			Type:    conditionTypeAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  "Ready",
			Message: fmt.Sprintf("%d nodes ready", ready),
		})
		meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
			Type:    conditionTypeProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  "Deployed",
			Message: "All components deployed",
		})
		meta.RemoveStatusCondition(&cxiDriver.Status.Conditions, conditionTypeDegraded)
	} else if failed > 0 {
		r.setDegradedCondition(cxiDriver, "NodesFailed", fmt.Sprintf("%d nodes failed", failed))
	}

	return r.updateStatusRetryOnConflict(ctx, cxiDriver)
}

func (r *CXIDriverReconciler) updateStatusRetryOnConflict(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Re-fetch latest version to avoid conflicts
		latest := &cxiv1.CXIDriver{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(cxiDriver), latest); err != nil {
			return fmt.Errorf("fetching latest CXIDriver: %w", err)
		}
		// Apply our status updates to the latest version
		latest.Status = cxiDriver.Status
		if err := r.Status().Update(ctx, latest); err != nil {
			return fmt.Errorf("updating CXIDriver status: %w", err)
		}
		return nil
	})
}

//nolint:unparam // req may be needed for future logging/context
func (r *CXIDriverReconciler) updateStatusWithRetry(ctx context.Context, req ctrl.Request, cxiDriver *cxiv1.CXIDriver) (ctrl.Result, error) {
	cxiDriver.Status.ObservedGeneration = cxiDriver.Generation
	if err := r.updateStatusRetryOnConflict(ctx, cxiDriver); err != nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *CXIDriverReconciler) setComponentCondition(cxiDriver *cxiv1.CXIDriver, conditionType string, ready bool, reason, message string) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func (r *CXIDriverReconciler) setDegradedCondition(cxiDriver *cxiv1.CXIDriver, reason, message string) {
	meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
		Type:    conditionTypeDegraded,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	meta.SetStatusCondition(&cxiDriver.Status.Conditions, metav1.Condition{
		Type:    conditionTypeAvailable,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

func (r *CXIDriverReconciler) reconcileSidecarWebhook(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	log := logf.FromContext(ctx)

	webhookName := fmt.Sprintf("%s-sidecar-injector", cxiDriver.Name)
	serviceName := "slingshot-operator-webhook-service"
	webhookPath := "/mutate-v1-pod"

	failurePolicy := admissionregistrationv1.Ignore
	sideEffects := admissionregistrationv1.SideEffectClassNone
	matchPolicy := admissionregistrationv1.Equivalent

	webhook := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
			Labels: map[string]string{
				labelManagedBy: "slingshot-operator",
				labelInstance:  cxiDriver.Name,
			},
			Annotations: map[string]string{
				// cert-manager will inject the CA bundle from the operator's serving certificate
				// Users must ensure cert-manager is installed and a Certificate resource exists
				// for the webhook service, or manually inject the CA bundle
				"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/slingshot-operator-serving-cert", r.Namespace),
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name: "sidecar-injector.slingshot.hpe.com",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      serviceName,
						Namespace: r.Namespace,
						Path:      &webhookPath,
					},
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
				FailurePolicy:           &failurePolicy,
				SideEffects:             &sideEffects,
				MatchPolicy:             &matchPolicy,
				AdmissionReviewVersions: []string{"v1"},
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "slingshot.hpe.com/inject-sidecar",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"enabled", "true"},
						},
					},
				},
			},
		},
	}

	existing := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := r.Get(ctx, client.ObjectKey{Name: webhookName}, existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating sidecar injector MutatingWebhookConfiguration", "name", webhookName)
		if err := r.Create(ctx, webhook); err != nil {
			return fmt.Errorf("creating MutatingWebhookConfiguration %s: %w", webhookName, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("getting MutatingWebhookConfiguration %s: %w", webhookName, err)
	}

	existing.Webhooks = webhook.Webhooks
	log.Info("Updating sidecar injector MutatingWebhookConfiguration", "name", webhookName)
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating MutatingWebhookConfiguration %s: %w", webhookName, err)
	}
	return nil
}

func (r *CXIDriverReconciler) cleanupResources(ctx context.Context, cxiDriver *cxiv1.CXIDriver) error {
	log := logf.FromContext(ctx)
	log.Info("Cleaning up resources for CXIDriver", "name", cxiDriver.Name)

	// Clean up webhook if sidecar mode was used
	if cxiDriver.Spec.RetryHandler.Mode == cxiv1.RetryHandlerModeSidecar {
		webhookName := fmt.Sprintf("%s-sidecar-injector", cxiDriver.Name)
		webhook := &admissionregistrationv1.MutatingWebhookConfiguration{}
		if err := r.Get(ctx, client.ObjectKey{Name: webhookName}, webhook); err == nil {
			log.Info("Deleting sidecar injector webhook", "name", webhookName)
			if err := r.Delete(ctx, webhook); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting MutatingWebhookConfiguration %s: %w", webhookName, err)
			}
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting MutatingWebhookConfiguration %s for cleanup: %w", webhookName, err)
		}
	}

	// DaemonSets will be garbage collected via owner references
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CXIDriverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cxiv1.CXIDriver{}).
		Owns(&appsv1.DaemonSet{}).
		Named("cxidriver").
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
				100*time.Millisecond, // min delay
				5*time.Minute,        // max delay
			),
		}).
		Complete(r)
}

// getActiveCXIDriver returns the name of the currently active CXIDriver from the singleton ConfigMap.
// Returns empty string if no active driver is set.
func (r *CXIDriverReconciler) getActiveCXIDriver(ctx context.Context) (string, error) {
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: singletonConfigMapName}, cm)
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("getting singleton ConfigMap: %w", err)
	}
	return cm.Data[singletonActiveKey], nil
}

// setActiveCXIDriver sets or clears the active CXIDriver in the singleton ConfigMap.
// Pass empty string to clear the active driver.
func (r *CXIDriverReconciler) setActiveCXIDriver(ctx context.Context, name string) error {
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Namespace: r.Namespace, Name: singletonConfigMapName}, cm)

	if apierrors.IsNotFound(err) {
		if name == "" {
			return nil
		}
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      singletonConfigMapName,
				Namespace: r.Namespace,
				Labels: map[string]string{
					labelManagedBy: "slingshot-operator",
				},
			},
			Data: map[string]string{
				singletonActiveKey: name,
			},
		}
		if err := r.Create(ctx, cm); err != nil {
			return fmt.Errorf("creating singleton ConfigMap: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting singleton ConfigMap: %w", err)
	}

	if name == "" {
		delete(cm.Data, singletonActiveKey)
	} else {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		cm.Data[singletonActiveKey] = name
	}

	if err := r.Update(ctx, cm); err != nil {
		return fmt.Errorf("updating singleton ConfigMap: %w", err)
	}
	return nil
}

// isActiveCXIDriver checks if the given CXIDriver is the active singleton.
// If no active driver is set and the given driver exists, it will be set as active.
func (r *CXIDriverReconciler) isActiveCXIDriver(ctx context.Context, name string) (bool, error) {
	active, err := r.getActiveCXIDriver(ctx)
	if err != nil {
		return false, err
	}

	if active == "" {
		if err := r.setActiveCXIDriver(ctx, name); err != nil {
			return false, err
		}
		return true, nil
	}

	if active == name {
		return true, nil
	}

	activeDriver := &cxiv1.CXIDriver{}
	if err := r.Get(ctx, client.ObjectKey{Name: active}, activeDriver); err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.setActiveCXIDriver(ctx, name); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, fmt.Errorf("checking active CXIDriver: %w", err)
	}

	return false, nil
}
