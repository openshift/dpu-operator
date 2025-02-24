package testutils

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/dpu-operator/api/v1"
)

type Cluster interface {
	EnsureExists() *rest.Config
	EnsureDeleted()
}

func GetPod(c client.Client, name, namespace string) *corev1.Pod {
	obj := client.ObjectKey{Namespace: namespace, Name: name}
	pod := &corev1.Pod{}
	err := c.Get(context.TODO(), obj, pod)
	if err != nil {
		return nil
	}
	return pod
}

func DpuOperatorNamespace() *corev1.Namespace {
	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
		Spec:   corev1.NamespaceSpec{},
		Status: corev1.NamespaceStatus{},
	}
	return namespace
}

func DpuOperatorCR(name string, mode string, ns *corev1.Namespace) *configv1.DpuOperatorConfig {
	config := &configv1.DpuOperatorConfig{}
	config.SetNamespace(ns.Name)
	config.SetName(name)
	config.Spec = configv1.DpuOperatorConfigSpec{
		Mode:     mode,
		LogLevel: 2,
	}
	return config
}


func CreateNamespace(client client.Client, ns *corev1.Namespace) {
	// ignore error when creating the namespace since it can already exist
	client.Create(context.Background(), ns)
	found := corev1.Namespace{}
	Eventually(func() error {
		return client.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: ns.GetName()}, &found)
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

func DeleteNamespace(client client.Client, ns *corev1.Namespace) {
	client.Delete(context.Background(), ns)
	found := corev1.Namespace{}
	Eventually(func() error {
		err := client.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: ns.GetName()}, &found)
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

func CreateDpuOperatorCR(client client.Client, cr *configv1.DpuOperatorConfig) {
	err := client.Create(context.Background(), cr)
	Expect(err).NotTo(HaveOccurred())
	found := configv1.DpuOperatorConfig{}
	Eventually(func() error {
		return client.Get(context.Background(), types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()}, &found)
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

func DeleteDpuOperatorCR(client client.Client, cr *configv1.DpuOperatorConfig) {
	client.Delete(context.Background(), cr)
	found := configv1.DpuOperatorConfig{}
	Eventually(func() error {
		err := client.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: cr.GetName()}, &found)
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}
