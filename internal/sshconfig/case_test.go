package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// ssh_config(5) keywords are case-insensitive. Every fixture elsewhere in
// this package spells every keyword in canonical CamelCase (Host, HostName,
// User, Port, IdentityFile, ProxyJump, ProxyCommand, Include, Match) — confirmed by
// inspection of every *_test.go and testdata/* file in this directory before
// writing these tests. That means sshconfig.go's two strings.ToLower calls
// (the stanza-keyword switch and the value-keyword store) are exercised by
// every existing test but never actually given a differently-cased input to
// prove they do anything: line coverage does not imply this behavior is
// under test.
//
// These tests close that gap. Each fixture holds every OTHER keyword in
// canonical case and varies only the one keyword class under test — stanza
// keywords (Host, Match, Include) in one set of tests, value keywords
// (HostName, User, Port, IdentityFile, ProxyJump, ProxyCommand) in another. That isolation
// is deliberate: it is what makes a stanza-keyword regression fail only the
// stanza tests below, and a value-keyword regression fail only the value
// test, rather than one mutant tripping every test in this file
// indiscriminately. New fixtures are scoped locally to these tests, per
// t.TempDir(); no shared fixture elsewhere in the package is edited.
//
// Host, Include, and the value keywords are verified here against real
// `ssh -G` (OpenSSH_10.3p1, matching the version cited elsewhere in this
// package): an all-lowercase and an all-uppercase config resolve identically
// to the canonical-case spelling. Match is the one exception, deliberately
// NOT checked against ssh -G — see TestParseMatchKeywordCaseInsensitive.

// TestParseHostKeywordCaseInsensitive covers the stanza-keyword switch at
// sshconfig.go:309 for "host". Verified against OpenSSH_10.3p1: `ssh -G
// cased-host` against a lowercase `host cased-host` / `HostName ...` config,
// and again against an uppercase `HOST cased-host`, both report the same
// hostname as canonical `Host` does.
func TestParseHostKeywordCaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"lowercase": "host",
		"uppercase": "HOST",
	}
	for name, kw := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "root"), kw+" cased-host\n  HostName 10.11.11.11\n")

			hosts, _, err := parse(filepath.Join(dir, "root"), dir)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			h := hostByAlias(t, hosts, "cased-host")
			if h.HostName != "10.11.11.11" {
				t.Errorf("cased-host HostName = %q, want 10.11.11.11", h.HostName)
			}
		})
	}
}

// TestParseMatchKeywordCaseInsensitive covers the stanza-keyword switch at
// sshconfig.go:309 for "match". Unlike Host/Include/the value keywords
// above and below, this is deliberately NOT checked against real ssh -G:
// sshconfig.go's "match" case treats every Match stanza as unconditionally
// inert without ever running `exec` or evaluating any criteria (see the
// comment on that case), which is a documented simplification that diverges
// from real ssh — `ssh -G` against `Match exec "true"` actually runs the
// command, finds it true, and applies the stanza's keywords, the opposite of
// what this package intends (confirmed empirically: real ssh reports
// user=leaked for the fixture below, not user=fallback). So this test holds
// this package's own documented behavior as the oracle — the same one
// TestParseBasic's "jumped" case already pins for canonical-case `Match` —
// and checks only that a differently-cased spelling of the keyword still
// triggers it.
//
// target declares no User of its own, so if the (cased) Match stanza is
// correctly recognized as a stanza-opener, "leaked" is swallowed into a block
// that never matches anyone and the trailing `Host *` default reaches
// target instead. If the switch fails to recognize the cased keyword, the
// line is instead stored as an ordinary value keyword inside target's own
// still-open block, and "leaked" wins outright.
func TestParseMatchKeywordCaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"lowercase": "match",
		"uppercase": "MATCH",
	}
	for name, kw := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "root"),
				"Host target\n  HostName 10.12.12.12\n\n"+
					kw+` exec "true"`+"\n  User leaked\n\n"+
					"Host *\n  User fallback\n")

			hosts, _, err := parse(filepath.Join(dir, "root"), dir)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			target := hostByAlias(t, hosts, "target")
			if target.User != "fallback" {
				t.Errorf("target User = %q, want fallback — a %s stanza must stay inert and not leak "+
					"its keywords into target", target.User, kw)
			}
		})
	}
}

// TestParseIncludeKeywordCaseInsensitive covers the stanza-keyword switch at
// sshconfig.go:309 for "include". Verified against OpenSSH_10.3p1: `ssh -G
// from-cased-include` reports the included hostname whether the directive
// is spelled `include` or `INCLUDE`, same as canonical `Include`.
func TestParseIncludeKeywordCaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"lowercase": "include",
		"uppercase": "INCLUDE",
	}
	for name, kw := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "extra"), "Host from-cased-include\n  HostName 10.13.13.13\n")
			write(t, filepath.Join(dir, "root"), kw+" extra\n")

			hosts, _, err := parse(filepath.Join(dir, "root"), dir)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			h := hostByAlias(t, hosts, "from-cased-include")
			if h.HostName != "10.13.13.13" {
				t.Errorf("from-cased-include HostName = %q, want 10.13.13.13", h.HostName)
			}
		})
	}
}

// TestParseValueKeywordsCaseInsensitive covers the value-keyword store at
// sshconfig.go:344 and the lookups at :408-412. Verified against
// OpenSSH_10.3p1: `ssh -G cased-values` against a canonical `Host` line
// whose HostName/User/Port/IdentityFile/ProxyJump keywords are spelled
// entirely lowercase, and again entirely uppercase, both report the same
// values as the canonical CamelCase spelling.
func TestParseValueKeywordsCaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"lowercase": "Host cased-values\n" +
			"  hostname 10.14.14.14\n" +
			"  user deploy\n" +
			"  port 2222\n" +
			"  identityfile ~/.ssh/id_deploy\n" +
			"  proxyjump bastion1\n",
		"uppercase": "Host cased-values\n" +
			"  HOSTNAME 10.14.14.14\n" +
			"  USER deploy\n" +
			"  PORT 2222\n" +
			"  IDENTITYFILE ~/.ssh/id_deploy\n" +
			"  PROXYJUMP bastion1\n",
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "root"), body)

			hosts, _, err := parse(filepath.Join(dir, "root"), dir)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			h := hostByAlias(t, hosts, "cased-values")
			if h.HostName != "10.14.14.14" {
				t.Errorf("HostName = %q, want 10.14.14.14", h.HostName)
			}
			if h.User != "deploy" {
				t.Errorf("User = %q, want deploy", h.User)
			}
			if h.Port != "2222" {
				t.Errorf("Port = %q, want 2222", h.Port)
			}
			if want := filepath.Join(home, ".ssh", "id_deploy"); h.IdentityFile != want {
				t.Errorf("IdentityFile = %q, want %q", h.IdentityFile, want)
			}
			if h.ProxyJump != "bastion1" {
				t.Errorf("ProxyJump = %q, want bastion1", h.ProxyJump)
			}
		})
	}
}

func TestParseProxyCommandKeywordCaseInsensitive(t *testing.T) {
	for name, keyword := range map[string]string{"lowercase": "proxycommand", "uppercase": "PROXYCOMMAND"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "root"), "Host command-proxy\n  "+keyword+" ssh gateway -W %h:%p\n")
			hosts, _, err := parse(filepath.Join(dir, "root"), dir)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := hostByAlias(t, hosts, "command-proxy").ProxyCommand; got != "ssh gateway -W %h:%p" {
				t.Errorf("ProxyCommand = %q, want the differently-cased keyword resolved", got)
			}
		})
	}
}
