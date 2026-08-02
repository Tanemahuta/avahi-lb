package controllers

import "errors"

var errNilAvahiConfig = errors.New("avahi config must not be nil")

// AvahiConfig contains the shared configuration for Avahi reconcilers.
type AvahiConfig struct {
	HostnameSuffix    string `env:"AVAHI_HOSTNAME_SUFFIX, default=$KUBERNETES_CLUSTER_DOMAIN"`
	AvahiPublishImage string `env:"AVAHI_PUBLISH_IMAGE, required"`
}

// Validate verifies that all required configuration is present.
func (c *AvahiConfig) Validate() error {
	if c.HostnameSuffix == "" {
		return errors.New("hostname suffix is required")
	}
	return nil
}
