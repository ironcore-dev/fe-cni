//go:build linux

package main

import (
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/ironcore-dev/feCNI/internal/ipam"
	"github.com/ironcore-dev/feCNI/internal/veth"
)

// cmdCheck implements the CNI CHECK command. It verifies that the resources
// feCNI created for the container are present and in the expected state. It ensures
// that the interface:
//   - is UP
//   - has expected address
//   - has expected default route (on the pod-side)
func cmdCheck(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}

	subnet, err := conf.parseSubnet()
	if err != nil {
		return err
	}

	if args.Netns == "" {
		return fmt.Errorf("CNI_NETNS is empty; cannot verify container interface %q", args.IfName)
	}

	allocator, err := ipam.New(ipam.Config{
		Subnet:  *subnet,
		DataDir: conf.DataDir,
		Network: conf.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize IPAM: %w", err)
	}
	defer func() { _ = allocator.Close() }()

	alloc, err := allocator.Lookup(args.ContainerID, args.IfName)
	if err != nil {
		return fmt.Errorf("failed to look up IPAM allocation: %w", err)
	}
	if alloc == nil {
		return fmt.Errorf("no IPAM allocation found for container %q ifname %q", args.ContainerID, args.IfName)
	}

	// Compare configuration supplied by runtime with the expected allocations
	prevResult, err := parsePrevResult(conf)
	if err != nil {
		return fmt.Errorf("failed to parse prevResult: %w", err)
	}

	if err := validatePrevResult(prevResult, alloc, args.IfName, args.Netns); err != nil {
		return err
	}

	// Compare state of the pod-side interface with the expected config
	err = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		return veth.CheckLink(args.IfName, alloc.PodIP, alloc.HostIP, true)
	})
	if err != nil {
		return fmt.Errorf("container interface check failed: %w", err)
	}

	// Compare state of the host-side interface with the expected config
	hostVeth, err := hostVethName(prevResult, args.IfName, args.Netns)
	if err != nil {
		return fmt.Errorf("failed to find host-side end of '%s' veth pair: %w", args.IfName, err)
	}

	if err := veth.CheckLink(hostVeth, alloc.HostIP, alloc.PodIP, false); err != nil {
		return fmt.Errorf("host interface check failed: %w", err)
	}
	return nil
}

// parsePrevResult parses and version-converts conf's prevResult into the
// current Result type.
//
// The CNI spec requires the runtime to supply a prevResult on CHECK, so a missing
// one is treated as an error.
func parsePrevResult(conf *NetConf) (*current.Result, error) {
	if conf.RawPrevResult == nil {
		return nil, fmt.Errorf("prevResult is required on CHECK but was not provided")
	}

	if err := version.ParsePrevResult(&conf.NetConf); err != nil {
		return nil, fmt.Errorf("failed to parse prevResult: %w", err)
	}

	result, err := current.NewResultFromResult(conf.PrevResult)
	if err != nil {
		return nil, fmt.Errorf("failed to convert prevResult: %w", err)
	}
	return result, nil
}

// validatePrevResult cross-checks the prevResult's container-interface address
// and gateway against the IPAM allocation, returning an error on any mismatch.
func validatePrevResult(result *current.Result, alloc *ipam.Allocation, ifName, netns string) error {
	containerIdx, ok := containerInterfaceIndex(result, ifName, netns)
	if !ok {
		return fmt.Errorf("prevResult has no interface %q in sandbox %q", ifName, netns)
	}

	prevPodIP, prevGW := podAddrFromResult(result, containerIdx)
	if prevPodIP == nil {
		return fmt.Errorf("prevResult has no IPv6 address for interface %q", ifName)
	}
	if !prevPodIP.Equal(alloc.PodIP) {
		return fmt.Errorf("prevResult pod address %s does not match IPAM allocation %s", prevPodIP, alloc.PodIP)
	}
	if prevGW != nil && !prevGW.Equal(alloc.HostIP) {
		return fmt.Errorf("prevResult gateway %s does not match IPAM host address %s", prevGW, alloc.HostIP)
	}
	return nil
}

// hostVethName returns the name of the host-side interface recorded in the
// prevResult — the one that is neither the container interface nor in a
// sandbox. It returns an error when the container interface or its host-side
// peer cannot be found (both are expected, since the prevResult is feCNI's own
// ADD output).
func hostVethName(result *current.Result, ifName, netns string) (string, error) {
	containerIdx, ok := containerInterfaceIndex(result, ifName, netns)
	if !ok {
		return "", fmt.Errorf("prevResult has no interface %q in sandbox %q", ifName, netns)
	}
	for i, intf := range result.Interfaces {
		if i != containerIdx && intf.Sandbox == "" {
			return intf.Name, nil
		}
	}
	return "", fmt.Errorf("failed to find host-side interface of the %q interface", ifName)
}

// containerInterfaceIndex returns the index of the container interface (matched
// by name and sandbox) in the prevResult, and whether it was found.
func containerInterfaceIndex(result *current.Result, ifName, netns string) (int, bool) {
	for i, intf := range result.Interfaces {
		if intf.Name == ifName && intf.Sandbox == netns {
			return i, true
		}
	}
	return 0, false
}

// podAddrFromResult returns the IPv6 pod address (and its gateway, if any) from
// the IP config bound to the interface at containerIdx, preferring the ::/0
// route gateway when the IP config omits one.
func podAddrFromResult(result *current.Result, containerIdx int) (podIP, gw net.IP) {
	for _, ipc := range result.IPs {
		if ipc.Interface != nil && *ipc.Interface == containerIdx && ipc.Address.IP.To4() == nil {
			podIP = ipc.Address.IP
			gw = ipc.Gateway
			break
		}
	}

	if gw == nil {
		for _, route := range result.Routes {
			ones, _ := route.Dst.Mask.Size()
			if ones == 0 && route.Dst.IP.Equal(net.IPv6zero) {
				gw = route.GW
				break
			}
		}
	}

	return podIP, gw
}
