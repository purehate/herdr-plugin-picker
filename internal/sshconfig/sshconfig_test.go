package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitLine(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		key, val  string
		ok        bool
		badQuotes bool
	}{
		{"space separated", "HostName example.com", "HostName", "example.com", true, false},
		{"equals separated", "Port=2222", "Port", "2222", true, false},
		{"leading whitespace", "    User root", "User", "root", true, false},
		{"quoted value", `IdentityFile "~/.ssh/id ed"`, "IdentityFile", "~/.ssh/id ed", true, false},
		{"comment", "# Host nope", "", "", false, false},
		{"blank", "   ", "", "", false, false},
		{"keyword only", "Host", "", "", false, false},
		// Verified against OpenSSH_10.3p1: a `#` starting a whitespace-delimited
		// token ends the line; ssh -G shows the value with no trailing comment.
		{"trailing comment stripped", "HostName example.com # comment", "HostName", "example.com", true, false},
		// Verified: `#` glued to a token with no preceding whitespace is literal.
		{"hash glued to token is literal", "HostName 10.0.0.7#tight", "HostName", "10.0.0.7#tight", true, false},
		// Verified: `#` inside a double-quoted value is literal even though it is
		// preceded by whitespace.
		{"hash inside quotes is literal", `IdentityFile "~/.ssh/id #1"`, "IdentityFile", "~/.ssh/id #1", true, false},
		// A value ending but not starting with a quote must not be mangled by an
		// independent trim off each end. ssh's own strdelim quote-joining would
		// produce ~/keys/id ed here; we deliberately do not emulate that and
		// leave the value as-is instead — an accepted, documented simplification.
		{"quote at end only is not stripped", `IdentityFile ~/keys/"id ed"`, "IdentityFile", `~/keys/"id ed"`, true, false},
		// Verified against OpenSSH_10.3p1: an odd number of quotes is fatal
		// ("invalid quotes"), and ssh refuses the whole file. We stay non-fatal
		// (best-effort value, unstripped) but must flag it so the operator does
		// not get a healthy-looking picker for a config ssh itself would reject.
		{"unbalanced quote is flagged", `HostName "10.0.0.5`, "HostName", `"10.0.0.5`, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, val, ok, badQuotes := splitLine(tc.in)
			if ok != tc.ok || key != tc.key || val != tc.val || badQuotes != tc.badQuotes {
				t.Fatalf("splitLine(%q) = (%q, %q, %v, %v), want (%q, %q, %v, %v)",
					tc.in, key, val, ok, badQuotes, tc.key, tc.val, tc.ok, tc.badQuotes)
			}
		})
	}
}

