package utils

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterEnvironment struct {
	client client.Client
}

func NewClusterEnvironment(client client.Client) *ClusterEnvironment {
	return &ClusterEnvironment{
		client: client,
	}
}

type Flavour string

const (
	OpenShiftFlavour  Flavour = "OpenShift"
	MicroShiftFlavour Flavour = "MicroShift"
	UnknownFlavour    Flavour = "Unknown"
)

func (ce *ClusterEnvironment) Flavour(ctx context.Context) (Flavour, error) {
	microShift, err := ce.isMicroShift(ctx)
	if err != nil {
		return UnknownFlavour, err
	}
	if microShift {
		return MicroShiftFlavour, nil
	}

	openShift, err := ce.isOpenShift(ctx)
	if err != nil {
		return UnknownFlavour, err
	}
	if openShift {
		return OpenShiftFlavour, nil
	}
	return UnknownFlavour, nil
}

func (ce *ClusterEnvironment) isMicroShift(ctx context.Context) (bool, error) {
	cm := v1.ConfigMap{}
	cm.SetName("microshift-version")
	cm.SetNamespace("kube-public")
	if err := ce.client.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, &cm); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("Failed to check if running on microshift: %v", err)
	}
	return true, nil
}

func (ce *ClusterEnvironment) isOpenShift(ctx context.Context) (bool, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	crd.SetName("clusterversions.config.openshift.io")

	err := ce.client.Get(ctx, types.NamespacedName{Name: crd.Name}, crd)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("Failed to check if running on openshift: %v", err)
	}
	return true, nil
}
