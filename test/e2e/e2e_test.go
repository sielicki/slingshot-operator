//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sielicki/slingshot-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "slingshot-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "slingshot-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "slingshot-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "slingshot-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=slingshot-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		It("should reconcile CXIDriver and create DaemonSets", func() {
			By("applying a CXIDriver resource")
			cmd := exec.Command("kubectl", "apply", "-f", "config/samples/cxi_v1_cxidriver.yaml")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply CXIDriver")

			By("verifying the device plugin DaemonSet is created")
			verifyDevicePluginDS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "daemonset", "cxi-driver-device-plugin",
					"-n", namespace, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("cxi-driver-device-plugin"))
			}
			Eventually(verifyDevicePluginDS, 2*time.Minute, time.Second).Should(Succeed())

			By("verifying the driver agent DaemonSet is created")
			verifyDriverAgentDS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "daemonset", "cxi-driver-driver-agent",
					"-n", namespace, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("cxi-driver-driver-agent"))
			}
			Eventually(verifyDriverAgentDS, 2*time.Minute, time.Second).Should(Succeed())

			By("verifying the CXIDriver status is updated with Available condition")
			verifyCXIDriverStatus := func(g Gomega) {
				// In a Kind cluster without actual CXI hardware and component images,
				// pods won't be ready, so Available may be False. We verify the condition exists.
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver",
					"-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				// Verify the Available condition has been set (either True or False)
				g.Expect(output).To(Or(Equal("True"), Equal("False")),
					"Available condition should be set")
			}
			Eventually(verifyCXIDriverStatus, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying reconcile metrics are recorded")
			verifyReconcileMetrics := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(ContainSubstring(`controller_runtime_reconcile_total{controller="cxidriver"`))
			}
			Eventually(verifyReconcileMetrics, 2*time.Minute).Should(Succeed())

			By("deleting the CXIDriver resource")
			cmd = exec.Command("kubectl", "delete", "-f", "config/samples/cxi_v1_cxidriver.yaml")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete CXIDriver")

			By("verifying the DaemonSets are cleaned up")
			verifyCleanup := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "daemonset", "cxi-driver-device-plugin",
					"-n", namespace, "-o", "jsonpath={.metadata.name}")
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}
			Eventually(verifyCleanup, 2*time.Minute, time.Second).Should(Succeed())
		})

		It("should handle retry handler mode configuration", func() {
			By("creating a CXIDriver with daemonset retry handler mode")
			cxiDriverYAML := `
apiVersion: cxi.hpe.com/v1
kind: CXIDriver
metadata:
  name: cxi-driver-rh-test
  namespace: ` + namespace + `
spec:
  version: "1.8.3"
  driverSource:
    type: preinstalled
  retryHandler:
    enabled: true
    mode: daemonset
  devicePlugin:
    enabled: true
    sharingMode: shared
`
			_, err := utils.Run(exec.Command("sh", "-c", fmt.Sprintf("echo '%s' | kubectl apply -f -", cxiDriverYAML)))
			Expect(err).NotTo(HaveOccurred())

			By("verifying the retry handler DaemonSet is created")
			verifyRetryHandlerDS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "daemonset", "cxi-driver-rh-test-retry-handler",
					"-n", namespace, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("cxi-driver-rh-test-retry-handler"))
			}
			Eventually(verifyRetryHandlerDS, 2*time.Minute, time.Second).Should(Succeed())

			By("cleaning up the test CXIDriver")
			cmd := exec.Command("kubectl", "delete", "cxidriver", "cxi-driver-rh-test", "-n", namespace)
			_, _ = utils.Run(cmd)
		})
	})

	Context("Singleton Enforcement", func() {
		AfterEach(func() {
			// Clean up any CXIDriver resources created during tests
			cmd := exec.Command("kubectl", "delete", "cxidriver", "--all", "--ignore-not-found")
			_, _ = utils.Run(cmd)
			// Wait for cleanup to complete
			time.Sleep(5 * time.Second)
		})

		It("should ignore second CXIDriver and mark it Degraded", func() {
			By("creating the first CXIDriver")
			firstDriverYAML := `
apiVersion: cxi.hpe.com/v1
kind: CXIDriver
metadata:
  name: cxi-driver-first
spec:
  version: "1.8.3"
  source:
    type: preinstalled
  devicePlugin:
    enabled: true
    sharingMode: shared
`
			_, err := utils.Run(exec.Command("sh", "-c", fmt.Sprintf("echo '%s' | kubectl apply -f -", firstDriverYAML)))
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the first CXIDriver to become active")
			verifyFirstActive := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-first",
					"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyFirstActive, 2*time.Minute, time.Second).Should(Succeed())

			By("creating the second CXIDriver")
			secondDriverYAML := `
apiVersion: cxi.hpe.com/v1
kind: CXIDriver
metadata:
  name: cxi-driver-second
spec:
  version: "1.8.3"
  source:
    type: preinstalled
  devicePlugin:
    enabled: true
    sharingMode: shared
`
			_, err = utils.Run(exec.Command("sh", "-c", fmt.Sprintf("echo '%s' | kubectl apply -f -", secondDriverYAML)))
			Expect(err).NotTo(HaveOccurred())

			By("verifying the second CXIDriver is marked as Degraded with AnotherCXIDriverActive")
			verifySecondDegraded := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-second",
					"-o", "jsonpath={.status.conditions[?(@.type=='Degraded')].reason}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("AnotherCXIDriverActive"))
			}
			Eventually(verifySecondDegraded, 2*time.Minute, time.Second).Should(Succeed())

			By("verifying the second CXIDriver has Available=False")
			verifySecondNotAvailable := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-second",
					"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("False"))
			}
			Eventually(verifySecondNotAvailable, 30*time.Second, time.Second).Should(Succeed())

			By("verifying no DaemonSets were created for the second CXIDriver")
			cmd := exec.Command("kubectl", "get", "daemonset", "cxi-driver-second-device-plugin",
				"-n", namespace, "-o", "jsonpath={.metadata.name}")
			_, err = utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "Second CXIDriver should not have created DaemonSets")
		})

		It("should allow second CXIDriver to become active after first is deleted", func() {
			By("creating the first CXIDriver")
			firstDriverYAML := `
apiVersion: cxi.hpe.com/v1
kind: CXIDriver
metadata:
  name: cxi-driver-primary
spec:
  version: "1.8.3"
  source:
    type: preinstalled
  devicePlugin:
    enabled: true
    sharingMode: shared
`
			_, err := utils.Run(exec.Command("sh", "-c", fmt.Sprintf("echo '%s' | kubectl apply -f -", firstDriverYAML)))
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the first CXIDriver to become active")
			verifyFirstActive := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-primary",
					"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyFirstActive, 2*time.Minute, time.Second).Should(Succeed())

			By("creating the second CXIDriver (will be ignored initially)")
			secondDriverYAML := `
apiVersion: cxi.hpe.com/v1
kind: CXIDriver
metadata:
  name: cxi-driver-secondary
spec:
  version: "1.8.3"
  source:
    type: preinstalled
  devicePlugin:
    enabled: true
    sharingMode: shared
`
			_, err = utils.Run(exec.Command("sh", "-c", fmt.Sprintf("echo '%s' | kubectl apply -f -", secondDriverYAML)))
			Expect(err).NotTo(HaveOccurred())

			By("verifying the second CXIDriver is initially Degraded")
			verifySecondDegraded := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-secondary",
					"-o", "jsonpath={.status.conditions[?(@.type=='Degraded')].reason}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("AnotherCXIDriverActive"))
			}
			Eventually(verifySecondDegraded, 2*time.Minute, time.Second).Should(Succeed())

			By("deleting the first CXIDriver")
			cmd := exec.Command("kubectl", "delete", "cxidriver", "cxi-driver-primary")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the first CXIDriver to be fully deleted")
			verifyFirstDeleted := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-primary",
					"-o", "jsonpath={.metadata.name}")
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}
			Eventually(verifyFirstDeleted, 2*time.Minute, time.Second).Should(Succeed())

			By("triggering reconciliation of the second CXIDriver by updating it")
			// Add an annotation to trigger reconciliation
			cmd = exec.Command("kubectl", "annotate", "cxidriver", "cxi-driver-secondary",
				"reconcile-trigger="+fmt.Sprintf("%d", time.Now().Unix()), "--overwrite")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the second CXIDriver becomes active")
			verifySecondActive := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-secondary",
					"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifySecondActive, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying the Degraded condition is removed from the second CXIDriver")
			verifyNoDegraded := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cxidriver", "cxi-driver-secondary",
					"-o", "jsonpath={.status.conditions[?(@.type=='Degraded')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				// Empty output means the condition doesn't exist, or it should be False
				g.Expect(output).To(SatisfyAny(BeEmpty(), Equal("False")))
			}
			Eventually(verifyNoDegraded, 30*time.Second, time.Second).Should(Succeed())

			By("verifying DaemonSets are created for the second CXIDriver")
			verifySecondDaemonSets := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "daemonset", "cxi-driver-secondary-device-plugin",
					"-n", namespace, "-o", "jsonpath={.metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("cxi-driver-secondary-device-plugin"))
			}
			Eventually(verifySecondDaemonSets, 2*time.Minute, time.Second).Should(Succeed())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
