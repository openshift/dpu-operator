package testutils

import (
	"k8s.io/client-go/rest"
)

type Cluster interface {
	EnsureExists() *rest.Config
	EnsureDeleted()
}
