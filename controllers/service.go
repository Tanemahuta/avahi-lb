package controllers

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete

// Service reconciles annotated Kubernetes Services.
type Service struct {
	client client.Client
	config *AvahiConfig
}

var _ AvahiReconciler = (*Service)(nil)

// NewService creates a Service reconciler.
func NewService() AvahiReconciler {
	return &Service{}
}

func (s *Service) Reconcile(
	ctx context.Context,
	request reconcile.Request,
) (reconcile.Result, error) {
	var (
		result  reconcile.Result
		service corev1.Service
	)
	getErr := s.client.Get(ctx, request.NamespacedName, &service)
	if getErr != nil && !k8serrors.IsNotFound(getErr) {
		return result, getErr
	}

	hostnames := expandHostnames(&service, serviceHostnames(&service), s.config.HostnameSuffix)
	address := serviceAddress(&service)
	if service.Name != "" &&
		service.DeletionTimestamp == nil &&
		len(hostnames) > 0 &&
		address != "" {
		return result, s.applyDeployment(ctx, &service, hostnames, address)
	}
	return result, deleteDeployment(ctx, s.client, serviceDeploymentKey(request.NamespacedName))
}

func (s *Service) SetupWithManager(
	mgr controllerruntime.Manager,
	config *AvahiConfig,
) error {
	if config == nil {
		return errNilAvahiConfig
	}
	s.client = mgr.GetClient()
	s.config = config
	return controllerruntime.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Complete(s)
}

func (s *Service) applyDeployment(
	ctx context.Context,
	service *corev1.Service,
	hostnames []string,
	address string,
) error {
	key := serviceDeploymentKey(client.ObjectKeyFromObject(service))
	return upsertDeployment(ctx, s.client, key, func(deployment *appsv1.Deployment) error {
		if ownerErr := controllerutil.SetOwnerReference(service, deployment, s.client.Scheme()); ownerErr != nil {
			return ownerErr
		}
		publications := []avahiPublication{{address: address, hostnames: hostnames}}
		applyPublisherDeployment(deployment, s.config, publications, map[string]string{
			"service.kubernetes.io/name":      service.Name,
			"service.kubernetes.io/namespace": service.Namespace,
		}, false)
		return nil
	})
}

func serviceHostnames(service *corev1.Service) []string {
	hostname, ok := service.Annotations[PublishAnnotation]
	if !ok {
		return nil
	}
	return []string{hostname}
}

func serviceAddress(service *corev1.Service) string {
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP
		}
	}
	return ""
}

func serviceDeploymentKey(serviceKey types.NamespacedName) client.ObjectKey {
	return client.ObjectKey{Namespace: serviceKey.Namespace, Name: "avahi-" + serviceKey.Name}
}
