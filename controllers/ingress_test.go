package controllers_test

import (
	"strings"

	"github.com/Tanemahuta/avahi-lb/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Ingress controller", Serial, func() {
	var (
		ingress       *networkingv1.Ingress
		status        networkingv1.IngressStatus
		expDeployment *appsv1.Deployment
		sut           controllers.AvahiReconciler
		request       reconcile.Request
	)

	BeforeEach(func() {
		ingress = ReadResource[*networkingv1.Ingress]("../config/samples/ingress.yaml")
		ingress.Status.DeepCopyInto(&status)
		ingress.Status = networkingv1.IngressStatus{}
		expDeployment = ReadResource[*appsv1.Deployment]("testdata/expected/ingress_deployment.yaml")
		Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
		sut = SetupReconciler(controllers.NewIngress(), &controllers.AvahiConfig{
			HostnameSuffix:    "my-cluster.local",
			AvahiPublishImage: testAvahiPublishImage,
		})
		request = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ingress)}
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, ingress)
		var deployments appsv1.DeploymentList
		_ = k8sClient.List(ctx, &deployments, client.InNamespace(namespace.Name))
		for index := range deployments.Items {
			if strings.HasPrefix(deployments.Items[index].Name, "avahi-ingress-") {
				_ = k8sClient.Delete(ctx, &deployments.Items[index])
			}
		}
	})

	It("does not create a Deployment before the Ingress has an IP address", func() {
		_, err := sut.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		var deployment appsv1.Deployment
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), &deployment)).
			To(WithTransform(k8serrors.IsNotFound, BeTrue()))
	})

	When("status contains an IP address", func() {
		BeforeEach(func() {
			ingressCopy := ingress.DeepCopy()
			patch := client.MergeFrom(ingressCopy)
			ingress.Status = status
			Expect(k8sClient.Status().Patch(ctx, ingress, patch)).To(Succeed())
		})

		It("creates a Deployment publishing every Ingress hostname", func() {
			ingress.Spec.Rules = append(ingress.Spec.Rules,
				networkingv1.IngressRule{Host: "first.my-cluster.local"},
				networkingv1.IngressRule{},
			)
			Expect(k8sClient.Update(ctx, ingress)).To(Succeed())

			_, err := sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())

			var deployment appsv1.Deployment
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), &deployment)
			}, timeout, poll).Should(Succeed())
			copyMeta(expDeployment, ingress, &deployment)
			expectedJSON, err := json.Marshal(expDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(json.Marshal(&deployment)).To(MatchJSON(expectedJSON))
		})

		It("uses an explicit annotation value as a hostname override", func() {
			ingress.Annotations[controllers.PublishAnnotation] = "override"
			Expect(k8sClient.Update(ctx, ingress)).To(Succeed())
			const configuredImage = "registry.example/avahi-publish:test"
			sut = SetupReconciler(controllers.NewIngress(), &controllers.AvahiConfig{
				HostnameSuffix:    "my-cluster.local",
				AvahiPublishImage: configuredImage,
			})

			_, err := sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			var deployment appsv1.Deployment
			Eventually(func() []string {
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), &deployment)
				if len(deployment.Spec.Template.Spec.Containers) == 0 {
					return nil
				}
				return deployment.Spec.Template.Spec.Containers[0].Args
			}, timeout, poll).Should(Equal([]string{"override.my-cluster.local", "10.0.0.1"}))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(configuredImage))
		})

		It("expands a dotless Ingress hostname with the configured suffix", func() {
			ingress.Spec.Rules = []networkingv1.IngressRule{{Host: "internal"}}
			Expect(k8sClient.Update(ctx, ingress)).To(Succeed())

			_, err := sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() []string {
				var deployment appsv1.Deployment
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), &deployment)
				if len(deployment.Spec.Template.Spec.Containers) == 0 {
					return nil
				}
				return deployment.Spec.Template.Spec.Containers[0].Args
			}, timeout, poll).Should(Equal([]string{"internal.my-cluster.local", "10.0.0.1"}))
		})

		It("publishes each expanded hostname only once", func() {
			ingress.Spec.Rules = []networkingv1.IngressRule{
				{Host: "internal"},
				{Host: "internal.my-cluster.local"},
				{Host: "internal"},
			}
			Expect(k8sClient.Update(ctx, ingress)).To(Succeed())

			_, err := sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() []corev1.Container {
				var deployment appsv1.Deployment
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), &deployment)
				return deployment.Spec.Template.Spec.Containers
			}, timeout, poll).Should(ConsistOf(
				WithTransform(func(container corev1.Container) []string {
					return container.Args
				}, Equal([]string{"internal.my-cluster.local", "10.0.0.1"})),
			))
		})

		It("patches the Deployment when the load-balancer IP changes", func() {
			_, err := sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			ingress.Status.LoadBalancer.Ingress[0].IP = "10.0.0.2"
			Expect(k8sClient.Status().Update(ctx, ingress)).To(Succeed())

			_, err = sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() string {
				var deployment appsv1.Deployment
				_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), &deployment)
				return deployment.Spec.Template.Spec.Containers[0].Args[1]
			}, timeout, poll).Should(Equal("10.0.0.2"))
		})

		It("removes the Deployment when publishing is disabled", func() {
			_, err := sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			delete(ingress.Annotations, controllers.PublishAnnotation)
			Expect(k8sClient.Update(ctx, ingress)).To(Succeed())

			_, err = sut.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				var deployment appsv1.Deployment
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(expDeployment), &deployment)
			}, timeout, poll).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))
		})
	})

	It("handles an Ingress that no longer exists", func() {
		Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())
		Eventually(func() error {
			var current networkingv1.Ingress
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(ingress), &current)
		}, timeout, poll).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

		_, err := sut.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
	})

	It("supports Ingress names longer than Kubernetes label values", func() {
		Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())

		ingress = ReadResource[*networkingv1.Ingress]("../config/samples/ingress.yaml")
		ingress.Name = strings.Repeat("long-name-", 9) + "long-name"
		ingress.Status.DeepCopyInto(&status)
		ingress.Status = networkingv1.IngressStatus{}
		Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
		ingress.Status = status
		Expect(k8sClient.Status().Update(ctx, ingress)).To(Succeed())

		_, err := sut.Reconcile(ctx, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(ingress),
		})
		Expect(err).NotTo(HaveOccurred())

		var deployments appsv1.DeploymentList
		Expect(k8sClient.List(ctx, &deployments, client.InNamespace(ingress.Namespace))).To(Succeed())
		Expect(deployments.Items).To(HaveLen(1))
		Expect(len(deployments.Items[0].Name)).To(BeNumerically("<=", 63))
		Expect(len(deployments.Items[0].Spec.Selector.MatchLabels["ingress.kubernetes.io/name"])).
			To(BeNumerically("<=", 63))
	})
})
