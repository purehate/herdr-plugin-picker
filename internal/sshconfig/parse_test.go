package sshconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func hostByAlias(t *testing.T, hosts []Host, alias string) Host {
	t.Helper()
	for _, h := range hosts {
		if h.Alias == alias {
			return h
		}
	}
	t.Fatalf("alias %q not found in %d hosts", alias, len(hosts))
	return Host{}
}

func TestParseBasic(t *testing.T) {
	hosts, warns, err := Parse(filepath.Join("testdata", "basic"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}

	var aliases []string
	for _, h := range hosts {
		aliases = append(aliases, h.Alias)
	}
	want := []string{"nixos-dev", "build", "box", "web1", "jumped"}
	if len(aliases) != len(want) {
		t.Fatalf("aliases = %v, want %v", aliases, want)
	}
	for i := range want {
		if aliases[i] != want[i] {
			t.Fatalf("aliases = %v, want %v", aliases, want)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	dev := hostByAlias(t, hosts, "nixos-dev")
	if dev.HostName != "192.0.2.10" || dev.User != "operator" || dev.Port != "22" {
		t.Errorf("nixos-dev = %+v", dev)
	}
	if dev.SourceLine == 0 || dev.SourceFile == "" {
		t.Errorf("nixos-dev missing provenance: %+v", dev)
	}
	// nixos-dev sets its own IdentityFile, which must win over `Host *`'s, and
	// the leading ~/ must be expanded against the real home directory.
	if want := filepath.Join(home, ".ssh", "id_nixos"); dev.IdentityFile != want {
		t.Errorf("nixos-dev IdentityFile = %q, want %q", dev.IdentityFile, want)
	}

	// `Host build box` is two aliases sharing one stanza.
	if got := hostByAlias(t, hosts, "box").HostName; got != "10.0.0.12" {
		t.Errorf("box HostName = %q, want 10.0.0.12", got)
	}

	// First value wins within a stanza. Against `Host *` the winner is decided
	// by file order, and this fixture keeps the wildcard last, as ssh expects.
	if got := hostByAlias(t, hosts, "web1").User; got != "root" {
		t.Errorf("web1 User = %q, want root (first value wins)", got)
	}

	// jumped declares no User of its own, and the trailing `Match exec` stanza
	// must be skipped rather than contributing "never-applied" — only the
	// trailing `Host *` default should reach it.
	jumped := hostByAlias(t, hosts, "jumped")
	if jumped.User != "fallback" {
		t.Errorf("jumped User = %q, want fallback — Match keywords must not apply", jumped.User)
	}
	// `Host *` supplies defaults but is not selectable, and its ~/ must expand
	// against the real home directory too.
	if want := filepath.Join(home, ".ssh", "id_default"); jumped.IdentityFile != want {
		t.Errorf("jumped IdentityFile = %q, want %q (the Host * default, expanded)", jumped.IdentityFile, want)
	}
	if jumped.ProxyJump != "bastion" {
		t.Errorf("jumped ProxyJump = %q, want bastion", jumped.ProxyJump)
	}

	// Unset HostName falls back to the alias; unset Port defaults to 22.
	hosts2, _, err := Parse(filepath.Join("testdata", "minimal"))
	if err != nil {
		t.Fatalf("Parse minimal: %v", err)
	}
	only := hosts2[0]
	if only.HostName != "solo" || only.Port != "22" {
		t.Errorf("minimal host = %+v, want HostName=solo Port=22", only)
	}
}

// IdentityFile keeps the operator's own spelling when the home directory cannot
// be resolved, and raises no warning for it.
//
// This one does not fail against the pre-fix code — expandTilde returned p
// unchanged then, which is the same value by a different route. It is here for
// the mutant the fix makes possible: expandTilde now returns "" with its error,
// so the obvious spelling of the call site,
//
//	h.IdentityFile, _ = expandTilde(values["identityfile"])
//
// silently blanks the field, and the preview panel — the only thing that reads
// IdentityFile, since `ssh <alias>` resolves the keyword itself — would stop
// showing a key the operator's config plainly sets. Nothing in this program
// ever opens the path, so there is no glob or read to fail closed about, and
// showing `~/.ssh/id_alpha` is the honest answer to "what does your config
// say". Two-sided via the warning count: this call site must not start
// reporting a problem the operator cannot act on either.
func TestParseKeepsALiteralIdentityFileWhenHomeIsUnresolvable(t *testing.T) {
	t.Setenv("HOME", "")
	// The fixture must be live, or IdentityFile expands and this measures the
	// ordinary path instead.
	if _, err := os.UserHomeDir(); err == nil {
		t.Fatal("UserHomeDir still resolves with HOME emptied; the fixture is not live")
	}

	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	write(t, root, "Host alpha\n  HostName 10.0.0.1\n  IdentityFile ~/.ssh/id_alpha\n")

	hosts, warns, err := Parse(root)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none — an IdentityFile is displayed, never opened here", warns)
	}
	if got, want := hostByAlias(t, hosts, "alpha").IdentityFile, "~/.ssh/id_alpha"; got != want {
		t.Errorf("alpha IdentityFile = %q, want the config's own spelling %q", got, want)
	}
}

func TestParseResolvesProxyCommandAndNormalizesDisabledProxies(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	write(t, root,
		"Host command-proxy\n  ProxyCommand ssh gateway -W %h:%p\n"+
			"Host direct\n  ProxyCommand NONE\n  ProxyJump none\n")

	hosts, warns, err := parse(root, dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings = %v, want none", warns)
	}
	if got := hostByAlias(t, hosts, "command-proxy").ProxyCommand; got != "ssh gateway -W %h:%p" {
		t.Errorf("ProxyCommand = %q, want the configured command", got)
	}
	direct := hostByAlias(t, hosts, "direct")
	if direct.ProxyCommand != "" || direct.ProxyJump != "" {
		t.Errorf("direct proxies = (%q, %q), want both disabled by none", direct.ProxyCommand, direct.ProxyJump)
	}
}

func TestParseMissingFile(t *testing.T) {
	_, _, err := Parse(filepath.Join("testdata", "does-not-exist"))
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("err = %v, want ErrNoConfig", err)
	}
}

// A wildcard that precedes a literal stanza wins, because ssh takes the first
// obtained value and does not rank literal stanzas above patterns. Operators
// who put `Host *` first really do get the wildcard's values, and the picker
// must show what ssh will actually do.
func TestParseWildcardBeforeLiteralWins(t *testing.T) {
	hosts, _, err := Parse(filepath.Join("testdata", "wildcard-first"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	h := hostByAlias(t, hosts, "late")
	if h.User != "fallback" {
		t.Errorf("late User = %q, want fallback — earlier `Host *` must win", h.User)
	}
	if h.Port != "2200" {
		t.Errorf("late Port = %q, want 2200 — earlier `Host *` must win", h.Port)
	}
	if h.HostName != "10.3.3.3" {
		t.Errorf("late HostName = %q, want 10.3.3.3 — wildcard sets no HostName", h.HostName)
	}
	if h.SourceLine == 0 {
		t.Error("late lost its provenance; the literal stanza still declares it")
	}
}

// A literal alias containing brackets must remain connectable: ssh_config
// PATTERNS has no bracket character classes, so "web[12]" is not a wildcard
// and must not be filtered out of the picker as one.
func TestParseBracketAliasIsLiteral(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"), "Host web[12]\n  HostName 10.4.4.4\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h := hostByAlias(t, hosts, "web[12]")
	if h.HostName != "10.4.4.4" {
		t.Errorf("web[12] HostName = %q, want 10.4.4.4", h.HostName)
	}
}

// A mid-string `!` is literal, so the alias stays connectable, while a leading
// `!` still negates. Verified against OpenSSH_10.3p1: `ssh -G foo!bar` reports
// user=banguser, and `!nodefault` keeps the trailing `Host *` User away from
// nodefault while `other` receives it.
func TestParseBangAliasIsLiteralAndNegationStillApplies(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"),
		"Host foo!bar\n  HostName 10.9.9.9\n  User banguser\n\n"+
			"Host nodefault\n  HostName 10.9.9.8\n\n"+
			"Host * !nodefault\n  User fallback\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	bang := hostByAlias(t, hosts, "foo!bar")
	if bang.HostName != "10.9.9.9" || bang.User != "banguser" {
		t.Errorf("foo!bar = %+v, want HostName=10.9.9.9 User=banguser", bang)
	}

	// The negation keeps `Host *`'s User away from nodefault without removing
	// nodefault from the picker.
	nd := hostByAlias(t, hosts, "nodefault")
	if nd.User != "" {
		t.Errorf("nodefault User = %q, want empty — !nodefault must exclude it", nd.User)
	}

	if len(hosts) != 2 {
		t.Errorf("hosts = %+v, want exactly foo!bar and nodefault", hosts)
	}
}

// Verified against OpenSSH_10.3p1: `Host f1 # my dev box` with `HostName` and
// `Port` lines each carrying their own trailing comment declares exactly one
// host, f1, with a clean HostName/Port — not five words split on whitespace.
func TestParseStripsTrailingComments(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"),
		"Host f1 # my dev box\n"+
			"  HostName 10.0.0.6 # the ip\n"+
			"  Port 2222 # work vpn\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts = %+v, want exactly one (f1)", hosts)
	}
	f1 := hostByAlias(t, hosts, "f1")
	if f1.HostName != "10.0.0.6" || f1.Port != "2222" {
		t.Errorf("f1 = %+v, want HostName=10.0.0.6 Port=2222", f1)
	}
}

// An alias declared by more than one stanza is one host, resolved first-wins,
// and its provenance points at the first declaration.
//
// Every field this asserts was already pinned individually — resolve's dedup by
// TestParseBasic's `Host build box`, first-obtained-value by
// TestParseWildcardBeforeLiteralWins, SourceFile/SourceLine being set at all by
// TestParseBasic and TestParseIncludeExpansion — and the combination was pinned
// nowhere, because no fixture in the package declared the same alias twice.
// Two mutations survived the whole suite on that gap: dropping resolve's seen[]
// dedup (the alias is emitted once per declaring stanza, so the picker shows a
// duplicate row), and dropping resolveHost's `h.SourceFile == ""` guard (the
// last declaration wins, so "reveal in config" jumps to the wrong stanza). A
// fixture with one stanza per alias cannot distinguish either mutation from the
// original: with nothing declared twice, the dedup never dedupes and the guard
// never guards. Both are one-line changes to hot code and neither turns a test
// red without this fixture.
func TestParseDuplicateAliasIsOneHostResolvedFirstWins(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"),
		"Host dup\n"+ // line 1 — the first declaration
			"  HostName 10.0.0.1\n"+
			"Host other\n"+
			"  HostName 10.0.0.2\n"+
			"Host dup\n"+ // line 5 — the second, and the one a last-wins bug picks
			"  HostName 10.9.9.9\n"+
			"  User second\n")

	hosts, _, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Declaration order, each alias once. Asserted as the full list rather than
	// by lookup: hostByAlias returns the first match, so a duplicated entry is
	// invisible to it.
	var aliases []string
	for _, h := range hosts {
		aliases = append(aliases, h.Alias)
	}
	if len(aliases) != 2 || aliases[0] != "dup" || aliases[1] != "other" {
		t.Fatalf("aliases = %v, want [dup other] — a second `Host dup` must not add a row", aliases)
	}

	dup := hostByAlias(t, hosts, "dup")
	if dup.HostName != "10.0.0.1" {
		t.Errorf("dup HostName = %q, want 10.0.0.1 — the first stanza's value wins", dup.HostName)
	}
	// The later stanza is not shadowed, it just loses the keys the first one
	// already set. Without this, a bug that ignored every declaration after the
	// first would pass the two checks above.
	if dup.User != "second" {
		t.Errorf("dup User = %q, want second — a later stanza still supplies keys the first omitted", dup.User)
	}
	if dup.SourceLine != 1 {
		t.Errorf("dup SourceLine = %d, want 1 — provenance is the first declaration, not the last", dup.SourceLine)
	}
}

