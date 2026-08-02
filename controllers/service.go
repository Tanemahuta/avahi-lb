package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete

// Service reconciles annotated Kubernetes Services.
type Service = genericAvahiController[*corev1.Service]

type serviceHandler struct{}

// NewService creates a Service reconciler.
func NewService() AvahiReconciler {
	return NewAvahiReconciler[*corev1.Service](serviceHandler{})
}

func (serviceHandler) Hostnames(service *corev1.Service) []string {
	hostname, ok := service.Annotations[PublishAnnotation]
	if !ok {
		return nil
	}
	return []string{hostname}
}

func (serviceHandler) Address(service *corev1.Service) string {
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP
		}
	}
	return ""
}

func (serviceHandler) DeploymentKey(service *corev1.Service) client.ObjectKey {
	return serviceDeploymentKey(client.ObjectKeyFromObject(service))
}

func (serviceHandler) DeploymentKeyByName(serviceKey types.NamespacedName) client.ObjectKey {
	return serviceDeploymentKey(serviceKey)
}

func (serviceHandler) Labels(service *corev1.Service) map[string]string {
	return map[string]string{
		"service.kubernetes.io/name":      service.Name,
		"service.kubernetes.io/namespace": service.Namespace,
	}
}

func serviceDeploymentKey(serviceKey types.NamespacedName) client.ObjectKey {
	return client.ObjectKey{Namespace: serviceKey.Namespace, Name: "avahi-" + serviceKey.Name}
}
