// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
)

func TestCmdStatus(t *testing.T) {
	tests := []struct {
		name    string
		conf    []byte
		wantErr string
	}{
		{
			name:    "no subnet is not ready (node not configured)",
			conf:    makeConf(t, nil),
			wantErr: "subnet is required",
		},
		{
			name:    "empty subnet is not ready (node not configured)",
			conf:    makeConf(t, map[string]any{"subnet": ""}),
			wantErr: "subnet is required",
		},
		{
			name:    "invalid subnet is not ready",
			conf:    makeConf(t, map[string]any{"subnet": "notacidr"}),
			wantErr: "invalid subnet",
		},
		{
			name: "valid subnet is ready",
			conf: makeConf(t, map[string]any{"subnet": "2001:db8::/120"}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmdStatus(&skel.CmdArgs{StdinData: tt.conf})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("cmdStatus: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("cmdStatus: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