// The same first-declaration rule across an Include boundary, which is what
// makes SourceFile — not just SourceLine — load-bearing. Within one file the
// two declarations share a path, so a last-wins bug is visible only in the line
// number; here the wrong answer is a different file, which is what "reveal in
// config" would open.
//
// The Include is at top level and the *included* file holds the first
// declaration, so the two stanzas differ in their file and not only their line.
// Both details were measured rather than assumed, and the obvious arrangement
// fails on both counts:
//
//   - Include last, inside `Host dup`. parseFile appends cur to blocks before
//     descending (sshconfig.go:328) and again when the file ends (:347), so a
//     stanza containing an Include appears on both sides of the included
//     blocks. The last block declaring dup is then the root's own, and a
//     last-wins bug still reports the root file — green either way, pinning
//     nothing.
//   - Include last, inside a different `Host carrier` stanza. That fixes the
//     ordering but makes the included `Host dup` inert: ssh processes an
//     Include with the caller's stanza still in effect, so it inherits carrier
//     as a NEVERMATCH guard and contributes nothing. Correct behaviour, useless
//     fixture.
func TestParseDuplicateAliasAcrossIncludeKeepsFirstSourceFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "extra"), "Host dup\n  User second\n")
	write(t, filepath.Join(dir, "root"),
		"Include extra\n"+
			"Host dup\n"+
			"  HostName 10.0.0.1\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings = %v, want none", warns)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts = %+v, want exactly one (dup)", hosts)
	}

	// Each file supplies a keyword the other omits, so both stanzas are shown
	// to contribute. Without this the test could pass while the included
	// stanza was being dropped entirely.
	dup := hostByAlias(t, hosts, "dup")
	if dup.User != "second" {
		t.Errorf("dup User = %q, want second — the included stanza supplies it", dup.User)
	}
	if dup.HostName != "10.0.0.1" {
		t.Errorf("dup HostName = %q, want 10.0.0.1 — the root stanza supplies it", dup.HostName)
	}

	wantFile, err := filepath.Abs(filepath.Join(dir, "extra"))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if dup.SourceFile != wantFile {
		t.Errorf("dup SourceFile = %q, want %q — provenance is the first declaration, which is in the included file", dup.SourceFile, wantFile)
	}
	if dup.SourceLine != 1 {
		t.Errorf("dup SourceLine = %d, want 1", dup.SourceLine)
	}
}

