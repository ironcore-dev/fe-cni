# feCNI

An IPv6-only Kubernetes CNI plugin for flat L3 pod networking with BGP route distribution.

## Design

- **IPv6 only** — no IPv4 code paths.
- **No overlays, no tunnels** — end-to-end IPv6 routing.
- **Point-to-point veth with /127** — each pod's veth pair gets a `/127` IPv6 address, ensuring L3 forwarding regardless of whether pods are on the same host or different hosts.
- **External BGP** — a BGP daemon (BIRD, FRR, or GoBGP) announces the node's podCIDR to the fabric. Cross-node traffic is routed directly.
- **Built-in /127 IPAM** — per-node IPv6 subnet allocated in `/127`-aligned pairs by a
  built-in allocator. Within each pair the host takes the lower (even) address and the pod
  the upper (odd) address, guaranteeing no two pods share a `/127`. Reservations persist via
  the standard host-local disk store.
- **Linux networking only** — uses netlink and network namespaces. No eBPF/DPDK.

## Architecture

```
                    ┌──────────────────────────┐
                    │        BGP Fabric        │
                    │  (podCIDRs announced     │
                    │   per-node by BGP daemon)│
                    └──────────┬───────────────┘
                               │
         ┌─────────────────────┼─────────────────────┐
         │                     │                     │
   ┌─────▼─────┐         ┌─────▼─────┐         ┌─────▼─────┐
   │  Node 1   │         │  Node 2   │         │  Node N   │
   │           │         │           │         │           │
   │ podCIDR:  │         │ podCIDR:  │         │ podCIDR:  │
   │ 2001:db8: │         │ 2001:db8: │         │ 2001:db8: │
   │   1::/80  │         │   2::/80  │         │   N::/80  │
   │           │         │           │         │           │
   │           │         │           │         │           │
   │   veth    │         │   veth    │         │   veth    │
   │  (host)   │         │  (host)   │         │  (host)   │
   │    /127   │         │    /127   │         │    /127   │
   │     │     │         │     │     │         │     │     │
   │     │     │         │     │     │         │     │     │
   │ ┌───┴───┐ │         │ ┌───┴───┐ │         │ ┌───┴───┐ │
   │ │  veth │ │         │ │  veth │ │         │ │  veth │ │
   │ │ (pod) │ │         │ │ (pod) │ │         │ │ (pod) │ │
   │ │  /127 │ │         │ │  /127 │ │         │ │  /127 │ │
   │ └───────┘ │         │ └───────┘ │         │ └───────┘ │
   └───────────┘         └───────────┘         └───────────┘
```

## Development, Build, and Tests

Common development workflows are defined in the [`justfile`](./justfile). You
can list all available recipes with:

```bash
just --list
```
