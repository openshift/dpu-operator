package utils

import (
	"context"
	"sync"

	ctrl "sigs.k8s.io/controller-runtime"
)

type ctxManager struct {
	ctx        context.Context
	cancelFunc context.CancelFunc
	once       sync.Once
}

var instance *ctxManager

func CancelFunc() (context.Context, context.CancelFunc) {
	instance.once.Do(func() {
		instance.ctx, instance.cancelFunc = context.WithCancel(ctrl.SetupSignalHandler())
	})
	return instance.ctx, instance.cancelFunc
}

func init() {
	instance = &ctxManager{}
}