// A malformed line (no key/value separator, or an empty value) is a warning,
// not silently dropped — the operator should know a line in their config
// didn't parse instead of quietly losing it.
func TestParseWarnsOnMalformedLines(t *testing.T) {
	hosts, warns, err := Parse(filepath.Join("testdata", "malformed"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := hostByAlias(t, hosts, "good").HostName; got != "10.5.5.5" {
		t.Errorf("good HostName = %q, want 10.5.5.5 — malformed lines must not break the rest of the file", got)
	}

	if len(warns) != 2 {
		t.Fatalf("warnings = %v, want exactly 2", warns)
	}
	wantLines := map[int]bool{4: false, 5: false}
	for _, w := range warns {
		if _, ok := wantLines[w.Line]; !ok {
			t.Errorf("unexpected warning line %d: %+v", w.Line, w)
			continue
		}
		wantLines[w.Line] = true
	}
	for line, seen := range wantLines {
		if !seen {
			t.Errorf("missing warning for malformed line %d", line)
		}
	}
}

// Verified against OpenSSH_10.3p1: HostName "10.0.0.5 (an odd number of `"`
// characters) is fatal — ssh reports "invalid quotes" and refuses the whole
// file. We stay non-fatal, but the operator must be told: silently
// accepting our best-effort value would otherwise render a healthy-looking
// picker for a config ssh itself would reject.
func TestParseWarnsOnUnbalancedQuotes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "root"), "Host odd\n  HostName \"10.0.0.5\n")

	hosts, warns, err := parse(filepath.Join(dir, "root"), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hostByAlias(t, hosts, "odd")

	found := false
	for _, w := range warns {
		if strings.Contains(w.Msg, "unbalanced quotes") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one mentioning unbalanced quotes", warns)
	}
}

