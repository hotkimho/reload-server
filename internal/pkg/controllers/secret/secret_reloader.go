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
			log:      ctrl.Log.WithName("controller").WithName("secret-reloader"),
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

func (r *SecretController) reload(ctx context.Context, obj client.Object, kind string) error {
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
