package controllers

import "errors"

var errNilAvahiConfig = errors.New("avahi config must not be nil")

// AvahiConfig contains the shared configuration for Avahi reconcilers.
type AvahiConfig struct {
	HostnameSuffix    string `env:"AVAHI_HOSTNAME_SUFFIX, default=$KUBERNETES_CLUSTER_DOMAIN"`
	AvahiPublishImage string `env:"AVAHI_PUBLISH_IMAGE, required"`
	PublishNamespace  string `env:"AVAHI_PUBLISH_NAMESPACE, default=$POD_NAMESPACE"`
}

// Validate verifies that all required configuration is present.
func (c *AvahiConfig) Validate() error {
	if c.HostnameSuffix == "" {
		return errors.New("hostname suffix is required")
	}
	if c.PublishNamespace == "" {
		return errors.New("publish namespace is required")
	}
	return nil
}
