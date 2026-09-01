package controllers //nolint:testpackage // The injected private deployment handler is tested directly.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Service deployment handling", func() {
	It("delegates a publishable Service to the deployment handler", func() {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "web",
				Namespace:   "apps",
				Annotations: map[string]string{PublishAnnotation: "-"},
			},
			Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "192.0.2.10"}},
			}},
		}
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(service).Build()
		config := &AvahiConfig{HostnameSuffix: "cluster.local", AllowedTLDs: []string{"local"}}
		handler := NewMockDeploymentHandler(gomock.NewController(GinkgoT()))
		handler.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, publication AvahiPublication) (client.ObjectKey, error) {
				Expect(publication.Owner).To(Equal(service))
				Expect(publication.Labels).To(Equal(map[string]string{
					"service.kubernetes.io/name":      "web",
					"service.kubernetes.io/namespace": "apps",
				}))
				Expect(publication.Addresses).To(Equal([]AvahiAddress{{
					Address: "192.0.2.10", Hostnames: []string{"web.apps.cluster.local"},
				}}))
				return client.ObjectKey{Namespace: "apps", Name: "avahi-service-web"}, nil
			},
		)
		sut := &Service{client: k8sClient, config: config, deployments: handler}

		_, err := sut.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: service.Namespace,
			Name:      service.Name,
		}})
		Expect(err).NotTo(HaveOccurred())
	})
})
