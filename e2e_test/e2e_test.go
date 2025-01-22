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
		dpuSideClient client.Client
	)

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

	g.It("Should create a pod when creating an SFC", func() {
		nfName := "example-nf"
		nfImage := "example-nf-image-url"
		ns := "openshift-dpu-operator"

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
