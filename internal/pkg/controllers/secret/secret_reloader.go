package secret

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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"

	"github.com/hotkimho/reloader-server/project/internal/pkg/constants"
	"github.com/hotkimho/reloader-server/project/internal/pkg/utils"
)

type SecretController struct {
	client   client.Client
	scheme   *runtime.Scheme
	log      logr.Logger
	recorder record.EventRecorder
}

func SetupSecretController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool { return false },
			UpdateFunc: secretUpdateEventFilter,
			DeleteFunc: func(e event.DeleteEvent) bool { return false },
		}).
		Complete(&SecretController{
			client:   mgr.GetClient(),
			scheme:   mgr.GetScheme(),
			log:      ctrl.Log.WithName("controller").WithName("reloader"),
			recorder: mgr.GetEventRecorderFor("reloader"),
		})
}

func secretUpdateEventFilter(e event.UpdateEvent) bool {
	oldObj, oldOk := e.ObjectOld.(*corev1.Secret)
	newObj, newOk := e.ObjectNew.(*corev1.Secret)
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

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets/finalizers,verbs=update

func (r *SecretController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	s := &corev1.Secret{}
	if err := r.client.Get(ctx, req.NamespacedName, s); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	nsdName := types.NamespacedName{
		Namespace: s.Namespace,
		Name:      s.GetLabels()[constants.ReloaderLabelKey],
	}

	rr, err := r.getReloadResource(ctx, nsdName)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reloadResource(ctx, rr); err != nil {
		return ctrl.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *SecretController) getReloadResource(ctx context.Context, namespacedName types.NamespacedName) (client.Object, error) {
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

func (r *SecretController) reloadResource(ctx context.Context, obj client.Object) error {
	switch rr := obj.(type) {
	case *appsv1.Deployment:
		if err := r.reloadDeployment(ctx, rr); err != nil {
			return err
		}

	case *appsv1.StatefulSet:
		if err := r.reloadStatefulSet(ctx, rr); err != nil {
			return err
		}

	case *appsv1.DaemonSet:
		if err := r.reloadDaemonSet(ctx, rr); err != nil {
			return err
		}
	default:
		return errors.New("cannot reload resource type")
	}

	return nil
}

func (r *SecretController) reloadDeployment(ctx context.Context, d *appsv1.Deployment) error {
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = make(map[string]string)
	}

	d.Spec.Template.Annotations[constants.ReloaderRolloutKey] = time.Now().Format(time.RFC3339)
	if err := r.client.Update(ctx, d); err != nil {
		return err
	}

	r.recorder.Eventf(d, corev1.EventTypeNormal, "Reloaded", "Deployment %s reloaded", d.Name)
	r.log.Info("d reloaded", "Deployment", d.Name)

	return nil
}

func (r *SecretController) reloadStatefulSet(ctx context.Context, ss *appsv1.StatefulSet) error {
	if ss.Spec.Template.Annotations == nil {
		ss.Spec.Template.Annotations = make(map[string]string)
	}

	ss.Spec.Template.Annotations[constants.ReloaderRolloutKey] = time.Now().Format(time.RFC3339)
	if err := r.client.Update(ctx, ss); err != nil {
		return err
	}

	r.recorder.Eventf(ss, corev1.EventTypeNormal, "Reloaded", "StatefulSet %s reloaded", ss.Name)
	r.log.Info("StatefulSet reloaded", "StatefulSet", ss.Name)

	return nil
}

func (r *SecretController) reloadDaemonSet(ctx context.Context, ds *appsv1.DaemonSet) error {
	if ds.Spec.Template.Annotations == nil {
		ds.Spec.Template.Annotations = make(map[string]string)
	}

	ds.Spec.Template.Annotations[constants.ReloaderRolloutKey] = time.Now().Format(time.RFC3339)
	if err := r.client.Update(ctx, ds); err != nil {
		return err
	}

	r.recorder.Eventf(ds, corev1.EventTypeNormal, "Reloaded", "DaemonSet %s reloaded", ds.Name)
	r.log.Info("DaemonSet reloaded", "DaemonSet", ds.Name)

	return nil
}
