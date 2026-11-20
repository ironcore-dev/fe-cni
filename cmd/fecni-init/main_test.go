// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRenderTemplate(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "fecni.conflist.tmpl")
	outPath := filepath.Join(dir, "fecni.conflist")

	tmpl := `{"subnet": "` + podCIDRPlaceholder + `"}`
	if err := os.WriteFile(tmplPath, []byte(tmpl), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}

	t.Run("renders subnet into placeholder", func(t *testing.T) {
		if err := renderTemplate(tmplPath, outPath, "2001:db8::/120"); err != nil {
			t.Fatalf("renderTemplate: %v", err)
		}
		got, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		if want := `{"subnet": "2001:db8::/120"}`; string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("no temp files left behind", func(t *testing.T) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir: %v", err)
		}
		for _, e := range entries {
			name := e.Name()
			if name != "fecni.conflist" && name != "fecni.conflist.tmpl" {
				t.Errorf("unexpected leftover file %q", name)
			}
		}
	})
}

func TestWaitForPodCIDR(t *testing.T) {
	// outcome is one scripted result for the fake fetch function.
	// Individual test cases use it to create an ordered list of
	// retried API call results.
	type outcome struct {
		cidr string
		err  error
	}

	for name, tc := range map[string]struct {
		// outcomes represent the in-order list of results returned
		// by the fetch function. It is expected that the last entry
		// will satisfy waitForPodCIDR, causing it to return.
		outcomes []outcome
		want     string
	}{
		"blocks through error and empty until valid prefix": {
			outcomes: []outcome{
				{"", errFake},
				{"", nil}, // parse error, treated same as any unusable value
				{"2001:db8::/120", nil},
			},
			want: "2001:db8::/120",
		},
		"rejects IPv4 prefix": {
			outcomes: []outcome{
				{"10.0.0.0/8", nil},
				{"fd00::/64", nil},
			},
			want: "fd00::/64",
		},
		"rejects IPv4-in-IPv6 prefix": {
			outcomes: []outcome{
				{"::ffff:10.0.0.0/104", nil},
				{"fd00::/64", nil},
			},
			want: "fd00::/64",
		},
		"normalizes unmasked prefix": {
			outcomes: []outcome{
				{"2001:db8::1/120", nil},
			},
			want: "2001:db8::/120",
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			fetch := func(_ string) (string, error) {
				// This condition prevents infinite loops in case that
				// none of the outcomes satisfy the caller
				if calls > len(tc.outcomes) {
					t.FailNow()
				}
				outcome := tc.outcomes[min(calls, len(tc.outcomes)-1)]
				calls++
				return outcome.cidr, outcome.err
			}

			got := waitForPodCIDR("node-1", fetch, time.Millisecond)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if calls != len(tc.outcomes) {
				t.Errorf("fetch called %d times, want %d", calls, len(tc.outcomes))
			}
		})
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "fake fetch error" }

var errFake = fakeErr{}
