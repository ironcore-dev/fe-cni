//go:build linux

package main

import (
	"net"
	"strings"
	"testing"

	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"

	"github.com/ironcore-dev/feCNI/internal/ipam"
)

const (
	testPodIP  = "2001:db8:1::1"
	testHostIP = "2001:db8:1::"
)

func testAlloc(t *testing.T) *ipam.Allocation {
	t.Helper()
	return &ipam.Allocation{
		PodIP:  net.ParseIP(testPodIP),
		HostIP: net.ParseIP(testHostIP),
	}
}

// prevResultConf builds a NetConf carrying a prevResult that describes a
// host/container veth pair with the given pod address and gateway.
func prevResultConf(hostVethName, containerIfName, netns, podIP, gateway string) *NetConf {
	conf := &NetConf{}
	conf.CNIVersion = "1.0.0"
	conf.Name = "test"
	conf.RawPrevResult = map[string]any{
		"cniVersion": "1.0.0",
		"interfaces": []any{
			map[string]any{"name": hostVethName},
			map[string]any{"name": containerIfName, "sandbox": netns},
		},
		"ips": []any{
			map[string]any{
				"interface": float64(1),
				"address":   podIP + "/128",
				"gateway":   gateway,
			},
		},
	}
	return conf
}

func TestParsePrevResult_MissingIsError(t *testing.T) {
	conf := &NetConf{}
	conf.NetConf = types.NetConf{} // RawPrevResult nil

	_, err := parsePrevResult(conf)
	if err == nil {
		t.Fatal("expected error for missing prevResult, got nil")
	}
	if !strings.Contains(err.Error(), "prevResult is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePrevResult_Matches(t *testing.T) {
	alloc := testAlloc(t)
	result := mustParse(t, prevResultConf("vethhost0", "eth0", "/proc/1/ns/net", testPodIP, testHostIP))

	if err := validatePrevResult(result, alloc, "eth0", "/proc/1/ns/net"); err != nil {
		t.Errorf("validatePrevResult: %v", err)
	}
}

func TestValidatePrevResult_PodMismatch(t *testing.T) {
	alloc := testAlloc(t)
	result := mustParse(t, prevResultConf("vethhost0", "eth0", "/proc/1/ns/net", "2001:db8:1::5", testHostIP))

	err := validatePrevResult(result, alloc, "eth0", "/proc/1/ns/net")
	if err == nil {
		t.Fatal("expected error on pod address mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "does not match IPAM allocation") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePrevResult_GatewayMismatch(t *testing.T) {
	alloc := testAlloc(t)
	result := mustParse(t, prevResultConf("vethhost0", "eth0", "/proc/1/ns/net", testPodIP, "2001:db8:1::4"))

	err := validatePrevResult(result, alloc, "eth0", "/proc/1/ns/net")
	if err == nil {
		t.Fatal("expected error on gateway mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "does not match IPAM host address") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePrevResult_MissingInterface(t *testing.T) {
	alloc := testAlloc(t)
	result := mustParse(t, prevResultConf("vethhost0", "eth0", "/proc/1/ns/net", testPodIP, testHostIP))

	// Ask for an interface name not present in the prevResult.
	err := validatePrevResult(result, alloc, "eth1", "/proc/1/ns/net")
	if err == nil {
		t.Fatal("expected error for missing interface, got nil")
	}
	if !strings.Contains(err.Error(), "no interface") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHostVethName_Found(t *testing.T) {
	result := mustParse(t, prevResultConf("vethhost0", "eth0", "/proc/1/ns/net", testPodIP, testHostIP))

	got, err := hostVethName(result, "eth0", "/proc/1/ns/net")
	if err != nil {
		t.Fatalf("hostVethName: %v", err)
	}
	if got != "vethhost0" {
		t.Errorf("host veth name: got %q, want %q", got, "vethhost0")
	}
}

func TestHostVethName_MissingContainerInterface(t *testing.T) {
	result := mustParse(t, prevResultConf("vethhost0", "eth0", "/proc/1/ns/net", testPodIP, testHostIP))

	_, err := hostVethName(result, "eth1", "/proc/1/ns/net")
	if err == nil {
		t.Fatal("expected error for missing container interface, got nil")
	}
	if !strings.Contains(err.Error(), "no interface") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHostVethName_MissingHostInterface(t *testing.T) {
	// A prevResult with only the container interface — no host-side peer.
	conf := &NetConf{}
	conf.CNIVersion = "1.0.0"
	conf.Name = "test"
	conf.RawPrevResult = map[string]any{
		"cniVersion": "1.0.0",
		"interfaces": []any{
			map[string]any{"name": "eth0", "sandbox": "/proc/1/ns/net"},
		},
	}
	result := mustParse(t, conf)

	_, err := hostVethName(result, "eth0", "/proc/1/ns/net")
	if err == nil {
		t.Fatal("expected error for missing host-side interface, got nil")
	}
	if !strings.Contains(err.Error(), "host-side interface") {
		t.Errorf("unexpected error: %v", err)
	}
}

// mustParse parses a NetConf's prevResult, failing the test on error or when no
// prevResult is present.
func mustParse(t *testing.T, conf *NetConf) *current.Result {
	t.Helper()
	result, err := parsePrevResult(conf)
	if err != nil {
		t.Fatalf("parsePrevResult: %v", err)
	}
	if result == nil {
		t.Fatal("parsePrevResult returned nil result")
	}
	return result
}
