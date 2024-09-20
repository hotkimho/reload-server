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
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-logr/logr"

	"github.com/hotkimho/reloader-server/project/internal/pkg/constants"
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
			log:      ctrl.Log.WithName("controller").WithName("reloader"),
			recorder: mgr.GetEventRecorderFor("reloader"),
		})
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update

func (r *ConfigMapController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cm := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, req.NamespacedName, cm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.reloadDeployments(ctx, cm); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ConfigMapController) reloadDeployments(ctx context.Context, cm *corev1.ConfigMap) error {
	// 레이블이 없는 키는 filter에서 처리됨
	k, v := constants.ReloaderLabelKey, cm.GetLabels()[constants.ReloaderLabelKey]
	dList := &appsv1.DeploymentList{}

	if err := r.client.List(ctx, dList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{k: v}),
		Namespace:     cm.GetNamespace(),
	}); err != nil {
		return err
	}

	// 조건에 맞는 디플로이먼트들에 대해 처리
	for _, d := range dList.Items {
		if d.Spec.Template.Annotations == nil {
			d.Spec.Template.Annotations = make(map[string]string)
		}

		// 재시작 시간 업데이트(rollout)
		d.Spec.Template.Annotations[constants.ReloaderRolloutKey] = time.Now().Format(time.RFC3339)
		if err := r.client.Update(ctx, &d); err != nil {
			return err
		}

		// 이벤트 및 로그 생성
		r.recorder.Eventf(&d, corev1.EventTypeNormal, "Reloaded", "Deployment %s reloaded", d.Name)
		r.log.Info("Deployment reloaded", "Deployment", d.Name)
	}

	return nil
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
