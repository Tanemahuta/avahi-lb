package controllers

import (
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AvahiReconciler reconciles a supported Kubernetes object into an Avahi
// publisher Deployment.
type AvahiReconciler interface {
	reconcile.Reconciler
	// SetupWithManager configures the reconciler and registers it with mgr.
	SetupWithManager(mgr controllerruntime.Manager, config *AvahiConfig) error
}
