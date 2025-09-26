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

package v1

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/dpu-operator/pkgs/vars"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Webhook Unit Test", func() {
	Context("Unit Unit", func() {
		It("check validate DpuOperatorConfig", func() {
			var err error

			config := &DpuOperatorConfig{}
			config.SetName(vars.DpuOperatorConfigName)
			config.Spec = DpuOperatorConfigSpec{
				LogLevel: 2,
			}

			_, err = config.validateDpuOperatorConfig()
			Expect(err).NotTo(HaveOccurred())

			config.SetName("dpu-operator-config2")
			_, err = config.validateDpuOperatorConfig()
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Webhook Test", func() {
	Context("Validating Webhook for DpuOperatorConfig", func() {

		BeforeEach(func() {
			// Ensure no DpuOperatorConfig exists before each test
			EventuallyNoDpuOperatorConfig(k8sClient, TestAPITimeout, TestRetryInterval)
		})

		AfterEach(func() {
			// Delete any remaining DpuOperatorConfig and ensure cleanup after each test
			config := &DpuOperatorConfig{}
			err := k8sClient.Get(ctx, DpuOperatorConfigNamespacedName, config)
			if err == nil {
				DeleteDpuOperatorCR(k8sClient, config)
			}
			EventuallyNoDpuOperatorConfig(k8sClient, TestAPITimeout, TestRetryInterval)
		})

		It("should initially not have a DpuOperatorConfig", func() {
			config := &DpuOperatorConfig{}
			err := k8sClient.Get(ctx, DpuOperatorConfigNamespacedName, config)
			Expect(err).To(HaveOccurred())
		})

		It("should succeed to create default DpuOperatorConfig", func() {
			var err error

			createValid := &DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vars.DpuOperatorConfigName,
					Namespace: vars.Namespace,
				},
				Spec: DpuOperatorConfigSpec{
					LogLevel: 0,
				},
			}
			err = k8sClient.Create(ctx, createValid)
			Expect(err).NotTo(HaveOccurred())

			config := &DpuOperatorConfig{}

			err = k8sClient.Get(ctx, DpuOperatorConfigNamespacedName, config)
			Expect(err).NotTo(HaveOccurred())

			DeleteDpuOperatorCR(k8sClient, config)
			EventuallyNoDpuOperatorConfig(k8sClient, TestAPITimeout, TestRetryInterval)
		})

		It("should reject invalid DpuOperatorConfig", func() {
			var err error

			createInvalidName := &DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid",
					Namespace: vars.Namespace,
				},
				Spec: DpuOperatorConfigSpec{
					LogLevel: 0,
				},
			}
			err = k8sClient.Create(ctx, createInvalidName)
			Expect(err).To(HaveOccurred())

			config := &DpuOperatorConfig{}
			err = k8sClient.Get(ctx, DpuOperatorConfigNamespacedName, config)
			Expect(err).To(HaveOccurred())
		})
	})
})

// TODO: remove this duplicate code when we properly start the webhook from internal/ just like the nri

// Constants duplicated from internal/testutils/kindcluster.go
var (
	TestAPITimeout    = time.Second * 10
	TestRetryInterval = time.Millisecond * 10
)

// DeleteDpuOperatorCR duplicated from internal/testutils/testcluster.go
func DeleteDpuOperatorCR(client client.Client, cr *DpuOperatorConfig) {
	err := client.Delete(context.Background(), cr)
	if err != nil && !errors.IsNotFound(err) {
		// If resource already doesn't exist, that's fine
		Expect(err).NotTo(HaveOccurred())
	}

	// Wait for the resource to be fully deleted
	found := DpuOperatorConfig{}
	Eventually(func() error {
		err := client.Get(context.Background(), types.NamespacedName{Namespace: vars.Namespace, Name: cr.GetName()}, &found)
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

// EventuallyNoDpuOperatorConfig duplicated from internal/testutils/testcluster.go
func EventuallyNoDpuOperatorConfig(c client.Client, timeout time.Duration, interval time.Duration) {
	formatCRDetails := func(cr DpuOperatorConfig) string {
		detail := fmt.Sprintf("%s/%s", cr.Namespace, cr.Name)
		if len(cr.Finalizers) > 0 {
			detail += fmt.Sprintf(" (finalizers: %v)", cr.Finalizers)
		}
		if cr.DeletionTimestamp != nil {
			detail += fmt.Sprintf(" (deletion started: %v)", cr.DeletionTimestamp)
		}
		return detail
	}

	onFailure := func() {
		crList := &DpuOperatorConfigList{}
		err := c.List(context.Background(), crList)
		if err != nil {
			fmt.Printf("Failed to list DpuOperatorConfigs for diagnostics: %v\n", err)
			return
		}
		if len(crList.Items) > 0 {
			fmt.Printf("Found %d DpuOperatorConfig CRs:\n", len(crList.Items))
			for _, cr := range crList.Items {
				fmt.Printf("  - %s\n", formatCRDetails(cr))
			}
		}
	}

	startTime := time.Now()

	Eventually(func() error {
		crList := &DpuOperatorConfigList{}
		err := c.List(context.Background(), crList)
		if err != nil {
			return fmt.Errorf("failed to list DpuOperatorConfigs: %v", err)
		}

		if len(crList.Items) > 0 {
			var details []string
			for _, cr := range crList.Items {
				details = append(details, formatCRDetails(cr))
			}
			return fmt.Errorf("found %d DpuOperatorConfig CRs still present: %v", len(crList.Items), details)
		}
		return nil
	}, timeout, interval).Should(Succeed(), func() string {
		onFailure()
		return "Expected no DpuOperatorConfig CRs to be present"
	})

	cleanupTime := time.Now()
	fmt.Printf("All DpuOperatorConfigs cleaned up after %v\n", cleanupTime.Sub(startTime))
}
