package sshconfig

import "testing"

func TestExclude(t *testing.T) {
	// Every field is populated, deliberately. Exclude's only structural
	// opportunity to mutate its input is to write through hosts[i], and a
	// zero-valued field cannot distinguish "clobbered" from "never set" — the
	// same fixture asymmetry that lets a hand-built Host literal pass a check a
	// parsed host would fail, since resolveHost defaults HostName to the alias
	// and Port to "22".
	hosts := []Host{
		{Alias: "nixos-dev", HostName: "192.0.2.10", User: "operator", Port: "22"},
		{Alias: "colima", HostName: "127.0.0.1", User: "colima", Port: "2222"},
		{Alias: "web-old", HostName: "10.0.0.9", User: "root", Port: "22"},
		{Alias: "web1", HostName: "10.0.0.1", User: "root", Port: "22"},
	}
	before := append([]Host(nil), hosts...)

	kept := Exclude(hosts, []string{"colima", "*-old"})
	if len(kept) != 2 || kept[0].Alias != "nixos-dev" || kept[1].Alias != "web1" {
		t.Fatalf("kept = %+v, want nixos-dev and web1", kept)
	}

	// Exclude's doc comment promises it never mutates hosts. Asserting
	// len(hosts) does not test that promise: Exclude never appends to or
	// reslices hosts, so the length is a quantity the function cannot change,
	// and a check on it passes for every possible implementation — including
	// one that writes through hosts[i] into the caller's backing array. The
	// mutation this now catches is `hosts[i].HostName = ...` on the excluded
	// entries, which the previous length check could not see. Host is
	// comparable (all string/int fields), so the whole struct is compared
	// rather than the one field a mutant happened to pick.
	if len(hosts) != len(before) {
		t.Fatalf("Exclude resliced its input: len = %d, want %d", len(hosts), len(before))
	}
	for i := range before {
		if hosts[i] != before[i] {
			t.Errorf("Exclude mutated its input at index %d:\n got  %+v\n want %+v", i, hosts[i], before[i])
		}
	}

	if got := Exclude(hosts, nil); len(got) != 4 {
		t.Errorf("Exclude with no globs dropped hosts: %d", len(got))
	}
}

// Exclude's doc comment promises that with nothing to exclude it "returns hosts
// itself rather than a copy". That is a stated contract, and it had no
// observer: two mutations survived the suite — removing the empty-patterns fast
// path so the general loop builds a copy, and replacing `return hosts` with an
// explicit copy.
//
// Neither can be caught by a value assertion. A copy is element-wise equal to
// the original and has the same length, so every check that inspects contents
// passes against both. Identity is the contract, so identity is what is
// asserted: the returned slice must share a backing array with the input, which
// &got[0] == &hosts[0] establishes without unsafe.
func TestExcludeWithNoPatternsReturnsTheInputItself(t *testing.T) {
	hosts := []Host{
		{Alias: "nixos-dev", HostName: "192.0.2.10"},
		{Alias: "web1", HostName: "10.0.0.1"},
	}

	for _, tc := range []struct {
		name     string
		patterns []string
	}{
		{"nil", nil},
		{"empty", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Exclude(hosts, tc.patterns)
			// Guard first: &got[0] would panic on an empty result, and an
			// empty result is itself a failure of the contract.
			if len(got) != len(hosts) {
				t.Fatalf("Exclude(hosts, %v) returned %d hosts, want %d", tc.patterns, len(got), len(hosts))
			}
			if &got[0] != &hosts[0] {
				t.Errorf("Exclude(hosts, %v) returned a copy; the contract is that it returns hosts itself", tc.patterns)
			}
		})
	}
}
