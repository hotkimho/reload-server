package config

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
)

type ReferenceControllerConfig struct {
	configName      string
	configNamespace string
	configDataKey   string
	client          typev1.ConfigMapInterface
}

func NewReferenceControllerConfig(configName, configNamespace, configDataKey string, client typev1.ConfigMapInterface) *ReferenceControllerConfig {
	return &ReferenceControllerConfig{
		configName:      configName,
		configNamespace: configNamespace,
		configDataKey:   configDataKey,
		client:          client,
	}
}

func (c *ReferenceControllerConfig) Start(ctx context.Context) {
	fieldSelector := fmt.Sprintf("metadata.name=%s", c.configName)

	_, controller := cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.FieldSelector = fieldSelector
				return c.client.List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.FieldSelector = fieldSelector
				return c.client.Watch(ctx, options)
			},
		},
		&corev1.ConfigMap{},
		0,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old, new interface{}) {
				oldCm := old.(*corev1.ConfigMap)
				newCm := new.(*corev1.ConfigMap)
				if oldCm.ResourceVersion == newCm.ResourceVersion {
					return
				}

				if oldCm.Data[c.configDataKey] != newCm.Data[c.configDataKey] {
					os.Exit(0)
				}
			},
		},
	)

	go controller.Run(ctx.Done())
}
