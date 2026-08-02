package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	aggregatedIngressLabel = "avahi-lb.tanemahuta.github.com/aggregated-ingress"
	ingressClassLabel      = "avahi-lb.tanemahuta.github.com/ingress-class"
	networkingAPIVersion   = "networking.k8s.io/v1"
	ingressKind            = "Ingress"
	legacyIngressClass     = "kubernetes.io/ingress.class"
	defaultIngressClass    = "ingressclass.kubernetes.io/is-default-class"
	maxDNSSubdomainLength  = 253
	trueValue              = "true"
)

//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete

// Ingress reconciles annotated Kubernetes Ingresses.
type Ingress struct {
	client client.Client
	config *AvahiConfig
}

// IngressPublicationGroups groups hostnames first by IngressClass and then by
// load-balancer IP address.
type IngressPublicationGroups map[string]map[string][]string

var _ AvahiReconciler = (*Ingress)(nil)

// NewIngress creates an Ingress reconciler.
func NewIngress() AvahiReconciler {
	return &Ingress{}
}

func (i *Ingress) Reconcile(
	ctx context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	publications, collectErr := i.collectPublications(ctx)
	if collectErr != nil {
		return reconcile.Result{}, collectErr
	}
	desired, applyErr := i.applyPublications(ctx, publications)
	if applyErr != nil {
		return reconcile.Result{}, applyErr
	}
	return reconcile.Result{}, i.removeObsoletePublications(ctx, desired)
}

func (i *Ingress) SetupWithManager(
	mgr controllerruntime.Manager,
	config *AvahiConfig,
) error {
	if config == nil {
		return errNilAvahiConfig
	}
	i.client = mgr.GetClient()
	i.config = config
	return controllerruntime.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Owns(&appsv1.Deployment{}).
		Complete(i)
}

func (i *Ingress) collectPublications(ctx context.Context) (IngressPublicationGroups, error) {
	var ingresses networkingv1.IngressList
	if listErr := i.client.List(ctx, &ingresses); listErr != nil {
		return nil, listErr
	}
	defaultClass, classErr := i.defaultIngressClass(ctx)
	if classErr != nil {
		return nil, classErr
	}
	return GroupIngresses(ingresses.Items, defaultClass, i.config.HostnameSuffix), nil
}

// GroupIngresses groups publishable Ingress hostnames by resolved IngressClass
// and load-balancer IP. Hostnames within each IP group are unique and sorted.
func GroupIngresses(
	ingresses []networkingv1.Ingress,
	defaultClass string,
	hostnameSuffix string,
) IngressPublicationGroups {
	groups := make(IngressPublicationGroups)
	for index := range ingresses {
		ingress := &ingresses[index]
		if ingress.DeletionTimestamp != nil {
			continue
		}
		address := ingressAddress(ingress)
		if address == "" {
			continue
		}
		className := ingressClassName(ingress, defaultClass)
		if className == "" {
			continue
		}
		for _, hostname := range expandHostnames(ingress, ingressHostnames(ingress), hostnameSuffix) {
			if groups[className] == nil {
				groups[className] = make(map[string][]string)
			}
			if !slices.Contains(groups[className][address], hostname) {
				groups[className][address] = append(groups[className][address], hostname)
			}
		}
	}
	for _, addresses := range groups {
		for address := range addresses {
			slices.Sort(addresses[address])
		}
	}
	return groups
}

func (i *Ingress) applyPublications(
	ctx context.Context,
	publications IngressPublicationGroups,
) (map[client.ObjectKey]struct{}, error) {
	desired := make(map[client.ObjectKey]struct{}, len(publications))
	for className, addressGroups := range publications {
		key := client.ObjectKey{
			Namespace: i.config.PublishNamespace,
			Name:      ingressDeploymentName(className),
		}
		desired[key] = struct{}{}
		if applyErr := i.applyDeployment(
			ctx,
			key,
			className,
			publicationsByAddress(addressGroups),
		); applyErr != nil {
			return nil, applyErr
		}
	}
	return desired, nil
}

