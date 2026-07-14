package main

import (
	"fmt"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func init() {
	runtime.LockOSThread()
}

func main() {
	skel.PluginMainFuncs(
		skel.CNIFuncs{
			Add:    cmdAdd,
			Del:    cmdDel,
			Check:  cmdCheck,
			Status: func(_ *skel.CmdArgs) error { return nil }, // Without any dependencies, we should be always good to go
		},
		version.All,
		bv.BuildString("fecni"),
	)
}

func cmdAdd(args *skel.CmdArgs) error {
	result := &current.Result{
		CNIVersion: current.ImplementedSpecVersion,
		Interfaces: []*current.Interface{},
		IPs:        []*current.IPConfig{},
		Routes:     []*types.Route{},
	}

	if err := types.PrintResult(result, result.CNIVersion); err != nil {
		return fmt.Errorf("failed to print result: %w", err)
	}

	return nil
}

func cmdDel(args *skel.CmdArgs) error {
	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}
