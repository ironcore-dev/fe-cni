// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/ironcore-dev/FeCNI/internal/ipam"
)

// cmdDel implements the CNI DEL command. It removes the veth from the
// container namespace (which also tears down the host-side peer) and
// attempts to releases the IPAM allocation.
//
// Note that the IPAM release is best-effort, and if it fails due to a
// problem with the data store, or the configuration, it will be skipped
// without an error. This can cause IPs leaking from the IPAM, but it's
// preferable to leaving pods stuck in a `Terminating` state.
func cmdDel(args *skel.CmdArgs) error {
	if err := releaseIPAM(args); err != nil {
		log.Printf("fecni DEL: %v", err)
	}

	if args.Netns == "" {
		return nil
	}

	// Delete the container-side veth. Deleting one end of a veth pair
	// causes the kernel to remove the other end as well.
	err := ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		if linkErr := ip.DelLinkByName(args.IfName); linkErr != nil {
			if errors.Is(linkErr, ip.ErrLinkNotFound) {
				return nil
			}
			return fmt.Errorf("failed to delete link %q: %w", args.IfName, linkErr)
		}
		return nil
	})
	if err != nil {
		var nsPathErr ns.NSPathNotExistErr
		if errors.As(err, &nsPathErr) {
			return nil
		}
		return fmt.Errorf("failed to clean up netns %q: %w", args.Netns, err)
	}

	return nil
}

// releaseIPAM attempts to release the IPAM allocation of the
// address assigned to the container specified in the args. The
// args are expected to be unmodified arguments passed by the container
// runtime to the DEL command of the CNI plugin.
//
// This function fails if it can't parse the configuration or if it can't
// manipulate the IPAM data store.
func releaseIPAM(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("skipping IPAM release for container %s: %w", args.ContainerID, err)
	}

	allocator, err := ipam.New(ipam.Config{
		Subnet:  *conf.ParsedSubnet,
		DataDir: conf.DataDir,
		Network: conf.Name,
	})
	if err != nil {
		return fmt.Errorf("skipping IPAM release for container %s: %w", args.ContainerID, err)
	}

	defer func() { _ = allocator.Close() }()

	err = allocator.Release(args.ContainerID, args.IfName)
	if err != nil {
		return fmt.Errorf("failed to release IPAM allocation for container %s: %w", args.ContainerID, err)
	}
	return nil
}
