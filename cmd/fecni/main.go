//go:build linux

package main

import (
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
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
			Status: cmdStatus,
		},
		version.All,
		bv.BuildString("fecni"),
	)
}
