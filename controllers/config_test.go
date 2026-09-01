package controllers_test

import (
	"context"

	"github.com/Tanemahuta/avahi-lb/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sethvargo/go-envconfig"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Controller configuration", func() {
	DescribeTable("rejecting nil configuration during setup",
		func(reconciler controllers.AvahiReconciler) {
			Expect(reconciler.SetupWithManager(nil, nil)).
				To(MatchError("avahi config must not be nil"))
		},
		Entry("Service controller", controllers.NewService()),
		Entry("Ingress controller", controllers.NewIngress()),
	)

	It("loads the hostname suffix and publisher image from the environment", func() {
		var config controllers.AvahiConfig
		err := envconfig.ProcessWith(context.Background(), &envconfig.Config{
			Target: &config,
			Lookuper: envconfig.MapLookuper(map[string]string{
				"AVAHI_HOSTNAME_SUFFIX":     "new.local",
				"KUBERNETES_CLUSTER_DOMAIN": "legacy.local",
				"AVAHI_PUBLISH_IMAGE":       "registry.example/avahi-publish:test",
				"AVAHI_PUBLISH_NAMESPACE":   "avahi-lb",
			}),
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(config.Validate()).To(Succeed())
		Expect(config.HostnameSuffix).To(Equal("new.local"))
		Expect(config.AllowedTLDs).To(Equal([]string{"local"}))
		Expect(config.AvahiPublishImage).To(Equal("registry.example/avahi-publish:test"))
		Expect(config.PublishNamespace).To(Equal("avahi-lb"))
	})

	It("loads a comma-separated allowed TLD list", func() {
		var config controllers.AvahiConfig
		err := envconfig.ProcessWith(context.Background(), &envconfig.Config{
			Target: &config,
			Lookuper: envconfig.MapLookuper(map[string]string{
				"AVAHI_HOSTNAME_SUFFIX":   "cluster.local",
				"AVAHI_ALLOWED_TLDS":      " local, .INTERNAL.,local",
				"AVAHI_PUBLISH_IMAGE":     "registry.example/avahi-publish:test",
				"AVAHI_PUBLISH_NAMESPACE": "avahi-lb",
			}),
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(config.Validate()).To(Succeed())
		Expect(config.AllowedTLDs).To(Equal([]string{"local", "internal"}))
		Expect(config.IsAllowed("host.cluster.LOCAL.")).To(BeTrue())
		Expect(config.IsAllowed("host.internal")).To(BeTrue())
		Expect(config.IsAllowed("host.notlocal")).To(BeFalse())
		Expect(config.FilterAllowedHostnames([]string{
			"host.local",
			"host.example",
			"host.internal",
		})).To(Equal([]string{"host.local", "host.internal"}))
	})

	It("expands, deduplicates, and filters hostnames", func() {
		config := controllers.AvahiConfig{
			HostnameSuffix:    "cluster.local",
			AllowedTLDs:       []string{"local"},
			AvahiPublishImage: "registry.example/avahi-publish:test",
			PublishNamespace:  "avahi-lb",
		}
		Expect(config.Validate()).To(Succeed())
		key := types.NamespacedName{
			Name:      "traefik",
			Namespace: "ingress-system",
		}
		hostnames := []string{
			"-",
			"dashboard",
			"dashboard.cluster.local",
			"qualified.example",
			"dashboard",
		}

		Expect(config.ExpandHostnames(key, hostnames)).To(Equal([]string{
			"traefik.ingress-system.cluster.local",
			"dashboard.cluster.local",
			"qualified.example",
		}))
		Expect(config.PublishableHostnames(key, hostnames)).To(Equal([]string{
			"traefik.ingress-system.cluster.local",
			"dashboard.cluster.local",
		}))
	})

	It("qualifies dotless names and preserves qualified local names", func() {
		config := controllers.AvahiConfig{
			HostnameSuffix: "xy.local",
			AllowedTLDs:    []string{"local"},
		}
		key := types.NamespacedName{Name: "service", Namespace: "apps"}

		Expect(config.PublishableHostnames(key, []string{"bla", "blubb.local"})).To(Equal(
			[]string{"bla.xy.local", "blubb.local"},
		))
	})

	It("requires at least one allowed TLD", func() {
		config := controllers.AvahiConfig{
			HostnameSuffix:    "cluster.local",
			AllowedTLDs:       []string{" ", "."},
			AvahiPublishImage: "registry.example/avahi-publish:test",
			PublishNamespace:  "avahi-lb",
		}

		Expect(config.Validate()).To(MatchError("at least one allowed TLD is required"))
	})

	It("supports the legacy hostname suffix environment variable", func() {
		var config controllers.AvahiConfig
		err := envconfig.ProcessWith(context.Background(), &envconfig.Config{
			Target: &config,
			Lookuper: envconfig.MapLookuper(map[string]string{
				"KUBERNETES_CLUSTER_DOMAIN": "legacy.local",
				"AVAHI_PUBLISH_IMAGE":       "registry.example/avahi-publish:test",
				"POD_NAMESPACE":             "avahi-lb",
			}),
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(config.Validate()).To(Succeed())
		Expect(config.HostnameSuffix).To(Equal("legacy.local"))
	})

	It("requires a publisher image", func() {
		var config controllers.AvahiConfig
		err := envconfig.ProcessWith(context.Background(), &envconfig.Config{
			Target:   &config,
			Lookuper: envconfig.MapLookuper(nil),
		})

		Expect(err).To(MatchError(ContainSubstring("AvahiPublishImage")))
	})

	It("requires a hostname suffix", func() {
		var config controllers.AvahiConfig
		err := envconfig.ProcessWith(context.Background(), &envconfig.Config{
			Target: &config,
			Lookuper: envconfig.MapLookuper(map[string]string{
				"AVAHI_PUBLISH_IMAGE": "registry.example/avahi-publish:test",
				"POD_NAMESPACE":       "avahi-lb",
			}),
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(config.Validate()).To(MatchError("hostname suffix is required"))
	})

	It("requires a publish namespace", func() {
		var config controllers.AvahiConfig
		err := envconfig.ProcessWith(context.Background(), &envconfig.Config{
			Target: &config,
			Lookuper: envconfig.MapLookuper(map[string]string{
				"AVAHI_HOSTNAME_SUFFIX": "cluster.local",
				"AVAHI_PUBLISH_IMAGE":   "registry.example/avahi-publish:test",
			}),
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(config.Validate()).To(MatchError("publish namespace is required"))
	})
})
