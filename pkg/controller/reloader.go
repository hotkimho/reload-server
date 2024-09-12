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

package controller

import (
	"context"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"time"
)

const (
	reloaderLabelKey   = "edu.accordions.reloader"
	reloaderRolloutKey = "edu.accordions.reloader/restartedAt"
	reloaderConfigKey  = "edu.accordions.reloader/config"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapReconciler struct {
	client   client.Client
	scheme   *runtime.Scheme
	log      logr.Logger
	recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cm := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, req.NamespacedName, cm); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	k, v := reloaderLabelKey, cm.Labels[reloaderLabelKey]
	dList := &appsv1.DeploymentList{}
	if err := r.client.List(ctx, dList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{k: v}),
		Namespace:     cm.Namespace,
	}); err != nil {
		return ctrl.Result{}, err
	}

	for _, d := range dList.Items {
		if d.Spec.Template.Annotations == nil {
			d.Spec.Template.Annotations = make(map[string]string)
		}

		// 디플로이먼트의 spec.Template 업데이트 시 rollout
		d.Spec.Template.Annotations[reloaderRolloutKey] = time.Now().Format(time.RFC3339)
		if err := r.client.Update(ctx, &d); err != nil {
			return ctrl.Result{}, err
		}

		// event, logging
		r.recorder.Event(&d, corev1.EventTypeNormal, "Deployment %s is reloaded", d.Name)
		r.log.Info("Deployment is reloaded", "Deployment", d.Name)
	}

	return ctrl.Result{}, nil
}

func SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool { return false },
			UpdateFunc: func(e event.UpdateEvent) bool {
				// TODO: secret 컨트롤러 개발할 때, 공통으로 사용할 수 있는 함수로 분리 및 가독성 개선
				oldCm, newCm := e.ObjectOld.(*corev1.ConfigMap), e.ObjectNew.(*corev1.ConfigMap)
				// data 변경이 없는 경우 스킵
				if equality.Semantic.DeepEqual(oldCm.Data, newCm.Data) {
					return false
				}

				// reloader 옵션을 사용하지 않는 경우
				if _, ok := newCm.Labels[reloaderLabelKey]; !ok {
					return false
				}

				if k, ok := newCm.Annotations[reloaderConfigKey]; ok {
					newData, ok := newCm.Data[k]
					if !ok {
						return false
					}

					oldData, ok := oldCm.Data[k]
					if ok && newData == oldData {
						return false
					}
				}
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool { return false },
		}).
		Complete(&ConfigMapReconciler{
			client:   mgr.GetClient(),
			scheme:   mgr.GetScheme(),
			log:      ctrl.Log.WithName("controller").WithName("reloader"),
			recorder: mgr.GetEventRecorderFor("reloader"),
		})
}