// Warning.File is absolute regardless of how the config was named, because it
// is the only thing identifying the file to the operator.
//
// This is a cross-package contract with nothing holding it up on either side.
// Warning.String() renders "%s:%d: %s" from File, and cmd/herdr-picker/hosts.go
// appends that string to the picker footer verbatim — no path prefix of its
// own, deliberately: 87ecc8a removed the prefix it used to add because the
// path was already in the rendered string, and the file was being named twice.
// So if File ever became the caller's spelling of the path, footer warnings
// would silently degrade to whatever the operator typed — a bare "config:7:"
// for `herdr-picker --config config` — and hosts_test.go would not notice, since
// its expectations are built from the same fixture path it passes in. Both
// halves stay green while the operator loses the ability to tell which file
// warned. Pinned here, where the contract is produced.
//
// The root is named by a genuinely relative path for that reason: parseFile
// absolutizes via filepath.Abs (sshconfig.go:258) and every Warning in the
// package draws its File from that one value, so passing an already-absolute
// path would make the assertion vacuous.
func TestParseWarningFileIsAbsoluteFromARelativeRoot(t *testing.T) {
	dir := t.TempDir()
	// A directory, not chmod 000, for the reason invariant_test.go gives: chmod
	// 000 is still readable as uid 0, so that fixture would prove nothing in a
	// root container.
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", blocked, err)
	}
	root := filepath.Join(dir, "root")
	write(t, root,
		"Host ok\n"+
			"  HostName 10.0.0.1\n"+
			"OrphanedKeyword\n"+ // parseFile's warning, built from abs
			"Include "+blocked+"\n") // parseIncludes' warning, built from parent

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	rel, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	if filepath.IsAbs(rel) {
		t.Fatalf("rel = %q is absolute; the fixture cannot test absolutisation", rel)
	}

	hosts, warns, err := parse(rel, dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hostByAlias(t, hosts, "ok")

	wantFile, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}

	// Both warning-construction families, asserted by name so this cannot pass
	// on an empty or one-sided warning list: parseFile builds Warning{File: abs}
	// and parseIncludes builds Warning{File: parent}. They agree today because
	// parent is passed that same abs, and that agreement is the thing under
	// test — a future refactor that let either one carry the caller's spelling
	// would fail here.
	var sawMalformed, sawInclude bool
	for _, w := range warns {
		switch {
		case strings.Contains(w.Msg, "malformed line"):
			sawMalformed = true
		case strings.Contains(w.Msg, "include unreadable"):
			sawInclude = true
		default:
			t.Errorf("unexpected warning %+v", w)
			continue
		}
		if !filepath.IsAbs(w.File) {
			t.Errorf("Warning.File = %q is not absolute; the footer cannot name the file", w.File)
		}
		if w.File != wantFile {
			t.Errorf("Warning.File = %q, want %q", w.File, wantFile)
		}
	}
	if !sawMalformed || !sawInclude {
		t.Fatalf("warnings = %v, want one malformed-line and one unreadable-include warning", warns)
	}
}

