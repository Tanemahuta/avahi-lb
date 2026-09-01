package controllers_test

import (
	"context"
	"errors"

	"github.com/Tanemahuta/avahi-lb/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Publisher Deployment cleanup", Serial, func() {
	config := &controllers.AvahiConfig{
		HostnameSuffix:    "my-cluster.local",
		AllowedTLDs:       []string{"local"},
		AvahiPublishImage: testAvahiPublishImage,
		PublishNamespace:  "avahi-lb-system",
	}

	It("requires configuration", func() {
		Expect(controllers.DeploymentCleanup(ctx, k8sClient, k8sClient, nil)).
			To(MatchError("avahi config must not be nil"))
	})

	It("returns deployment listing errors", func() {
		expectedErr := errors.New("list deployments")
		reader := failingListReader{Reader: k8sClient, err: expectedErr}

		Expect(controllers.DeploymentCleanup(ctx, reader, k8sClient, config)).
			To(MatchError(expectedErr))
	})

	DescribeTable("validating Service-owned Deployments",
		func(prepare func(*corev1.Service, *appsv1.Deployment), shouldRemain bool) {
			service := cleanupService()
			deployment := cleanupDeployment("avahi-service-"+service.Name, service.Namespace)
			prepare(service, deployment)
			if service.Name != "" {
				desiredStatus := service.Status
				service.Status = corev1.ServiceStatus{}
				Expect(k8sClient.Create(ctx, service)).To(Succeed())
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, service) })
				setControllerOwner(deployment, "v1", "Service", service.Name, service.UID)
				if len(desiredStatus.LoadBalancer.Ingress) > 0 {
					service.Status = desiredStatus
					Expect(k8sClient.Status().Update(ctx, service)).To(Succeed())
				}
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, deployment) })

			Expect(controllers.DeploymentCleanup(ctx, k8sClient, k8sClient, config)).To(Succeed())
			getErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
			if shouldRemain {
				Expect(getErr).NotTo(HaveOccurred())
			} else {
				Expect(k8serrors.IsNotFound(getErr)).To(BeTrue())
			}
		},
		Entry("retains a valid deployment",
			func(*corev1.Service, *appsv1.Deployment) {},
			true,
		),
		Entry("removes a deployment without a publication annotation",
			func(service *corev1.Service, _ *appsv1.Deployment) {
				service.Annotations = nil
			},
			false,
		),
		Entry("removes a deployment without a load-balancer IP",
			func(service *corev1.Service, _ *appsv1.Deployment) {
				service.Status = corev1.ServiceStatus{}
			},
			false,
		),
		Entry("removes a deployment with the wrong name",
			func(_ *corev1.Service, deployment *appsv1.Deployment) {
				deployment.Name = "avahi-wrong-name"
			},
			false,
		),
	)

	It("removes a Service Deployment with a stale owner UID", func() {
		service := cleanupService()
		service.Status = corev1.ServiceStatus{}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, service) })
		deployment := cleanupDeployment("avahi-service-"+service.Name, service.Namespace)
		setControllerOwner(deployment, "v1", "Service", service.Name, types.UID("stale"))
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, deployment) })

		Expect(controllers.DeploymentCleanup(ctx, k8sClient, k8sClient, config)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).
			To(WithTransform(k8serrors.IsNotFound, BeTrue()))
	})

	It("removes a Service-owned Deployment whose owner is missing", func() {
		deployment := cleanupDeployment("avahi-missing-service", namespace.Name)
		setControllerOwner(deployment, "v1", "Service", "missing-service", types.UID("missing"))
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, deployment) })

		Expect(controllers.DeploymentCleanup(ctx, k8sClient, k8sClient, config)).To(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
		}, timeout, poll).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))
	})

	It("returns Service owner lookup errors", func() {
		deployment := cleanupDeployment("avahi-owner-error", namespace.Name)
		setControllerOwner(deployment, "v1", "Service", "owner-error", types.UID("owner-error"))
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, deployment) })
		expectedErr := errors.New("get service owner")
		reader := failingGetReader{Reader: k8sClient, err: expectedErr}

		Expect(controllers.DeploymentCleanup(ctx, reader, k8sClient, config)).
			To(MatchError(expectedErr))
	})

	It("removes legacy Ingress-owned Deployments", func() {
		ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
			Name:      "cleanup-ingress",
			Namespace: namespace.Name,
		}, Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{Host: "cleanup.example"}},
		}}
		Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ingress) })
		deployment := cleanupDeployment("avahi-ingress-old-class", ingress.Namespace)
		setControllerOwner(
			deployment,
			"networking.k8s.io/v1",
			"Ingress",
			ingress.Name,
			ingress.UID,
		)
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, deployment) })

		Expect(controllers.DeploymentCleanup(ctx, k8sClient, k8sClient, config)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).
			To(WithTransform(k8serrors.IsNotFound, BeTrue()))
	})

	It("preserves unowned and non-controller-owned Deployments", func() {
		unowned := cleanupDeployment("unowned-publisher", namespace.Name)
		Expect(k8sClient.Create(ctx, unowned)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, unowned) })
		referenced := cleanupDeployment("referenced-publisher", namespace.Name)
		controller := false
		referenced.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       "anything",
			UID:        "anything",
			Controller: &controller,
		}}
		Expect(k8sClient.Create(ctx, referenced)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, referenced) })

		Expect(controllers.DeploymentCleanup(ctx, k8sClient, k8sClient, config)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(unowned), unowned)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(referenced), referenced)).To(Succeed())
	})

	It("returns Deployment deletion errors", func() {
		deployment := cleanupDeployment("avahi-ingress-delete-error", namespace.Name)
		setControllerOwner(
			deployment,
			"networking.k8s.io/v1",
			"Ingress",
			"delete-error",
			types.UID("delete-error"),
		)
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, deployment) })
		expectedErr := errors.New("delete deployment")
		writer := failingDeleteWriter{Writer: k8sClient, err: expectedErr}

		Expect(controllers.DeploymentCleanup(ctx, k8sClient, writer, config)).
			To(MatchError(expectedErr))
	})
})

func cleanupService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "cleanup-service",
			Namespace:   namespace.Name,
			Annotations: map[string]string{controllers.PublishAnnotation: "-"},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Name: "web", Port: 80}},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}},
			},
		},
	}
}

func cleanupDeployment(name, deploymentNamespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: deploymentNamespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"cleanup": "true"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"cleanup": "true"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "publisher", Image: testAvahiPublishImage}},
				},
			},
		},
	}
}

func setControllerOwner(
	deployment *appsv1.Deployment,
	apiVersion string,
	kind string,
	name string,
	uid types.UID,
) {
	controller := true
	deployment.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		UID:        uid,
		Controller: &controller,
	}}
}

type failingListReader struct {
	client.Reader
	err error
}

func (r failingListReader) List(
	context.Context,
	client.ObjectList,
	...client.ListOption,
) error {
	return r.err
}

type failingGetReader struct {
	client.Reader
	err error
}

func (r failingGetReader) Get(
	context.Context,
	client.ObjectKey,
	client.Object,
	...client.GetOption,
) error {
	return r.err
}

type failingDeleteWriter struct {
	client.Writer
	err error
}

func (w failingDeleteWriter) Delete(
	context.Context,
	client.Object,
	...client.DeleteOption,
) error {
	return w.err
}
