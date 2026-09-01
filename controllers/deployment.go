package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	PublishAnnotation           = "service.beta.kubernetes.io/avahi-publish"
	MountNameDBUS               = "dbus"
	MountPathDBUS               = "/var/run/dbus"
	avahiPublishName            = "avahi-publish"
	maxDNSSubdomainLength       = 253
	groupedIngressPublishScript = `set -eu; pids=""; ` +
		`trap 'kill $pids 2>/dev/null || true' EXIT INT TERM; ` +
		`address="$1"; shift; ` +
		`for hostname do /usr/bin/avahi-publish -a -R "$hostname" "$address" & pids="$pids $!"; done; ` +
		`wait -n`
)

// AvahiAddress maps hostnames to the IP address advertised for them.
type AvahiAddress struct {
	Address   string
	Hostnames []string
}

// AvahiPublication describes one desired Avahi publisher Deployment.
type AvahiPublication struct {
	Kind           string
	Name           string
	Namespace      string
	Owner          client.Object
	Labels         map[string]string
	Addresses      []AvahiAddress
	DisableReverse bool
}

//go:generate go run go.uber.org/mock/mockgen -source=deployment.go -destination=mock_deployment_handler_test.go -package=controllers

// DeploymentHandler reconciles publisher Deployments for Avahi publications.
type DeploymentHandler interface {
	Publish(context.Context, AvahiPublication) (client.ObjectKey, error)
	Delete(context.Context, string, types.NamespacedName) error
}

type kubernetesDeploymentHandler struct {
	client client.Client
	config *AvahiConfig
}

// NewDeploymentHandler creates a Kubernetes-backed DeploymentHandler.
func NewDeploymentHandler(k8sClient client.Client, config *AvahiConfig) (DeploymentHandler, error) {
	if k8sClient == nil {
		return nil, errors.New("deployment handler client must not be nil")
	}
	if config == nil {
		return nil, errNilAvahiConfig
	}
	return &kubernetesDeploymentHandler{client: k8sClient, config: config}, nil
}

func (h *kubernetesDeploymentHandler) Publish(
	ctx context.Context,
	publication AvahiPublication,
) (client.ObjectKey, error) {
	key, keyErr := h.key(publication)
	if keyErr != nil {
		return client.ObjectKey{}, keyErr
	}
	return key, h.upsert(ctx, key, func(deployment *appsv1.Deployment) error {
		if publication.Owner != nil {
			if ownerErr := controllerutil.SetOwnerReference(
				publication.Owner,
				deployment,
				h.client.Scheme(),
			); ownerErr != nil {
				return ownerErr
			}
		} else {
			deployment.Labels = publication.Labels
		}
		h.apply(deployment, publication)
		return nil
	})
}

func (h *kubernetesDeploymentHandler) key(publication AvahiPublication) (client.ObjectKey, error) {
	kind := publication.Kind
	name := publication.Name
	namespace := publication.Namespace
	if publication.Owner != nil {
		gvk, gvkErr := apiutil.GVKForObject(publication.Owner, h.client.Scheme())
		if gvkErr != nil {
			return client.ObjectKey{}, gvkErr
		}
		kind = gvk.Kind
		name = publication.Owner.GetName()
		namespace = publication.Owner.GetNamespace()
	}
	return publicationDeploymentKey(kind, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, publicationOwnerUID(publication.Owner)), nil
}

func publicationOwnerUID(owner client.Object) types.UID {
	if owner == nil {
		return ""
	}
	return owner.GetUID()
}

func publicationDeploymentKey(
	kind string,
	source types.NamespacedName,
	ownerUID types.UID,
) client.ObjectKey {
	name := "avahi-" + strings.ToLower(kind) + "-" + source.Name
	if len(name) > maxDNSSubdomainLength {
		if ownerUID != "" {
			name = "avahi-" + strings.ToLower(kind) + "-" + strings.ToLower(string(ownerUID))
		}
		if len(name) > maxDNSSubdomainLength {
			suffix := "-" + shortHash(name)
			name = strings.TrimRight(name[:maxDNSSubdomainLength-len(suffix)], "-.") + suffix
		}
	}
	return client.ObjectKey{Namespace: source.Namespace, Name: name}
}

func (h *kubernetesDeploymentHandler) upsert(
	ctx context.Context,
	key client.ObjectKey,
	apply func(*appsv1.Deployment) error,
) error {
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
	}
	getErr := h.client.Get(ctx, key, &deployment)
	create := k8serrors.IsNotFound(getErr)
	if getErr != nil && !create {
		return getErr
	}
	var patchSource *appsv1.Deployment
	if !create {
		patchSource = deployment.DeepCopy()
	}
	if applyErr := apply(&deployment); applyErr != nil {
		return applyErr
	}
	if create {
		return h.client.Create(ctx, &deployment)
	}
	return h.client.Patch(ctx, &deployment, client.MergeFrom(patchSource))
}

func (h *kubernetesDeploymentHandler) apply(
	deployment *appsv1.Deployment,
	publication AvahiPublication,
) {
	containers := make([]corev1.Container, 0, len(publication.Addresses))
	for index, address := range publication.Addresses {
		name := avahiPublishName
		if len(publication.Addresses) > 1 {
			name = fmt.Sprintf("%s-%d", avahiPublishName, index)
		}
		var (
			command []string
			args    []string
		)
		if publication.DisableReverse {
			command = []string{"/bin/sh", "-c"}
			args = append(
				[]string{groupedIngressPublishScript, avahiPublishName, address.Address},
				address.Hostnames...,
			)
		} else {
			args = []string{address.Hostnames[0], address.Address}
		}
		containers = append(containers, corev1.Container{
			Name:            name,
			Image:           h.config.AvahiPublishImage,
			Command:         command,
			Args:            args,
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: &corev1.SecurityContext{Privileged: ptr(true)},
			VolumeMounts: []corev1.VolumeMount{{
				Name: MountNameDBUS, ReadOnly: true, MountPath: MountPathDBUS,
			}},
		})
	}
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas: ptr(int32(1)),
		Selector: &metav1.LabelSelector{MatchLabels: publication.Labels},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: publication.Labels},
			Spec: corev1.PodSpec{
				Containers: containers,
				Volumes: []corev1.Volume{{
					Name: MountNameDBUS,
					VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
						Path: MountPathDBUS,
					}},
				}},
			},
		},
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
	}
}

func (h *kubernetesDeploymentHandler) Delete(
	ctx context.Context,
	kind string,
	source types.NamespacedName,
) error {
	key := publicationDeploymentKey(kind, source, "")
	deployment := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Namespace: key.Namespace,
		Name:      key.Name,
	}}
	return deleteObject(ctx, h.client, &deployment)
}

func deleteObject(ctx context.Context, writer client.Writer, object client.Object) error {
	deleteErr := writer.Delete(ctx, object)
	if k8serrors.IsNotFound(deleteErr) {
		return nil
	}
	return deleteErr
}

func ptr[T any](value T) *T {
	return &value
}
