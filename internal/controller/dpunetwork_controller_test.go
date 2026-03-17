/*
Copyright 2024.

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
	"encoding/json"
	"os"
	"sync"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type testDevicePluginConfig struct {
	Resources []testDevicePluginResource `json:"resources"`
}

type testDevicePluginResource struct {
	ResourceName   string `json:"resourceName"`
	DpuNetworkName string `json:"dpuNetworkName"`
}

func startDpuNetworkControllerManager(ctx context.Context, client *rest.Config, wg *sync.WaitGroup) ctrl.Manager {
	mgr, err := ctrl.NewManager(client, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: ":18002",
		},
		WebhookServer:    webhook.NewServer(webhook.Options{Port: 9444}),
		LeaderElectionID: "dpunetwork-controller-test.openshift.io",
	})
	Expect(err).NotTo(HaveOccurred())

	reconciler := &DpuNetworkReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
	err = reconciler.SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	wg.Add(1)
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
		wg.Done()
	}()

	<-mgr.Elected()
	return mgr
}

var _ = Describe("DpuNetwork Controller", Ordered, func() {
	var (
		cancel      context.CancelFunc
		ctx         context.Context
		wg          sync.WaitGroup
		restConfig  *rest.Config
		mgr         ctrl.Manager
		testCluster testutils.KindCluster
	)

	BeforeAll(func() {
		opts := zap.Options{Development: true}
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

		testCluster = testutils.KindCluster{Name: "dpu-operator-dpunetwork-test-cluster"}
		restConfig = testCluster.EnsureExists()
		ctx, cancel = context.WithCancel(context.Background())
		mgr = startDpuNetworkControllerManager(ctx, restConfig, &wg)

		// DpuNetwork controller writes into vars.Namespace, so make sure it exists.
		ns := testutils.DpuOperatorNamespace()
		testutils.CreateNamespace(mgr.GetClient(), ns)
	})

	AfterAll(func() {
		cancel()
		wg.Wait()
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
	})

	It("should create/update ConfigMap and NAD and set status", func() {
		net := &configv1.DpuNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: "net1"},
			Spec: configv1.DpuNetworkSpec{
				DpuSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      "vfId",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"1-3", "5"},
					}},
				},
			},
		}

		Expect(mgr.GetClient().Create(context.Background(), net)).To(Succeed())

		expectedResource := "openshift.io/dpunetwork-net1"

		By("Ensuring ConfigMap is written")
		Eventually(func() (string, error) {
			cm := &corev1.ConfigMap{}
			err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Name: "dpu-device-plugin-config", Namespace: vars.Namespace}, cm)
			if err != nil {
				return "", err
			}
			return cm.Data["config.json"], nil
		}, testutils.TestAPITimeout*5, testutils.TestRetryInterval).ShouldNot(BeEmpty())

		cm := &corev1.ConfigMap{}
		Expect(mgr.GetClient().Get(context.Background(), types.NamespacedName{Name: "dpu-device-plugin-config", Namespace: vars.Namespace}, cm)).To(Succeed())
		cfg := testDevicePluginConfig{}
		Expect(json.Unmarshal([]byte(cm.Data["config.json"]), &cfg)).To(Succeed())

		found := false
		for _, r := range cfg.Resources {
			if r.DpuNetworkName == "net1" {
				Expect(r.ResourceName).To(Equal(expectedResource))
				found = true
				break
			}
		}
		Expect(found).To(BeTrue())

		By("Ensuring NAD is created")
		Eventually(func() (map[string]string, error) {
			nad := &netattdefv1.NetworkAttachmentDefinition{}
			err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Name: "net1-nad", Namespace: "default"}, nad)
			if err != nil {
				return nil, err
			}
			return nad.Annotations, nil
		}, testutils.TestAPITimeout*5, testutils.TestRetryInterval).Should(HaveKeyWithValue("k8s.v1.cni.cncf.io/resourceName", expectedResource))

		By("Ensuring status is set")
		Eventually(func() string {
			latest := &configv1.DpuNetwork{}
			if err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Name: "net1"}, latest); err != nil {
				return ""
			}
			if !meta.IsStatusConditionTrue(latest.Status.Conditions, "Ready") {
				return ""
			}
			return latest.Status.ResourceName
		}, testutils.TestAPITimeout*5, testutils.TestRetryInterval).Should(Equal(expectedResource))
	})
})
