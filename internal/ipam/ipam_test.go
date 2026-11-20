// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package ipam

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func testConfig(t *testing.T, cidr string) Config {
	t.Helper()
	return Config{
		Subnet:  mustCIDR(t, cidr),
		DataDir: t.TempDir(),
		Network: "fecni",
	}
}

func TestNew_RejectsNonMaskAlignedSubnet(t *testing.T) {
	cfg := Config{
		Subnet:  net.IPNet{IP: net.ParseIP("2001:db8:1::5"), Mask: net.CIDRMask(120, 128)},
		DataDir: t.TempDir(),
		Network: "fecni",
	}
	if _, err := New(cfg); err == nil {
		t.Error("expected error for non-mask-aligned subnet, got nil")
	}
}

func TestAllocator_PairInvariants(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/120"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	for _, id := range []string{"container-1", "container-2", "container-3"} {
		alloc, err := a.Get(id, "eth0")
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if len(alloc.PodIP) != net.IPv6len || len(alloc.HostIP) != net.IPv6len {
			t.Errorf("%s: addresses not 16 bytes: pod=%d host=%d", id, len(alloc.PodIP), len(alloc.HostIP))
		}
		if alloc.HostIP[len(alloc.HostIP)-1]&1 != 0 {
			t.Errorf("%s: host %s is not even", id, alloc.HostIP)
		}
		if alloc.PodIP[len(alloc.PodIP)-1]&1 != 1 {
			t.Errorf("%s: pod %s is not odd", id, alloc.PodIP)
		}
		if !alloc.HostIP.Equal(clearLowBit(alloc.PodIP)) {
			t.Errorf("%s: host %s is not the peer of pod %s", id, alloc.HostIP, alloc.PodIP)
		}
	}
}

func TestAllocator_DistinctPodsGetNonCollidingBlocks(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/120"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	allocA, err := a.Get("container-1", "eth0")
	if err != nil {
		t.Fatalf("Get container-1: %v", err)
	}
	allocB, err := a.Get("container-2", "eth0")
	if err != nil {
		t.Fatalf("Get container-2: %v", err)
	}

	if clearLowBit(allocA.PodIP).Equal(clearLowBit(allocB.PodIP)) {
		t.Errorf("container-1 (%s) and container-2 (%s) share a /127 block", allocA.PodIP, allocB.PodIP)
	}
}

func TestAllocator_Idempotent(t *testing.T) {
	tests := []struct {
		name         string
		sameInstance bool
	}{
		{"same instance", true},
		{"fresh instance", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig(t, "2001:db8:1::/120")
			a, err := New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			first, err := a.Get("container-1", "eth0")
			if err != nil {
				t.Fatalf("Get first: %v", err)
			}

			if !tt.sameInstance {
				_ = a.Close()
				a, err = New(cfg)
				if err != nil {
					t.Fatalf("New (second): %v", err)
				}
			}
			defer func() { _ = a.Close() }()

			second, err := a.Get("container-1", "eth0")
			if err != nil {
				t.Fatalf("Get second: %v", err)
			}
			if !first.PodIP.Equal(second.PodIP) || !first.HostIP.Equal(second.HostIP) {
				t.Errorf("non-idempotent: first %+v, second %+v", first, second)
			}
		})
	}
}

func TestAllocator_ReleaseAllowsReuse(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/127"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	first, err := a.Get("container-1", "eth0")
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	if err := a.Release("container-1", "eth0"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	reused, err := a.Get("container-2", "eth0")
	if err != nil {
		t.Fatalf("Get after release: %v", err)
	}
	if !reused.PodIP.Equal(first.PodIP) {
		t.Errorf("freed block not reused: got %s, want %s", reused.PodIP, first.PodIP)
	}
}

func TestAllocator_ReleasedKeyGetsNewBlock(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/126"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	if _, err := a.Get("container-1", "eth0"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := a.Release("container-1", "eth0"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	alloc, err := a.Get("container-1", "eth0")
	if err != nil {
		t.Fatalf("Get after release: %v", err)
	}
	if alloc.PodIP[len(alloc.PodIP)-1]&1 != 1 {
		t.Errorf("reallocated pod %s is not odd", alloc.PodIP)
	}
}

func TestAllocator_ExhaustionReturnsError(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/126"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	if _, err := a.Get("container-1", "eth0"); err != nil {
		t.Fatalf("Get container-1: %v", err)
	}
	if _, err := a.Get("container-2", "eth0"); err != nil {
		t.Fatalf("Get container-2: %v", err)
	}
	if _, err := a.Get("container-3", "eth0"); err == nil {
		t.Error("expected exhaustion error for third allocation, got nil")
	}
}

