package controllers

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AvahiHandler extracts the publication data and Deployment metadata for a
// supported Kubernetes object.
type AvahiHandler[T client.Object] interface {
	// Hostnames returns the hostnames that should resolve to the object's address.
	// A hostname of "-" is expanded to <name>.<namespace>.<suffix>.
	Hostnames(T) []string
	// Address returns the IP address published for the object.
	Address(T) string
	// DeploymentKey returns the key of the Deployment owned by the object.
	DeploymentKey(T) client.ObjectKey
	// DeploymentKeyByName returns the Deployment key for a reconciliation request.
	DeploymentKeyByName(types.NamespacedName) client.ObjectKey
	// Labels returns the labels applied to the managed Deployment and its Pods.
	Labels(T) map[string]string
}
