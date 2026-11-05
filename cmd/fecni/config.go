//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/types"
)

const defaultMTU = 1500

// NetConf is the FeCNI plugin configuration parsed from the CNI network config.
type NetConf struct {
	types.NetConf
	// MTU is the MTU for the veth interfaces. Defaults to 1500 when unset.
	MTU int `json:"mtu"`
	// Subnet is the node's podCIDR, an IPv6 network. Addresses are allocated
	// from it in /127-aligned pairs by the built-in IPAM allocator.
	Subnet string `json:"subnet"`
	// ParsedSubnet is the parsed and validated form of Subnet, populated by
	// loadNetConf. Guaranteed non-nil for configs returned without error.
	ParsedSubnet *net.IPNet `json:"-"`
	// DataDir is where the IPAM allocator persists reservations. Optional;
	// when empty the store's default (/var/lib/cni/networks) is used.
	DataDir string `json:"dataDir"`
}

// loadNetConf parses the CNI network configuration JSON bytes into a NetConf
// and validates it.
func loadNetConf(bytes []byte) (*NetConf, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, fmt.Errorf("failed to load netconf: %w", err)
	}

	if conf.MTU == 0 {
		conf.MTU = defaultMTU
	}

	if conf.IPAM.Type != "" {
		return nil, fmt.Errorf("FeCNI has a built-in IPAM; remove the \"ipam\" stanza from the network config")
	}

	subnet, err := parseSubnet(conf.Subnet)
	if err != nil {
		return nil, err
	}
	conf.ParsedSubnet = subnet

	return conf, nil
}

// parseSubnet parses and validates subnet as an IPv6 network. FeCNI is
// IPv6-only, so an IPv4 subnet is rejected.
func parseSubnet(subnet string) (*net.IPNet, error) {
	if subnet == "" {
		return nil, fmt.Errorf("subnet is required")
	}

	ip, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet %q: %w", subnet, err)
	}
	if ip.To4() != nil {
		return nil, fmt.Errorf("FeCNI is IPv6-only but subnet %q is IPv4", subnet)
	}
	if !ip.Equal(ipNet.IP) {
		return nil, fmt.Errorf("subnet %q has host bits set; did you mean %s?", subnet, ipNet)
	}

	return ipNet, nil
}
