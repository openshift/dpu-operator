package testutils

import (
	"context"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func WaitAllNodesReady(client client.Client) {
	var nodes corev1.NodeList
	Eventually(func() error {
		return client.List(context.Background(), &nodes)
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())

	Eventually(func() bool {
		var latestNodes corev1.NodeList
		if err := client.List(context.Background(), &latestNodes); err != nil {
			return false
		}
		readyNodes := 0
		for _, node := range latestNodes.Items {
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					readyNodes++
					break
				}
			}
		}
		return readyNodes == len(latestNodes.Items)
	}, TestInitialSetupTimeout, TestRetryInterval).Should(BeTrue())
}
