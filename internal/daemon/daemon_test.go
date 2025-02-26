package daemon

import (
	"context"
	"fmt"
	"os"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"

	"github.com/jaypipes/ghw"
	"github.com/jaypipes/pcidb"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	mockvsp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/mock-vsp"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/spf13/afero"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createVspTestImages() map[string]string {
	vspImages := make(map[string]string)
	vspImages["IntelVspImage"] = "Intel-image"
	vspImages["MarvellVspImage"] = "Marvell-image"
	return vspImages
}


func EventuallyNoDpuCR(k8sClient client.Client) {
	Eventually(func() error {
		dpuList := &configv1.DataProcessingUnitList{}
		err := k8sClient.List(context.TODO(), dpuList)
		if err != nil {
			return err
		}
		if len(dpuList.Items) != 0 {
			return fmt.Errorf("Found %v DPU CRs but expecting 0", len(dpuList.Items))
		}
		return nil
	}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(Succeed())
}

var _ = g.Describe("Full Daemon", func() {
	var (
		testCluster  *testutils.KindCluster
		daemon       Daemon
		fakePlatform *platform.FakePlatform
		cancel       context.CancelFunc
		k8sClient    client.Client
		fs           afero.Fs
	)

	g.BeforeEach(func() {
		fs = afero.NewMemMapFs()
		utils.Touch(fs, "/dpu-cni")
	})

	g.Context("Running on a DPU", func() {
		g.BeforeEach(func() {
			var ctx context.Context
			var err error
			testCluster = &testutils.KindCluster{Name: "dpu-operator-test-cluster"}
			config := testCluster.EnsureExists()
			pathManager := utils.NewPathManager(testCluster.TempDirPath())
			k8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
			Expect(err).NotTo(HaveOccurred())
			ns := testutils.DpuOperatorNamespace()
			cr := testutils.DpuOperatorCR("dpu-operator-config", "host", ns)
			testutils.CreateNamespace(k8sClient, ns)
			testutils.CreateDpuOperatorCR(k8sClient, cr)

			fakePlatform = platform.NewFakePlatform("IPU Adapter E2100-CCQDA2")

			mockVsp := mockvsp.NewMockVsp(mockvsp.WithPathManager(*pathManager))
			mockVspListen, err := mockVsp.Listen()
			Expect(err).NotTo(HaveOccurred())
			go func() {
				err = mockVsp.Serve(mockVspListen)
				Expect(err).NotTo(HaveOccurred())
			}()

			daemon = NewDaemon(fs, fakePlatform, "dpu", config, createVspTestImages(), pathManager)
			ctx, cancel = context.WithCancel(context.Background())
			go func() {
				daemon.Start(ctx)
			}()
		})

		g.It("Should have one DPU CR with IsDpuSide set", func() {
			Eventually(func() error {
				dpuList := &configv1.DataProcessingUnitList{}
				err := k8sClient.List(context.TODO(), dpuList)
				if err != nil {
					return err
				}
				if len(dpuList.Items) != 1 {
					return fmt.Errorf("Got %v DPUs", len(dpuList.Items))
				}
				if !dpuList.Items[0].Spec.IsDpuSide {
					return fmt.Errorf("Got a single DPU but it has DPU side set to false")
				}
				return nil
			}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(Succeed())
		})

		g.AfterEach(func() {
			klog.Info("Cleaning up")
			cancel()
			daemon.Wait()
			ns := testutils.DpuOperatorNamespace()
			cr := testutils.DpuOperatorCR("dpu-operator-config", "host", ns)
			testutils.DeleteDpuOperatorCR(k8sClient, cr)

			if os.Getenv("FAST_TEST") == "false" {
				testCluster.EnsureDeleted()
			}
			EventuallyNoDpuCR(k8sClient)
		})
	})

	g.Context("Running on a Host", func() {
		g.BeforeEach(func() {
			var ctx context.Context
			var err error
			testCluster = &testutils.KindCluster{Name: "dpu-operator-test-cluster"}
			config := testCluster.EnsureExists()
			pathManager := utils.NewPathManager(testCluster.TempDirPath())
			k8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
			Expect(err).NotTo(HaveOccurred())
			ns := testutils.DpuOperatorNamespace()
			cr := testutils.DpuOperatorCR("dpu-operator-config", "host", ns)
			testutils.CreateNamespace(k8sClient, ns)
			testutils.CreateDpuOperatorCR(k8sClient, cr)

			fakePlatform = platform.NewFakePlatform("Host")

			device := &ghw.PCIDevice{
				Address: "0000:00:1f.6",
				Vendor: &pcidb.Vendor{
					Name: "Intel Corporation",
				},
				Product: &pcidb.Product{
					Name: "Infrastructure Data Path Function",
				},
				Class: &pcidb.Class{
					Name: "Network controller",
				},
			}
			fakePlatform.AddPciDevice(device)

			mockVsp := mockvsp.NewMockVsp(mockvsp.WithPathManager(*pathManager))
			mockVspListen, err := mockVsp.Listen()
			Expect(err).NotTo(HaveOccurred())
			go func() {
				err = mockVsp.Serve(mockVspListen)
				Expect(err).NotTo(HaveOccurred())
			}()

			daemon = NewDaemon(fs, fakePlatform, "dpu", config, createVspTestImages(), pathManager)
			ctx, cancel = context.WithCancel(context.Background())
			go func() {
				daemon.Start(ctx)
			}()
		})

		g.It("Should have one DPU CR with IsDpuSide set", func() {
			Eventually(func() error {
				dpuList := &configv1.DataProcessingUnitList{}
				err := k8sClient.List(context.TODO(), dpuList)
				if err != nil {
					return err
				}
				if len(dpuList.Items) != 1 {
					return fmt.Errorf("Got %v DPUs", len(dpuList.Items))
				}
				if dpuList.Items[0].Spec.IsDpuSide {
					return fmt.Errorf("Got a single DPU but it has DPU side set to true")
				}
				return nil
			}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(Succeed())
		})

		g.AfterEach(func() {
			klog.Info("Cleaning up")
			cancel()
			daemon.Wait()
			ns := testutils.DpuOperatorNamespace()
			cr := testutils.DpuOperatorCR("dpu-operator-config", "host", ns)
			testutils.DeleteDpuOperatorCR(k8sClient, cr)

			if os.Getenv("FAST_TEST") == "false" {
				testCluster.EnsureDeleted()
			}
			EventuallyNoDpuCR(k8sClient)
		})
	})
})
