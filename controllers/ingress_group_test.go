package controllers_test

import (
	"github.com/Tanemahuta/avahi-lb/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Grouping Ingress publications", func() {
	It("groups unique sorted hostnames by class and IP", func() {
		ingresses := []networkingv1.Ingress{
			publishableIngress("first", "traefik", "192.0.2.10", "z.example", "a.example"),
			publishableIngress("second", "traefik", "192.0.2.10", "a.example", "middle"),
			publishableIngress("third", "traefik", "192.0.2.11", "other.example"),
			publishableIngress("fourth", "nginx", "192.0.2.10", "nginx.example"),
		}

		Expect(controllers.GroupIngresses(ingresses, "", "cluster.local")).To(Equal(
			controllers.IngressPublicationGroups{
				"traefik": {
					"192.0.2.10": {"a.example", "middle.cluster.local", "z.example"},
					"192.0.2.11": {"other.example"},
				},
				"nginx": {
					"192.0.2.10": {"nginx.example"},
				},
			},
		))
	})

	It("resolves legacy and default Ingress classes", func() {
		legacy := publishableIngress("legacy", "", "192.0.2.10", "legacy.example")
		legacy.Annotations["kubernetes.io/ingress.class"] = "legacy-class"
		withoutClass := publishableIngress("default", "", "192.0.2.11", "default.example")

		Expect(controllers.GroupIngresses(
			[]networkingv1.Ingress{legacy, withoutClass},
			"default-class",
			"cluster.local",
		)).To(Equal(controllers.IngressPublicationGroups{
			"legacy-class": {
				"192.0.2.10": {"legacy.example"},
			},
			"default-class": {
				"192.0.2.11": {"default.example"},
			},
		}))
	})

	It("ignores Ingresses without a class, address, or publication annotation", func() {
		withoutClass := publishableIngress("classless", "", "192.0.2.10", "classless.example")
		withoutAddress := publishableIngress("addressless", "traefik", "", "addressless.example")
		withoutAnnotation := publishableIngress("unannotated", "traefik", "192.0.2.11", "ignored.example")
		delete(withoutAnnotation.Annotations, controllers.PublishAnnotation)

		Expect(controllers.GroupIngresses(
			[]networkingv1.Ingress{withoutClass, withoutAddress, withoutAnnotation},
			"",
			"cluster.local",
		)).To(BeEmpty())
	})
})

func publishableIngress(
	name string,
	className string,
	address string,
	hostnames ...string,
) networkingv1.Ingress {
	rules := make([]networkingv1.IngressRule, 0, len(hostnames))
	for _, hostname := range hostnames {
		rules = append(rules, networkingv1.IngressRule{Host: hostname})
	}
	ingress := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "test",
			Annotations: map[string]string{controllers.PublishAnnotation: "-"},
		},
		Spec: networkingv1.IngressSpec{Rules: rules},
	}
	if className != "" {
		ingress.Spec.IngressClassName = &className
	}
	if address != "" {
		ingress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{{IP: address}}
	}
	return ingress
}
