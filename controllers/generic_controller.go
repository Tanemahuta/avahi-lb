package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var errNilAvahiHandler = errors.New("avahi handler must not be nil")

const (
	PublishAnnotation = "service.beta.kubernetes.io/avahi-publish"
	MountNameDBUS     = "dbus"
	MountPathDBUS     = "/var/run/dbus"
)

type genericAvahiController[T client.Object] struct {
	client  client.Client
	config  *AvahiConfig
	handler AvahiHandler[T]
}

// NewAvahiReconciler creates a reconciler for Kubernetes objects of type T.
// SetupWithManager must be called before the reconciler is used.
func NewAvahiReconciler[T client.Object](handler AvahiHandler[T]) AvahiReconciler {
	return &genericAvahiController[T]{
		handler: handler,
	}
}

func (c *genericAvahiController[T]) Reconcile(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	var result reconcile.Result
	object, err := newClientObject[T]()
	if err != nil {
		return result, err
	}
	if err = c.client.Get(ctx, request.NamespacedName, object); err != nil && !k8serrors.IsNotFound(err) {
		return result, err
	}

	hostnames := c.expandHostnames(object, c.handler.Hostnames(object))
	address := c.handler.Address(object)
	ctx = logr.NewContext(ctx, logr.FromContextOrDiscard(ctx).V(1))
	if object.GetName() != "" &&
		object.GetDeletionTimestamp() == nil &&
		len(hostnames) > 0 &&
		address != "" {
		return result, c.createDeployment(ctx, object, hostnames, address)
	}
	return result, c.removeDeployment(ctx, request.NamespacedName)
}

func (c *genericAvahiController[T]) expandHostnames(object T, hostnames []string) []string {
	expanded := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		if hostname == "-" {
			hostname = object.GetName() + "." + object.GetNamespace() + "." + c.config.HostnameSuffix
		} else if !strings.Contains(hostname, ".") {
			hostname += "." + c.config.HostnameSuffix
		}
		if !slices.Contains(expanded, hostname) {
			expanded = append(expanded, hostname)
		}
	}
	return expanded
}

func (c *genericAvahiController[T]) SetupWithManager(
	mgr controllerruntime.Manager,
	config *AvahiConfig,
) error {
	if config == nil {
		return errNilAvahiConfig
	}
	if c.handler == nil {
		return errNilAvahiHandler
	}
	object, err := newClientObject[T]()
	if err != nil {
		return err
	}
	c.client = mgr.GetClient()
	c.config = config
	return controllerruntime.NewControllerManagedBy(mgr).
		For(object).
		Owns(&appsv1.Deployment{}).
		Complete(c)
}

func (c *genericAvahiController[T]) createDeployment(
	ctx context.Context,
	owner T,
	hostnames []string,
	address string,
) error {
	deploymentKey := c.handler.DeploymentKey(owner)
	log := logr.FromContextOrDiscard(ctx).WithValues("deployment", deploymentKey)
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: deploymentKey.Namespace, Name: deploymentKey.Name},
	}
	if err := c.client.Get(ctx, deploymentKey, &deployment); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		log.Info("creating the deployment")
		if err = c.applyValues(owner, hostnames, address, &deployment); err != nil {
			return err
		}
		return c.client.Create(ctx, &deployment)
	}

	patchSource := deployment.DeepCopy()
	if err := c.applyValues(owner, hostnames, address, &deployment); err != nil {
		return err
	}
	log.Info("patching the deployment")
	return c.client.Patch(ctx, &deployment, client.MergeFrom(patchSource))
}

func (c *genericAvahiController[T]) applyValues(
	owner T,
	hostnames []string,
	address string,
	deployment *appsv1.Deployment,
) error {
	if err := controllerutil.SetOwnerReference(owner, deployment, c.client.Scheme()); err != nil {
		return err
	}
	labels := c.handler.Labels(owner)
	containers := make([]corev1.Container, 0, len(hostnames))
	for index, hostname := range hostnames {
		name := "avahi-publish"
		if len(hostnames) > 1 {
			name = fmt.Sprintf("avahi-publish-%d", index)
		}
		containers = append(containers, corev1.Container{
			Name:            name,
			Image:           c.config.AvahiPublishImage,
			Args:            []string{hostname, address},
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: &corev1.SecurityContext{Privileged: ptr(true)},
			VolumeMounts: []corev1.VolumeMount{{
				Name: MountNameDBUS, ReadOnly: true, MountPath: MountPathDBUS,
			}},
		})
	}
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas: ptr(int32(1)),
		Selector: &metav1.LabelSelector{MatchLabels: labels},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				Containers: containers,
				Volumes: []corev1.Volume{{
					Name: MountNameDBUS,
					VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
						Path: MountPathDBUS,
					}},
				}},
			},
		},
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
	}
	return nil
}

func (c *genericAvahiController[T]) removeDeployment(
	ctx context.Context,
	name types.NamespacedName,
) error {
	deploymentKey := c.handler.DeploymentKeyByName(name)
	log := logr.FromContextOrDiscard(ctx).WithValues("deployment", deploymentKey)
	var deployment appsv1.Deployment
	if err := c.client.Get(ctx, deploymentKey, &deployment); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("deployment not found")
			return nil
		}
		return err
	}
	log.Info("deleting deployment")
	if err := c.client.Delete(ctx, &deployment); !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}

func newClientObject[T client.Object]() (T, error) {
	var zero T
	objectType := reflect.TypeFor[T]()
	if objectType.Kind() != reflect.Pointer {
		return zero, fmt.Errorf("client object type %s must be a pointer", objectType)
	}
	object, ok := reflect.New(objectType.Elem()).Interface().(T)
	if !ok {
		return zero, fmt.Errorf("cannot create client object type %s", objectType)
	}
	return object, nil
}

func ptr[T any](value T) *T {
	return &value
}
