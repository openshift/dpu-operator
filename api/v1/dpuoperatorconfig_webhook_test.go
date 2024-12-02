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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func check_validateDpuOperatorConfig() {
	var err error

	config := &DpuOperatorConfig{}
	config.SetName("dpu-operator-config")
	config.Spec = DpuOperatorConfigSpec{
		Mode:     "host",
		LogLevel: 2,
	}

	_, err = config.validateDpuOperatorConfig()
	Expect(err).NotTo(HaveOccurred())

	config.SetName("dpu-operator-config2")
	_, err = config.validateDpuOperatorConfig()
	Expect(err).To(HaveOccurred())
}

var _ = Describe("DpuOperatorConfig Webhook", func() {

	Context("When creating DpuOperatorConfig under Validating Webhook", func() {
		It("Validation should happen", func() {
			var err error

			check_validateDpuOperatorConfig()

			config := &DpuOperatorConfig{}

			key := types.NamespacedName{
				Name:      "dpu-operator-config",
				Namespace: "openshift-dpu-operator",
			}

			err = k8sClient.Get(ctx, key, config)
			Expect(err).To(HaveOccurred())

			create := &DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: DpuOperatorConfigSpec{
					Mode: "host",
				},
			}
			err = k8sClient.Create(ctx, create)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, key, config)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, config)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, key, config)
			Expect(err).To(HaveOccurred())

			create2 := &DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: DpuOperatorConfigSpec{
					Mode: "invalid",
				},
			}
			err = k8sClient.Create(ctx, create2)
			Expect(err).To(HaveOccurred())

			create3 := &DpuOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid",
					Namespace: key.Namespace,
				},
				Spec: DpuOperatorConfigSpec{
					Mode: "host",
				},
			}
			err = k8sClient.Create(ctx, create3)
			Expect(err).To(HaveOccurred())

			err = k8sClient.Get(ctx, key, config)
			Expect(err).To(HaveOccurred())
		})
	})

})
