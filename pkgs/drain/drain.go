package drain

import (
	"context"

	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/drain"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/platforms"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/vars"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

// DrainerFacade provides a wrapper around the SR-IOV DrainInterface implementation
// to simplify creation and reuse of the drainer across Dpu Operator.
type Drainer struct {
	drainer drain.DrainInterface
}

func NewDrainer(config *rest.Config) (*Drainer, error) {
	platform, err := platforms.NewDefaultPlatformHelper()
	if err != nil {
		return nil, err
	}

	// TODO: Make sriov network operator drain module modular from the rest of the repo.
	// In the meantime we will need to set arguments for drain interface creation via sriov's vars package
	vars.Config = config

	drainerInstance, err := drain.NewDrainer(platform)
	if err != nil {
		return nil, err
	}

	return &Drainer{drainer: drainerInstance}, nil
}

func (df *Drainer) DrainNode(ctx context.Context, node *corev1.Node, force bool) (bool, error) {
	return df.drainer.DrainNode(ctx, node, force)
}

func (df *Drainer) CompleteDrainNode(ctx context.Context, node *corev1.Node) (bool, error) {
	return df.drainer.CompleteDrainNode(ctx, node)
}
