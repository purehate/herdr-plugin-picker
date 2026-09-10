package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestParseIncludeExpansion(t *testing.T) {
	hosts, warns, err := parse(filepath.Join("testdata", "with-include"), "testdata")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	inc := hostByAlias(t, hosts, "from-include")
	if inc.HostName != "10.2.2.2" || inc.User != "included-user" {
		t.Errorf("from-include = %+v", inc)
	}
	if !strings.Contains(inc.SourceFile, filepath.Join("included", "extra")) {
		t.Errorf("SourceFile = %q, want the included file", inc.SourceFile)
	}

	// Hosts on both sides of the Include survive.
	hostByAlias(t, hosts, "local-one")
	hostByAlias(t, hosts, "local-two")

	// A zero-match Include (./missing/nothing-here resolves to nothing) is
	// silent — verified against OpenSSH_10.3p1, ssh does not treat a missing
	// Include target as a misconfiguration.
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none — a zero-match include is silent", warns)
	}
}

// An Include cycle must terminate and still yield the hosts that legitimately
// resolve. cyc-b is deliberately NOT one of them: cyclic-a reaches it through
// an Include nested inside `Host cyc-a`, which does not match "cyc-b", so the
// stanza is parsed under NEVERMATCH and never activates.
//
// Real ssh cannot be used as the oracle for cyc-b directly — a true cycle makes
// it fatal (`Too many recursive configuration includes`) with no resolution
// output at all. So it has no answer here, and applying the NEVERMATCH guard
// uniformly is the only self-consistent choice for a parser that, by design,
// does not fatal. The equivalent acyclic shape was verified against
// OpenSSH_10.3p1: with `Host cyc-a` / `Include cb` where cb declares
// `Host cyc-b` / `Port 2222`, `ssh -G cyc-b` reports `port 22` — identical to a
// wholly undeclared alias — while hoisting cyc-b to top level reports 2222.
func TestParseIncludeCycle(t *testing.T) {
	hosts, _, err := parse(filepath.Join("testdata", "cyclic-a"), "testdata")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	hostByAlias(t, hosts, "cyc-a")

	for _, h := range hosts {
		if h.Alias == "cyc-b" {
			t.Errorf("cyc-b resolved to %+v; it is declared inside an Include "+
				"enclosed by the non-matching `Host cyc-a`, so it is a phantom "+
				"host and must not reach the picker", h)
		}
	}
}

// A shared fragment included from two different Host stanzas must resolve for
// both. Verified against OpenSSH_10.3p1: with `common` = `User shared`,
// `Host a` and `Host b` each `Include common` both report user=shared. A
// global visited set (keyed only by path, scoped to the whole parse) breaks
// this by treating the second stanza's Include as already-handled.
func TestParseIncludeSiblingReinclusion(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "common"), "User shared\n")
	write(t, filepath.Join(dir, "root"),
		"Host a\n  HostName 10.0.0.1\n  Include common\n\n"+
			"Host b\n  HostName 10.0.0.2\n  Include common\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if got := hostByAlias(t, hosts, "a").User; got != "shared" {
		t.Errorf("a User = %q, want shared", got)
	}
	if got := hostByAlias(t, hosts, "b").User; got != "shared" {
		t.Errorf("b User = %q, want shared — a global visited set must not block a legitimate sibling re-include", got)
	}
}