// Warning.String() feeds the picker footer directly, so its file:line: msg
// format is a user-visible contract, not an incidental detail.
func TestWarningString(t *testing.T) {
	w := Warning{File: "testdata/with-include", Line: 7, Msg: "include unreadable: foo"}
	if got, want := w.String(), "testdata/with-include:7: include unreadable: foo"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestBlockMatches(t *testing.T) {
	b := block{positive: []string{"*.internal", "nixos-dev"}, negative: []string{"secret.internal"}}
	cases := map[string]bool{
		"nixos-dev":       true,
		"box.internal":    true,
		"secret.internal": false,
		"unrelated":       false,
	}
	for alias, want := range cases {
		if got := b.matches(alias); got != want {
			t.Errorf("matches(%q) = %v, want %v", alias, got, want)
		}
	}
}

func TestIsPattern(t *testing.T) {
	for _, p := range []string{"*", "*.dev", "web?", "a*b", "?"} {
		if !isPattern(p) {
			t.Errorf("isPattern(%q) = false, want true", p)
		}
	}
	// Only `*` and `?` are ssh_config wildcards. Brackets have no character-class
	// meaning, and a mid-string `!` is literal — `!` negates only as the leading
	// character of a pattern-list entry, which newHostBlock strips into
	// block.negative before anything reaches isPattern. These are all connectable
	// host aliases, not wildcards.
	for _, p := range []string{"nixos-dev", "10.0.0.1", "build_box", "[ab]host", "web[12]", "foo!bar"} {
		if isPattern(p) {
			t.Errorf("isPattern(%q) = true, want false", p)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, alias string
		want           bool
	}{
		// Exact literal match.
		{"web1", "web1", true},
		{"web1", "web2", false},
		// '*' matches zero or more of any character.
		{"*", "anything", true},
		{"*", "", true},
		{"*.internal", "box.internal", true},
		{"*.internal", "internal", false},
		{"web*", "web1", true},
		{"web*", "web", true},
		{"web*", "xweb1", false},
		{"*web*", "xwebx", true},
		{"a*b*c", "aXbXXc", true},
		{"a*b*c", "abc", true},
		{"a*b*c", "ac", false},
		// '?' matches exactly one character.
		{"web?", "web1", true},
		{"web?", "web", false},
		{"web?", "web12", false},
		{"?ost", "host", true},
		// '[', ']', '\' are literal in ssh_config PATTERNS, unlike filepath.Match.
		{"web[12]", "web1", false},
		{"web[12]", "web[12]", true},
		{`a\b`, `a\b`, true},
		{`a\b`, "ab", false},
	}
	for _, tc := range cases {
		if got := matchPattern(tc.pattern, tc.alias); got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.pattern, tc.alias, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 60); got != "short" {
		t.Errorf("truncate(%q, 60) = %q, want it unchanged", "short", got)
	}
	if got := truncate("abcdef", 3); got != "abc…" {
		t.Errorf("truncate(%q, 3) = %q, want abc…", "abcdef", got)
	}
	// A 3-byte rune straddling the limit must not be severed into invalid UTF-8.
	long := strings.Repeat("a", 58) + "日本語"
	got := truncate(long, 60)
	if !utf8.ValidString(got) {
		t.Errorf("truncate returned invalid UTF-8: %q", got)
	}
	if want := strings.Repeat("a", 58) + "…"; got != want {
		t.Errorf("truncate = %q, want %q", got, want)
	}
}

// truncate's boundary is `len(s) <= n`, and the cases above sit far on either
// side of it — n=60 against 5 bytes, n=3 against 6. Nothing pinned n == len(s)
// itself, so flipping <= to < survived the suite.
//
// Bracketing the boundary from both sides, and asserting the returned value at
// three adjacent lengths, is deliberate. At n == len(s) that mutant does not
// return a wrong string: it falls into the truncation path, sets cut = n, and
// reads s[cut] with cut == len(s), so it dies of an index-out-of-range panic
// raised in the rune-boundary loop — one step removed from the comparison that
// is actually wrong. A kill by panic pins nothing. It would read the same if
// the comparison were right and something else crashed, and it tells a later
// reader nothing about which side of <= is intended. The len±1 rows are
// ordinary value assertions and do carry that meaning: they fail with a wrong
// string at a named length, which is why an off-by-one that shifts the
// boundary without ever indexing out of range is caught here too.
//
// ASCII throughout, so the rune-boundary backoff is not also in play — the
// multibyte cut is covered above.
func TestTruncateBoundary(t *testing.T) {
	const s = "abcdef" // 6 bytes, one byte per rune
	if len(s) != 6 {
		t.Fatalf("fixture is not 6 bytes: %d", len(s))
	}

	// Above the limit: unchanged.
	if got := truncate(s, len(s)+1); got != s {
		t.Errorf("truncate(%q, %d) = %q, want %q unchanged", s, len(s)+1, got, s)
	}
	// Exactly at the limit: unchanged. `len(s) <= n` is the whole contract.
	if got := truncate(s, len(s)); got != s {
		t.Errorf("truncate(%q, %d) = %q, want %q unchanged — n == len(s) must not truncate", s, len(s), got, s)
	}
	// One below: truncated, keeping n bytes and appending the ellipsis, so the
	// result is n+1 runes rather than n. That convention is asserted, not
	// assumed, because a fix that "corrected" it would otherwise be silent.
	if want, got := "abcde…", truncate(s, len(s)-1); got != want {
		t.Errorf("truncate(%q, %d) = %q, want %q", s, len(s)-1, got, want)
	}
}

// expandTilde had no test at all. Its guard is
//
//	if p != "~" && !strings.HasPrefix(p, "~/")
//
// and dropping the first clause stops a bare "~" from expanding while leaving
// "~/..." working, so an IdentityFile of exactly "~" silently stays literal.
//
// HOME is set for the test rather than read from the environment. expandTilde
// resolves through os.UserHomeDir, which on darwin and linux is $HOME and
// returns an error when it is unset or empty. Pinning HOME keeps that branch
// out of this fixture — it is the subject of the test below instead — and the
// guards here assert the fixture is live rather than assuming it: with a
// degenerate home every assertion would be comparing "~" against "~".
func TestExpandTilde(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v — the fixture is not live and this test would prove nothing", err)
	}
	if home != dir {
		t.Fatalf("UserHomeDir = %q, want the pinned HOME %q", home, dir)
	}
	if home == "~" || home == "" {
		t.Fatalf("home = %q is degenerate; the assertions below would be vacuous", home)
	}

	// A bare tilde is a path in its own right and must expand.
	got, err := expandTilde("~")
	if err != nil {
		t.Fatalf("expandTilde(%q): %v", "~", err)
	}
	if got != home {
		t.Errorf("expandTilde(%q) = %q, want %q", "~", got, home)
	}
	got, err = expandTilde("~/.ssh/id_ed25519")
	if err != nil {
		t.Fatalf("expandTilde(%q): %v", "~/.ssh/id_ed25519", err)
	}
	if want := filepath.Join(home, ".ssh", "id_ed25519"); got != want {
		t.Errorf("expandTilde(%q) = %q, want %q", "~/.ssh/id_ed25519", got, want)
	}

	// Everything else is returned untouched. "~user" in particular is not
	// expanded: ssh resolves it, we deliberately do not, and the guard's second
	// clause is what keeps it literal.
	for _, p := range []string{"~other", "~other/x", "/etc/ssh/config", "relative/path", ""} {
		got, err := expandTilde(p)
		if err != nil {
			t.Errorf("expandTilde(%q): %v, want it returned unchanged with no error", p, err)
			continue
		}
		if got != p {
			t.Errorf("expandTilde(%q) = %q, want it unchanged", p, got)
		}
	}
}

// An unresolvable home directory is reported, not papered over. expandTilde
// used to return p unchanged there, which handed the caller a *relative* path
// containing a directory literally named `~`: it matches nothing an operator
// intended, and parseIncludes' zero-match branch is silent by design, so an
// Include disappeared without a word. The error is the caller's cue to warn and
// skip instead.
//
// Both spellings of a stripped HOME are covered because os.UserHomeDir treats
// them identically — "$HOME is not defined" for each — and only the empty one
// is reachable through t.Setenv alone.
//
// The non-tilde row in each case is the control. It shares the stripped
// environment and must still come back unchanged with a nil error, because the
// guard returns before os.UserHomeDir is ever consulted; without it, an
// expandTilde that simply errored unconditionally would satisfy every other
// assertion here.
func TestExpandTildeReportsAnUnresolvableHome(t *testing.T) {
	cases := []struct {
		name  string
		strip func(t *testing.T)
	}{
		{"empty", func(t *testing.T) { t.Setenv("HOME", "") }},
		{"unset", func(t *testing.T) {
			// t.Setenv first for its restore-on-cleanup; the Unsetenv is the
			// case under test.
			t.Setenv("HOME", "")
			if err := os.Unsetenv("HOME"); err != nil {
				t.Fatalf("unset HOME: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.strip(t)
			// Deliberately not printed: on this workstation the resolved home
			// directory is operator data, and the message is diagnostic without
			// it.
			if _, err := os.UserHomeDir(); err == nil {
				t.Fatal("UserHomeDir still resolves with HOME stripped; the fixture is not live")
			}

			for _, p := range []string{"~", "~/.ssh/conf.d/extra"} {
				got, err := expandTilde(p)
				if err == nil {
					t.Errorf("expandTilde(%q) = %q with no error; an unresolved ~ in a returned path fails silently downstream", p, got)
					continue
				}
				// Empty, not p: a caller that ignores the error must not be
				// able to open a tilde path by accident.
				if got != "" {
					t.Errorf("expandTilde(%q) = %q alongside its error, want \"\"", p, got)
				}
				// The message has to name both the path it could not expand and
				// why, because parseIncludes puts it straight in the footer.
				if !strings.Contains(err.Error(), p) {
					t.Errorf("error %q does not name the path %q", err, p)
				}
				if !strings.Contains(err.Error(), "home directory") {
					t.Errorf("error %q does not name the home directory as the thing that could not be resolved", err)
				}
			}

			// Control: no tilde, so no home directory is needed.
			if got, err := expandTilde("/etc/ssh/ssh_config"); err != nil || got != "/etc/ssh/ssh_config" {
				t.Errorf("expandTilde(%q) = (%q, %v), want it unchanged with no error even without a home directory", "/etc/ssh/ssh_config", got, err)
			}
		})
	}
}

func TestBlockDeclares(t *testing.T) {
	b := block{positive: []string{"*.internal", "nixos-dev"}}
	if !b.declares("nixos-dev") {
		t.Error("declares(nixos-dev) = false, want true")
	}
	if b.declares("box.internal") {
		t.Error("declares(box.internal) = true, want false — only exact patterns declare")
	}
}
