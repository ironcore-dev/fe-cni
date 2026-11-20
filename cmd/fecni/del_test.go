// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
)

// TestCmdDel_OnFailedIPAM verifies DEL does not fail even if
// it's not able to release the IPAM allocation.
func TestCmdDel_OnFailedIPAM(t *testing.T) {
	// An empty config will fail the releaseIPAM function because
	// it can't find the data store nor the subnet from which to release
	// the IP
	conf := makeConf(t, nil)
	err := cmdDel(&skel.CmdArgs{
		ContainerID: "test-container",
		IfName:      "eth0",
		Netns:       "",
		StdinData:   conf,
	})
	if err != nil {
		t.Errorf("cmdDel unexpectedly failed: %v", err)
	}
}

func TestReleaseIPAM(t *testing.T) {
	cases := []struct {
		name    string
		conf    []byte
		wantErr bool
	}{
		{
			name:    "subnet missing",
			conf:    makeConf(t, nil),
			wantErr: true,
		},
		{
			name:    "subnet empty",
			conf:    makeConf(t, map[string]any{"subnet": ""}),
			wantErr: true,
		},
		{
			// The store directory cannot be created under a regular
			// file, so allocator initialization fails
			name: "dataDir unresolvable",
			conf: makeConf(t, map[string]any{
				"subnet":  "2001:db8::/120",
				"dataDir": filepath.Join(makeFile(t), "child"),
			}),
			wantErr: true,
		},
		{
			name: "config OK",
			conf: makeConf(t, map[string]any{
				"subnet":  "2001:db8::/120",
				"dataDir": t.TempDir(),
			}),
			wantErr: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			args := skel.CmdArgs{
				ContainerID: "test-container",
				IfName:      "eth0",
				Netns:       "",
				StdinData:   tt.conf,
			}

			err := releaseIPAM(&args)
			if tt.wantErr && err == nil {
				t.Errorf("releaseIPAM expected to return error, but passed.")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("releaseIPAM return unexpected error: %v", err)
			}
		})
	}
}

// makeFile creates an empty regular file inside a temporary directory and
// returns its path.
func makeFile(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatalf("create regular file: %v", err)
	}
	return f
}