// A file that includes itself must not hang or error — it must stop with a
// warning. ssh has no visited set; a self-include is simply a cycle on the
// current include path, and re-entering an already-open file must produce a
// diagnostic rather than the old silent no-op.
func TestParseIncludeSelfTerminatesWithWarning(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "self"), "Host looper\n  HostName 10.0.0.9\nInclude self\n")

	hosts, warns, err := parse(filepath.Join(dir, "self"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hostByAlias(t, hosts, "looper")
	// Must be caught as a cycle on the current ancestor path, not merely by
	// eventually exhausting the separate depth cap — a self-include is a
	// 1-hop repeat, and the ancestor guard must fire immediately rather than
	// silently falling through to the 16-hop backstop.
	found := false
	for _, w := range warns {
		if strings.Contains(w.Msg, "cycle") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one mentioning a cycle", warns)
	}
}

// Verified against OpenSSH_10.3p1: a chain of Include directives resolves
// through depth 16 and fails at depth 17 with "Too many recursive
// configuration includes". Every intermediate file is a bare Include with no
// Host line of its own, so each hop stays under the root's implicit `Host *`
// and none of them trip the separate NEVERMATCH-guard behavior a nested Host
// stanza would (that is covered by TestParseIncludeHostStanzaRespectsEnclosingRestriction
// and TestParseIncludeSeedKeywordsRespectEnclosingRestrictionAcrossNesting) —
// this test isolates the depth cap itself. "reached" sits at depth 16 (16
// hops from the root) and must resolve; "beyond" is one hop past the cap and
// must not, with a warning left behind instead of a hang or error.
func TestParseIncludeDepthCap(t *testing.T) {
	dir := t.TempDir()
	const bareHops = 16 // chain0..chain15: bare Include, no Host line
	for i := 0; i < bareHops; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("chain%d", i)), fmt.Sprintf("Include chain%d\n", i+1))
	}
	// chain16 sits at depth 16 (16 hops from chain0) and declares the host the
	// cap must still allow. Its own Include chain17 is the 17th hop, which the
	// cap must block.
	write(t, filepath.Join(dir, "chain16"), "Host reached\n  Port 1234\nInclude chain17\n")
	write(t, filepath.Join(dir, "chain17"), "Host beyond\n  Port 9999\n")

	hosts, warns, err := parse(filepath.Join(dir, "chain0"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	reached := hostByAlias(t, hosts, "reached")
	if reached.Port != "1234" {
		t.Errorf("reached = %+v, want Port=1234 — depth 16 must resolve fine", reached)
	}
	for _, h := range hosts {
		if h.Alias == "beyond" {
			t.Fatal("beyond resolved — the depth cap did not stop the 17th include hop")
		}
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Msg, "too many recursive") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one mentioning too many recursive includes", warns)
	}
}

// A `Host` stanza declared inside an included file is still subject to the
// restriction that was active when the Include ran. Verified against
// OpenSSH_10.3p1: with `inc` = `Host other` / `  Port 999`, `Host web1 /
// HostName 10.0.0.1 / Include inc` reports `ssh -G other` -> port 22, not
// 999 — ssh parses the include under SSHCONF_NEVERMATCH because the
// enclosing `Host web1` didn't match "other", so the nested stanza never
// activates. Listing "other" as a pickable host with Port 999 would be a
// phantom target that doesn't match what `ssh other` actually does.
func TestParseIncludeHostStanzaRespectsEnclosingRestriction(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "inc"), "Host other\n  Port 999\n")
	write(t, filepath.Join(dir, "root"), "Host web1\n  HostName 10.0.0.1\n  Include inc\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, h := range hosts {
		if h.Alias == "other" {
			t.Fatalf("other resolved as a phantom host: %+v — the enclosing Host web1 did not match it", h)
		}
	}
	web1 := hostByAlias(t, hosts, "web1")
	if web1.HostName != "10.0.0.1" {
		t.Errorf("web1 = %+v, want HostName=10.0.0.1 — must be unaffected by the guard on the nested stanza", web1)
	}
}

// A nested include's own leading keywords (held in the per-file seed block,
// before any Host line appears) are just as subject to the enclosing
// restriction as a nested Host stanza is. Verified against OpenSSH_10.3p1:
// with `leaf` = `User leafuser`, `mid` = `Host y` / `Port 2222` /
// `Include leaf`, `root` = `Host x` / `Port 1111` / `Include mid`, `ssh -G y`
// reports port 22 and the OS-default user — identical to a wholly undeclared
// alias — because `Host y` is already NEVERMATCH (the enclosing `Host x`
// didn't match "y"), so leaf's Include runs under that same NEVERMATCH and
// its inherited seed must not launder "y" back in as a resolvable alias.
func TestParseIncludeSeedKeywordsRespectEnclosingRestrictionAcrossNesting(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "leaf"), "User leafuser\n")
	write(t, filepath.Join(dir, "mid"), "Host y\n  Port 2222\nInclude leaf\n")
	write(t, filepath.Join(dir, "root"), "Host x\n  Port 1111\nInclude mid\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, h := range hosts {
		if h.Alias == "y" {
			t.Fatalf("y resolved as a phantom host via leaf's inherited seed: %+v — Host y is itself NEVERMATCH under x", h)
		}
	}
	x := hostByAlias(t, hosts, "x")
	if x.Port != "1111" {
		t.Errorf("x = %+v, want Port=1111 — must be unaffected", x)
	}
}

