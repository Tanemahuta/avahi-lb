package controllers

import (
	"errors"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

var errNilAvahiConfig = errors.New("avahi config must not be nil")

// AvahiConfig contains the shared configuration for Avahi reconcilers.
type AvahiConfig struct {
	HostnameSuffix    string   `env:"AVAHI_HOSTNAME_SUFFIX, default=$KUBERNETES_CLUSTER_DOMAIN"`
	AllowedTLDs       []string `env:"AVAHI_ALLOWED_TLDS, default=local"`
	AvahiPublishImage string   `env:"AVAHI_PUBLISH_IMAGE, required"`
	PublishNamespace  string   `env:"AVAHI_PUBLISH_NAMESPACE, default=$POD_NAMESPACE"`
}

// Validate verifies that all required configuration is present.
func (c *AvahiConfig) Validate() error {
	if c.HostnameSuffix == "" {
		return errors.New("hostname suffix is required")
	}
	c.AllowedTLDs = normalizeTLDs(c.AllowedTLDs)
	if len(c.AllowedTLDs) == 0 {
		return errors.New("at least one allowed TLD is required")
	}
	if c.PublishNamespace == "" {
		return errors.New("publish namespace is required")
	}
	return nil
}

// IsAllowed reports whether hostname ends on a configured DNS suffix boundary.
func (c *AvahiConfig) IsAllowed(hostname string) bool {
	name := strings.ToLower(strings.TrimSuffix(hostname, "."))
	for _, tld := range c.AllowedTLDs {
		if name == tld || strings.HasSuffix(name, "."+tld) {
			return true
		}
	}
	return false
}

// FilterAllowedHostnames returns hostnames permitted by the configured TLDs.
func (c *AvahiConfig) FilterAllowedHostnames(hostnames []string) []string {
	return slices.DeleteFunc(slices.Clone(hostnames), func(hostname string) bool {
		return !c.IsAllowed(hostname)
	})
}

// ExpandHostnames qualifies generated and dotless hostnames with the configured suffix.
func (c *AvahiConfig) ExpandHostnames(key types.NamespacedName, hostnames []string) []string {
	expanded := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		if hostname == "-" {
			hostname = key.Name + "." + key.Namespace + "." + c.HostnameSuffix
		} else if !strings.Contains(hostname, ".") {
			hostname += "." + c.HostnameSuffix
		}
		if !slices.Contains(expanded, hostname) {
			expanded = append(expanded, hostname)
		}
	}
	return expanded
}

// PublishableHostnames expands hostnames and applies the configured TLD policy.
func (c *AvahiConfig) PublishableHostnames(key types.NamespacedName, hostnames []string) []string {
	return c.FilterAllowedHostnames(c.ExpandHostnames(key, hostnames))
}

func normalizeTLDs(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		tld := strings.ToLower(strings.Trim(strings.TrimSpace(value), "."))
		if tld != "" && !slices.Contains(normalized, tld) {
			normalized = append(normalized, tld)
		}
	}
	return normalized
}
