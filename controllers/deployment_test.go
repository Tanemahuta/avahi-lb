package controllers //nolint:testpackage // Shared package-private helpers are tested directly.

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Shared publisher Deployment helpers", func() {
	It("requires handler dependencies", func() {
		_, err := NewDeploymentHandler(nil, &AvahiConfig{})
		Expect(err).To(MatchError("deployment handler client must not be nil"))

		_, err = NewDeploymentHandler(fake.NewClientBuilder().Build(), nil)
		Expect(err).To(MatchError("avahi config must not be nil"))
	})

	It("uses the owner UID when the generated name would exceed the DNS limit", func() {
		key := publicationDeploymentKey("Service", types.NamespacedName{
			Namespace: "apps",
			Name:      strings.Repeat("a", maxDNSSubdomainLength),
		}, types.UID("12345678-1234-1234-1234-123456789abc"))

		Expect(key).To(Equal(types.NamespacedName{
			Namespace: "apps",
			Name:      "avahi-service-12345678-1234-1234-1234-123456789abc",
		}))
	})

	It("bounds ownerless publication names with a deterministic hash", func() {
		key := publicationDeploymentKey("Ingress", types.NamespacedName{
			Namespace: "avahi-lb",
			Name:      strings.Repeat("a", maxDNSSubdomainLength),
		}, "")

		Expect(key.Name).To(HaveLen(maxDNSSubdomainLength))
		Expect(key.Name).To(HaveSuffix("-" + shortHash("avahi-ingress-"+
			strings.Repeat("a", maxDNSSubdomainLength))))
	})

	DescribeTable("configures reverse publication",
		func(disableReverse bool, expectedArgs []string) {
			var deployment appsv1.Deployment
			handler := kubernetesDeploymentHandler{config: &AvahiConfig{AvahiPublishImage: "publisher:test"}}
			handler.apply(
				&deployment,
				AvahiPublication{
					Labels:         map[string]string{"app": "publisher"},
					Addresses:      []AvahiAddress{{Address: "192.0.2.1", Hostnames: []string{"host.cluster.local"}}},
					DisableReverse: disableReverse,
				},
			)

			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal(expectedArgs))
			if disableReverse {
				Expect(deployment.Spec.Template.Spec.Containers[0].Command).
					To(Equal([]string{"/bin/sh", "-c"}))
			} else {
				Expect(deployment.Spec.Template.Spec.Containers[0].Command).To(BeEmpty())
			}
		},
		Entry("enabled", false, []string{"host.cluster.local", "192.0.2.1"}),
		Entry("disabled", true, []string{
			groupedIngressPublishScript,
			"avahi-publish",
			"192.0.2.1",
			"host.cluster.local",
		}),
	)

	It("creates deterministically named containers for multiple addresses", func() {
		var deployment appsv1.Deployment
		handler := kubernetesDeploymentHandler{config: &AvahiConfig{AvahiPublishImage: "publisher:test"}}
		handler.apply(
			&deployment,
			AvahiPublication{
				Labels: map[string]string{"app": "publisher"},
				Addresses: []AvahiAddress{
					{Address: "192.0.2.1", Hostnames: []string{"first.example"}},
					{Address: "192.0.2.2", Hostnames: []string{"second.example"}},
				},
				DisableReverse: true,
			},
		)

		Expect(deployment.Spec.Template.Spec.Containers).To(HaveExactElements(
			HaveField("Name", "avahi-publish-0"),
			HaveField("Name", "avahi-publish-1"),
		))
		Expect(deployment.Spec.Selector.MatchLabels).To(Equal(map[string]string{"app": "publisher"}))
		Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
	})

	It("sorts publications by address", func() {
		Expect(publicationsByAddress(map[string][]string{
			"192.0.2.2": {"second.example"},
			"192.0.2.1": {"first.example"},
		})).To(Equal([]AvahiAddress{
			{Address: "192.0.2.1", Hostnames: []string{"first.example"}},
			{Address: "192.0.2.2", Hostnames: []string{"second.example"}},
		}))
	})

	DescribeTable("resolves a single default IngressClass",
		func(classes []networkingv1.IngressClass, expected string) {
			Expect(resolveDefaultIngressClass(classes)).To(Equal(expected))
		},
		Entry("without classes", nil, ""),
		Entry("without a default", []networkingv1.IngressClass{
			{ObjectMeta: metav1.ObjectMeta{Name: "traefik"}},
		}, ""),
		Entry("with one default", []networkingv1.IngressClass{
			{ObjectMeta: metav1.ObjectMeta{
				Name: "traefik",
				Annotations: map[string]string{
					defaultIngressClass: "true",
				},
			}},
		}, "traefik"),
		Entry("with ambiguous defaults", []networkingv1.IngressClass{
			{ObjectMeta: metav1.ObjectMeta{
				Name:        "traefik",
				Annotations: map[string]string{defaultIngressClass: "true"},
			}},
			{ObjectMeta: metav1.ObjectMeta{
				Name:        "nginx",
				Annotations: map[string]string{defaultIngressClass: "true"},
			}},
		}, ""),
	)

	It("recognizes only controller-owned legacy Ingress Deployments", func() {
		controller := true
		reference := metav1.OwnerReference{
			APIVersion: networkingAPIVersion,
			Kind:       ingressKind,
			Controller: &controller,
		}
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name:            "avahi-ingress-traefik",
			OwnerReferences: []metav1.OwnerReference{reference},
		}}
		Expect(isLegacyIngressDeployment(deployment)).To(BeTrue())

		controller = false
		deployment.OwnerReferences[0].Controller = &controller
		Expect(isLegacyIngressDeployment(deployment)).To(BeFalse())
		deployment.Name = "unrelated"
		Expect(isLegacyIngressDeployment(deployment)).To(BeFalse())
	})
})