// ssh processes an Include with the enclosing stanza still active, so an
// included file's leading keywords must not become global defaults.
func TestParseIncludeInsideHostDoesNotLeakGlobally(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "inc.conf"), "User workuser\nPort 2022\n")
	write(t, filepath.Join(dir, "root"), "Host work1\n  HostName 10.0.0.1\n  Include inc.conf\n\nHost personal\n  HostName 10.0.0.5\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	work := hostByAlias(t, hosts, "work1")
	if work.User != "workuser" || work.Port != "2022" {
		t.Errorf("work1 = %+v, want the include applied to the enclosing stanza", work)
	}
	personal := hostByAlias(t, hosts, "personal")
	if personal.User != "" {
		t.Errorf("personal User = %q, want empty — the include was scoped to work1", personal.User)
	}
	if personal.Port != "22" {
		t.Errorf("personal Port = %q, want 22 — the include was scoped to work1", personal.Port)
	}
}

// The enclosing stanza's negations cross into the included file along with its
// positive patterns. Verified against OpenSSH_10.3p1: given `Host * !nope`
// wrapping an Include, `ssh -G yes` reports the included User and `ssh -G nope`
// does not.
func TestParseIncludeInheritsNegatedPatterns(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "inc"), "User inherited\n")
	write(t, filepath.Join(dir, "root"), "Host * !nope\n  Include inc\n\nHost nope\n  HostName 10.0.0.9\n\nHost yes\n  HostName 10.0.0.8\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if got := hostByAlias(t, hosts, "nope").User; got != "" {
		t.Errorf("nope User = %q, want empty — !nope must survive the Include", got)
	}
	if got := hostByAlias(t, hosts, "yes").User; got != "inherited" {
		t.Errorf("yes User = %q, want inherited", got)
	}
}

// The include base is fixed, so a nested relative Include resolves beside the
// base and not beside the file that included it.
func TestParseIncludeBaseIsFixedAcrossDepth(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "d"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write(t, filepath.Join(dir, "d", "work"), "Include shared\n")
	write(t, filepath.Join(dir, "shared"), "Host shared-host\n  HostName 10.8.8.8\n")
	write(t, filepath.Join(dir, "root"), "Include d/work\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if got := hostByAlias(t, hosts, "shared-host").HostName; got != "10.8.8.8" {
		t.Errorf("shared-host HostName = %q, want 10.8.8.8", got)
	}
}

