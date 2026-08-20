//go:build linux

package veth

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

// v6 parses an IPv6 literal into a 16-byte net.IP, failing the test otherwise.
func v6(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil || ip.To16() == nil || ip.To4() != nil {
		t.Fatalf("not an IPv6 address: %q", s)
	}
	return ip
}

// addr builds a netlink.Addr with the given IP and prefix length.
func addr(ip net.IP, prefix int) netlink.Addr {
	return netlink.Addr{IPNet: &net.IPNet{IP: ip, Mask: net.CIDRMask(prefix, ipv6Bits)}}
}

func TestHasAddr(t *testing.T) {
	target := v6(t, "2001:db8:1::1")

	tests := []struct {
		name      string
		addrs     []netlink.Addr
		wantFound bool
	}{
		{
			name:      "present as /128",
			addrs:     []netlink.Addr{addr(target, 128)},
			wantFound: true,
		},
		{
			name:      "present among others",
			addrs:     []netlink.Addr{addr(v6(t, "fe80::1"), 64), addr(target, 128)},
			wantFound: true,
		},
		{
			name:      "absent",
			addrs:     []netlink.Addr{addr(v6(t, "2001:db8:1::2"), 128)},
			wantFound: false,
		},
		{
			name:      "right ip wrong prefix",
			addrs:     []netlink.Addr{addr(target, 127)},
			wantFound: false,
		},
		{
			name:      "nil IPNet is skipped",
			addrs:     []netlink.Addr{{IPNet: nil}, addr(target, 128)},
			wantFound: true,
		},
		{
			name:      "empty list",
			addrs:     nil,
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAddr(tt.addrs, target); got != tt.wantFound {
				t.Errorf("hasAddr = %v, want %v", got, tt.wantFound)
			}
		})
	}
}

func TestHasDefaultRoute(t *testing.T) {
	gw := v6(t, "2001:db8:1::")
	defaultDst := &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, ipv6Bits)}

	tests := []struct {
		name      string
		routes    []netlink.Route
		wantFound bool
	}{
		{
			name:      "default route with nil Dst",
			routes:    []netlink.Route{{Gw: gw}},
			wantFound: true,
		},
		{
			name:      "default route with ::/0 Dst",
			routes:    []netlink.Route{{Dst: defaultDst, Gw: gw}},
			wantFound: true,
		},
		{
			name: "default route among others",
			routes: []netlink.Route{
				{Dst: &net.IPNet{IP: v6(t, "2001:db8:2::"), Mask: net.CIDRMask(64, ipv6Bits)}, Gw: gw},
				{Gw: gw},
			},
			wantFound: true,
		},
		{
			name:      "right gateway but non-default destination",
			routes:    []netlink.Route{{Dst: &net.IPNet{IP: v6(t, "2001:db8:2::"), Mask: net.CIDRMask(64, ipv6Bits)}, Gw: gw}},
			wantFound: false,
		},
		{
			name:      "default destination but wrong gateway",
			routes:    []netlink.Route{{Gw: v6(t, "2001:db8:1::4")}},
			wantFound: false,
		},
		{
			name:      "empty list",
			routes:    nil,
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasDefaultRoute(tt.routes, gw); got != tt.wantFound {
				t.Errorf("hasDefaultRoute = %v, want %v", got, tt.wantFound)
			}
		})
	}
}