func TestAllocator_ReleaseIsIdempotent(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/120"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	if err := a.Release("never-allocated", "eth0"); err != nil {
		t.Errorf("Release of unknown key errored: %v", err)
	}
}

func TestAllocator_OnlyPodIPWrittenToStore(t *testing.T) {
	cfg := testConfig(t, "2001:db8:1::/120")
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	alloc, err := a.Get("container-1", "eth0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	dir := filepath.Join(cfg.DataDir, cfg.Network)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var podFile bool
	for _, e := range entries {
		if e.Name() == alloc.PodIP.String() {
			podFile = true
		}
		if e.Name() == alloc.HostIP.String() {
			t.Errorf("host address %s was reserved in the store", alloc.HostIP)
		}
	}
	if !podFile {
		t.Errorf("no store file for pod IP %s in %v", alloc.PodIP, entries)
	}
}

func TestAllocator_StaleCursorAfterSubnetChange(t *testing.T) {
	oldCfg := Config{
		Subnet:  mustCIDR(t, "2001:db8:1::/120"),
		DataDir: t.TempDir(),
		Network: "fecni",
	}
	oldAlloc, err := New(oldCfg)
	if err != nil {
		t.Fatalf("New (old): %v", err)
	}
	if _, err := oldAlloc.Get("container-1", "eth0"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = oldAlloc.Close()

	newCfg := Config{
		Subnet:  mustCIDR(t, "2001:db8:2::/120"),
		DataDir: oldCfg.DataDir,
		Network: oldCfg.Network,
	}
	newAlloc, err := New(newCfg)
	if err != nil {
		t.Fatalf("New (new): %v", err)
	}
	defer func() { _ = newAlloc.Close() }()

	alloc, err := newAlloc.Get("container-2", "eth0")
	if err != nil {
		t.Fatalf("Get after subnet change: %v", err)
	}
	if !newCfg.Subnet.Contains(alloc.PodIP) {
		t.Errorf("allocated pod %s is outside new subnet %s", alloc.PodIP, &newCfg.Subnet)
	}
}

func TestAllocator_LookupMissing(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/120"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	alloc, err := a.Lookup("never-allocated", "eth0")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if alloc != nil {
		t.Errorf("expected nil allocation for unallocated key, got %+v", alloc)
	}
}

func TestAllocator_LookupDoesNotReserve(t *testing.T) {
	cfg := testConfig(t, "2001:db8:1::/120")
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	// Reserve one key so the store is non-empty but the subnet still has room.
	if _, err := a.Get("container-1", "eth0"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	dir := filepath.Join(cfg.DataDir, cfg.Network)
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir before: %v", err)
	}

	// Look up a different, unreserved key; it must miss and must not reserve.
	looked, err := a.Lookup("container-2", "eth0")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if looked != nil {
		t.Errorf("expected nil allocation for unreserved key, got %+v", looked)
	}

	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir after: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("Lookup reserved a block: store file count before %d, after %d", len(before), len(after))
	}
}

func TestAllocator_LookupAfterReleaseIsNil(t *testing.T) {
	a, err := New(testConfig(t, "2001:db8:1::/120"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = a.Close() }()

	if _, err := a.Get("container-1", "eth0"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := a.Release("container-1", "eth0"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	alloc, err := a.Lookup("container-1", "eth0")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if alloc != nil {
		t.Errorf("expected nil allocation after release, got %+v", alloc)
	}
}
