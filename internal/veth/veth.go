//go:build linux

// veth package contains functionality related to creation and configuration
// of Virtual Ethernet pairs
package veth

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/netlinksafe"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
)

const (
	// ipv6Bits is a constant number of bits in an IPv6 address.
	ipv6Bits = net.IPv6len * 8

	// ipv6PointToPointPrefix is the prefix length used for each end of the
	// point-to-point veth link.
	ipv6PointToPointPrefix = ipv6Bits - 1
)

var (
	// vethOptions is a common set of IPv6 sysctl options to be applied on both ends of container's veth pair
	vethOptions = map[string]string{
		// Enable IPv6 on the interface.
		"disable_ipv6": "0",
		// Do not accept router advertisements - the point-to-point link between host and container is statically configured.
		"accept_ra": "0",
		// Disable DAD - addresses are preassigned from a controlled IPAM system, there's no risk of duplicate addresses
		"accept_dad": "0",
		// Keep addresses on the interface in case it flaps DOWN/UP.
		"keep_addr_on_down": "1",
	}

	// loOptions is a set of IPv6 sysctl options to be applied on container's loopback interface
	loOptions = map[string]string{
		// Enable IPv6 on the interface. The IPv6 stack does not work if the address family is not enabled on the loopback
		"disable_ipv6": "0",
	}
)

// SetupPair creates a veth pair and configures both ends as an IPv6
// /127 point-to-point link with peer addresses. The pod end is
// created inside podNS and the host end is moved into the host
// namespace. The pod side also gets a default route via the host peer.
func SetupPair(
	podNS ns.NetNS,
	ifName string,
	mtu int,
	podIP, hostIP net.IP,
) (*current.Interface, *current.Interface, error) {
	hostInterface := &current.Interface{}
	podInterface := &current.Interface{}

	err := podNS.Do(func(hostNS ns.NetNS) error {
		hostVeth, podVeth, err := ip.SetupVeth(ifName, mtu, "", hostNS)
		if err != nil {
			return fmt.Errorf("failed to set up veth pair: %w", err)
		}

		hostInterface.Name = hostVeth.Name
		hostInterface.Mac = hostVeth.HardwareAddr.String()
		podInterface.Name = podVeth.Name
		podInterface.Mac = podVeth.HardwareAddr.String()
		podInterface.Sandbox = podNS.Path()

		// Any error from this point should attempt to teardown the partially
		// created link
		if err := setIPv6Sysctl("lo", loOptions); err != nil {
			return teardownOnErr(ifName, err)
		}

		if err := configurePodVeth(ifName, podIP, hostIP); err != nil {
			return teardownOnErr(ifName, err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to configure container veth: %w", err)
	}

	if err := configureHostVeth(hostInterface.Name, hostIP, podIP); err != nil {
		// If configuration of host-side veth fails, remove the veth from inside the
		// container.
		teardownErr := podNS.Do(func(_ ns.NetNS) error {
			return teardownOnErr(ifName, err)
		})
		return nil, nil, fmt.Errorf("failed to configure host veth: %w", teardownErr)
	}

	return hostInterface, podInterface, nil
}

// teardownOnErr deletes the pod-side veth (reaping its host peer) after a
// setup failure, wrapping the original cause.
func teardownOnErr(ifName string, cause error) error {
	if delErr := ip.DelLinkByName(ifName); delErr != nil && !errors.Is(delErr, ip.ErrLinkNotFound) {
		return fmt.Errorf("%w (cleanup also failed: %v)", cause, delErr)
	}
	return cause
}

// CheckLink verifies, from within the current network namespace, that ifName
// exists as a veth, is administratively up, and carries expectedIP as a /128
// address. When expectDefaultRoute is true it also requires an IPv6 default
// route (::/0) via peerIP. It returns a descriptive error on the first
// discrepancy and nil when everything matches.
//
// The verifier only asserts the presence of the expected address/route and
// tolerates additional ones, so a later chained plugin that adds resources does
// not cause a spurious failure. It performs read-only netlink queries and never
// mutates state, so it is safe to call repeatedly (e.g. from CNI CHECK).
//
// Note the peer-address form used by configurePodVeth/configureHostVeth: the
// local address is added without a mask (a /128), so the expected on-link
// address is expectedIP/128. The Addr.Peer field is intentionally not compared
// as netlink does not round-trip it reliably.
func CheckLink(ifName string, expectedIP, peerIP net.IP, expectDefaultRoute bool) error {
	link, err := netlinksafe.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", ifName, err)
	}

	if _, ok := link.(*netlink.Veth); !ok {
		return fmt.Errorf("interface %q is not a veth (type %q)", ifName, link.Type())
	}

	if link.Attrs().Flags&net.FlagUp == 0 {
		return fmt.Errorf("interface %q is not up", ifName)
	}

	addrs, err := netlinksafe.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return fmt.Errorf("failed to list addresses on %q: %w", ifName, err)
	}
	if !hasAddr(addrs, expectedIP) {
		return fmt.Errorf("interface %q is missing expected address %s/128", ifName, expectedIP)
	}

	if !expectDefaultRoute {
		return nil
	}

	routes, err := netlinksafe.RouteList(link, netlink.FAMILY_V6)
	if err != nil {
		return fmt.Errorf("failed to list routes on %q: %w", ifName, err)
	}
	if !hasDefaultRoute(routes, peerIP) {
		return fmt.Errorf("interface %q is missing default route via %s", ifName, peerIP)
	}

	return nil
}

// hasAddr reports whether addrs contains want as a /128 host address.
func hasAddr(addrs []netlink.Addr, want net.IP) bool {
	for _, addr := range addrs {
		if addr.IPNet == nil || !addr.IP.Equal(want) {
			continue
		}
		ones, bits := addr.Mask.Size()
		if ones == ipv6Bits && bits == ipv6Bits {
			return true
		}
	}
	return false
}

// hasDefaultRoute reports whether routes contains an IPv6 default route
// (Dst nil or ::/0) whose gateway is gw.
func hasDefaultRoute(routes []netlink.Route, gw net.IP) bool {
	for _, route := range routes {
		if !route.Gw.Equal(gw) {
			continue
		}
		if route.Dst == nil {
			return true
		}
		if ones, _ := route.Dst.Mask.Size(); ones == 0 && route.Dst.IP.Equal(net.IPv6zero) {
			return true
		}
	}
	return false
}

// configurePodVeth configures the pod-side veth with the pod
// address (/127, peer = host address), brings it up, and installs a default
// route via the host peer.
func configurePodVeth(ifName string, podIP, hostIP net.IP) error {
	link, err := netlinksafe.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to look up %q: %w", ifName, err)
	}

	if err := setIPv6Sysctl(ifName, vethOptions); err != nil {
		return err
	}

	// Note that per ip-address[0] documentation, if peer address is set,
	// it should be the peer that defines prefix length.
	//
	// [0] https://man7.org/linux/man-pages/man8/ip-address.8.html
	addr := &netlink.Addr{
		IPNet: &net.IPNet{IP: podIP},
		Peer: &net.IPNet{
			IP:   hostIP,
			Mask: net.CIDRMask(ipv6PointToPointPrefix, ipv6Bits),
		},
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to add address %s to %q: %w", podIP, ifName, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set %q up: %w", ifName, err)
	}

	if err := ip.AddDefaultRoute(hostIP, link); err != nil {
		return fmt.Errorf("failed to add default route via %s: %w", hostIP, err)
	}

	return nil
}

