// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package ipam

import (
	"net"
	"testing"
)

func mustCIDR(t *testing.T, s string) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", s, err)
	}
	return *n
}

// collect drains an iterator into a slice of strings for easy comparison.
func collect(it *blockIter) []string {
	var out []string
	for pod := it.Next(); pod != nil; pod = it.Next() {
		out = append(out, pod.String())
	}
	return out
}

func TestBlockIter_FullSequence(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want []string
	}{
		{
			name: "/127 single block",
			cidr: "2001:db8:1::/127",
			want: []string{"2001:db8:1::1"},
		},
		{
			name: "/126 two blocks",
			cidr: "2001:db8:1::/126",
			want: []string{"2001:db8:1::1", "2001:db8:1::3"},
		},
		{
			name: "/124 eight blocks",
			cidr: "2001:db8:1::/124",
			want: []string{
				"2001:db8:1::1", "2001:db8:1::3",
				"2001:db8:1::5", "2001:db8:1::7",
				"2001:db8:1::9", "2001:db8:1::b",
				"2001:db8:1::d", "2001:db8:1::f",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collect(newBlockIter(mustCIDR(t, tt.cidr), nil))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("block %d: got %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBlockIter_RoundRobin(t *testing.T) {
	tests := []struct {
		name       string
		cidr       string
		startAfter string // empty string → nil
		want       []string
	}{
		{
			name:       "start after first block wraps and covers all",
			cidr:       "2001:db8:1::/126",
			startAfter: "2001:db8:1::1",
			want:       []string{"2001:db8:1::3", "2001:db8:1::1"},
		},
		{
			name:       "start after last block wraps to subnet start",
			cidr:       "2001:db8:1::/126",
			startAfter: "2001:db8:1::3",
			want:       []string{"2001:db8:1::1", "2001:db8:1::3"},
		},
		{
			name:       "single-block subnet with round-robin start yields that block",
			cidr:       "2001:db8:1::/127",
			startAfter: "2001:db8:1::1",
			want:       []string{"2001:db8:1::1"},
		},
		{
			name:       "startAfter outside subnet falls back to full scan",
			cidr:       "2001:db8:1::/126",
			startAfter: "2001:db8:2::1",
			want:       []string{"2001:db8:1::1", "2001:db8:1::3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var startAfter net.IP
			if tt.startAfter != "" {
				startAfter = net.ParseIP(tt.startAfter)
			}
			got := collect(newBlockIter(mustCIDR(t, tt.cidr), startAfter))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("step %d: got %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNextPrefix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "aligned base advances by two",
			in:   "2001:db8::2",
			want: "2001:db8::4",
		},
		{
			name: "odd input aligns down before advancing",
			in:   "2001:db8::3",
			want: "2001:db8::4",
		},
		{
			name: "carry propagates across byte boundary",
			in:   "2001:db8::fe",
			want: "2001:db8::100",
		},
		{
			name: "odd input at byte boundary aligns then carries",
			in:   "2001:db8::ff",
			want: "2001:db8::100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextPrefix(net.ParseIP(tt.in))
			if !got.Equal(net.ParseIP(tt.want)) {
				t.Errorf("nextPrefix(%s) = %s, want %s", tt.in, got, tt.want)
			}
			if got[len(got)-1]&1 != 0 {
				t.Errorf("nextPrefix(%s) = %s is not /127-aligned (low bit set)", tt.in, got)
			}
		})
	}
}

func TestBlockIter_PodIsOddHostIsEven(t *testing.T) {
	it := newBlockIter(mustCIDR(t, "2001:db8:1::/124"), nil)
	for pod := it.Next(); pod != nil; pod = it.Next() {
		if pod[len(pod)-1]&1 != 1 {
			t.Errorf("pod IP %s does not have low bit set", pod)
		}
		host := hostForPod(pod)
		if host[len(host)-1]&1 != 0 {
			t.Errorf("host IP %s does not have low bit clear", host)
		}
		if !host.Equal(clearLowBit(pod)) || !setLowBit(host).Equal(pod) {
			t.Errorf("host %s and pod %s are not a /127 pair", host, pod)
		}
	}
}

func TestBlockIter_ExhaustedKeepsReturningNil(t *testing.T) {
	it := newBlockIter(mustCIDR(t, "2001:db8:1::/127"), nil)
	it.Next() // drain the single block
	for i := range 3 {
		if got := it.Next(); got != nil {
			t.Errorf("call %d after exhaustion: got %s, want nil", i+1, got)
		}
	}
}
