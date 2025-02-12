package utils

import (
	"context"
	"sync"

	ctrl "sigs.k8s.io/controller-runtime"
)

type ctxManager struct {
	ctx  context.Context
	once sync.Once
}

var instance *ctxManager

func CancelFunc() (context.Context, context.CancelFunc) {
	instance.once.Do(func() {
		instance.ctx = ctrl.SetupSignalHandler()
	})
	return context.WithCancel(instance.ctx)
}

func init() {
	instance = &ctxManager{}
}
