package utils

import (
	"context"
	"fmt"
	"os"
	"strings"

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
type FlavourSet map[Flavour]struct{}

// Add a flavour to the set
func (f FlavourSet) Add(flavour Flavour) {
	f[flavour] = struct{}{}
}

// Remove a flavour from the set
func (f FlavourSet) Remove(flavour Flavour) {
	delete(f, flavour)
}

// Check if a flavour exists in the set
func (f FlavourSet) Contains(flavour Flavour) bool {
	_, exists := f[flavour]
	return exists
}

// Convert the set to a slice
func (f FlavourSet) ToSlice() []Flavour {
	keys := make([]Flavour, 0, len(f))
	for key := range f {
		keys = append(keys, key)
	}
	return keys
}

const (
	OpenShiftFlavour   Flavour = "OpenShift"
	MicroShiftFlavour  Flavour = "MicroShift"
	OstreeFlavour      Flavour = "Ostree"
	ClassicRhelFlavour Flavour = "ClassicRHEL"
	UnknownFlavour     Flavour = "Unknown"
)

func (ce *ClusterEnvironment) Flavours(ctx context.Context) (FlavourSet, error) {
	detectedFlavours := FlavourSet{}

	microShift, err := ce.isMicroShift(ctx)
	if err != nil {
		return nil, err
	}
	if microShift {
		detectedFlavours.Add(MicroShiftFlavour)
	}

	openShift, err := ce.isOpenShift(ctx)
	if err != nil {
		return nil, err
	}
	if openShift {
		detectedFlavours.Add(OpenShiftFlavour)
	}

	ostree, err := ce.isOSTree()
	if err != nil {
		return nil, err
	}
	if ostree {
		detectedFlavours.Add(OstreeFlavour)
	}

	classic, err := ce.isClassicRHEL(ctx)
	if err != nil {
		return nil, err
	}
	if classic {
		detectedFlavours.Add(ClassicRhelFlavour)
	}

	if len(detectedFlavours) == 0 {
		detectedFlavours.Add(UnknownFlavour)
	}

	return detectedFlavours, nil
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

// isClassicRHEL checks if the OS running on the current node is classic RHEL.
func (ce *ClusterEnvironment) isClassicRHEL(ctx context.Context) (bool, error) {
	// Retrieve the node name from the K8S_NODE environment variable
	nodeName := os.Getenv("K8S_NODE")
	if nodeName == "" {
		return false, fmt.Errorf("K8S_NODE environment variable is not set")
	}

	// Fetch the Node object using the Kubernetes client
	node := &v1.Node{}
	if err := ce.client.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return false, fmt.Errorf("failed to retrieve node information: %v", err)
	}

	// Extract OSImage from the node's status
	osImage := node.Status.NodeInfo.OSImage
	fmt.Printf("Node OSImage: %s\n", osImage)

	isOstree, err := ce.isOSTree()
	if err != nil {
		return false, err
	}
	// Determine if the OS is classic RHEL
	if strings.Contains(osImage, "Red Hat Enterprise Linux") && !isOstree {
		return true, nil // Classic RHEL detected
	}

	return false, nil // Not classic RHEL (could be OSTree-based or something else)
}

// isOSTree checks for the presence of the /run/ostree-booted file to determine if running on OSTree
func (ce *ClusterEnvironment) isOSTree() (bool, error) {
	if _, err := os.Stat("/run/ostree-booted"); err != nil {
		if os.IsNotExist(err) {
			return false, nil // Not an OSTree-based OS
		}
		return false, fmt.Errorf("Failed to check OSTree status: %v", err)
	}
	return true, nil // OSTree-based OS detected
}
