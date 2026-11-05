//go:build linux

package main

import (
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
)

// cmdStatus implements the CNI STATUS command. The container runtime uses
// STATUS to determine whether the node is network-ready. It should not
// succeed if CNI is aware of a condition that would prevent ADD command from
// succeeding.
func cmdStatus(args *skel.CmdArgs) error {
	if _, err := loadNetConf(args.StdinData); err != nil {
		return fmt.Errorf("fecni is not ready: %w", err)
	}

	return nil
}
