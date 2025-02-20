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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/dpu-operator/pkgs/vars"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Webhook Unit Test", func() {
	Context("Unit Unit", func() {
		It("check validate DpuOperatorConfig", func() {
			var err error

			config := &DpuOperatorConfig{}
			config.SetName(vars.DpuOperatorConfigName)
			config.Spec = DpuOperatorConfigSpec{
				Mode:     "host",
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
					Mode: "host",
				},
			}
			err = k8sClient.Create(ctx, createValid)
			Expect(err).NotTo(HaveOccurred())

			config := &DpuOperatorConfig{}

			err = k8sClient.Get(ctx, DpuOperatorConfigNamespacedName, config)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, config)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, DpuOperatorConfigNamespacedName, config)
			Expect(err).To(HaveOccurred())
		})

		It("should reject invalid DpuOperatorConfig", func() {
			var err error

			createInvalidMode := &DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vars.DpuOperatorConfigName,
					Namespace: vars.Namespace,
				},
				Spec: DpuOperatorConfigSpec{
					Mode: "invalid",
				},
			}
			err = k8sClient.Create(ctx, createInvalidMode)
			Expect(err).To(HaveOccurred())

			createInvalidName := &DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid",
					Namespace: vars.Namespace,
				},
				Spec: DpuOperatorConfigSpec{
					Mode: "host",
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
