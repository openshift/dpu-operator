package testutils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"fmt"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/kind/pkg/cluster"
)

var (
	testNamespace             = "openshift-dpu-operator"
	testDpuOperatorConfigName = "default"
	testDpuOperatorConfigKind = "DpuOperatorConfig"
	testDpuDaemonName         = "dpu-daemon"
	testNetworkFunctionNAD    = "dpunfcni-conf"
	testClusterName           = "dpu-operator-test-cluster"
	TestAPITimeout            = time.Second * 2
	TestRetryInterval         = time.Millisecond * 10
	TestInitialSetupTimeout   = time.Minute
	setupLog                  = ctrl.Log.WithName("setup")
)

func bootstrapTestEnv(restConfig *rest.Config) {
	var err error
	trueVal := true
	By("bootstrapping test environment")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../../config/crd/bases", "../../test/crd"},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    &trueVal,
		Config:                restConfig,
	}
	By("starting the test env")
	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
}

type TestCluster struct {
	Name string
}

func (t *TestCluster) EnsureExists() *rest.Config {
	client := prepareTestCluster(t.Name)
	bootstrapTestEnv(client)
	return client
}

func (t *TestCluster) EnsureDeleted() {
	deleteKindTestCluster(t.Name)
}

func deleteKindTestCluster(name string) {
	provider := cluster.NewProvider()
	provider.Delete(name, "")
}

func envToKubeConfig() ([]byte, error) {
	varName := "TEST_KUBECONFIG"
	kubeconfigPath, ok := os.LookupEnv(varName)
	if !ok {
		return nil, fmt.Errorf("No %v env var defined", varName)
	}
	f, err := os.Open(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stats, err := f.Stat()
	if err != nil {
		return nil, err
	}

	cfg := make([]byte, stats.Size())
	_, err = f.Read(cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func clusterExists(p *cluster.Provider, name string) bool {
	clusters, err := p.List()
	Expect(err).NotTo(HaveOccurred())
	for _, n := range clusters {
		if n == name {
			return true
		}
	}
	return false
}

func prepareTestCluster(name string) *rest.Config {
	var cfg []byte
	var err error

	cfg, err = envToKubeConfig()
	if err != nil {
		provider := cluster.NewProvider()
		if !clusterExists(provider, name) {
			err := provider.Create(name)
			Expect(err).NotTo(HaveOccurred())
		}
		cfgString, err := provider.KubeConfig(name, false)
		Expect(err).NotTo(HaveOccurred())
		cfg = []byte(cfgString)
	}
	config, err := clientcmd.NewClientConfigFromBytes([]byte(cfg))
	Expect(err).NotTo(HaveOccurred())
	restCfg, err := config.ClientConfig()
	Expect(err).NotTo(HaveOccurred())
	return restCfg
}

func WaitForDaemonSetReady(daemonSet *appsv1.DaemonSet, k8sClient client.Client, namespace, name string) {
	Eventually(func() error {
		return k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, daemonSet)
	}, TestAPITimeout, TestRetryInterval).ShouldNot(HaveOccurred())
}
