package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	//+kubebuilder:scaffold:imports
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg     *rest.Config
	testEnv *envtest.Environment
	// TODO: reduce to 2 seconds
	timeout  = 2 * time.Minute
	interval = 1 * time.Second
	nfImage  = "ghcr.io/ovn-kubernetes/kubernetes-traffic-flow-tests:latest"
	nfName   = "test-nf"
	sfcName  = "sfc-test"
)

func pingTest(srcClientSet kubernetes.Interface, srcRestConfig *rest.Config, srcPod *corev1.Pod, destIP, srcName, destName string) {
	Eventually(func() bool {
		out, err := testutils.ExecInPod(srcClientSet, srcRestConfig, srcPod, fmt.Sprintf("ping -c 4 %s", destIP))
		if err != nil {
			fmt.Printf("Ping failed: %v\n%s\n", err, out)
		}
		fmt.Printf("%s -> %s ping: %s\n", srcName, destName, out)
		return err == nil
	}, testutils.TestAPITimeout*15, testutils.TestRetryInterval).Should(BeTrue(), "%s failed to reach %s via ping", srcName, destName)
}

func getEnv(key string) (string, error) {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return "", fmt.Errorf("missing required environment variable: %s", key)
	}
	return value, nil
}

// Configure external client and defer clean up to maintain idempotency
func setupExternalClient(externalClientIp, externalClientDev, workloadSubnet string) {
	fmt.Printf("Assigning external client device %s IP address %s\n", externalClientDev, externalClientIp)
	cmd := exec.Command("sudo", "ip", "addr", "replace", externalClientIp+"/24", "dev", externalClientDev)
	_, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred())

	g.DeferCleanup(func() {
		fmt.Printf("Removing device %s IP address %s\n", externalClientDev, externalClientIp)
		cmd := exec.Command("sudo", "ip", "addr", "del", externalClientIp+"/24", "dev", externalClientDev)
		_, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: Failed to remove IP from external client: %v\n", err)
		}
	})

	fmt.Printf("Setting route on external client to reach pod network %s over device %s\n", workloadSubnet, externalClientDev)
	cmd = exec.Command("sudo", "ip", "route", "replace", workloadSubnet, "dev", externalClientDev)
	_, err = cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred())

	g.DeferCleanup(func() {
		fmt.Printf("Removing route on external client for device %s\n", externalClientDev)
		cmd := exec.Command("sudo", "ip", "route", "del", workloadSubnet, "dev", externalClientDev)
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: Failed to remove route: %v, output: %s\n", err, output)
		}
	})
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(g.Fail)

	g.RunSpecs(t, "e2e tests")
}

var _ = g.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(g.GinkgoWriter), zap.UseDevMode(true)))
})

var _ = g.AfterSuite(func() {
	// Nothing needed
})

