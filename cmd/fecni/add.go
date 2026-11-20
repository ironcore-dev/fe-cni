// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/ironcore-dev/FeCNI/internal/ipam"
	"github.com/ironcore-dev/FeCNI/internal/veth"
)

// cmdAdd implements the CNI ADD command. It allocates a /127-aligned IPv6
// address pair via the built-in IPAM allocator, creates a point-to-point veth
// pair between the host and the container namespace, and installs a default
// route in the container via the host address. Within each /127 the host takes
// the lower (even) address and the pod takes the upper (odd) address.
func cmdAdd(args *skel.CmdArgs) (err error) {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}

	allocator, err := ipam.New(ipam.Config{
		Subnet:  *conf.ParsedSubnet,
		DataDir: conf.DataDir,
		Network: conf.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize IPAM: %w", err)
	}
	defer func() { _ = allocator.Close() }()

	prefix, err := allocator.Get(args.ContainerID, args.IfName)
	if err != nil {
		return fmt.Errorf("failed to allocate prefix: %w", err)
	}

	defer func() {
		if err != nil {
			_ = allocator.Release(args.ContainerID, args.IfName)
			_ = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
				return ip.DelLinkByName(args.IfName)
			})
		}
	}()

	podIP := prefix.PodIP
	hostIP := prefix.HostIP

	if podIP.To4() != nil {
		return fmt.Errorf("allocated address %s is not IPv6", podIP)
	}

	containerNS, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %w", args.Netns, err)
	}
	defer func() { _ = containerNS.Close() }()

	hostInterface, containerInterface, err := veth.SetupPair(containerNS, args.IfName, conf.MTU, podIP, hostIP)
	if err != nil {
		return fmt.Errorf("failed to setup veth pair: %w", err)
	}

	result := &current.Result{CNIVersion: conf.CNIVersion}
	result.Interfaces = []*current.Interface{hostInterface, containerInterface}
	result.IPs = []*current.IPConfig{
		{
			Interface: current.Int(1),
			Address: net.IPNet{
				IP:   podIP,
				Mask: net.CIDRMask(128, net.IPv6len*8),
			},
			Gateway: hostIP,
		},
	}
	result.Routes = []*types.Route{
		{
			Dst: net.IPNet{
				IP:   net.IPv6zero,
				Mask: net.CIDRMask(0, net.IPv6len*8),
			},
			GW: hostIP,
		},
	}
	result.DNS = conf.DNS

	if err = types.PrintResult(result, conf.CNIVersion); err != nil {
		return fmt.Errorf("failed to print result: %w", err)
	}

	return nil
}