func (i *Ingress) applyDeployment(
	ctx context.Context,
	key client.ObjectKey,
	className string,
	publications []avahiPublication,
) error {
	return upsertDeployment(ctx, i.client, key, func(deployment *appsv1.Deployment) error {
		labels := map[string]string{
			aggregatedIngressLabel: trueValue,
			ingressClassLabel:      shortHash(className),
		}
		deployment.Labels = labels
		applyPublisherDeployment(deployment, i.config, publications, labels, true)
		return nil
	})
}

func (i *Ingress) defaultIngressClass(ctx context.Context) (string, error) {
	var classes networkingv1.IngressClassList
	if listErr := i.client.List(ctx, &classes); listErr != nil {
		return "", listErr
	}
	return resolveDefaultIngressClass(classes.Items), nil
}

func resolveDefaultIngressClass(classes []networkingv1.IngressClass) string {
	var defaultClass string
	for index := range classes {
		class := &classes[index]
		if class.Annotations[defaultIngressClass] != trueValue {
			continue
		}
		if defaultClass != "" {
			return ""
		}
		defaultClass = class.Name
	}
	return defaultClass
}

func (i *Ingress) removeObsoletePublications(
	ctx context.Context,
	desired map[client.ObjectKey]struct{},
) error {
	var deployments appsv1.DeploymentList
	if listErr := i.client.List(ctx, &deployments); listErr != nil {
		return listErr
	}
	for index := range deployments.Items {
		deployment := &deployments.Items[index]
		_, isDesired := desired[client.ObjectKeyFromObject(deployment)]
		remove := isLegacyIngressDeployment(deployment) ||
			isAggregatedIngressDeployment(deployment) && !isDesired
		if remove {
			if deleteErr := deleteObject(ctx, i.client, deployment); deleteErr != nil {
				return deleteErr
			}
		}
	}
	return nil
}

func ingressHostnames(ingress *networkingv1.Ingress) []string {
	annotation, ok := ingress.Annotations[PublishAnnotation]
	if !ok {
		return nil
	}
	if annotation != "-" {
		if annotation == "" {
			return nil
		}
		return []string{annotation}
	}
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

func ingressAddress(ingress *networkingv1.Ingress) string {
	for _, address := range ingress.Status.LoadBalancer.Ingress {
		if address.IP != "" {
			return address.IP
		}
	}
	return ""
}

func ingressClassName(ingress *networkingv1.Ingress, defaultClass string) string {
	if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName != "" {
		return *ingress.Spec.IngressClassName
	}
	if className := ingress.Annotations[legacyIngressClass]; className != "" {
		return className
	}
	return defaultClass
}

func ingressDeploymentName(className string) string {
	name := "avahi-ingress-" + className
	if len(name) <= maxDNSSubdomainLength {
		return name
	}
	suffix := "-" + shortHash(name)
	return strings.TrimRight(name[:maxDNSSubdomainLength-len(suffix)], "-.") + suffix
}

func isAggregatedIngressDeployment(deployment *appsv1.Deployment) bool {
	return deployment.Labels[aggregatedIngressLabel] == trueValue
}

func isLegacyIngressDeployment(deployment *appsv1.Deployment) bool {
	return strings.HasPrefix(deployment.Name, "avahi-ingress-") &&
		controlledBy(deployment, networkingAPIVersion, ingressKind) != nil
}

func publicationsByAddress(addressGroups map[string][]string) []avahiPublication {
	addresses := make([]string, 0, len(addressGroups))
	for address := range addressGroups {
		addresses = append(addresses, address)
	}
	slices.Sort(addresses)
	publications := make([]avahiPublication, 0, len(addresses))
	for _, address := range addresses {
		publications = append(publications, avahiPublication{
			address:   address,
			hostnames: addressGroups[address],
		})
	}
	return publications
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:10]
}
