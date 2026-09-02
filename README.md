![build](https://github.com/Tanemahuta/avahi-lb/actions/workflows/verify.yml/badge.svg)
[![codecov](https://codecov.io/gh/Tanemahuta/avahi-lb/branch/main/graph/badge.svg?token=FHO3AAZ41O)](https://codecov.io/gh/Tanemahuta/avahi-lb)
[![Go Reference](https://pkg.go.dev/badge/github.com/Tanemahuta/avahi-lb.svg)](https://pkg.go.dev/github.com/Tanemahuta/avahi-lb)
[![GHCR](https://ghcr-badge.egpl.dev/tanemahuta/avahi-lb/tags?trim=major,minor&label=latest&ignore=sha256*,v*)](https://github.com/Tanemahuta/avahi-lb/pkgs/container/avahi-lb/)

# avahi-lb

an operator which publishes hostnames for IPs assigned to Kubernetes
`type: LoadBalancer`
[`Service`](https://kubernetes.io/docs/concepts/services-networking/service/)s
and [`Ingress`](https://kubernetes.io/docs/concepts/services-networking/ingress/)
resources.

## Description

When using [k3s](https://k3s.io/) with [metallb](https://metallb.universe.tf/), each `Service` obtains an IP from the
pool.

You can use this operator to publish DNS names for this IP, either directly
from the `Service` or from the host rules of an `Ingress` that reports the same
load-balancer address.

## Usage

Set `AVAHI_HOSTNAME_SUFFIX` to define the suffix for generated hostnames.
The previous `KUBERNETES_CLUSTER_DOMAIN` variable remains supported as a
fallback for compatibility. `AVAHI_PUBLISH_IMAGE` selects the publisher image;
the Helm chart supplies its default value.

`AVAHI_ALLOWED_TLDS` is a comma-separated allowlist of DNS suffixes that may
be published. It defaults to `local`. Matching is case-insensitive and occurs
on complete DNS label boundaries, so `local` permits both `host.local` and
`host.cluster.local`, but not `host.notlocal`. Hostnames outside the allowlist
are ignored.

When publishing a service, add the annotation `service.beta.kubernetes.io/avahi-publish` and:

- either set it to `"-"` in order to generate
  `<name>.<namespace>.${AVAHI_HOSTNAME_SUFFIX}` ([example](config/samples/service.yaml))
- or use an explicit prefix in order to generate
  `<prefix>.${AVAHI_HOSTNAME_SUFFIX}` ([example](config/samples/service_explicit.yaml))

The same annotation can be added to an `Ingress`. Set it to `"-"` to publish
all non-empty hostnames from `spec.rules`, using the IP reported in
`status.loadBalancer.ingress`. An explicit annotation value publishes
`<value>.${AVAHI_HOSTNAME_SUFFIX}` instead. See the
[Ingress example](config/samples/ingress.yaml).

Ingresses are grouped by IngressClass. The operator creates one publisher
Deployment per class in its own namespace, with one container per IP address.
Each container publishes all unique hostnames grouped under its IP. Ingress
aliases are published with Avahi reverse publication disabled (`-R`), so
multiple hostnames can safely share the same Traefik or other ingress IP. The
class is resolved from `spec.ingressClassName`, the legacy
`kubernetes.io/ingress.class` annotation, or the single default IngressClass.

During startup, before registering the controllers, avahi-lb removes stale
owner-based publisher Deployments left by missing or no-longer-publishable
Services and by the former per-Ingress deployment model. Current class-grouped
Ingress Deployments are ownerless and are rebuilt from the live Ingress state.

Hostnames containing a dot are treated as already qualified. The configured
suffix is appended only to dotless hostnames.

## Helm chart

Helm charts are created from the [charts directory](charts) and published
to [this repository](https://tanemahuta.github.io/avahi-lb).
Use `--set-string kubernetesClusterDomain=<clustername>.local` to configure
`AVAHI_HOSTNAME_SUFFIX`. The existing Helm value name is retained for
compatibility.

## Getting Started

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for
testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever
cluster `kubectl cluster-info` shows).

### Configuration

The controller reads its configuration from the environment:

- `AVAHI_HOSTNAME_SUFFIX` is the preferred suffix appended to generated
  hostnames.
- `AVAHI_ALLOWED_TLDS` is the comma-separated publication allowlist and
  defaults to `local`.
- `AVAHI_PUBLISH_IMAGE` is required and selects the image used by publisher
  Deployments.
- `AVAHI_PUBLISH_NAMESPACE` selects the namespace for aggregated Ingress
  publisher Deployments. The chart sets it to the operator Pod namespace.
- `KUBERNETES_CLUSTER_DOMAIN` is supported as a legacy fallback when
  `AVAHI_HOSTNAME_SUFFIX` is not set.

One of the two suffix variables must resolve to a non-empty value.

The Helm chart configures both required values. Its default publisher image is
`ghcr.io/tanemahuta/avahi-publish:0.9_rc4-r0-alpine3.24`.

### Running on the cluster

1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/avahi-lb:tag
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/avahi-lb:tag
```

### Uninstall CRDs

To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller

UnDeploy the controller from the cluster:

```sh
make undeploy
```

## Contributing

Feel free to PR or create issues.

### How it works

This project aims to follow the
Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the
cluster.

### Test It Out

1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions

If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