// Forwarding keywords are additive: ssh applies every occurrence, so the
// first-wins map that resolves the other keywords would silently drop all but
// the first of each. The order within a keyword is the config order.
func TestParseCollectsRepeatedPortForwards(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	write(t, root,
		"Host tunnel\n  HostName 10.0.0.1\n"+
			"  LocalForward 8080 localhost:80\n"+
			"  LocalForward 9090 localhost:90\n"+
			"  RemoteForward 3000 localhost:3000\n"+
			"  DynamicForward 1080\n"+
			"  DynamicForward 1081\n")

	hosts, warns, err := parse(root, dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings = %v, want none", warns)
	}
	h := hostByAlias(t, hosts, "tunnel")
	if want := []string{"8080 localhost:80", "9090 localhost:90"}; !reflect.DeepEqual(h.LocalForward, want) {
		t.Errorf("LocalForward = %v, want %v", h.LocalForward, want)
	}
	if want := []string{"3000 localhost:3000"}; !reflect.DeepEqual(h.RemoteForward, want) {
		t.Errorf("RemoteForward = %v, want %v", h.RemoteForward, want)
	}
	if want := []string{"1080", "1081"}; !reflect.DeepEqual(h.DynamicForward, want) {
		t.Errorf("DynamicForward = %v, want %v", h.DynamicForward, want)
	}
}

func TestParseWildcardForwardAppliesToNamedHost(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	write(t, root, "Host web\n  HostName 10.0.0.2\nHost *\n  LocalForward 1080 localhost:1080\n")

	hosts, _, err := parse(root, dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := hostByAlias(t, hosts, "web").LocalForward; len(got) != 1 || got[0] != "1080 localhost:1080" {
		t.Fatalf("web LocalForward = %v, want the Host * forward", got)
	}
}
