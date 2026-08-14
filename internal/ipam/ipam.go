//go:build linux

// Package ipam implements a built-in /127-aligned IPv6 address allocator for
// feCNI. Each pod veth pair occupies one /127 block: the host takes the lower
// (even) address (see [RFC 4291], Required Anycast Address) and the pod
// takes the upper (odd) address.
//
// Persistence and locking mechanisms are reused from the [host-local IPAM] disk
// store.
//
// [RFC 4291]: https://datatracker.ietf.org/doc/html/rfc4291#section-2.6.1
// [host-local IPAM]: https://pkg.go.dev/github.com/containernetworking/plugins@v1.9.1/plugins/ipam/host-local/backend/disk
package ipam

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/disk"
)

// rangeID is used by the upstream 'host-local' ipam storage to support
// multiple ranges. However, FeCNI does not need the multi-range support
// as it aims to use only host's podCIDR range.
const rangeID = "0"

// Config carries what the allocator needs, derived from the CNI network config.
type Config struct {
	// Subnet is the node podCIDR (an IPv6 network).
	Subnet net.IPNet
	// DataDir is the directory the disk store writes allocation files to. When
	// empty the store's default (/var/lib/cni/networks) is used.
	DataDir string
	// Network namespaces the store on disk (files live under DataDir/Network).
	Network string
}

// Allocation is a /127 IPv6 subnet reserved for one pod veth link.
type Allocation struct {
	// PodIP is the upper/odd address of the /127 block.
	PodIP net.IP
	// HostIP is the lower/even address of the /127 block
	HostIP net.IP
}

// makeAllocation builds an Allocation from a pod address, deriving the host
// address as the lower/even member of the same /127 block.
func makeAllocation(pod net.IP) *Allocation {
	return &Allocation{
		PodIP:  cloneIP(pod),
		HostIP: hostForPod(pod),
	}
}

// Allocator reserves /127-aligned address pairs backed by the host-local disk
// store within the provided subnet.
//
// It's worth noting that the underlying "disk store" is used to record only one
// (upper) address as "reserved" on the disk. The lower address is reserved
// implicitly.
type Allocator struct {
	store  *disk.Store
	subnet net.IPNet
}

// New constructs an Allocator, opening (and creating if necessary) the on-disk
// store for the cfg.Network in the cfg.DataDir.
func New(cfg Config) (*Allocator, error) {
	if !cfg.Subnet.IP.Equal(cfg.Subnet.IP.Mask(cfg.Subnet.Mask)) {
		return nil, fmt.Errorf("subnet %s is not mask-aligned", cfg.Subnet.String())
	}
	store, err := disk.New(cfg.Network, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open IPAM store: %w", err)
	}
	return &Allocator{store: store, subnet: cfg.Subnet}, nil
}

// Get reserves a /127 block for the given container/interface and returns its
// host and pod addresses. It is idempotent, if an allocation already exists for
// (id, ifname) it is returned unchanged, which keeps CNI ADD retries safe.
func (a *Allocator) Get(id, ifname string) (*Allocation, error) {
	if err := a.store.Lock(); err != nil {
		return nil, fmt.Errorf("failed to lock IPAM store: %w", err)
	}
	defer func() { _ = a.store.Unlock() }()

	// Idempotency: return any existing in-subnet reservation for this key.
	for _, ip := range a.store.GetByID(id, ifname) {
		if a.subnet.Contains(ip) {
			return makeAllocation(ip), nil
		}
	}

	start, err := a.store.LastReservedIP(rangeID)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// No marker yet — expected on first use. Scan from the subnet start.
		start = nil
	case err != nil:
		// Other types of errors indicate problem with the store
		return nil, fmt.Errorf("failed to read last-reserved marker: %w", err)
	case start == nil:
		// No error, but no valid 'start' value either, indicates malformed file content (not a valid IP).
		// It's not worth erroring out, but we might as well log it.
		log.Printf("ipam: last-reserved marker for range %q is unparseable; scanning from subnet start", rangeID)
	}

	it := newBlockIter(a.subnet, start)
	for pod := it.Next(); pod != nil; pod = it.Next() {
		reserved, err := a.store.Reserve(id, ifname, pod, rangeID)
		if err != nil {
			return nil, fmt.Errorf("failed to reserve %s: %w", pod, err)
		}
		if reserved {
			return makeAllocation(pod), nil
		}
	}

	return nil, fmt.Errorf("no free /127 blocks in %s", a.subnet.String())
}

// Release frees the block reserved for (id, ifname). It is idempotent, no
// error is returned if nothing is reserved for the key.
func (a *Allocator) Release(id, ifname string) error {
	if err := a.store.Lock(); err != nil {
		return fmt.Errorf("failed to lock IPAM store: %w", err)
	}
	defer func() { _ = a.store.Unlock() }()

	if err := a.store.ReleaseByID(id, ifname); err != nil {
		return fmt.Errorf("failed to release allocation: %w", err)
	}
	return nil
}

// Close releases the store's lock handle.
func (a *Allocator) Close() error {
	if err := a.store.Close(); err != nil {
		return fmt.Errorf("failed to close IPAM store: %w", err)
	}
	return nil
}
