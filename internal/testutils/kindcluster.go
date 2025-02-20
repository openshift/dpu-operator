package testutils

import (
	"path/filepath"
	"runtime"

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

	"github.com/openshift/dpu-operator/pkgs/vars"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

var (
	testNamespace             = vars.Namespace
	testDpuOperatorConfigName = vars.DpuOperatorConfigName
	testDpuOperatorConfigKind = "DpuOperatorConfig"
	testDpuDaemonName         = "dpu-daemon"
	testClusterName           = "dpu-operator-test-cluster"
	TestAPITimeout            = time.Second * 2
	TestRetryInterval         = time.Millisecond * 10
	TestInitialSetupTimeout   = time.Minute
	setupLog                  = ctrl.Log.WithName("setup")
)

func relativeToAbs(path string) string {
	_, file, _, _ := runtime.Caller(0)
	file, err := filepath.Abs(file)
	Expect(err).NotTo(HaveOccurred())
	return filepath.Join(filepath.Dir(file), path)
}

func bootstrapTestEnv(restConfig *rest.Config) {
	var err error
	trueVal := true
	By("bootstrapping test environment")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			relativeToAbs("../../config/crd/bases"),
			relativeToAbs("../../test/crd"),
		},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    &trueVal,
		Config:                restConfig,
	}
	By("starting the test env")
	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
}

type KindCluster struct {
	Name string
}

func (t *KindCluster) TempDirPath() string {
	return filepath.Join("/tmp", t.Name)
}

func (t *KindCluster) ensureTempDir() error {
	dirPath := t.TempDirPath()
	_, err := os.Stat(dirPath)
	if err == nil {
		return nil
	} else if os.IsNotExist(err) {
		err = os.MkdirAll(dirPath, 0755)
		if err != nil {
			return err
		}
		return nil
	}
	return err
}

func (t *KindCluster) ensureTempDirDeleted() error {
	dirPath := t.TempDirPath()
	err := os.RemoveAll(dirPath)
	if err != nil {
		return fmt.Errorf("failed to delete temporary directory %s: %w", dirPath, err)
	}
	return nil
}

func (t *KindCluster) EnsureExists() *rest.Config {
	client := t.prepareKindCluster()
	bootstrapTestEnv(client)
	return client
}

func (t *KindCluster) EnsureDeleted() {
	deleteKindCluster(t.Name)
	err := t.ensureTempDirDeleted()
	Expect(err).NotTo(HaveOccurred())
}

func deleteKindCluster(name string) {
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

func (t *KindCluster) prepareKindCluster() *rest.Config {
	var cfg []byte
	var err error

	cfg, err = envToKubeConfig()
	if err != nil {
		provider := cluster.NewProvider()
		if !clusterExists(provider, t.Name) {
			t.ensureTempDirDeleted()
			err := t.ensureTempDir()
			Expect(err).NotTo(HaveOccurred())
			kubeletPath := filepath.Join(t.TempDirPath(), "/var/lib/kubelet")
			err = os.MkdirAll(kubeletPath, 0755)
			Expect(err).NotTo(HaveOccurred())
			c := v1alpha4.Cluster{
				TypeMeta: v1alpha4.TypeMeta{
					Kind:       "Cluster",
					APIVersion: "kind.x-k8s.io/v1alpha4",
				},
				Name: t.Name,
				Nodes: []v1alpha4.Node{
					{
						Role: v1alpha4.NodeRole("control-plane"),
						ExtraMounts: []v1alpha4.Mount{
							{
								HostPath:      kubeletPath,
								ContainerPath: "/var/lib/kubelet/",
							},
						},
					},
				},
			}
			v1alpha4.SetDefaultsCluster(&c)

			err = provider.Create(t.Name,
				cluster.CreateWithV1Alpha4Config(&c),
				cluster.CreateWithWaitForReady(time.Minute))
			Expect(err).NotTo(HaveOccurred())
		}
		var cfgString string
		Eventually(func() error {
			var err error
			cfgString, err = provider.KubeConfig(t.Name, false)
			return err
		}, TestAPITimeout, TestRetryInterval).ShouldNot(HaveOccurred())
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
	}, TestAPITimeout*2, TestRetryInterval).ShouldNot(HaveOccurred())
}
