package controllers

import (
	"context"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	PublishAnnotation = "service.beta.kubernetes.io/avahi-publish"
	MountNameDBUS     = "dbus"
	MountPathDBUS     = "/var/run/dbus"
)

var _ reconcile.Reconciler = &Service{}

//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete

type Service struct {
	// HostnameSuffix to be appended.
	HostnameSuffix string
	// Client to be used to apply the deployments.
	Client client.Client
}

func (s *Service) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var (
		result reconcile.Result
		svc    corev1.Service
	)
	if err := s.Client.Get(ctx, request.NamespacedName, &svc); err != nil {
		if !k8serrors.IsNotFound(err) {
			return result, err
		}
	}
	hostname, address := s.hostnameAndAddress(&svc)
	ctx = logr.NewContext(ctx, logr.FromContextOrDiscard(ctx).V(1))
	if svc.GetDeletionTimestamp() == nil && len(hostname) > 0 && len(address) > 0 {
		return result, s.createDeployment(ctx, &svc, hostname, address)
	}
	return result, s.removeDeployment(ctx, request.NamespacedName)
}

func (s *Service) SetupWithManager(mgr controllerruntime.Manager) error {
	s.Client = mgr.GetClient()
	return controllerruntime.NewControllerManagedBy(mgr).For(&corev1.Service{}).Owns(&appsv1.Deployment{}).Complete(s)
}

func (s *Service) hostnameAndAddress(svc *corev1.Service) (string, string) {
	if svc.Annotations == nil {
		return "", ""
	}
	// Stop if publishing is not requested
	hostname, ok := svc.Annotations[PublishAnnotation]
	if !ok {
		return "", ""
	}
	if hostname == "-" {
		hostname = svc.Name + "." + svc.Namespace
	}
	// Add the hostname suffix
	hostname += "." + s.HostnameSuffix
	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if address := ingress.IP; len(address) > 0 {
			return hostname, address
		}
	}
	return "", ""
}

func (s *Service) deploymentKey(serviceKey types.NamespacedName) client.ObjectKey {
	return client.ObjectKey{Namespace: serviceKey.Namespace, Name: "avahi-" + serviceKey.Name}
}

func (s *Service) createDeployment(ctx context.Context, svc *corev1.Service, hostname string, address string) error {
	deploymentKey := s.deploymentKey(client.ObjectKeyFromObject(svc))
	log := logr.FromContextOrDiscard(ctx).WithValues("deployment", deploymentKey)
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: deploymentKey.Namespace, Name: deploymentKey.Name},
	}
	if err := s.Client.Get(ctx, deploymentKey, &deployment); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("creating the deployment")
			if err = s.applyValues(svc, hostname, address, &deployment); err != nil {
				return err
			}
			return s.Client.Create(ctx, &deployment)
		}
		return err
	}
	var patchSource appsv1.Deployment
	deployment.DeepCopyInto(&patchSource)
	patch := client.MergeFrom(&patchSource)
	if err := s.applyValues(svc, hostname, address, &deployment); err != nil {
		return err
	}
	log.Info("patching the deployment")
	return s.Client.Patch(ctx, &deployment, patch)
}

func (s *Service) applyValues(svc *corev1.Service, hostname string, address string, d *appsv1.Deployment) error {
	if err := controllerutil.SetOwnerReference(svc, d, s.Client.Scheme()); err != nil {
		return err
	}
	labels := map[string]string{
		"service.kubernetes.io/name":      svc.Name,
		"service.kubernetes.io/namespace": svc.Namespace,
	}
	d.Spec = appsv1.DeploymentSpec{
		Replicas: ptr(int32(1)),
		Selector: &metav1.LabelSelector{MatchLabels: labels},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "avahi-publish",
						Image:   "ydkn/avahi:latest",
						Command: []string{"avahi-publish"},
						Args:    []string{"-a", hostname, address},
						VolumeMounts: []corev1.VolumeMount{
							{Name: MountNameDBUS, ReadOnly: true, MountPath: MountPathDBUS},
						},
						ImagePullPolicy: corev1.PullAlways,
						SecurityContext: &corev1.SecurityContext{Privileged: ptr(true)},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name:         MountNameDBUS,
						VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: MountPathDBUS}},
					},
				},
			},
		},
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
	}
	return nil
}

func (s *Service) removeDeployment(ctx context.Context, name types.NamespacedName) error {
	deploymentKey := s.deploymentKey(name)
	log := logr.FromContextOrDiscard(ctx).WithValues("deployment", deploymentKey)
	var deployment appsv1.Deployment
	if err := s.Client.Get(ctx, deploymentKey, &deployment); err != nil {
		// Skip not found
		if k8serrors.IsNotFound(err) {
			log.Info("deployment not found")
			return nil
		}
		return err
	}
	log.Info("deleting deployment")
	// Skip not found
	if err := s.Client.Delete(ctx, &deployment); !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}

func ptr[T any](t T) *T {
	return &t
}
