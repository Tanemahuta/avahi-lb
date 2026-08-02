package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const maxKubernetesLabelLength = 63

//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete

// Ingress reconciles annotated Kubernetes Ingresses.
type Ingress = genericAvahiController[*networkingv1.Ingress]

type ingressHandler struct{}

// NewIngress creates an Ingress reconciler.
func NewIngress() AvahiReconciler {
	return NewAvahiReconciler[*networkingv1.Ingress](ingressHandler{})
}

func (ingressHandler) Hostnames(ingress *networkingv1.Ingress) []string {
	annotation, ok := ingress.Annotations[PublishAnnotation]
	if !ok {
		return nil
	}

	if annotation == "-" {
		seen := make(map[string]struct{})
		for _, rule := range ingress.Spec.Rules {
			if rule.Host != "" {
				seen[rule.Host] = struct{}{}
			}
		}
		hostnames := make([]string, 0, len(seen))
		for hostname := range seen {
			hostnames = append(hostnames, hostname)
		}
		sort.Strings(hostnames)
		return hostnames
	}
	if annotation != "" {
		return []string{annotation}
	}
	return nil
}

func (ingressHandler) Address(ingress *networkingv1.Ingress) string {
	for _, address := range ingress.Status.LoadBalancer.Ingress {
		if address.IP != "" {
			return address.IP
		}
	}
	return ""
}

func (ingressHandler) DeploymentKey(ingress *networkingv1.Ingress) client.ObjectKey {
	return ingressDeploymentKey(client.ObjectKeyFromObject(ingress))
}

func (ingressHandler) DeploymentKeyByName(ingressKey types.NamespacedName) client.ObjectKey {
	return ingressDeploymentKey(ingressKey)
}

func (ingressHandler) Labels(ingress *networkingv1.Ingress) map[string]string {
	return map[string]string{
		"ingress.kubernetes.io/name":      labelValue(ingress.Name),
		"ingress.kubernetes.io/namespace": ingress.Namespace,
	}
}

func ingressDeploymentKey(ingressKey types.NamespacedName) client.ObjectKey {
	return client.ObjectKey{
		Namespace: ingressKey.Namespace,
		Name:      dnsLabel("avahi-ingress-" + ingressKey.Name),
	}
}

func dnsLabel(value string) string {
	if len(value) <= maxKubernetesLabelLength {
		return value
	}
	suffix := "-" + shortHash(value)
	return strings.TrimRight(value[:maxKubernetesLabelLength-len(suffix)], "-") + suffix
}

func labelValue(value string) string {
	if len(value) <= maxKubernetesLabelLength {
		return value
	}
	suffix := "-" + shortHash(value)
	return strings.TrimRight(value[:maxKubernetesLabelLength-len(suffix)], "-._") + suffix
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:10]
}
