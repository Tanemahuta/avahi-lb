![build](https://github.com/Tanemahuta/avahi-lb/actions/workflows/verify.yml/badge.svg?branch=main)
[![go report](https://goreportcard.com/badge/github.com/Tanemahuta/avahi-lb)](https://goreportcard.com/report/github.com/Tanemahuta/avahi-lb)
[![codecov](https://codecov.io/gh/Tanemahuta/avahi-lb/branch/main/graph/badge.svg?token=FHO3AAZ41O)](https://codecov.io/gh/Tanemahuta/avahi-lb)
[![Go Reference](https://pkg.go.dev/badge/github.com/Tanemahuta/avahi-lb.svg)](https://pkg.go.dev/github.com/Tanemahuta/avahi-lb)
[![GHCR](https://ghcr-badge.egpl.dev/tanemahuta/avahi-lb/tags?trim=major,minor&label=latest&ignore=sha256*,v*)](https://github.com/Tanemahuta/avahi-lb/pkgs/container/avahi-lb/)

# avahi-lb

an operator which publishes a hostname for IPs for `type: LoadBalancer` `Service`s in kubernetes.

## Description

When using [k3s](https://k3s.io/) with [metallb](https://metallb.universe.tf/), each `Service` obtains an IP from the
pool.

You can use this operator in order to propagate a DNS name for this IP.

## Usage

You need to set the environment variable `KUBERNETES_CLUSTER_DOMAIN` in order to define the suffixes for the hosts.

When publishing a service, add the annotation `service.beta.kubernetes.io/avahi-publish` and:

- either set it to `"-"` in order to generate
  `<name>.<namespace>.${KUBERNETES_CLUSTER_DOMAIN}` ([example](config/samples/service.yaml))
- or use an explicit prefix in order to generate
  `<prefix>.${KUBERNETES_CLUSTER_DOMAIN}` ([example](config/samples/service_explicit.yaml))

## Helm chart

Helm charts are created from the [charts directory](charts) and published
to [this repository](https://tanemahuta.github.io/avahi-lb).
Use `--set-string kubernetesClusterDomain=<clustername>.local` to set `KUBERNETES_CLUSTER_DOMAIN`.

## Getting Started

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for
testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever
cluster `kubectl cluster-info` shows).

### Configuration

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

Copyright 2023 christian.heike@icloud.com.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