// Parse itself must use ~/.ssh as the base, not the config's own directory.
// A negative check alone (a sibling of the config file must not resolve) is
// consistent with "the base is ~/.ssh" but equally consistent with "the base
// is anything other than the config's own directory" — it never actually
// proves ~/.ssh is the base used. t.Setenv redirects HOME to a temp dir for
// this test only (Go's testing package restores it afterward, so the real
// ~/.ssh is never read or written) and plants a distinctively-keyworded file
// at <fake-home>/.ssh/, giving a positive signal: the Include must resolve it,
// which only happens if Parse computed the base as $HOME/.ssh.
func TestParseUsesSSHDirAsIncludeBase(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	sshDir := filepath.Join(fakeHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const distinctiveHost = "herdr-ssh-test-real-base-proof"
	write(t, filepath.Join(sshDir, "from-real-base"), "Host "+distinctiveHost+"\n  Port 4242\n")

	dir := t.TempDir()
	const sibling = "herdr-ssh-test-sibling-absent"
	write(t, filepath.Join(dir, sibling), "Host sibling-host\n")
	write(t, filepath.Join(dir, "root"), "Include from-real-base\nInclude "+sibling+"\n")

	hosts, warns, err := Parse(filepath.Join(dir, "root"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Positive: the Include resolved against ~/.ssh (now the faked HOME),
	// proven by a distinctive keyword from the file that was actually reached.
	proof := hostByAlias(t, hosts, distinctiveHost)
	if proof.Port != "4242" {
		t.Errorf("%s = %+v, want Port=4242 from the real-base include", distinctiveHost, proof)
	}

	// Negative, kept alongside: a sibling of the config file's OWN directory
	// must not resolve — the base is ~/.ssh, never the including file's dir.
	for _, h := range hosts {
		if h.Alias == "sibling-host" {
			t.Fatal("Parse resolved a relative include against the config's own directory")
		}
	}
	// The zero-match include itself is silent (see TestParseIncludeExpansion).
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none — a zero-match include is silent", warns)
	}
}

// A keyword set before an Include must survive a keyword set after it, in
// the same stanza. This pins the resumed.keys = nil reset in parseFile's
// "include" case: cur is copied into blocks just before the reset, and a
// struct copy shares the keys slice's backing array, so resetting via
// cur.keys[:0] instead of nil would let a later append in this stanza reuse
// that array and silently overwrite index 0 of the copy already sitting in
// blocks — corrupting the pre-Include keyword.
func TestParseIncludeResumeDoesNotAliasPriorKeys(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"), "Host x\n  User a\n  Include nothing-here\n  Port 100\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none — the include is a silent zero-match", warns)
	}
	x := hostByAlias(t, hosts, "x")
	if x.User != "a" {
		t.Errorf("x User = %q, want a — a keyword before Include must survive a keyword set after it", x.User)
	}
	if x.Port != "100" {
		t.Errorf("x Port = %q, want 100", x.Port)
	}
}

// A present-but-unreadable Include target is a real misconfiguration, unlike
// a merely-absent one. Verified against OpenSSH_10.3p1: ssh reports "Can't
// open user config file <path>: Permission denied" and exits 255. We stay
// non-fatal but must warn.
//
// This pins behavior that was previously untested, not behavior that was
// previously broken — Parse already warned here. The test exists so that
// "unreadable" cannot later be folded into the "absent" branch, which is
// silent by design and would swallow the warning without failing anything.
func TestParseIncludeUnreadableFileWarns(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 000 does not block reads")
	}
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	write(t, blocked, "Host from-blocked\n  HostName 10.9.9.1\n")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o600); err != nil {
			t.Fatalf("cleanup chmod: %v", err)
		}
	})
	write(t, filepath.Join(dir, "root"), "Include blocked\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, h := range hosts {
		if h.Alias == "from-blocked" {
			t.Fatal("resolved a host from an unreadable include")
		}
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w.Msg, "include unreadable") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one mentioning include unreadable", warns)
	}
}

// A malformed Include pattern is a warning; an Include that simply matches
// nothing is silent. Both branches are asserted together, because each is the
// other's control.
//
// parseIncludes distinguishes them by filepath.Glob's error, and only the error
// branch warns:
//
//	matches, err := filepath.Glob(pattern)
//	if err != nil { warn; continue }   // malformed pattern
//	if len(matches) == 0 { continue }  // absent: silent by design
//
// Glob returns zero matches in both cases, so a fixture that merely fails to
// match reaches the silent branch and proves nothing about the warning — and
// dropping the warn, or checking len(matches) first so a bad pattern takes the
// silent path, both survived the suite. The unterminated "[" is what separates
// them: filepath.Glob is a real glob, unlike matchPattern in Exclude where "["
// is deliberately literal, so "frag[" is a syntax error rather than a filename.
//
// The warning carries the pattern as written. That is asserted rather than
// matched loosely, because it is also what identifies the branch: the two other
// "include unreadable" warnings in parseIncludes are built from a resolved
// match, which could not contain the unterminated bracket.
func TestParseIncludeMalformedPatternWarnsButAbsentIsSilent(t *testing.T) {
	t.Run("malformed pattern warns", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "root"), "Host keeper\n  HostName 10.4.4.4\nInclude frag[\n")

		hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		// One bad include must not cost the operator the rest of their hosts.
		if got := hostByAlias(t, hosts, "keeper").HostName; got != "10.4.4.4" {
			t.Errorf("keeper HostName = %q, want 10.4.4.4", got)
		}
		if len(warns) != 1 {
			t.Fatalf("warnings = %v, want exactly one", warns)
		}
		if want := "include unreadable: frag["; warns[0].Msg != want {
			t.Errorf("warning = %q, want %q — the pattern as written identifies the ErrBadPattern branch", warns[0].Msg, want)
		}
		if warns[0].Line != 3 {
			t.Errorf("warning line = %d, want 3", warns[0].Line)
		}
	})

	t.Run("absent include is silent", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "root"), "Host keeper\n  HostName 10.4.4.4\nInclude nothing-here*\n")

		hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		hostByAlias(t, hosts, "keeper")
		// Verified against OpenSSH_10.3p1: a zero-match Include is not a
		// misconfiguration. This is the control for the case above — it shows
		// the warning there comes from the malformed pattern and not merely
		// from having matched no files.
		if len(warns) != 0 {
			t.Errorf("warnings = %v, want none — an absent Include is silent by design", warns)
		}
	})
}