var _ = g.Describe("E2E integration testing", g.Ordered, func() {
	var (
		dpuSideClient                      client.Client
		hostSideClient                     client.Client
		hostRestConfig                     *rest.Config
		dpuRestConfig                      *rest.Config
		hostClientSet                      *kubernetes.Clientset
		dpuClientSet                       *kubernetes.Clientset
		restoreDpuOperatorConfigInAfterAll func()
	)

	g.AfterAll(func() {
		if restoreDpuOperatorConfigInAfterAll != nil {
			restoreDpuOperatorConfigInAfterAll()
			restoreDpuOperatorConfigInAfterAll = nil
		}
	})

	g.BeforeEach(func() {
		var err error
		cluster := testutils.CdaCluster{
			Name:           "",
			HostConfigPath: "hack/cluster-configs/config-dpu-host.yaml",
			DpuConfigPath:  "hack/cluster-configs/config-dpu.yaml",
		}
		hostRestConfig, dpuRestConfig, err = cluster.EnsureExists()
		Expect(err).NotTo(HaveOccurred())

		hostSideClient, err = client.New(hostRestConfig, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		dpuSideClient, err = client.New(dpuRestConfig, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		hostClientSet, err = kubernetes.NewForConfig(hostRestConfig)
		Expect(err).NotTo(HaveOccurred())

		dpuClientSet, err = kubernetes.NewForConfig(dpuRestConfig)
		Expect(err).NotTo(HaveOccurred())

	})

	g.AfterEach(func() {
	})

	g.Context("when Validating Webhook for DpuOperatorConfig is running", func() {

		skipTests := false

		g.It("should handle precondition of existing DpuOperatorConfig to prepare test environment", func() {
			dpuOperatorConfig0 := &configv1.DpuOperatorConfig{}
			err := dpuSideClient.Get(context.TODO(), configv1.DpuOperatorConfigNamespacedName, dpuOperatorConfig0)
			if err == nil {
				/* This test runs against a real cluster. Such a cluster commonly already
				 * has a DpuOperatorConfig. This test wants to create different DpuOperatorConfig
				 * to check whether the validating webhook is running. For that, it first needs
				 * to delete (and later restore, see "restoreDpuOperatorConfigInAfterAll") the
				 * DpuOperatorConfig.
				 *
				 * However, the followup test for SFC has a problem with that, and will fail
				 * when recreating the DpuOperatorConfig.
				 *
				 * As temporary workaround, skip.
				 */
				skipTests = true
				g.Skip("Skipping test as external DpuOperatorConfig already exists")
			}
			if err == nil {
				spec := dpuOperatorConfig0.Spec

				restoreDpuOperatorConfigInAfterAll = func() {
					/* We are about to delete the DpuOperatorConfig so we can run our test.
					 * At the end, we will create a new one via this function. */
					config := configv1.DpuOperatorConfig{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: vars.Namespace,
							Name:      vars.DpuOperatorConfigName,
						},
						Spec: spec,
					}
					err := dpuSideClient.Create(context.TODO(), &config)
					Expect(err).NotTo(HaveOccurred())
					config = configv1.DpuOperatorConfig{}
					Eventually(func() error {
						return dpuSideClient.Get(context.TODO(), configv1.DpuOperatorConfigNamespacedName, &config)
					}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(Succeed())
				}

				err = dpuSideClient.Delete(context.TODO(), dpuOperatorConfig0)
				Expect(err).NotTo(HaveOccurred())
			}

			Eventually(func() error {
				config := configv1.DpuOperatorConfig{}
				return dpuSideClient.Get(context.TODO(), configv1.DpuOperatorConfigNamespacedName, &config)
			}, testutils.TestAPITimeout, testutils.TestRetryInterval).ShouldNot(Succeed())
		})

		g.It("should fail to add DpuOperatorConfig with invalid Name", func() {
			if skipTests {
				g.Skip("Skipping test as external DpuOperatorConfig already exists")
			}
			configInvalidName := configv1.DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: vars.Namespace,
					Name:      "invalidname",
				},
				Spec: configv1.DpuOperatorConfigSpec{
					Mode:     "host",
					LogLevel: 2,
				},
			}
			err := dpuSideClient.Create(context.TODO(), &configInvalidName)
			Expect(err).To(MatchError(ContainSubstring("DpuOperatorConfig must have standard name")))
		})

		g.It("should fail to add DpuOperatorConfig with invalid Mode", func() {
			if skipTests {
				g.Skip("Skipping test as external DpuOperatorConfig already exists")
			}
			configInvalidMode := configv1.DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: vars.Namespace,
					Name:      vars.DpuOperatorConfigName,
				},
				Spec: configv1.DpuOperatorConfigSpec{
					Mode:     "invalidmode",
					LogLevel: 2,
				},
			}
			err := dpuSideClient.Create(context.TODO(), &configInvalidMode)
			Expect(err).To(MatchError(ContainSubstring("Invalid mode")))
		})

		createDpuOperatorConfig := func() error {
			configGood := configv1.DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: vars.Namespace,
					Name:      vars.DpuOperatorConfigName,
				},
				Spec: configv1.DpuOperatorConfigSpec{
					Mode:     "auto",
					LogLevel: 2,
				},
			}

			return dpuSideClient.Create(context.TODO(), &configGood)
		}

		deleteDpuOperatorConfig := func() error {
			configToDelete := configv1.DpuOperatorConfig{}
			err := dpuSideClient.Get(context.TODO(), configv1.DpuOperatorConfigNamespacedName, &configToDelete)
			if err != nil {
				return err
			}
			return dpuSideClient.Delete(context.TODO(), &configToDelete)
		}

		g.It("should succeed to add a standard DpuOperatorConfig", func() {
			if skipTests {
				g.Skip("Skipping test as external DpuOperatorConfig already exists")
			}
			Expect(createDpuOperatorConfig()).NotTo(HaveOccurred())
			Expect(deleteDpuOperatorConfig()).NotTo(HaveOccurred())
		})

		g.It("Should handle update to DpuOperatorConfig", func() {
			if skipTests {
				g.Skip("Skipping test as external DpuOperatorConfig already exists")
			}

			Expect(createDpuOperatorConfig()).NotTo(HaveOccurred())

			configToUpdate := configv1.DpuOperatorConfig{}
			err := dpuSideClient.Get(context.TODO(), configv1.DpuOperatorConfigNamespacedName, &configToUpdate)
			Expect(err).NotTo(HaveOccurred())

			configToUpdate.Spec.Mode = "invalidmode"
			err = dpuSideClient.Update(context.TODO(), &configToUpdate)
			Expect(err).To(MatchError(ContainSubstring("Invalid mode")))

			configToUpdate.Spec.Mode = "dpu"
			err = dpuSideClient.Update(context.TODO(), &configToUpdate)
			Expect(err).NotTo(HaveOccurred())

			Expect(deleteDpuOperatorConfig()).NotTo(HaveOccurred())
		})
	})

	g.Context("When Dpu Operator components are deployed and configured", g.Ordered, func() {
		var (
			testPodName                = "test-pod-1"
			testPod2Name               = "test-pod-2"
			secondaryNetDev            = "net1"
			pod1                       *corev1.Pod
			pod2                       *corev1.Pod
			nfPod                      *corev1.Pod
			pod1_ip                    string
			pod2_ip                    string
			workloadSubnet             string
			nfIngressIp                string
			nfEgressIp                 string
			externalClientIp           string
			externalClientDev          string
			externalSubnet             string
			skipNetworkFunctionTesting = false
		)

		sfc := &configv1.ServiceFunctionChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sfcName,
				Namespace: vars.Namespace,
			},
			Spec: configv1.ServiceFunctionChainSpec{
				NetworkFunctions: []configv1.NetworkFunction{
					{
						Name:  nfName,
						Image: nfImage,
					},
				},
			},
		}

		g.BeforeAll(func() {
			nodeList, err := testutils.GetDPUNodes(hostSideClient)
			Expect(err).NotTo(HaveOccurred())
			pod := testutils.NewTestPod(testPodName, nodeList[0].Name)
			pod2 := testutils.NewTestPod(testPod2Name, nodeList[0].Name)

			err = hostSideClient.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			err = hostSideClient.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			// We do not know anything about the topology where these tests run. Therefore in order to test external connectivity we
			// should allow to user to provide this information via environment variables. This script will configure the routes / networks
			// necessary to validate connectivity to the external client in an idempotent fashion

			// TODO: We currently make the assumption that the external client is also the host where we are running these tests. This
			// is not necessarily true in all cases, we should update this test to handle an arbitrary external client (i.e. another Node)

			// This should be the IP we expect to be able to reach from the DPU's secondary network
			externalClientIp, err = getEnv("EXTERNAL_CLIENT_IP")
			Expect(err).NotTo(HaveOccurred())
			// This should be the dev on the external client that is connected to the DPU's secondary network
			externalClientDev, err = getEnv("EXTERNAL_CLIENT_DEV")
			Expect(err).NotTo(HaveOccurred())
			// This should be the IP we want to have assigned to the ingress of the NF and reachable from the external client
			nfIngressIp, err = getEnv("NF_INGRESS_IP")
			Expect(err).NotTo(HaveOccurred())

			// Currently Marvell does have support for layer 3 network function connectivity, and it is possible future DPU vendors / solutions
			// may take a similar approach. By default we will run the full test-suite but the user can opt out of the network function connectivity
			// tests if running on hardware where this does not make sense.
			if skipNF, exists := os.LookupEnv("SKIP_NF_TESTING"); exists && skipNF != "" {
				skipNetworkFunctionTesting = true
			}

			externalSubnet = testutils.GetSubnet(externalClientIp)
			nfSubnet := testutils.GetSubnet(nfIngressIp)
			Expect(externalSubnet).To(Equal(nfSubnet),
				fmt.Sprintf("External client IP %s (subnet: %s) and NF ingress IP %s (subnet: %s) are not in the same subnet",
					externalClientIp, externalSubnet, nfIngressIp, nfSubnet))
		})

		g.It("Should be able to start host workload pods", func() {
			fmt.Println("Creating workload pods")
			Eventually(func() bool {
				return testutils.PodIsRunning(hostSideClient, testPodName, "default")
			}, testutils.TestAPITimeout*45*2, testutils.TestRetryInterval).Should(BeTrue(), "Pod did not become running in expected time")
			Eventually(func() bool {
				return testutils.PodIsRunning(hostSideClient, testPod2Name, "default")
			}, testutils.TestAPITimeout*45*2, testutils.TestRetryInterval).Should(BeTrue(), "Pod did not become running in expected time")
			fmt.Println("Workload pods reached Ready state")

		})
		g.It("Should be able to pass traffic between workload pods", func() {
			var err error
			pod1 = testutils.GetPod(hostSideClient, testPodName, "default")
			Expect(pod1).NotTo(BeNil(), "Unable to retrieve pod1")
			pod2 = testutils.GetPod(hostSideClient, testPod2Name, "default")
			Expect(pod2).NotTo(BeNil(), "Unable to retrieve pod2")

			pod1_ip, err = testutils.GetSecondaryNetworkIP(pod1, secondaryNetDev)
			Expect(err).NotTo(HaveOccurred())
			pod2_ip, err = testutils.GetSecondaryNetworkIP(pod2, secondaryNetDev)
			Expect(err).NotTo(HaveOccurred())
			fmt.Printf("pod1 ip: %s\n pod2 ip: %s\n", pod1_ip, pod2_ip)

			// Test pod-to-pod connectivity
			fmt.Println("Testing pod-to-pod connectivity")
			pingTest(hostClientSet, hostRestConfig, pod1, pod2_ip, pod1.Name, pod2.Name)
			pingTest(hostClientSet, hostRestConfig, pod2, pod1_ip, pod2.Name, pod1.Name)
		})
		g.Context("ServiceFunctionChain", g.Ordered, func() {
			g.It("Should create a pod when creating an SFC", func() {
				// TODO: This test has a race condition, it may pass if the pod is being created
				Eventually(func() bool {
					return testutils.GetPod(dpuSideClient, nfName, vars.Namespace) == nil
				}, timeout, interval).Should(BeTrue())

				fmt.Println("Creating test SFC")
				err := dpuSideClient.Create(context.TODO(), sfc)
				Expect(err).NotTo(HaveOccurred())

				podList := &corev1.PodList{}
				err = dpuSideClient.List(context.TODO(), podList, client.InNamespace(vars.Namespace))
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() bool {
					nfPod = testutils.GetPod(dpuSideClient, nfName, vars.Namespace)
					if nfPod != nil {
						return nfPod.Spec.Containers[0].Image == nfImage && nfPod.Status.Phase == corev1.PodRunning
					}
					return false
				}, timeout, interval).Should(BeTrue())
				fmt.Println("Nf pod successfully created")
			})
			g.It("Should support pod -> pod with Network-Function deployed", func() {
				fmt.Println("Testing pod-to-pod connectivity w/ Network Function Deployed")
				pingTest(hostClientSet, hostRestConfig, pod1, pod2_ip, pod1.Name, pod2.Name)
				pingTest(hostClientSet, hostRestConfig, pod2, pod1_ip, pod2.Name, pod1.Name)
			})
			g.It("Should support pod -> Network-Function", func() {
				if skipNetworkFunctionTesting {
					g.Skip("Skipping Network Function Testing")
				}

				// We are assuming that net2 will always be ingress and net1 will always be egress, need to verify this assumption is reasonable
				// In this context, egress refers to traffic between the network function and the host and ingress refers to traffic between an
				// external client and the network function
				usedIPs := map[string]bool{
					pod1_ip: true,
					pod2_ip: true,
				}
				workloadSubnet = testutils.GetSubnet(pod1_ip)
				nfEgressIp = testutils.GenerateAvailableIP(workloadSubnet, usedIPs)
				fmt.Printf("Assigning NF egress port IP %s\n", nfEgressIp)
				_, err := testutils.ExecInPod(dpuClientSet, dpuRestConfig, nfPod, fmt.Sprintf("ip addr add %s dev net1", nfEgressIp+"/24"))
				Expect(err).NotTo(HaveOccurred())
				// Explicitly set the route just incase
				_, err = testutils.ExecInPod(dpuClientSet, dpuRestConfig, nfPod, fmt.Sprintf("ip route add %s dev net1", workloadSubnet))

				fmt.Println("Testing pod-to-NF connectivity")
				pingTest(hostClientSet, hostRestConfig, pod1, nfEgressIp, pod1.Name, nfPod.Name)
				pingTest(hostClientSet, hostRestConfig, pod2, nfEgressIp, pod2.Name, nfPod.Name)
				pingTest(dpuClientSet, dpuRestConfig, nfPod, pod1_ip, nfPod.Name, pod1.Name)
				pingTest(dpuClientSet, dpuRestConfig, nfPod, pod2_ip, nfPod.Name, pod2.Name)
			})
			g.It("Should support Network-function -> external", func() {
				if skipNetworkFunctionTesting {
					g.Skip("Skipping Network Function Testing")
				}

				fmt.Printf("Assigning NF ingress port IP %s\n", nfIngressIp)
				_, err := testutils.ExecInPod(dpuClientSet, dpuRestConfig, nfPod, fmt.Sprintf("ip addr add %s dev net2", nfIngressIp+"/24"))
				Expect(err).NotTo(HaveOccurred())
				// Explicitly set the route just incase
				_, err = testutils.ExecInPod(dpuClientSet, dpuRestConfig, nfPod, fmt.Sprintf("ip route replace %s dev net2", externalSubnet))

				setupExternalClient(externalClientIp, externalClientDev, workloadSubnet)

				fmt.Println("Testing NF-to-external connectivity")
				pingTest(dpuClientSet, dpuRestConfig, nfPod, externalClientIp, nfPod.Name, "external")
			})
			g.It("Should support pod -> external", func() {
				if skipNetworkFunctionTesting {
					g.Skip("Skipping Network Function Testing")
				}

				fmt.Printf("Setting route to %s in workload pods\n", externalSubnet)
				_, err := testutils.ExecInPod(hostClientSet, hostRestConfig, pod1, fmt.Sprintf("ip route add %s dev net1", externalSubnet))
				Expect(err).NotTo(HaveOccurred())
				_, err = testutils.ExecInPod(hostClientSet, hostRestConfig, pod2, fmt.Sprintf("ip route add %s dev net1", externalSubnet))
				Expect(err).NotTo(HaveOccurred())

				setupExternalClient(externalClientIp, externalClientDev, workloadSubnet)

				fmt.Println("Testing pod-to-external connectivity")
				pingTest(hostClientSet, hostRestConfig, pod1, externalClientIp, pod1.Name, "external")
				pingTest(hostClientSet, hostRestConfig, pod2, externalClientIp, pod2.Name, "external")

			})
			g.It("Should delete the network function pod when deleting an SFC", func() {
				err := dpuSideClient.Delete(context.TODO(), sfc)
				Expect(err).NotTo(HaveOccurred())
				fmt.Println("Finishing up")

				Eventually(func() bool {
					return testutils.GetPod(dpuSideClient, nfName, vars.Namespace) == nil
				}, timeout, interval).Should(BeTrue())
			})
		})
		g.AfterAll(func() {
			// To maintain idempotency, make sure to clean up the SFC we created incase an earlier test failed
			err := dpuSideClient.Delete(context.TODO(), sfc)
			if err != nil && !errors.IsNotFound(err) {
				fmt.Printf("Failed to delete SFC: %v\n", err)
			}

			for _, podName := range []string{testPodName, testPod2Name} {
				err := hostSideClient.Delete(context.TODO(), &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      podName,
						Namespace: "default",
					},
				})
				if err != nil && !errors.IsNotFound(err) {
					fmt.Printf("Failed to delete pod %s\n", podName)
					continue
				}

				fmt.Printf("Waiting for pod %s to be fully deleted...\n", podName)
				Eventually(func() bool {
					pod := &corev1.Pod{}
					err := hostSideClient.Get(context.TODO(), client.ObjectKey{
						Name:      podName,
						Namespace: "default",
					}, pod)

					return err != nil && errors.IsNotFound(err)
				}, testutils.TestAPITimeout*30*2, testutils.TestRetryInterval).Should(BeTrue(), "Pod %s was not fully deleted in time", podName)
			}
		})
	})
})
