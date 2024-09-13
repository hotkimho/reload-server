package controller

import (
	"bytes"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

type SecretController struct {
	client   client.Client
	scheme   *runtime.Scheme
	log      logr.Logger
	recorder record.EventRecorder
}

func (c SecretController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	s := &corev1.Secret{}
	if err := c.client.Get(ctx, request.NamespacedName, s); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	k, v := reloaderLabelKey, s.Labels[reloaderLabelKey]
	dList := &appsv1.DeploymentList{}
	if err := c.client.List(ctx, dList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{k: v}),
		Namespace:     s.Namespace,
	}); err != nil {
		return reconcile.Result{}, err
	}

	for _, d := range dList.Items {
		if d.Spec.Template.Annotations == nil {
			d.Spec.Template.Annotations = make(map[string]string)
		}

		d.Spec.Template.Annotations[reloaderRolloutKey] = time.Now().Format(time.RFC3339)
		if err := c.client.Update(ctx, &d); err != nil {
			return reconcile.Result{}, err
		}

		c.recorder.Eventf(s, corev1.EventTypeNormal, "Updated", "Deployment %s updated", d.Name)
		c.log.Info("Deployment is reloaded", "Deployment", d.Name)
	}

	return reconcile.Result{}, nil
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets/finalizers,verbs=update

func SetupSecretController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool { return false },
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldSec, newSec := e.ObjectOld.(*corev1.Secret), e.ObjectNew.(*corev1.Secret)

				if equality.Semantic.DeepEqual(oldSec.Data, newSec.Data) {
					return false
				}

				if _, ok := newSec.Labels[reloaderLabelKey]; !ok {
					return false
				}

				if k, ok := newSec.Annotations[reloaderConfigKey]; ok {
					newData, ok := newSec.Data[k]
					if !ok {
						return false
					}

					oldData, ok := oldSec.Data[k]
					if ok && bytes.Equal(oldData, newData) {
						return false
					}
				}
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool { return false },
		}).
		Complete(&SecretController{
			client:   mgr.GetClient(),
			scheme:   mgr.GetScheme(),
			log:      ctrl.Log.WithName("controller").WithName("reloader"),
			recorder: mgr.GetEventRecorderFor("reloader"),
		})
}
