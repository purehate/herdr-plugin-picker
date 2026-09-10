package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

func writeSSHConfig(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// Include is the only way a second file reaches the picker, so it is the only
// multi-file arrangement worth pinning here. The alias is what gets exec'd —
// `ssh <alias>` with no -F — so ssh resolves it against the operator's own
// config and its Include chain and nothing else. A host sourced from anywhere
// outside that chain would render a row the picker can describe and ssh cannot
// reach, silently connecting somewhere other than the preview says.
//
// Include sits at the top, before the first Host stanza, because that is the
// only position that makes the included stanzas live: ssh processes an Include
// with the caller's active block still in effect, so one nested inside `Host
// colima` inherits it as a guard and every alias inside is a phantom.
func TestLoadHostsFoldsInIncludedHostsAndHidesGlobs(t *testing.T) {
	included := writeSSHConfig(t, "work", "Host client-jump\n  HostName 10.9.9.9\n\nHost old-box\n  HostName 10.9.9.10\n")
	primary := writeSSHConfig(t, "config", "Include "+included+"\n\nHost nixos-dev\n  HostName 10.0.0.1\n\nHost colima\n  HostName 127.0.0.1\n")

	cfg := pluginconfig.Defaults()
	cfg.Hidden = []string{"colima", "*-box"}

	hosts, warns := loadHosts(primary, cfg)
	var got []string
	for _, h := range hosts {
		got = append(got, h.Alias)
	}
	// Declaration order, which puts the included file's surviving host first.
	// Asserted as the whole list rather than by lookup: that is what shows the
	// hidden pair was dropped from both files and not merely reordered.
	want := "client-jump nixos-dev"
	if strings.Join(got, " ") != want {
		t.Fatalf("aliases = %v, want %q", got, want)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
}

func TestLoadHostsTreatsAMissingPrimaryConfigAsEmpty(t *testing.T) {
	hosts, warns := loadHosts(filepath.Join(t.TempDir(), "absent"), pluginconfig.Defaults())
	if len(hosts) != 0 {
		t.Fatalf("hosts = %v, want none", hosts)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none — a missing config is normal", warns)
	}
}

func TestLoadHostsWarnsWhenThePrimaryConfigIsUnreadable(t *testing.T) {
	// The primary config's third arm: the file exists and cannot be read.
	// ErrNoConfig (absent) is silent by design and a malformed line is a parse
	// warning, but an unreadable primary has to reach the footer — otherwise an
	// empty picker is indistinguishable from having no hosts configured, and
	// the operator's own config is the one thing they will not think to suspect.
	//
	// A directory at the config path rather than chmod 000, deliberately.
	// chmod 000 is still readable as uid 0, so that fixture passes for the
	// wrong reason in a root container while proving nothing about the
	// permission check — and CI runs ubuntu-latest and macos-latest. A
	// directory fails the read for every uid ("is a directory"), so this test
	// means the same thing wherever it runs. ErrNoConfig is reserved for
	// os.ErrNotExist, so this lands on the error arm and not the silent one.
	primary := filepath.Join(t.TempDir(), "config")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", primary, err)
	}

	hosts, warns := loadHosts(primary, pluginconfig.Defaults())
	if len(hosts) != 0 {
		t.Fatalf("hosts = %+v, want none", hosts)
	}
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warns)
	}
	// Exactly once, not Contains. The reachable error here is os.ReadFile's
	// *fs.PathError, which already embeds the path, so prefixing it again
	// printed the file twice in the footer — and a Contains check is blind to
	// that, passing at one occurrence and at two. The count is two-sided on
	// purpose: it also fails at zero, which is the case that matters if
	// sshconfig ever stops embedding the path, because then the prefix-less
	// version silently loses which file failed.
	if n := strings.Count(warns[0], primary); n != 1 {
		t.Fatalf("path appears %d times in %q, want once", n, warns[0])
	}
}

func TestTargetsForJoinsHostAndPortAndSkipsProxyJump(t *testing.T) {
	hosts := []sshconfig.Host{
		{Alias: "a", HostName: "10.0.0.1", Port: "22"},
		{Alias: "b", HostName: "10.0.0.2", Port: "2222"},
		{Alias: "c", HostName: "10.0.0.3", Port: "22", ProxyJump: "a"},
		{Alias: "v6", HostName: "::1", Port: "22"},
	}
	got := targetsFor(hosts)
	if len(got) != 4 {
		t.Fatalf("targets = %+v", got)
	}
	if got[0].Addr != "10.0.0.1:22" || got[1].Addr != "10.0.0.2:2222" {
		t.Errorf("addrs = %q, %q", got[0].Addr, got[1].Addr)
	}
	// The IPv6 row is why this uses net.JoinHostPort and not h.HostName+":"+h.Port.
	// For every IPv4 fixture above the two are byte-identical, so IPv6 is the
	// only case that tells them apart: concatenation yields "::1:22", which
	// net.Dial rejects with "too many colons in address", so probe.run reports
	// Up: false and every IPv6 host renders a false down. Assert the bracketed
	// form specifically — a strings.Contains(addr, "::1") check passes under
	// the concatenating version and proves nothing.
	if got[3].Addr != "[::1]:22" {
		t.Errorf("IPv6 addr = %q, want %q", got[3].Addr, "[::1]:22")
	}
	if got[2].Skip != true {
		t.Error("a ProxyJump host was not skipped — probing it would dial the wrong network")
	}
	if got[0].Skip || got[1].Skip {
		t.Error("direct hosts were skipped")
	}
}

