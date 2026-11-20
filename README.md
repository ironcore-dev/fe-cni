# FeCNI
[![REUSE status](https://api.reuse.software/badge/github.com/ironcore-dev/fe-cni)](https://api.reuse.software/info/github.com/ironcore-dev/fe-cni)
[![GitHub License](https://img.shields.io/static/v1?label=License&message=Apache-2.0&color=blue)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://makeapullrequest.com)

An IPv6-only Kubernetes CNI plugin for flat L3 pod networking. This CNI forgoes
overlay networks and tunnels in favor of globally routable IPv6 addresses
assigned to pods. The goal of this CNI is to provide a simple solution with
minimal configuration for IPv6 environments.

## Addressing

FeCNI focuses on end-to-end routable connectivity without overlay networks or
tunnels between nodes. It is meant to configure globally routable IPv6
addresses for pods.

> [!IMPORTANT]
> The source of the IP address allocations for FeCNI is the node's [podCIDR][pod-cidr]
> specification field. It is assumed that the value in this field is a valid
> and globally routable IPv6 subnet. It is also assumed that the rest of the
> network fabric knows how to route individual `PodCIDR` subnets to the correct
> nodes.

```
                        ┌───────────────────────────┐
                        │        BGP Fabric         │
                        │  handle's podCIDR prefix  │
                        │        distribution       │
                        └─────────────┬─────────────┘
                                      │
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
   ┌──────────▼──────────┐ ┌──────────▼──────────┐ ┌──────────▼──────────┐
   │        Node 1       │ │        Node 1       │ │        Node 1       │
   │                     │ │                     │ │                     │
   │     podCIDR:        │ │     podCIDR:        │ │     podCIDR:        │
   │  2001:db8:1::/80    │ │  2001:db8:2::/80    │ │  2001:db8:3::/80    │
   │                     │ │                     │ │                     │
   │                     │ │                     │ │                     │
   │                     │ │                     │ │                     │
   │        veth         │ │        veth         │ │        veth         │
   │       (host)        │ │       (host)        │ │       (host)        │
   │  <pod_cidr>::2/127  │ │  <pod_cidr>::2/127  │ │  <pod_cidr>::2/127  │
   │          │          │ │          │          │ │          │          │
   │          │          │ │          │          │ │          │          │
   │ ┌────────┴────────┐ │ │ ┌────────┴────────┐ │ │ ┌────────┴────────┐ │
   │ │      veth       │ │ │ │      veth       │ │ │ │      veth       │ │
   │ │     (pod)       │ │ │ │     (pod)       │ │ │ │     (pod)       │ │
   │ │<pod_cidr>::1/127│ │ │ │<pod_cidr>::1/127│ │ │ │<pod_cidr>::1/127│ │
   │ └─────────────────┘ │ │ └─────────────────┘ │ │ └─────────────────┘ │
   └─────────────────────┘ └─────────────────────┘ └─────────────────────┘
```

### Address Allocations

The plugin comes with a built-in `IPAM` functionality. It reads the node's
[`podCIDR`][pod-cidr] value, and then allocates a `/127` prefix for each pod
from the subnet. This effectively reserves two IPv6 addresses per pod. The
lower (even) address is assigned to the veth interface on the host and the
upper (odd) address is assigned in the pod.

### Point-to-Point Links

The interface on each end of the veth pair configures the IP address of the other
end as its "peer". This turns the interface into a point-to-point link,
eliminating the need for L2 neighbor discovery.

## Usage

Prerequisite for deploying and using FeCNI is that you have at least one
Kubernetes node. If you need help with setting up the nodes, you can follow
the official [kubeadm guide][kubeadm] until the step to deploy CNI. This guide
assumes a 3-node deployment, similar to the one in the
[Addressing section](#addressing).

Start by building the FeCNI image.

```bash
task image
```

Take the resulting image tar ball from the `build/` directory and copy it to
each node, we'll need to side-load it to local image registry.

```bash
# Run this import command on each k8s node
ctr -n k8s.io image import /path/to/fecni.tar.gz
```

Before we deploy FeCNI, it's a good idea to make sure that each node has
correct `podCIDR` configured.

```bash
# On any of the k8s nodes
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.podCIDR}{"\n"}{end}'
```

For our example setup, the output looks like this:

```
node-1	2001:db8:1::/80
node-2	2001:db8:2::/80
node-3	2001:db8:3::/80
```

Now take the file `manifests/fecni.yaml` from this repository and copy it to
any of the k8s nodes. We are ready to deploy FeCNI.

```bash
# On the node with FeCNI manifest
kubectl apply -f /path/to/manifests/fecni.yaml
```

Shortly after that, running `kubectl get pods -A` should show `fecni` pod
running on each node in the cluster.

## For Developers

FeCNI uses `task` as its developer interface. See [Task's homepage][task]
for more info about its installation and usage. Common development workflows are
defined in the [`Taskfile`](./Taskfile.yml). You can list all available recipes
with:

```bash
task --list
```

### Building

All build recipes target `linux` OS, because the CNI is not meant to be run on
any other OS. However, you can control for which architecture it's built by
exporting `GOARCH` variable (see [supported GOARCH values][goarch]).

To build just the FeCNI binaries, run:

```bash
task build
```

This produces two binaries in the `build/` folder:

  * `fecni` - the CNI binary itself
  * `fecni-init` - the binary that configures the node before FeCNI can run

To build the complete `OCI` image that can be imported into a registry, run:

```bash
task image
```

This produces a tarball image in the `build/` folder. The image contains both
of the binaries mentioned above.

> [!NOTE]
> To build binaries or images for different architecture than the current
> host, you can pass `GOARCH=<arch>` variable to the `build` and `image`
> tasks. For example:
>
> ```bash
> task image GOARCH=arm64
> ```

### Testing

The project contains both unit test and end-to-end tests.:

#### Unit Tests

These are fast tests that rely only on `Go`. You can run them with:

```bash
task test:unit
```

#### End-to-End Tests

These tests are more complex and rely on [docker][docker] and [Kind][kind] to
spin up a Kubernetes cluster for testing. They also require passwordless access
to the `root` account and as such it's not recommended to run them on your
primary machine. It's best to run them in a VM environment.

```bash
task test:e2e
```

<!-- Link references -->

[docker]: https://www.docker.com
[goarch]: https://go.dev/doc/install/source#environment
[kind]: https://kind.sigs.k8s.io
[kubeadm]: https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/create-cluster-kubeadm/
[pod-cidr]: https://kubernetes.io/docs/reference/kubernetes-api/core/node-v1/#NodeSpec
[task]: https://taskfile.dev/docs/

## Licensing

Copyright 2025 SAP SE or an SAP affiliate company and IronCore contributors. Please see our [LICENSE](LICENSE) for
copyright and license information. Detailed information including third-party components and their licensing/copyright
information is available [via the REUSE tool](https://api.reuse.software/info/github.com/ironcore-dev/fe-cni).

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