// An Include this package cannot resolve because there is no home directory
// warns and is skipped, instead of being globbed against the working directory.
//
// Two shapes reach it, and they used to be two halves of the same silence:
//
//   - `Include conf.d/*` is relative, so it resolves against the include base,
//     which ssh_config(5) fixes at ~/.ssh for a user config. With the home
//     directory unresolvable there is no base.
//   - `Include ~/conf.d/*` carries a tilde expandTilde cannot expand.
//
// Before the fix, expandTilde returned its input unchanged on that error, so
// the first globbed "~/.ssh/conf.d/*" and the second "~/.ssh/~/conf.d/*" —
// both *relative* paths containing a directory literally named `~`, both
// matching nothing, and both therefore landing on the zero-match branch above,
// which is silent by design. The operator lost every host in the included file
// with nothing on screen saying so. Worse, a relative pattern is joined onto
// the base, and an empty base leaves it relative to the process working
// directory: in a directory the operator did not author, `conf.d/*` would have
// been read as config.
//
// Exported Parse, not parse: the include base is the thing under test here, and
// only Parse derives it from the home directory. That is also the only way this
// path is reachable at all — cmd/herdr-ssh stops at sshConfigPath returning ""
// before it ever calls Parse, so what is pinned here is the package's own
// contract for a caller that supplies a root explicitly.
func TestParseWarnsOnAnIncludeItCannotResolveWithoutHome(t *testing.T) {
	cases := []struct{ name, include string }{
		{"relative include has no base", "conf.d/*"},
		{"tilde include cannot expand", "~/conf.d/*"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", "")
			// The fixture must be live: with a working home directory both
			// patterns resolve and every assertion below measures nothing.
			if _, err := os.UserHomeDir(); err == nil {
				t.Fatal("UserHomeDir still resolves with HOME emptied; the fixture is not live")
			}
			dir := t.TempDir()
			root := filepath.Join(dir, "root")
			write(t, root, "Include "+tc.include+"\nHost alpha\n  HostName 10.0.0.1\n")

			hosts, warns, err := Parse(root)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			// One unresolvable include must not cost the rest of the config.
			if got := hostByAlias(t, hosts, "alpha").HostName; got != "10.0.0.1" {
				t.Errorf("alpha HostName = %q, want 10.0.0.1", got)
			}
			if len(warns) != 1 {
				t.Fatalf("warnings = %v, want exactly one — a zero-match glob is silent by design, so silence is the defect this pins", warns)
			}
			// Content, not just a count: the pattern as written is what lets the
			// operator find the Include line, and "home directory" is the only
			// part that says why it could not be resolved. A warning naming
			// neither would satisfy the count above.
			if !strings.Contains(warns[0].Msg, tc.include) {
				t.Errorf("warning = %q, want it to name the pattern %q as written", warns[0].Msg, tc.include)
			}
			if !strings.Contains(warns[0].Msg, "home directory") {
				t.Errorf("warning = %q, want it to name the home directory as the thing that could not be resolved", warns[0].Msg)
			}
			if warns[0].Line != 1 {
				t.Errorf("warning line = %d, want 1", warns[0].Line)
			}
		})
	}
}