func TestTargetsForSkipsUnexpandedPercentTokens(t *testing.T) {
	// ssh expands %h, %p, %r and the rest at connect time; this picker does not.
	// So the literal token is what gets dialed, the lookup cannot resolve, and
	// probe.run reports Up: false — a host ssh reaches perfectly well renders as
	// down. That is the expensive direction for this marker to fail in: the
	// operator skips a live host on the picker's word.
	//
	// Asserted through Skip rather than through the rendered glyph because Skip
	// is the thing that suppresses the dial. probe.run continues past a skipped
	// target and emits no Result at all, so the row stays unprobed rather than
	// being handed a wrong answer.
	hosts := []sshconfig.Host{
		{Alias: "tok", HostName: "%h.example.invalid", Port: "22"},
		{Alias: "esc", HostName: "literal%%percent.example.invalid", Port: "22"},
		{Alias: "plain", HostName: "10.0.0.1", Port: "22"},
	}
	got := targetsFor(hosts)
	if len(got) != 3 {
		t.Fatalf("targets = %+v", got)
	}
	if !got[0].Skip {
		t.Errorf("%q was probed; an unexpanded token cannot resolve, so it renders a false down", got[0].Addr)
	}
	// `%%` is ssh's escape for a literal percent, which is not a resolvable name
	// either — RFC 1123 has no `%` in a hostname. Both spellings are covered by
	// the same check on purpose; a token-shaped pattern like `%[a-z]` would pass
	// the row above and still dial this one.
	if !got[1].Skip {
		t.Errorf("%q was probed; an escaped percent is not resolvable either", got[1].Addr)
	}
	// Two-sided. A Skip hardcoded to true satisfies both assertions above while
	// suppressing every reachability dot in the picker.
	if got[2].Skip {
		t.Error("a plain host was skipped — nothing would ever be probed")
	}
}

func TestSSHConfigPathUsesHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/fake-home")
	if got := sshConfigPath(); got != "/tmp/fake-home/.ssh/config" {
		t.Fatalf("sshConfigPath = %q", got)
	}
}

// os.UserHomeDir fails when HOME is empty or unset, which is how the plugin
// runs under a stripped environment. There is no config path in that state, and
// sshConfigPath says so with "" rather than inventing one.
//
// The rejected alternative was filepath.Join(".ssh", "config"), which is
// cwd-relative: ssh never reads $CWD/.ssh/config, so the picker would enumerate
// hosts from a file `ssh <alias>` provably ignores and describe each row with a
// HostName, Port, User and ProxyJump the connection would not use. In a
// directory the operator did not author — a loot share, an extracted archive, a
// mounted target filesystem — a planted .ssh/config would become the host list.
// That is the extra_config_paths defect (5a31f54) reached by another route, and
// the ruling there was that the rows the picker shows must be the set ssh can
// reach. loadHosts turns "" into a footer warning; see the test below.
//
// Both spellings of a stripped HOME are covered because os.UserHomeDir treats
// them identically (it reports "$HOME is not defined" for each), and because
// only the empty one is reachable through t.Setenv alone — a caller that
// deleted the variable rather than blanking it must land on the same path.
// Each case asserts home resolution is actually broken before relying on it: if
// it still worked, sshConfigPath would return a real path and the assertion
// would be measuring nothing.
func TestSSHConfigPathIsEmptyWithoutHome(t *testing.T) {
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
				t.Fatal("UserHomeDir still resolves with HOME stripped; the fixture is not live and the assertion below would prove nothing")
			}
			if got := sshConfigPath(); got != "" {
				t.Fatalf("sshConfigPath = %q, want \"\" — a relative path here enumerates hosts from a config ssh does not read", got)
			}
		})
	}
}

