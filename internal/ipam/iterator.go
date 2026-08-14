package ipam

import (
	"net"

	cniip "github.com/containernetworking/plugins/pkg/ip"
)

// blockIter yields the pod address (upper/odd) of each /127-aligned block
// within a subnet, striding by two addresses per step.
//
// Iteration begins at netBase (the block following an optional round-robin
// start point) and proceeds upward. When it runs past the end of the subnet it
// wraps to the subnet's network address exactly once, and stops when it returns
// to the block it started yielding from (stopBase).
type blockIter struct {
	subnet net.IPNet
	// netBase is the even network address of the subnet (first possible block).
	netBase net.IP
	// base is the even base address of the block to yield on the next call, or
	// nil once the iterator is exhausted.
	base net.IP
	// stopBase is the block at which iteration halts after a wrap. When round
	// robin is not used this is netBase, which is only revisited after a full
	// pass, so it doubles as the natural end sentinel.
	stopBase net.IP
	// started guards the stopBase check so the very first yield of stopBase is
	// not mistaken for a completed wrap.
	started bool
}

// newBlockIter returns an iterator over subnet. When startAfter is non-nil the
// first yielded block is the one following startAfter's block (round-robin),
// wrapping to the start of the subnet once and stopping when it revisits the
// block it began with.
func newBlockIter(subnet net.IPNet, startAfter net.IP) *blockIter {
	netBase := subnet.IP.Mask(subnet.Mask)
	it := &blockIter{subnet: subnet, netBase: netBase, base: netBase, stopBase: netBase}

	if startAfter != nil {
		if b := clearLowBit(startAfter); subnet.Contains(b) {
			// Start one block past startAfter's block, and stop when we come
			// back around to that same starting block.
			first := nextPrefix(b)
			if !subnet.Contains(first) {
				first = netBase // startAfter was the last block; wrap immediately.
			}
			it.base = first
			it.stopBase = first
		}
	}
	return it
}

// Next returns the pod address of the next /127-aligned block, or nil when the
// iterator is exhausted.
func (it *blockIter) Next() net.IP {
	if it.base == nil {
		return nil
	}

	// Detect completion: revisiting the starting block after at least one yield.
	if it.started && it.base.Equal(it.stopBase) {
		it.base = nil
		return nil
	}
	it.started = true

	pod := setLowBit(it.base)

	next := nextPrefix(it.base)
	if !it.subnet.Contains(next) {
		next = it.netBase // Wrap around the subnet if the "next" address would be "outside" of it
	}
	it.base = next

	return pod
}

// hostForPod derives address that should be used on the host-side of the veth pair from
// the given pod address.
func hostForPod(pod net.IP) net.IP {
	return clearLowBit(pod)
}

// nextPrefix returns the base address of the next /127 block following the
// one that contains ip.
func nextPrefix(ip net.IP) net.IP {
	base := clearLowBit(ip)
	return cloneIP(cniip.NextIP(cniip.NextIP(base)))
}

// setLowBit sets the least significant bit of the ip address to '1'.
func setLowBit(ip net.IP) net.IP {
	out := cloneIP(ip)
	out[len(out)-1] |= 1
	return out
}

// clearLowBit sets the least significant bit of the ip address to '0'.
func clearLowBit(ip net.IP) net.IP {
	out := cloneIP(ip)
	out[len(out)-1] &^= 1
	return out
}

// cloneIP returns a 16-byte canonical copy of ip.
func cloneIP(ip net.IP) net.IP {
	out := make(net.IP, net.IPv6len)
	copy(out, ip.To16())
	return out
}