// configureHostVeth configures the host-side veth with the host address
// (/127, peer = pod address) and brings it up. The kernel automatically
// installs a connected route to the pod IP while the link is up.
func configureHostVeth(vethName string, hostIP, podIP net.IP) error {
	link, err := netlinksafe.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to look up %q: %w", vethName, err)
	}

	if err := setIPv6Sysctl(vethName, vethOptions); err != nil {
		return err
	}

	// Note that per ip-address[0] documentation, if peer address is set,
	// it should be the peer that defines prefix length.
	//
	// [0] https://man7.org/linux/man-pages/man8/ip-address.8.html
	addr := &netlink.Addr{
		IPNet: &net.IPNet{IP: hostIP},
		Peer: &net.IPNet{
			IP:   podIP,
			Mask: net.CIDRMask(ipv6PointToPointPrefix, ipv6Bits),
		},
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to add address %s to %q: %w", hostIP, vethName, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set %q up: %w", vethName, err)
	}

	return nil
}

// setIPv6Sysctl sets sysctl's net/ipv6/conf/<iface>/<key> to the given <value> for each
// key/value in 'options' argument.
func setIPv6Sysctl(iface string, options map[string]string) error {
	for key, value := range options {
		name := "net/ipv6/conf/" + iface + "/" + key
		if _, err := sysctl.Sysctl(name, value); err != nil {
			return fmt.Errorf("failed to set %q to %q: %w", name, value, err)
		}
	}
	return nil
}
