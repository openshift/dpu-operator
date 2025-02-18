package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	//+kubebuilder:scaffold:imports
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

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

var _ = g.Describe("Dpu side", g.Ordered, func() {
	var (
		dpuSideClient                      client.Client
		restoreDpuOperatorConfigInAfterAll func()
	)

	g.AfterAll(func() {
		if restoreDpuOperatorConfigInAfterAll != nil {
			restoreDpuOperatorConfigInAfterAll()
			restoreDpuOperatorConfigInAfterAll = nil
		}
	})

	g.BeforeEach(func() {
		cluster := testutils.CdaCluster{
			Name:           "",
			HostConfigPath: "hack/cluster-configs/config-dpu-host.yaml",
			DpuConfigPath:  "hack/cluster-configs/config-dpu.yaml",
		}
		_, dpuConfig, err := cluster.EnsureExists()
		Expect(err).NotTo(HaveOccurred())

		dpuSideClient, err = client.New(dpuConfig, client.Options{Scheme: scheme.Scheme})
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

	g.Context("ServiceFunctionChain", func() {
		g.It("Should create a pod when creating an SFC", func() {
			nfName := "example-nf"
			nfImage := "example-nf-image-url"
			ns := vars.Namespace

			Eventually(func() bool {
				return testutils.GetPod(dpuSideClient, nfName, ns) == nil
			}, timeout, interval).Should(BeTrue())

			sfc := &configv1.ServiceFunctionChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sfc-test",
					Namespace: ns,
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
			err := dpuSideClient.Create(context.TODO(), sfc)
			Expect(err).NotTo(HaveOccurred())

			podList := &corev1.PodList{}
			err = dpuSideClient.List(context.TODO(), podList, client.InNamespace(ns))
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				pod := testutils.GetPod(dpuSideClient, nfName, ns)
				if pod != nil {
					return pod.Spec.Containers[0].Image == nfImage
				}
				return false
			}, timeout, interval).Should(BeTrue())

			err = dpuSideClient.Delete(context.TODO(), sfc)
			Expect(err).NotTo(HaveOccurred())
			fmt.Println("Finishing up")

			Eventually(func() bool {
				return testutils.GetPod(dpuSideClient, nfName, ns) == nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})
