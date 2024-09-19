package controller

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/go-logr/logr"
)

func reloadDeployments(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	log logr.Logger,
	recorder record.EventRecorder,
) error {
	// 레이블이 없는 키는 filter에서 처리됨
	k, v := reloaderLabelKey, obj.GetLabels()[reloaderLabelKey]
	dList := &appsv1.DeploymentList{}
	if err := c.List(ctx, dList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{k: v}),
		Namespace:     obj.GetNamespace(),
	}); err != nil {
		return err
	}

	// 조건에 맞는 디플로이먼트들에 대해 처리
	for _, d := range dList.Items {
		if d.Spec.Template.Annotations == nil {
			d.Spec.Template.Annotations = make(map[string]string)
		}

		// 재시작 시간 업데이트(rollout)
		d.Spec.Template.Annotations[reloaderRolloutKey] = time.Now().Format(time.RFC3339)
		if err := c.Update(ctx, &d); err != nil {
			return err
		}

		// 이벤트 및 로그 생성
		recorder.Eventf(&d, corev1.EventTypeNormal, "Reloaded", "Deployment %s reloaded", d.Name)
		log.Info("Deployment reloaded", "Deployment", d.Name)
	}

	return nil
}

func reloaderUpdateEventFilter(e event.UpdateEvent) bool {
	oldObj, newObj := e.ObjectOld, e.ObjectNew

	// reloader 옵션을 사용하지 않는 filter 처리
	if _, ok := newObj.GetLabels()[reloaderLabelKey]; !ok {
		return false
	}

	// configMap, secret의 Data 조회
	oldData, newData, err := getDataFromObject(oldObj, newObj)
	if err != nil {
		return false
	}

	if reflect.DeepEqual(oldData, newData) {
		return false
	}

	// 설정된 필드가 변경되지 않은 경우 filter 처리
	if k, ok := newObj.GetAnnotations()[reloaderFieldKey]; ok {
		fieldKeys := strings.Split(k, ",")
		for _, fk := range fieldKeys {
			if isFieldValueChanged(oldData, newData, fk) {
				return true
			}
		}

		return false
	}

	return true
}

func getDataFromObject(oldObj, newObj client.Object) (interface{}, interface{}, error) {
	switch oldType := oldObj.(type) {
	case *corev1.ConfigMap:
		if newType, ok := newObj.(*corev1.ConfigMap); ok {
			return oldType.Data, newType.Data, nil
		}
	case *corev1.Secret:
		if newType, ok := newObj.(*corev1.Secret); ok {
			return oldType.Data, newType.Data, nil
		}
	}

	return nil, nil, errors.New("unsupported object type")
}

func isFieldValueChanged(oldData, newData interface{}, fieldKey string) bool {
	oldValue, oldOk := getDataValueByKey(oldData, fieldKey)
	newValue, newOk := getDataValueByKey(newData, fieldKey)
	if !newOk {
		return false
	}

	return !(oldOk && reflect.DeepEqual(newValue, oldValue))
}

func getDataValueByKey(dataField interface{}, key string) (interface{}, bool) {
	switch objType := dataField.(type) {
	case map[string]string: // configmap
		if v, ok := objType[key]; ok {
			return v, true
		}
	case map[string][]byte: // secret
		if v, ok := objType[key]; ok {
			return v, true
		}
	}

	return nil, false
}
