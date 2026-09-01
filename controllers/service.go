package controllers

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete

// Service reconciles annotated Kubernetes Services.
type Service struct {
	client      client.Client
	config      *AvahiConfig
	deployments DeploymentHandler
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

	hostnames := s.config.PublishableHostnames(
		client.ObjectKeyFromObject(&service),
		serviceHostnames(&service),
	)
	address := serviceAddress(&service)
	if service.Name != "" &&
		service.DeletionTimestamp == nil &&
		len(hostnames) > 0 &&
		address != "" {
		_, publishErr := s.deployments.Publish(ctx, AvahiPublication{
			Owner: &service,
			Labels: map[string]string{
				"service.kubernetes.io/name":      service.Name,
				"service.kubernetes.io/namespace": service.Namespace,
			},
			Addresses: []AvahiAddress{{Address: address, Hostnames: hostnames}},
		})
		return result, publishErr
	}
	return result, s.deployments.Delete(ctx, serviceKind, request.NamespacedName)
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
	var handlerErr error
	s.deployments, handlerErr = NewDeploymentHandler(s.client, config)
	if handlerErr != nil {
		return handlerErr
	}
	return controllerruntime.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Complete(s)
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
