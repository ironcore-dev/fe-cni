//go:build linux

package main

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

func makeConf(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	m := map[string]any{
		"cniVersion": "1.0.0",
		"name":       "test",
		"type":       "fecni",
	}
	maps.Copy(m, fields)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestLoadNetConf_MTU(t *testing.T) {
	tests := []struct {
		name    string
		mtu     any
		wantMTU int
	}{
		{
			name:    "defaults to 1500 when unset",
			mtu:     nil,
			wantMTU: defaultMTU,
		},
		{
			name:    "explicit value preserved",
			mtu:     9000,
			wantMTU: 9000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := map[string]any{"subnet": "2001:db8::/120"}
			if tt.mtu != nil {
				fields["mtu"] = tt.mtu
			}
			conf, err := loadNetConf(makeConf(t, fields))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if conf.MTU != tt.wantMTU {
				t.Errorf("MTU: got %d, want %d", conf.MTU, tt.wantMTU)
			}
		})
	}
}

func TestLoadNetConf_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr string
	}{
		{
			name:    "invalid json",
			input:   []byte(`{not json}`),
			wantErr: "failed to load netconf",
		},
		{
			name:    "ipam stanza present",
			input:   makeConf(t, map[string]any{"subnet": "2001:db8::/120", "ipam": map[string]any{"type": "host-local"}}),
			wantErr: "built-in IPAM",
		},
		{
			name:    "subnet missing",
			input:   makeConf(t, nil),
			wantErr: "subnet is required",
		},
		{
			name:    "subnet invalid cidr",
			input:   makeConf(t, map[string]any{"subnet": "notacidr"}),
			wantErr: "invalid subnet",
		},
		{
			name:    "ipv4 subnet",
			input:   makeConf(t, map[string]any{"subnet": "10.0.0.0/24"}),
			wantErr: "IPv6-only",
		},
		{
			name:    "host bits set",
			input:   makeConf(t, map[string]any{"subnet": "2001:db8::5/120"}),
			wantErr: "host bits set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadNetConf(tt.input)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestLoadNetConf_SubnetParsed verifies that a valid subnet is parsed and
// exposed on the returned NetConf.
func TestLoadNetConf_SubnetParsed(t *testing.T) {
	conf, err := loadNetConf(makeConf(t, map[string]any{"subnet": "2001:db8::/120"}))
	if err != nil {
		t.Fatalf("loadNetConf: unexpected error: %v", err)
	}
	if conf.ParsedSubnet == nil {
		t.Fatal("loadNetConf: ParsedSubnet is nil")
	}
	if got := conf.ParsedSubnet.String(); got != "2001:db8::/120" {
		t.Errorf("ParsedSubnet: got %q, want %q", got, "2001:db8::/120")
	}
}

func TestParseSubnet(t *testing.T) {
	tests := []struct {
		name    string
		subnet  string
		wantErr string
	}{
		{
			name:    "empty",
			subnet:  "",
			wantErr: "subnet is required",
		},
		{
			name:    "invalid cidr",
			subnet:  "notacidr",
			wantErr: "invalid subnet",
		},
		{
			name:    "ipv4",
			subnet:  "192.168.0.0/24",
			wantErr: "IPv6-only",
		},
		{
			name:    "host bits set includes corrected hint",
			subnet:  "2001:db8::1/64",
			wantErr: "2001:db8::/64",
		},
		{
			name:   "valid /120",
			subnet: "2001:db8::/120",
		},
		{
			name:   "valid /127",
			subnet: "2001:db8::/127",
		},
		{
			name:   "valid /64",
			subnet: "2001:db8::/64",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipNet, err := parseSubnet(tt.subnet)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ipNet == nil {
				t.Fatal("expected non-nil IPNet")
			}
			if tt.subnet != ipNet.String() {
				t.Errorf("parsed subnet: got %q, want %q", ipNet, tt.subnet)
			}
		})
	}
}
