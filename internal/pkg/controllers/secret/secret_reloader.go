package secret

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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"

	"github.com/hotkimho/reloader-server/project/internal/pkg/constants"
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

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets/finalizers,verbs=update

func (r *SecretController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	s := &corev1.Secret{}
	if err := r.client.Get(ctx, req.NamespacedName, s); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.reloadDeployments(ctx, s); err != nil {
		return ctrl.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *SecretController) reloadDeployments(ctx context.Context, secret *corev1.Secret) error {
	k, v := constants.ReloaderLabelKey, secret.GetLabels()[constants.ReloaderLabelKey]
	dList := &appsv1.DeploymentList{}

	if err := r.client.List(ctx, dList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{k: v}),
		Namespace:     secret.GetNamespace(),
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
