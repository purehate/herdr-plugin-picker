package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// parseInvariantCase drives the error/warning mutual-exclusivity table below.
// setup builds the fixture and returns the root path to hand to Parse; the
// want* flags say what that input is supposed to produce.
type parseInvariantCase struct {
	name      string
	setup     func(t *testing.T, dir string) string
	wantErr   bool
	wantWarns bool
	wantHosts bool
	needsPerm bool // fixture relies on chmod, which does not bind uid 0
}

var parseInvariantCases = []parseInvariantCase{
	// Negative control: the only row that satisfies the invariant vacuously,
	// and the reason every other row carries want* flags. It goes red if a
	// clean config ever starts producing an error or a warning, which is the
	// regression that would otherwise make the whole table pass for free.
	{
		name: "well-formed config",
		setup: func(t *testing.T, dir string) string {
			root := filepath.Join(dir, "root")
			write(t, root, "Host ok\n  HostName 10.0.0.1\n")
			return root
		},
		wantHosts: true,
	},

	// Error-producing inputs. parseFile has three error returns and these cover
	// both reachable ones, which both sit under os.ReadFile: the ErrNoConfig
	// wrap on ENOENT, and the raw fall-through, entered twice here because
	// EISDIR and EACCES reach it by different syscalls and only one of the two
	// fixtures survives a root uid. The third return, filepath.Abs, needs
	// os.Getwd to fail and has no fixture.
	{
		name: "root absent",
		setup: func(t *testing.T, dir string) string {
			return filepath.Join(dir, "absent")
		},
		wantErr: true,
	},
	{
		name: "root is a directory",
		setup: func(t *testing.T, dir string) string {
			root := filepath.Join(dir, "root")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatalf("mkdir %s: %v", root, err)
			}
			return root
		},
		wantErr: true,
	},
	{
		name: "root unreadable",
		setup: func(t *testing.T, dir string) string {
			root := filepath.Join(dir, "root")
			write(t, root, "Host ok\n  HostName 10.0.0.1\n")
			if err := os.Chmod(root, 0o000); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(root, 0o600); err != nil {
					t.Fatalf("cleanup chmod: %v", err)
				}
			})
			return root
		},
		wantErr:   true,
		needsPerm: true,
	},

	// Warning-producing inputs: the line loop and each parseIncludes downgrade.
	{
		name: "malformed line",
		setup: func(t *testing.T, dir string) string {
			root := filepath.Join(dir, "root")
			write(t, root, "Host ok\n  HostName 10.0.0.1\nOrphanedKeyword\n")
			return root
		},
		wantWarns: true,
		wantHosts: true,
	},
	{
		name: "unbalanced quotes",
		setup: func(t *testing.T, dir string) string {
			root := filepath.Join(dir, "root")
			write(t, root, "Host ok\n  HostName \"10.0.0.1\n")
			return root
		},
		wantWarns: true,
		wantHosts: true,
	},
	{
		// A directory rather than chmod 000, for the same reason the
		// unreadable-primary test in cmd/herdr-picker uses one: chmod 000 is still
		// readable as uid 0, so that fixture would skip on a root container and
		// prove nothing there, while a directory fails the read for every uid.
		// This is the include-unreadable downgrade, which is the parseIncludes
		// path that could most plausibly grow an error return later.
		name: "include is unreadable",
		setup: func(t *testing.T, dir string) string {
			blocked := filepath.Join(dir, "blocked")
			if err := os.Mkdir(blocked, 0o700); err != nil {
				t.Fatalf("mkdir %s: %v", blocked, err)
			}
			root := filepath.Join(dir, "root")
			write(t, root, "Include "+blocked+"\n")
			return root
		},
		wantWarns: true,
	},
	{
		// Absolute Include paths, here and above, so every row exercises the
		// exported Parse — the function cmd/herdr-picker actually calls — rather
		// than the unexported parse that the temp-dir fixtures in parse_test.go
		// and include_test.go need in order to resolve relative includes.
		// parseIncludes only joins includeBase onto a non-absolute pattern, so
		// Parse's real ~/.ssh base is inert here.
		name: "include cycle",
		setup: func(t *testing.T, dir string) string {
			root := filepath.Join(dir, "self")
			write(t, root, "Host looper\n  HostName 10.0.0.9\nInclude "+root+"\n")
			return root
		},
		wantWarns: true,
		wantHosts: true,
	},
}

// TestParseNeverReturnsAnErrorAndWarningsTogether pins the contract that a
// non-nil error and a non-empty []Warning are mutually exclusive. It holds
// structurally today — every error return in parseFile sits before the line
// loop that produces warnings, and parseIncludes downgrades an unreadable,
// cyclic or too-deep include into a Warning rather than propagating it — but
// nothing enforced it, and the caller outside this package is written against
// it. cmd/herdr-picker/loadHosts takes the parsed hosts on one switch arm and
// reports err.Error() into the picker footer on another, then appends
// w.String() for every warning regardless of which arm ran. That pairing
// assumes the error arm yields neither: hosts, because it drops `found` on the
// floor there and would silently lose every host in a file that also errored;
// and warnings, because the ones it prints would then be describing a parse
// whose result was discarded, pointing the operator at lines in a file the
// picker is showing nothing from. It is why wantHosts is asserted here and not
// just wantWarns.
//
// Each case asserts it still reaches the state it claims before the invariant
// is checked. Without that, ¬(err ∧ warns) is vacuously true of any input
// producing neither one, so a fixture that quietly stopped erroring — a
// renamed sentinel, a path that no longer resolves, a downgrade that moved —
// would keep this green while pinning nothing. The want* flags are also what
// makes the assertion two-sided: on a warning case wantErr asserts the error
// is nil, and on an error case wantWarns asserts no warning escaped alongside
// it. Those flags are what actually kills a regression here, which was measured
// rather than assumed — three mutants (a warning added to each of parseFile's
// two pre-loop error returns, and a new error return added after the line loop)
// are all caught by the flags, and none of the three reaches the conjunction
// below.
//
// So the conjunction is unreachable while every row declares at most one of
// err/warns, and it is kept deliberately: it guards the cheapest wrong repair.
// Break the invariant and root_unreadable goes red; set wantWarns: true on that
// row and the flag checks pass again, the suite is green, and the contract has
// been renegotiated instead of fixed. With the conjunction that row still
// fails, and its message names the caller that breaks. That specific repair is
// the one input observed to make this line fire.
func TestParseNeverReturnsAnErrorAndWarningsTogether(t *testing.T) {
	for _, tc := range parseInvariantCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsPerm && os.Geteuid() == 0 {
				t.Skip("running as root: chmod 000 does not block reads")
			}
			root := tc.setup(t, t.TempDir())

			hosts, warns, err := Parse(root)

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want an error: %v", err, tc.wantErr)
			}
			if (len(warns) != 0) != tc.wantWarns {
				t.Fatalf("warnings = %v, want warnings: %v", warns, tc.wantWarns)
			}
			if (len(hosts) != 0) != tc.wantHosts {
				t.Fatalf("hosts = %+v, want hosts: %v", hosts, tc.wantHosts)
			}
			if err != nil && len(warns) != 0 {
				t.Fatalf("invariant broken: err = %v returned alongside warnings %v; "+
					"loadHosts now prints those warnings for a parse it discarded", err, warns)
			}
		})
	}
}
