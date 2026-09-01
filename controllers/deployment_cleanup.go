package controllers

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	serviceAPIVersion = "v1"
	serviceKind       = "Service"
)

// DeploymentCleanup removes stale owner-based publisher Deployments before
// the controllers start. The reader should bypass the manager cache because
// cleanup runs before that cache is started.
func DeploymentCleanup(
	ctx context.Context,
	reader client.Reader,
	writer client.Writer,
	config *AvahiConfig,
) error {
	if config == nil {
		return errNilAvahiConfig
	}
	var deployments appsv1.DeploymentList
	if listErr := reader.List(ctx, &deployments); listErr != nil {
		return listErr
	}
	for index := range deployments.Items {
		deployment := &deployments.Items[index]
		remove, validationErr := shouldRemoveOwnedDeployment(ctx, reader, deployment, config)
		if validationErr != nil {
			return validationErr
		}
		if remove {
			if deleteErr := deleteObject(ctx, writer, deployment); deleteErr != nil {
				return deleteErr
			}
		}
	}
	return nil
}

func shouldRemoveOwnedDeployment(
	ctx context.Context,
	reader client.Reader,
	deployment *appsv1.Deployment,
	config *AvahiConfig,
) (bool, error) {
	if controlledBy(deployment, networkingAPIVersion, ingressKind) != nil {
		return true, nil
	}
	owner := controlledBy(deployment, serviceAPIVersion, serviceKind)
	if owner == nil {
		return false, nil
	}
	return invalidServiceDeployment(ctx, reader, deployment, owner, config)
}

func invalidServiceDeployment(
	ctx context.Context,
	reader client.Reader,
	deployment *appsv1.Deployment,
	owner *metav1.OwnerReference,
	config *AvahiConfig,
) (bool, error) {
	var service corev1.Service
	serviceKey := client.ObjectKey{Namespace: deployment.Namespace, Name: owner.Name}
	if getErr := reader.Get(ctx, serviceKey, &service); getErr != nil {
		if k8serrors.IsNotFound(getErr) {
			return true, nil
		}
		return false, getErr
	}
	if service.UID != owner.UID ||
		client.ObjectKeyFromObject(deployment) != publicationDeploymentKey(serviceKind, serviceKey, service.UID) ||
		service.DeletionTimestamp != nil {
		return true, nil
	}
	hostnames := config.PublishableHostnames(serviceKey, serviceHostnames(&service))
	return len(hostnames) == 0 || serviceAddress(&service) == "", nil
}

func controlledBy(
	deployment *appsv1.Deployment,
	apiVersion string,
	kind string,
) *metav1.OwnerReference {
	for index := range deployment.OwnerReferences {
		owner := &deployment.OwnerReferences[index]
		if owner.Controller != nil && *owner.Controller &&
			owner.APIVersion == apiVersion &&
			owner.Kind == kind {
			return owner
		}
	}
	return nil
}
