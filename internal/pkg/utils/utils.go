package utils

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetDeployment(client client.Client, ctx context.Context, namespacedName types.NamespacedName) (*appsv1.Deployment, error) {
	d := &appsv1.Deployment{}
	if err := client.Get(ctx, namespacedName, d); err != nil {
		return nil, err
	}

	return d, nil
}

func GetStatefulSet(client client.Client, ctx context.Context, namespacedName types.NamespacedName) (*appsv1.StatefulSet, error) {
	ss := &appsv1.StatefulSet{}
	if err := client.Get(ctx, namespacedName, ss); err != nil {
		return nil, err
	}

	return ss, nil
}

func GetDaemonSet(client client.Client, ctx context.Context, namespacedName types.NamespacedName) (*appsv1.DaemonSet, error) {
	ds := &appsv1.DaemonSet{}
	if err := client.Get(ctx, namespacedName, ds); err != nil {
		return nil, err
	}

	return ds, nil
}
