/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configmap

import (
	"context"
	"errors"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-logr/logr"

	"github.com/hotkimho/reloader-server/project/internal/pkg/constants"
	"github.com/hotkimho/reloader-server/project/internal/pkg/utils"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapController struct {
	client   client.Client
	scheme   *runtime.Scheme
	log      logr.Logger
	recorder record.EventRecorder
}

func SetupConfigMapController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool { return false },
			UpdateFunc: configMapUpdateEventFilter,
			DeleteFunc: func(e event.DeleteEvent) bool { return false },
		}).
		Complete(&ConfigMapController{
			client:   mgr.GetClient(),
			scheme:   mgr.GetScheme(),
			log:      ctrl.Log.WithName("controller").WithName("configmap-reloader"),
			recorder: mgr.GetEventRecorderFor("reloader"),
		})
}

func configMapUpdateEventFilter(e event.UpdateEvent) bool {
	oldObj, oldOk := e.ObjectOld.(*corev1.ConfigMap)
	newObj, newOk := e.ObjectNew.(*corev1.ConfigMap)
	if !oldOk || !newOk {
		return false
	}

	if _, ok := newObj.GetLabels()[constants.ReloaderLabelKey]; !ok {
		return false
	}

	if reflect.DeepEqual(oldObj.Data, newObj.Data) {
		return false
	}

	return true
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update

func (r *ConfigMapController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cm := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, req.NamespacedName, cm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	nsdName := types.NamespacedName{
		Namespace: cm.Namespace,
		Name:      cm.GetLabels()[constants.ReloaderLabelKey],
	}

	rr, err := r.getReloadResource(ctx, nsdName)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reloadResource(ctx, rr); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ConfigMapController) getReloadResource(ctx context.Context, namespacedName types.NamespacedName) (client.Object, error) {
	if d, err := utils.GetDeployment(r.client, ctx, namespacedName); err == nil {
		return d, nil
	}

	if ss, err := utils.GetStatefulSet(r.client, ctx, namespacedName); err == nil {
		return ss, nil
	}

	if ds, err := utils.GetDaemonSet(r.client, ctx, namespacedName); err == nil {
		return ds, nil
	}

	return nil, errors.New("not found resource type")
}

func (r *ConfigMapController) reloadResource(ctx context.Context, obj client.Object) error {
	var kind string
	switch obj.(type) {
	case *appsv1.Deployment:
		kind = "Deployment"
	case *appsv1.StatefulSet:
		kind = "StatefulSet"
	case *appsv1.DaemonSet:
		kind = "DaemonSet"
	default:
		return errors.New("cannot reload resource type")
	}

	return r.reload(ctx, obj, kind)
}

func (r *ConfigMapController) reload(ctx context.Context, obj client.Object, kind string) error {
	var podTemplate *corev1.PodTemplateSpec

	switch rr := obj.(type) {
	case *appsv1.Deployment:
		podTemplate = &rr.Spec.Template
	case *appsv1.StatefulSet:
		podTemplate = &rr.Spec.Template
	case *appsv1.DaemonSet:
		podTemplate = &rr.Spec.Template
	default:
		return errors.New("cannot reload resource type")
	}

	if podTemplate.Annotations == nil {
		podTemplate.Annotations = make(map[string]string)
	}

	podTemplate.Annotations[constants.ReloaderRolloutKey] = time.Now().Format(time.RFC3339)
	if err := r.client.Update(ctx, obj); err != nil {
		return err
	}

	r.recorder.Eventf(obj, corev1.EventTypeNormal, "Reloaded", "%s %s reloaded", kind, obj.GetName())
	r.log.Info("Resource reloaded", kind, obj.GetName())

	return nil
}
