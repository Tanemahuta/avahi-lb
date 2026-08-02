package controllers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	PublishAnnotation           = "service.beta.kubernetes.io/avahi-publish"
	MountNameDBUS               = "dbus"
	MountPathDBUS               = "/var/run/dbus"
	avahiPublishName            = "avahi-publish"
	groupedIngressPublishScript = `set -eu; pids=""; ` +
		`trap 'kill $pids 2>/dev/null || true' EXIT INT TERM; ` +
		`address="$1"; shift; ` +
		`for hostname do /usr/bin/avahi-publish -a -R "$hostname" "$address" & pids="$pids $!"; done; ` +
		`wait -n`
)

type avahiPublication struct {
	address   string
	hostnames []string
}

func expandHostnames(object client.Object, hostnames []string, suffix string) []string {
	expanded := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		if hostname == "-" {
			hostname = object.GetName() + "." + object.GetNamespace() + "." + suffix
		} else if !strings.Contains(hostname, ".") {
			hostname += "." + suffix
		}
		if !slices.Contains(expanded, hostname) {
			expanded = append(expanded, hostname)
		}
	}
	return expanded
}

func upsertDeployment(
	ctx context.Context,
	k8sClient client.Client,
	key client.ObjectKey,
	apply func(*appsv1.Deployment) error,
) error {
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
	}
	getErr := k8sClient.Get(ctx, key, &deployment)
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
		return k8sClient.Create(ctx, &deployment)
	}
	return k8sClient.Patch(ctx, &deployment, client.MergeFrom(patchSource))
}

func applyPublisherDeployment(
	deployment *appsv1.Deployment,
	config *AvahiConfig,
	publications []avahiPublication,
	labels map[string]string,
	disableReverse bool,
) {
	containers := make([]corev1.Container, 0, len(publications))
	for index, publication := range publications {
		name := avahiPublishName
		if len(publications) > 1 {
			name = fmt.Sprintf("%s-%d", avahiPublishName, index)
		}
		var (
			command []string
			args    []string
		)
		if disableReverse {
			command = []string{"/bin/sh", "-c"}
			args = append(
				[]string{groupedIngressPublishScript, avahiPublishName, publication.address},
				publication.hostnames...,
			)
		} else {
			args = []string{publication.hostnames[0], publication.address}
		}
		containers = append(containers, corev1.Container{
			Name:            name,
			Image:           config.AvahiPublishImage,
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
		Selector: &metav1.LabelSelector{MatchLabels: labels},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
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

func deleteDeployment(ctx context.Context, k8sClient client.Client, key client.ObjectKey) error {
	deployment := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Namespace: key.Namespace,
		Name:      key.Name,
	}}
	return deleteObject(ctx, k8sClient, &deployment)
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
