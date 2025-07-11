package testutils

import (
	"context"
	"fmt"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TryEventually(check func() error, timeout, interval time.Duration) error {
	var err error
	first := true
	starttime := time.Now()
	for first || time.Now().Sub(starttime) < timeout {
		first = false
		err = check()
		if err == nil {
			return nil
		}
		time.Sleep(interval)
	}
	return err
}

func AssertEventually(check func() error, timeout, interval, additional_timeout time.Duration,
	msg string, onFirstFailure func(), onSecondFailure func()) {
	start := time.Now()
	err1 := TryEventually(check, timeout, interval)
	if err1 == nil {
		return
	}

	ts1 := time.Now()

	if onFirstFailure != nil {
		onFirstFailure()
	}

	err2 := TryEventually(check, additional_timeout, interval)

	ts2 := time.Now()

	if onSecondFailure != nil {
		onSecondFailure()
	}

	if err2 == nil {
		g.Fail(fmt.Sprintf("Condition '%s' was not satisfied within %v (%v), but would have been satisfied after %v", msg, ts1.Sub(start), err1, ts2.Sub(start)))
	} else {
		g.Fail(fmt.Sprintf("Condition '%s' was not satisfied within %v (%v), and still not satisfied after additional %v (%v)", msg, ts1.Sub(start), err1, ts2.Sub(start), err2))
	}
}

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
