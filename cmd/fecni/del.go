//go:build linux

package main

import (
	"errors"
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/ironcore-dev/FeCNI/internal/ipam"
)

// cmdDel implements the CNI DEL command. It releases the IPAM allocation
// and removes the veth from the container namespace (which also tears down
// the host-side peer automatically).
func cmdDel(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}

	subnet, err := conf.parseSubnet()
	if err != nil {
		return err
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

	if err := allocator.Release(args.ContainerID, args.IfName); err != nil {
		return fmt.Errorf("failed to release IPAM allocation: %w", err)
	}

	if args.Netns == "" {
		return nil
	}

	// Delete the container-side veth. Deleting one end of a veth pair
	// causes the kernel to remove the other end as well.
	err = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
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