// The other half of that contract: "" has to reach the operator as a warning
// naming the home directory, not as a parse of whatever the working directory
// happens to be.
//
// The count alone is not the assertion. sshconfig.Parse("") also produces
// exactly one warning — filepath.Abs("") is the working directory, so
// os.ReadFile fails with "read <cwd>: is a directory" — so a test that only
// counted would pass against a loadHosts with no empty-path arm at all, having
// pinned a message that names a directory the operator never configured and
// says nothing about $HOME. The content checks are what carry the meaning, and
// the cwd check is what fails if the empty-path arm is removed.
func TestLoadHostsWarnsWhenTheHomeDirectoryIsUnresolvable(t *testing.T) {
	hosts, warns := loadHosts("", pluginconfig.Defaults())
	if len(hosts) != 0 {
		t.Fatalf("hosts = %+v, want none", hosts)
	}
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warns)
	}
	// Names the thing that could not be resolved, and the variable the operator
	// would fix. The picker's empty state says "no ~/.ssh/config — nothing to
	// pick"; this is the footer line that says why.
	for _, want := range []string{"home directory", "$HOME"} {
		if !strings.Contains(warns[0], want) {
			t.Errorf("warning %q does not mention %q", warns[0], want)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// Not printed on failure, for the same reason as above.
	if strings.Contains(warns[0], cwd) {
		t.Fatal("the warning names the working directory, so loadHosts parsed the cwd instead of reporting an unresolvable home directory")
	}
}

func TestTargetsForCarriesTheAlias(t *testing.T) {
	// The alias is the join key for the whole probe feature: probe.Run copies
	// Target.Alias into Result.Alias, and the picker looks its status up by
	// m.probed[h.Alias]. Drop it here and every result comes back keyed to "",
	// no row ever matches, and every host renders unprobed forever — with the
	// Addr and Skip assertions above still passing.
	hosts := []sshconfig.Host{
		{Alias: "a", HostName: "10.0.0.1", Port: "22"},
		{Alias: "b", HostName: "10.0.0.2", Port: "2222"},
	}
	got := targetsFor(hosts)
	if len(got) != 2 {
		t.Fatalf("targets = %+v", got)
	}
	if got[0].Alias != "a" || got[1].Alias != "b" {
		t.Fatalf("aliases = %q, %q, want a, b", got[0].Alias, got[1].Alias)
	}
}

func TestLoadHostsSurfacesPrimaryParseWarnings(t *testing.T) {
	// A malformed line is collected, not fatal, and costs that line rather than
	// the host block — sshconfig's continue advances the line loop, leaving the
	// block intact — which is why the assertion below is that `good` survives.
	// What is lost is a setting, and that is why the warning has to reach the
	// footer: lose `Port 2222` and targetsFor builds JoinHostPort(HostName,
	// "22"), so the picker probes the wrong port and renders a reachable host
	// as down; lose `ProxyJump` and Skip goes false, so a jump-only address
	// gets dialed directly and reports the same false down. The host is present
	// and wrong, which is worse than absent, and the warning is the only thing
	// that says why.
	primary := writeSSHConfig(t, "config", "Host good\n  HostName 10.5.5.5\nOrphanedKeyword\n")

	hosts, warns := loadHosts(primary, pluginconfig.Defaults())
	if len(hosts) != 1 {
		t.Fatalf("hosts = %+v, want the good host to survive", hosts)
	}
	// Asserted whole, not by Contains("malformed line"): the message alone is
	// useless without the file and line, and loadHosts calls w.String() to get
	// them. A Contains check on the message text passes just as happily if
	// String() is replaced by the bare Msg field, so it cannot tell
	// "config:3: malformed line: X" from "malformed line: X" — and it is the
	// first form that lets the operator find the line.
	want := primary + ":3: malformed line: OrphanedKeyword"
	if len(warns) != 1 || warns[0] != want {
		t.Fatalf("warnings = %v, want exactly [%q]", warns, want)
	}
}

func TestLoadHostsSurfacesIncludedFileParseWarnings(t *testing.T) {
	// The same warning, raised inside an included file rather than the root
	// config. Include is the only thing that puts a second file in front of the
	// operator, so it is the only place this can still go wrong, and it is a
	// worse place to lose a setting: the file the warning names is one the
	// operator did not open and would not think to suspect.
	included := writeSSHConfig(t, "work", "Host b\n  HostName 10.9.9.9\nOrphanedKeyword\n")
	primary := writeSSHConfig(t, "config", "Include "+included+"\nHost a\n")

	hosts, warns := loadHosts(primary, pluginconfig.Defaults())
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v, want both files' hosts", hosts)
	}
	// Whole-string, and naming the included file rather than the primary. Both
	// halves matter: the line number is relative to the file it came from, so a
	// warning misattributed to the root config sends the operator to the wrong
	// line of the wrong file with nothing on screen saying so.
	want := included + ":3: malformed line: OrphanedKeyword"
	if len(warns) != 1 || warns[0] != want {
		t.Fatalf("warnings = %v, want exactly [%q]", warns, want)
	}
}
