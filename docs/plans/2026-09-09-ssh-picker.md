# SSH Picker for Herdr Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `purehate.herdr-ssh`, a Herdr plugin that opens a floating fuzzy picker over `~/.ssh/config` hosts and SSHes into a new pane, tab, or zoomed pane on selection.

**Architecture:** One Go binary with four verbs (`plugin open-picker`, `picker`, `session`, `connect`) declared in `herdr-plugin.toml` as an action plus two pane entrypoints. The picker runs in an `overlay` pane; selecting a host opens the `session` entrypoint with the host passed via `--env`, and that process renames its own pane to `ssh:<alias>` before `exec`ing ssh. Every `herdr` CLI call goes through one injectable `Runner` seam so the whole flow is testable without a running Herdr.

**Tech Stack:** Go 1.27, `charm.land/bubbletea/v2 v2.0.9`, `charm.land/lipgloss/v2 v2.0.6`, `github.com/pelletier/go-toml/v2 v2.4.3`. Spec: `docs/specs/2026-09-09-ssh-picker-design.md`.

---

## File Structure

| File                                    | Responsibility                                             |
| --------------------------------------- | ---------------------------------------------------------- |
| `herdr-plugin.toml`                     | Manifest: build, action, two pane entrypoints              |
| `internal/sshconfig/sshconfig.go`       | Parse SSH config → `[]Host`; `Exclude` for hidden globs    |
| `internal/theme/theme.go`               | Herdr `[theme].name` + `[ui].accent` → color tokens        |
| `internal/pluginconfig/pluginconfig.go` | Plugin `config.toml` → `Config` with defaults + validation |
| `internal/herdrapi/herdrapi.go`         | Every `herdr` CLI call behind a `Runner` seam              |
| `internal/probe/probe.go`               | Async TCP reachability                                     |
| `internal/picker/rank.go`               | Fuzzy ranking                                              |
| `internal/picker/model.go`              | Bubble Tea model: keymap, filter, cursor                   |
| `internal/picker/view.go`               | Rendering + `Run`                                          |
| `cmd/herdr-ssh/main.go`                 | Verb dispatch; `picker`, `open-picker`, `connect` verbs    |
| `cmd/herdr-ssh/caller.go`               | `caller.json` read/write                                   |
| `cmd/herdr-ssh/connect.go`              | `performSelection`: reuse-or-open                          |
| `cmd/herdr-ssh/session.go`              | Rename own pane, exec ssh                                  |
| `cmd/herdr-ssh/hosts.go`                | Load hosts from configs, build probe targets               |

Note: the spec's `internal/probe` interface was `Probe(ctx, []Host, timeout)`, but the spec also lists that package as depending on stdlib only. This plan keeps the stdlib-only boundary and has `probe.Run(ctx, []Target, timeout)` take its own `Target` type, with `cmd` doing the `Host → Target` conversion.

**Deliberate deviations from the spec:**

1. **`herdrapi` test seam.** Spec suggested a fake `herdr` binary on `PATH` (sesh's `HERDR_FAKE_LOG` pattern). This plan injects a `Runner func([]string) ([]byte, error)` instead — same argv assertions, no subprocess, faster tests. The real exec seam lives in `New()`.
2. **No `bubbles` dependency.** Spec's package table implied a text input component. Hand-rolling the query string (append `k.Text`, trim on backspace) drops a dependency and an API-surface risk, and is directly testable through `Update`.
3. **Signatures.** `picker.Run(Options)` rather than `Run([]Host, Theme, Config)` — five call-site arguments that all mean "config" is worse than one struct. `FocusPane(pane, currentWorkspace, currentTab)` rather than `FocusPane(pane)`, because skipping already-current focus steps requires knowing what is current.
4. **Overlay-close argv corrected.** The spec's data flow says `herdr plugin pane close picker`. The live CLI is `herdr plugin pane close <PANE_ID>` — it takes a pane id, not an entrypoint name. The overlay closes itself with its own `$HERDR_PANE_ID`.
5. **"Overlay stays open" on a `herdr` CLI failure is implemented as hold-then-close.** The spec's error table wants the overlay to survive a failed `herdr` call with the error in the footer. `performSelection` runs after the Bubble Tea program has already exited, so this plan prints the error in the pane and waits for enter before closing — the message is readable, but the picker does not re-enter. Re-entering would mean moving pane-opening inside the event loop; that is a follow-up, not this cycle.
6. **`herdr config reload` and `herdr plugin search` do not exist.** Verified against herdr 0.9.0: the reload is `herdr server reload-config`, and there is no plugin search subcommand. Tasks 20 and 21 use the real commands.

---

### Task 1: Repo scaffold

**Files:**

- Create: `go.mod`, `.gitignore`, `herdr-plugin.toml`

- [ ] **Step 1: Initialize the module**

```bash
cd ~/DEVELOPMENT/herdr-plugin-ssh
go mod init github.com/purehate/herdr-plugin-ssh
```

Expected: `go: creating new go.mod: module github.com/purehate/herdr-plugin-ssh`

- [ ] **Step 2: Add the pinned dependencies**

```bash
go get charm.land/bubbletea/v2@v2.0.9 charm.land/lipgloss/v2@v2.0.6 github.com/pelletier/go-toml/v2@v2.4.3
```

Expected: `go: added charm.land/bubbletea/v2 v2.0.9` and similar lines. Requires network.

- [ ] **Step 3: Write `.gitignore`**

```gitignore
bin/
.DS_Store
/tmp/
```

- [ ] **Step 4: Write `herdr-plugin.toml`**

```toml
id = "purehate.herdr-ssh"
name = "SSH Picker"
version = "0.1.0"
min_herdr_version = "0.9.0"
description = "Fuzzy-pick a host from ~/.ssh/config and SSH into a new pane, tab, or zoomed pane."
platforms = ["macos", "linux"]

[[build]]
command = ["go", "build", "-o", "bin/", "./cmd/herdr-ssh"]

[[actions]]
id = "open-picker"
title = "Open SSH Picker"
contexts = ["workspace", "pane"]
command = ["./bin/herdr-ssh", "plugin", "open-picker"]

[[panes]]
id = "picker"
title = "SSH Hosts"
placement = "overlay"
command = ["./bin/herdr-ssh", "picker"]

[[panes]]
id = "session"
title = "ssh"
placement = "split"
command = ["./bin/herdr-ssh", "session"]
```

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum .gitignore herdr-plugin.toml
git commit -m "chore: scaffold go module and plugin manifest"
```

---

### Task 2: sshconfig — types and line splitting

**Files:**

- Create: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/sshconfig_test.go`

- [ ] **Step 1: Write the failing test**

```go
package sshconfig

import "testing"

func TestSplitLine(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		key, val string
		ok            bool
	}{
		{"space separated", "HostName example.com", "HostName", "example.com", true},
		{"equals separated", "Port=2222", "Port", "2222", true},
		{"leading whitespace", "    User root", "User", "root", true},
		{"quoted value", `IdentityFile "~/.ssh/id ed"`, "IdentityFile", "~/.ssh/id ed", true},
		{"comment", "# Host nope", "", "", false},
		{"blank", "   ", "", "", false},
		{"keyword only", "Host", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, val, ok := splitLine(tc.in)
			if ok != tc.ok || key != tc.key || val != tc.val {
				t.Fatalf("splitLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, key, val, ok, tc.key, tc.val, tc.ok)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestSplitLine`
Expected: FAIL — `undefined: splitLine`

- [ ] **Step 3: Write the types and `splitLine`**

```go
// Package sshconfig parses OpenSSH client configuration into connectable hosts.
package sshconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoConfig reports a config file that does not exist. Callers treat this as
// "nothing to pick" rather than a failure.
var ErrNoConfig = errors.New("ssh config not found")

// Host is one connectable target with its keywords already resolved.
type Host struct {
	Alias        string
	HostName     string
	User         string
	Port         string
	IdentityFile string
	ProxyJump    string
	SourceFile   string
	SourceLine   int
}

// Warning is a non-fatal parse problem, surfaced in the picker footer.
type Warning struct {
	File string
	Line int
	Msg  string
}

func (w Warning) String() string { return fmt.Sprintf("%s:%d: %s", w.File, w.Line, w.Msg) }

type kv struct{ key, value string }

// block is one `Host <patterns>` stanza and the keywords under it.
type block struct {
	positive []string
	negative []string
	keys     []kv
	file     string
	line     int
}

func splitLine(raw string) (string, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexAny(line, " \t=")
	if i <= 0 {
		return "", "", false
	}
	key := line[:i]
	value := strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
	value = strings.Trim(value, `"`)
	if value == "" {
		return "", "", false
	}
	return key, value, true
}

func expandTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run TestSplitLine`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): add host types and config line splitting"
```

---

### Task 3: sshconfig — pattern matching

**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/sshconfig_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
	for _, p := range []string{"*", "*.dev", "web?", "!deny", "[ab]host"} {
		if !isPattern(p) {
			t.Errorf("isPattern(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"nixos-dev", "10.0.0.1", "build_box"} {
		if isPattern(p) {
			t.Errorf("isPattern(%q) = true, want false", p)
		}
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run 'TestBlock|TestIsPattern'`
Expected: FAIL — `b.matches undefined`

- [ ] **Step 3: Add the matching methods**

```go
func matchPattern(pattern, alias string) bool {
	ok, err := filepath.Match(pattern, alias)
	return err == nil && ok
}

func isPattern(s string) bool { return strings.ContainsAny(s, "*?![]") }

// matches reports whether this stanza applies to alias. A negated pattern wins
// over any positive match, mirroring ssh_config semantics.
func (b block) matches(alias string) bool {
	for _, n := range b.negative {
		if matchPattern(n, alias) {
			return false
		}
	}
	for _, p := range b.positive {
		if matchPattern(p, alias) {
			return true
		}
	}
	return false
}

// declares reports whether alias is named literally, which is what makes it a
// selectable target and fixes its source location.
func (b block) declares(alias string) bool {
	for _, p := range b.positive {
		if p == alias {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run 'TestBlock|TestIsPattern'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): add host pattern matching with negation"
```

---

### Task 4: sshconfig — parse and resolve a single file

**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/parse_test.go`
- Create: `internal/sshconfig/testdata/basic`

- [ ] **Step 1: Write the fixture**

`internal/sshconfig/testdata/basic`:

```
# global defaults
ServerAliveInterval 30

Host *
  User fallback
  IdentityFile ~/.ssh/id_default

Host nixos-dev
  HostName 192.0.2.10
  User operator
  Port 22
  IdentityFile ~/.ssh/id_nixos

Host build box
  HostName 10.0.0.12
  User root

Host web1
  HostName 10.0.0.20
  User root
  User ignored-second-value

Host jumped
  HostName 10.9.9.9
  ProxyJump bastion

Match exec "true"
  User never-applied

Host *.internal
  Port 2222
```

- [ ] **Step 2: Write the failing test**

```go
package sshconfig

import (
	"errors"
	"path/filepath"
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

	dev := hostByAlias(t, hosts, "nixos-dev")
	if dev.HostName != "192.0.2.10" || dev.User != "operator" || dev.Port != "22" {
		t.Errorf("nixos-dev = %+v", dev)
	}
	if dev.SourceLine == 0 || dev.SourceFile == "" {
		t.Errorf("nixos-dev missing provenance: %+v", dev)
	}

	// `Host build box` is two aliases sharing one stanza.
	if got := hostByAlias(t, hosts, "box").HostName; got != "10.0.0.12" {
		t.Errorf("box HostName = %q, want 10.0.0.12", got)
	}

	// First value wins, both within a stanza and against `Host *`.
	if got := hostByAlias(t, hosts, "web1").User; got != "root" {
		t.Errorf("web1 User = %q, want root (first value wins)", got)
	}

	// `Host *` supplies defaults but is not selectable.
	if got := hostByAlias(t, hosts, "jumped").IdentityFile; got == "" {
		t.Error("jumped IdentityFile empty, want the Host * default applied")
	}
	if got := hostByAlias(t, hosts, "jumped").ProxyJump; got != "bastion" {
		t.Errorf("jumped ProxyJump = %q, want bastion", got)
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

func TestParseMissingFile(t *testing.T) {
	_, _, err := Parse(filepath.Join("testdata", "does-not-exist"))
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("err = %v, want ErrNoConfig", err)
	}
}
```

Also create `internal/sshconfig/testdata/minimal`:

```
Host solo
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestParse`
Expected: FAIL — `undefined: Parse`

- [ ] **Step 4: Write `Parse`, `parseFile`, and `resolve`**

```go
// Parse reads root and returns its connectable hosts in declaration order.
// Non-fatal problems come back as warnings; only an unreadable root is an error.
func Parse(root string) ([]Host, []Warning, error) {
	visited := map[string]bool{}
	blocks, warns, err := parseFile(root, visited, nil)
	if err != nil {
		return nil, warns, err
	}
	return resolve(blocks), warns, nil
}

func parseFile(path string, visited map[string]bool, warns []Warning) ([]block, []Warning, error) {
	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		return nil, warns, err
	}
	if visited[abs] {
		return nil, warns, nil
	}
	visited[abs] = true

	raw, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, warns, fmt.Errorf("%w: %s", ErrNoConfig, abs)
		}
		return nil, warns, err
	}

	var blocks []block
	// Keywords before the first Host stanza apply to every host.
	cur := block{positive: []string{"*"}, file: abs}

	for i, line := range strings.Split(string(raw), "\n") {
		lineNo := i + 1
		key, value, ok := splitLine(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			blocks = append(blocks, cur)
			cur = newHostBlock(value, abs, lineNo)
		case "match":
			// A Match stanza has no static host to offer, and `Match exec` would
			// mean running commands to build a picker list. Skip its keywords by
			// starting a stanza that matches nothing.
			blocks = append(blocks, cur)
			cur = block{file: abs, line: lineNo}
		case "include":
			blocks = append(blocks, cur)
			var included []block
			included, warns = parseIncludes(value, abs, lineNo, visited, warns)
			blocks = append(blocks, included...)
			resumed := cur
			resumed.keys = nil
			cur = resumed
		default:
			cur.keys = append(cur.keys, kv{strings.ToLower(key), value})
		}
	}
	return append(blocks, cur), warns, nil
}

func newHostBlock(value, file string, line int) block {
	b := block{file: file, line: line}
	for _, field := range strings.Fields(value) {
		if strings.HasPrefix(field, "!") {
			b.negative = append(b.negative, strings.TrimPrefix(field, "!"))
			continue
		}
		b.positive = append(b.positive, field)
	}
	return b
}

func resolve(blocks []block) []Host {
	var order []string
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, p := range b.positive {
			if isPattern(p) || seen[p] {
				continue
			}
			seen[p] = true
			order = append(order, p)
		}
	}

	hosts := make([]Host, 0, len(order))
	for _, alias := range order {
		h := Host{Alias: alias}
		values := map[string]string{}
		for _, b := range blocks {
			if !b.matches(alias) {
				continue
			}
			if h.SourceFile == "" && b.declares(alias) {
				h.SourceFile, h.SourceLine = b.file, b.line
			}
			for _, pair := range b.keys {
				if _, exists := values[pair.key]; !exists {
					values[pair.key] = pair.value
				}
			}
		}
		h.HostName = firstNonEmpty(values["hostname"], alias)
		h.User = values["user"]
		h.Port = firstNonEmpty(values["port"], "22")
		h.IdentityFile = expandTilde(values["identityfile"])
		h.ProxyJump = values["proxyjump"]
		hosts = append(hosts, h)
	}
	return hosts
}
```

- [ ] **Step 5: Add a stub `parseIncludes` so the package compiles**

```go
func parseIncludes(value, parent string, line int, visited map[string]bool, warns []Warning) ([]block, []Warning) {
	return nil, warns
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run TestParse -v`
Expected: PASS for `TestParseBasic` and `TestParseMissingFile`

- [ ] **Step 7: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): parse and resolve hosts with ssh precedence rules"
```

---

### Task 5: sshconfig — Include expansion

**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/include_test.go`
- Create: `internal/sshconfig/testdata/with-include`, `testdata/included/extra`, `testdata/cyclic-a`, `testdata/cyclic-b`

- [ ] **Step 1: Write the fixtures**

`internal/sshconfig/testdata/with-include`:

```
Host local-one
  HostName 10.1.1.1

Include included/*

Include ./missing/nothing-here

Host local-two
  HostName 10.1.1.2
```

`internal/sshconfig/testdata/included/extra`:

```
Host from-include
  HostName 10.2.2.2
  User included-user
```

`internal/sshconfig/testdata/cyclic-a`:

```
Host cyc-a
Include cyclic-b
```

`internal/sshconfig/testdata/cyclic-b`:

```
Host cyc-b
Include cyclic-a
```

- [ ] **Step 2: Write the failing test**

```go
package sshconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIncludeExpansion(t *testing.T) {
	hosts, warns, err := Parse(filepath.Join("testdata", "with-include"))
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

	// The unreadable include is a warning, not a failure.
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warns)
	}
	if !strings.Contains(warns[0].Msg, "include unreadable") {
		t.Errorf("warning = %q", warns[0].Msg)
	}
}

func TestParseIncludeCycle(t *testing.T) {
	hosts, _, err := Parse(filepath.Join("testdata", "cyclic-a"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	hostByAlias(t, hosts, "cyc-a")
	hostByAlias(t, hosts, "cyc-b")
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestParseInclude`
Expected: FAIL — `alias "from-include" not found in 2 hosts` (the stub returns nothing)

- [ ] **Step 4: Replace the stub with the real implementation**

```go
// parseIncludes expands one Include directive. Relative patterns resolve
// against the including file's directory, which equals ~/.ssh for the primary
// config — the case that matters — and keeps nested includes testable from a
// temp dir. An unreadable or unmatched pattern is a warning: one bad include
// must not cost the operator the rest of their hosts.
func parseIncludes(value, parent string, line int, visited map[string]bool, warns []Warning) ([]block, []Warning) {
	var out []block
	base := filepath.Dir(parent)
	for _, rawPattern := range strings.Fields(value) {
		pattern := expandTilde(rawPattern)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(base, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			warns = append(warns, Warning{File: parent, Line: line, Msg: "include unreadable: " + rawPattern})
			continue
		}
		for _, match := range matches {
			blocks, updated, err := parseFile(match, visited, warns)
			warns = updated
			if err != nil {
				warns = append(warns, Warning{File: parent, Line: line, Msg: "include unreadable: " + match})
				continue
			}
			out = append(out, blocks...)
		}
	}
	return out, warns
}
```

- [ ] **Step 5: Run the whole package**

Run: `go test ./internal/sshconfig/ -v`
Expected: PASS — all tests including the earlier ones

- [ ] **Step 6: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): expand Include directives with cycle and error guards"
```

---

### Task 6: sshconfig — Exclude hidden hosts

**Files:**

- Modify: `internal/sshconfig/sshconfig.go`
- Test: `internal/sshconfig/exclude_test.go`

- [ ] **Step 1: Write the failing test**

```go
package sshconfig

import "testing"

func TestExclude(t *testing.T) {
	hosts := []Host{{Alias: "nixos-dev"}, {Alias: "colima"}, {Alias: "web-old"}, {Alias: "web1"}}

	kept := Exclude(hosts, []string{"colima", "*-old"})
	if len(kept) != 2 || kept[0].Alias != "nixos-dev" || kept[1].Alias != "web1" {
		t.Fatalf("kept = %+v, want nixos-dev and web1", kept)
	}
	if len(hosts) != 4 {
		t.Error("Exclude mutated its input")
	}

	if got := Exclude(hosts, nil); len(got) != 4 {
		t.Errorf("Exclude with no globs dropped hosts: %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshconfig/ -run TestExclude`
Expected: FAIL — `undefined: Exclude`

- [ ] **Step 3: Implement `Exclude`**

```go
// Exclude returns a new slice without hosts whose alias matches any glob.
func Exclude(hosts []Host, globs []string) []Host {
	if len(globs) == 0 {
		return hosts
	}
	kept := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		hidden := false
		for _, g := range globs {
			if matchPattern(g, h.Alias) {
				hidden = true
				break
			}
		}
		if !hidden {
			kept = append(kept, h)
		}
	}
	return kept
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshconfig/ -run TestExclude`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): add Exclude for hidden host globs"
```

---

### Task 7: theme

**Files:**

- Create: `internal/theme/theme.go`
- Test: `internal/theme/theme_test.go`

- [ ] **Step 1: Write the failing test**

```go
package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadMissingFileYieldsDefault(t *testing.T) {
	if got := Load(filepath.Join(t.TempDir(), "absent.toml")); got != Default() {
		t.Fatalf("Load = %+v, want %+v", got, Default())
	}
}

func TestLoadAccentOverride(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"terminal\"\n\n[ui]\naccent = \"#14e21a\"\n")
	if got := Load(path).Accent; got != "#14e21a" {
		t.Fatalf("Accent = %q, want #14e21a", got)
	}
}

func TestLoadThemeNameAccent(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"tokyonight\"\n")
	if got := Load(path).Accent; got != "#7aa2f7" {
		t.Fatalf("Accent = %q, want the tokyonight accent", got)
	}
}

func TestLoadIgnoresBadAccentAndBadTOML(t *testing.T) {
	bad := writeConfig(t, "[ui]\naccent = \"not-a-color\"\n")
	if got := Load(bad).Accent; got != Default().Accent {
		t.Errorf("Accent = %q, want the default for an invalid hex value", got)
	}
	broken := writeConfig(t, "[theme\nname =")
	if got := Load(broken); got != Default() {
		t.Errorf("Load on malformed TOML = %+v, want default", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/theme/`
Expected: FAIL — `undefined: Load`

- [ ] **Step 3: Implement the package**

```go
// Package theme reads Herdr's active theme so the picker matches the rest of
// the workspace.
package theme

import (
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

// Theme holds the only tokens the picker draws with.
type Theme struct {
	Accent string
	Text   string
	Muted  string
	Up     string
	Down   string
}

const (
	defaultAccent = "#89b4fa"
	defaultText   = "#edf1f3"
	defaultMuted  = "#7b8496"
	defaultUp     = "#2ecc71"
)

// accentByTheme covers the themes Herdr ships. An unknown name keeps the
// default, which is always readable.
var accentByTheme = map[string]string{
	"terminal":         defaultAccent,
	"catppuccin-mocha": "#89b4fa",
	"catppuccin-latte": "#1e66f5",
	"tokyonight":       "#7aa2f7",
	"dracula":          "#bd93f9",
	"nord":             "#88c0d0",
	"gruvbox":          "#d79921",
	"solarized":        "#268bd2",
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type herdrConfig struct {
	Theme struct {
		Name string `toml:"name"`
	} `toml:"theme"`
	UI struct {
		Accent string `toml:"accent"`
	} `toml:"ui"`
}

// Default is the theme used when Herdr's config is absent or unreadable.
func Default() Theme {
	return Theme{Accent: defaultAccent, Text: defaultText, Muted: defaultMuted, Up: defaultUp, Down: defaultMuted}
}

// Load never fails. A picker that refuses to open because of a color lookup is
// worse than a picker with the wrong accent.
func Load(configPath string) Theme {
	t := Default()
	if configPath == "" {
		return t
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return t
	}
	var cfg herdrConfig
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return t
	}
	if accent, ok := accentByTheme[cfg.Theme.Name]; ok {
		t.Accent = accent
	}
	if hexColor.MatchString(cfg.UI.Accent) {
		t.Accent = cfg.UI.Accent
	}
	return t
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/theme/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/theme/
git commit -m "feat(theme): resolve picker colors from herdr theme and accent"
```

---

### Task 8: pluginconfig

**Files:**

- Create: `internal/pluginconfig/pluginconfig.go`
- Test: `internal/pluginconfig/pluginconfig_test.go`

- [ ] **Step 1: Write the failing test**

```go
package pluginconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg != Defaults() {
		t.Fatalf("cfg = %+v, want %+v", cfg, Defaults())
	}
}

func TestLoadEmptyDirYieldsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil || cfg != Defaults() {
		t.Fatalf("Load(\"\") = (%+v, %v)", cfg, err)
	}
}

func TestLoadPartialOverride(t *testing.T) {
	dir := writeConfig(t, "probe = false\nsplit_direction = \"down\"\nhidden = [\"colima\"]\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false")
	}
	if cfg.SplitDirection != "down" {
		t.Errorf("SplitDirection = %q, want down", cfg.SplitDirection)
	}
	if len(cfg.Hidden) != 1 || cfg.Hidden[0] != "colima" {
		t.Errorf("Hidden = %v", cfg.Hidden)
	}
	// Untouched keys keep their defaults.
	if cfg.ProbeTimeoutMS != 300 || !cfg.ReusePanes || !cfg.ShowPreview {
		t.Errorf("defaults not preserved: %+v", cfg)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	dir := writeConfig(t, "split_direction = \"sideways\"\n")
	if _, err := Load(dir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	broken := writeConfig(t, "probe = yes-please\n")
	if _, err := Load(broken); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid for malformed TOML", err)
	}
}

func TestLoadClampsProbeTimeout(t *testing.T) {
	dir := writeConfig(t, "probe_timeout_ms = 0\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProbeTimeoutMS != 300 {
		t.Errorf("ProbeTimeoutMS = %d, want the 300 default", cfg.ProbeTimeoutMS)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pluginconfig/`
Expected: FAIL — `undefined: Load`

- [ ] **Step 3: Implement the package**

```go
// Package pluginconfig loads the plugin's own config.toml from
// $HERDR_PLUGIN_CONFIG_DIR.
package pluginconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// ErrInvalid reports a config file that exists but cannot be used.
var ErrInvalid = errors.New("invalid plugin config")

const defaultProbeTimeoutMS = 300

// Config is the operator-facing plugin configuration. Every key is optional.
type Config struct {
	Probe            bool     `toml:"probe"`
	ProbeTimeoutMS   int      `toml:"probe_timeout_ms"`
	SplitDirection   string   `toml:"split_direction"`
	ShowPreview      bool     `toml:"show_preview"`
	ReusePanes       bool     `toml:"reuse_panes"`
	Hidden           []string `toml:"hidden"`
	ExtraConfigPaths []string `toml:"extra_config_paths"`
	SSHArgs          []string `toml:"ssh_args"`
}

// Defaults returns the configuration used when no file is present.
func Defaults() Config {
	return Config{
		Probe:          true,
		ProbeTimeoutMS: defaultProbeTimeoutMS,
		SplitDirection: "right",
		ShowPreview:    true,
		ReusePanes:     true,
	}
}

// Load reads dir/config.toml over the defaults. A missing file is not an error;
// a malformed or invalid one is, so the operator finds out instead of silently
// getting different behavior.
func Load(dir string) (Config, error) {
	cfg := Defaults()
	if dir == "" {
		return cfg, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return Defaults(), fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if cfg.SplitDirection != "right" && cfg.SplitDirection != "down" {
		return Defaults(), fmt.Errorf("%w: split_direction must be \"right\" or \"down\", got %q", ErrInvalid, cfg.SplitDirection)
	}
	if cfg.ProbeTimeoutMS <= 0 {
		cfg.ProbeTimeoutMS = defaultProbeTimeoutMS
	}
	return cfg, nil
}
```

Note: `Config` contains slices, so the `cfg != Defaults()` comparisons in the tests only compile because the zero-value slices are `nil` in both. Keep the struct comparable — do not add maps.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pluginconfig/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pluginconfig/
git commit -m "feat(pluginconfig): load and validate plugin config with defaults"
```

---

### Task 9: herdrapi — Runner seam and PaneList

**Files:**

- Create: `internal/herdrapi/herdrapi.go`
- Test: `internal/herdrapi/herdrapi_test.go`

Background: every `herdr` CLI call returns a JSON envelope `{"id": 1, "result": {...}}`. `herdr pane list --json` returns `{"result":{"panes":[{"pane_id":"w5:pB","tab_id":"w5:t7","workspace_id":"w5","label":"ssh:nixos-dev"}]}}`. `label` is `null` when unset. `pane list` is global — it returns panes from every workspace, which is what makes cross-workspace reuse possible.

- [ ] **Step 1: Write the failing test**

```go
package herdrapi

import (
	"errors"
	"strings"
	"testing"
)

// fakeRunner records argv and replays canned output.
func fakeRunner(out string, err error) (Runner, *[][]string) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), err
	}
	return run, &calls
}

const panesJSON = `{"id":1,"result":{"panes":[
  {"pane_id":"w5:pA","tab_id":"w5:t1","workspace_id":"w5","label":null},
  {"pane_id":"w5:pB","tab_id":"w5:t7","workspace_id":"w5","label":"ssh:nixos-dev"}
]}}`

func TestPaneList(t *testing.T) {
	run, calls := fakeRunner(panesJSON, nil)
	c := Client{Run: run}

	panes, err := c.PaneList()
	if err != nil {
		t.Fatalf("PaneList: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(panes))
	}
	if panes[0].Label != nil {
		t.Errorf("panes[0].Label = %v, want nil", panes[0].Label)
	}
	if panes[1].Label == nil || *panes[1].Label != "ssh:nixos-dev" {
		t.Errorf("panes[1].Label = %v", panes[1].Label)
	}
	if panes[1].PaneID != "w5:pB" || panes[1].TabID != "w5:t7" || panes[1].WorkspaceID != "w5" {
		t.Errorf("panes[1] = %+v", panes[1])
	}

	want := []string{"pane", "list", "--json"}
	if len(*calls) != 1 || strings.Join((*calls)[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", *calls, want)
	}
}

func TestPaneListPropagatesCLIError(t *testing.T) {
	boom := errors.New("exit status 1")
	run, _ := fakeRunner("socket not found", boom)
	c := Client{Run: run}

	_, err := c.PaneList()
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the runner error", err)
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("err = %v, want a *CLIError", err)
	}
	if !strings.Contains(cliErr.Error(), "socket not found") {
		t.Errorf("CLIError message lost the output: %q", cliErr.Error())
	}
}

func TestPaneListRejectsBadJSON(t *testing.T) {
	run, _ := fakeRunner("not json at all", nil)
	c := Client{Run: run}
	if _, err := c.PaneList(); err == nil {
		t.Fatal("err = nil, want a decode error")
	}
}

func TestFindLabeled(t *testing.T) {
	label := "ssh:nixos-dev"
	panes := []Pane{{PaneID: "w5:pA"}, {PaneID: "w5:pB", Label: &label}}

	got, ok := FindLabeled(panes, "ssh:nixos-dev")
	if !ok || got.PaneID != "w5:pB" {
		t.Fatalf("FindLabeled = (%+v, %v)", got, ok)
	}
	if _, ok := FindLabeled(panes, "ssh:absent"); ok {
		t.Error("FindLabeled matched a label that is not present")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/herdrapi/`
Expected: FAIL — `undefined: Runner`

- [ ] **Step 3: Implement the seam, error type, and `PaneList`**

```go
// Package herdrapi wraps the herdr CLI. Every call goes through one Runner so
// the whole plugin is testable without a live herdr socket.
package herdrapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes a herdr subcommand and returns its stdout.
type Runner func(args []string) ([]byte, error)

// Client talks to herdr through a Runner.
type Client struct {
	Run Runner
}

// CLIError carries the argv and output of a failed herdr call. The output is
// the only useful diagnostic when the socket or a flag is wrong.
type CLIError struct {
	Args   []string
	Output string
	Err    error
}

func (e *CLIError) Error() string {
	return fmt.Sprintf("herdr %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Output))
}

func (e *CLIError) Unwrap() error { return e.Err }

// New returns a Client bound to the herdr binary the plugin was launched with.
func New() Client {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	return Client{Run: func(args []string) ([]byte, error) {
		cmd := exec.Command(bin, args...)
		out, err := cmd.Output()
		if err == nil {
			return out, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return append(out, exitErr.Stderr...), err
		}
		return out, err
	}}
}

// Pane is the subset of herdr's PaneInfo the picker needs. Label is a pointer
// because herdr reports an unlabeled pane as null, and "no label" has to be
// distinguishable from the empty string.
type Pane struct {
	PaneID      string  `json:"pane_id"`
	TabID       string  `json:"tab_id"`
	WorkspaceID string  `json:"workspace_id"`
	Label       *string `json:"label"`
}

type envelope struct {
	Result json.RawMessage `json:"result"`
}

func (c Client) call(args ...string) ([]byte, error) {
	out, err := c.Run(args)
	if err != nil {
		return nil, &CLIError{Args: args, Output: string(out), Err: err}
	}
	return out, nil
}

func (c Client) callJSON(target any, args ...string) error {
	out, err := c.call(args...)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("herdr %s: decode envelope: %w", strings.Join(args, " "), err)
	}
	if err := json.Unmarshal(env.Result, target); err != nil {
		return fmt.Errorf("herdr %s: decode result: %w", strings.Join(args, " "), err)
	}
	return nil
}

// PaneList returns every pane herdr knows about, across all workspaces.
func (c Client) PaneList() ([]Pane, error) {
	var result struct {
		Panes []Pane `json:"panes"`
	}
	if err := c.callJSON(&result, "pane", "list", "--json"); err != nil {
		return nil, err
	}
	return result.Panes, nil
}

// FindLabeled returns the first pane carrying label.
func FindLabeled(panes []Pane, label string) (Pane, bool) {
	for _, p := range panes {
		if p.Label != nil && *p.Label == label {
			return p, true
		}
	}
	return Pane{}, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/herdrapi/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/herdrapi/
git commit -m "feat(herdrapi): add injectable herdr CLI client with pane listing"
```

---

### Task 10: herdrapi — rename, close, focus, and plugin pane open

**Files:**

- Modify: `internal/herdrapi/herdrapi.go`
- Test: `internal/herdrapi/commands_test.go`

Background on focus: `herdr pane focus` takes only `--direction`, so it cannot jump to an arbitrary pane id. The working sequence is `workspace focus <ws>` → `tab focus <tab>` → `plugin pane focus <pane>`, skipping any step already current.

- [ ] **Step 1: Write the failing test**

```go
package herdrapi

import (
	"strings"
	"testing"
)

func recorder() (Runner, *[][]string) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}
	return run, &calls
}

func argvLines(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func assertArgv(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	got := argvLines(calls)
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPaneRename(t *testing.T) {
	run, calls := recorder()
	if err := (Client{Run: run}).PaneRename("w5:pB", "ssh:nixos-dev"); err != nil {
		t.Fatalf("PaneRename: %v", err)
	}
	assertArgv(t, *calls, []string{"pane rename w5:pB ssh:nixos-dev"})
}

func TestPaneClose(t *testing.T) {
	run, calls := recorder()
	if err := (Client{Run: run}).PaneClose("w5:pOverlay"); err != nil {
		t.Fatalf("PaneClose: %v", err)
	}
	assertArgv(t, *calls, []string{"plugin pane close w5:pOverlay"})
}

func TestFocusPaneFullSequence(t *testing.T) {
	run, calls := recorder()
	target := Pane{PaneID: "w8:p3", TabID: "w8:t2", WorkspaceID: "w8"}
	if err := (Client{Run: run}).FocusPane(target, "w5", "w5:t1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	assertArgv(t, *calls, []string{
		"workspace focus w8",
		"tab focus w8:t2",
		"plugin pane focus w8:p3",
	})
}

func TestFocusPaneSkipsCurrentSteps(t *testing.T) {
	run, calls := recorder()
	target := Pane{PaneID: "w5:pB", TabID: "w5:t1", WorkspaceID: "w5"}
	if err := (Client{Run: run}).FocusPane(target, "w5", "w5:t1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	assertArgv(t, *calls, []string{"plugin pane focus w5:pB"})
}

func TestPluginPaneOpenSplit(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "purehate.herdr-ssh",
		Entrypoint: "session",
		Placement:  "split",
		TargetPane: "w5:pA",
		Direction:  "right",
		Env:        map[string]string{"HERDR_SSH_TARGET": "nixos-dev"},
		Focus:      true,
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	assertArgv(t, *calls, []string{
		"plugin pane open --plugin purehate.herdr-ssh --entrypoint session " +
			"--placement split --target-pane w5:pA --direction right " +
			"--env HERDR_SSH_TARGET=nixos-dev --focus",
	})
}

func TestPluginPaneOpenTabOmitsDirection(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "purehate.herdr-ssh",
		Entrypoint: "session",
		Placement:  "tab",
		Direction:  "right",
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	// --direction is meaningless outside a split and herdr rejects it there.
	if got := argvLines(*calls)[0]; strings.Contains(got, "--direction") {
		t.Fatalf("argv = %q, want no --direction for a tab placement", got)
	}
}

func TestPluginPaneOpenEnvIsSorted(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "p",
		Entrypoint: "session",
		Placement:  "zoomed",
		Env:        map[string]string{"B": "2", "A": "1", "C": "3"},
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	got := argvLines(*calls)[0]
	if !strings.Contains(got, "--env A=1 --env B=2 --env C=3") {
		t.Fatalf("argv = %q, want env flags in sorted order", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/herdrapi/ -run 'TestPaneRename|TestFocus|TestPluginPaneOpen'`
Expected: FAIL — `c.PaneRename undefined`

- [ ] **Step 3: Implement the three commands**

Add to `internal/herdrapi/herdrapi.go` (and add `"sort"` to the imports):

```go
// PaneRename sets a pane's label. The label is how the picker recognizes a pane
// it opened earlier.
func (c Client) PaneRename(paneID, label string) error {
	_, err := c.call("pane", "rename", paneID, label)
	return err
}

// PaneClose closes a plugin-owned pane. Verified signature: `herdr plugin pane
// close <PANE_ID>` — it takes a pane id, not an entrypoint name.
func (c Client) PaneClose(paneID string) error {
	_, err := c.call("plugin", "pane", "close", paneID)
	return err
}

// FocusPane moves the operator's view to p. herdr's `pane focus` only accepts a
// direction, so reaching an arbitrary pane means walking workspace → tab → pane.
// Steps that are already current are skipped: refocusing the current workspace
// is a visible flicker for no gain.
func (c Client) FocusPane(p Pane, currentWorkspace, currentTab string) error {
	if p.WorkspaceID != "" && p.WorkspaceID != currentWorkspace {
		if _, err := c.call("workspace", "focus", p.WorkspaceID); err != nil {
			return err
		}
	}
	if p.TabID != "" && p.TabID != currentTab {
		if _, err := c.call("tab", "focus", p.TabID); err != nil {
			return err
		}
	}
	_, err := c.call("plugin", "pane", "focus", p.PaneID)
	return err
}

// OpenOpts describes a pane to open from a declared plugin entrypoint.
type OpenOpts struct {
	Plugin     string
	Entrypoint string
	Placement  string // split | tab | zoomed | overlay
	TargetPane string
	Direction  string // right | down; only meaningful for a split
	Env        map[string]string
	Focus      bool
}

// PluginPaneOpen launches one of this plugin's pane entrypoints.
func (c Client) PluginPaneOpen(o OpenOpts) error {
	args := []string{"plugin", "pane", "open", "--plugin", o.Plugin, "--entrypoint", o.Entrypoint}
	if o.Placement != "" {
		args = append(args, "--placement", o.Placement)
	}
	if o.TargetPane != "" {
		args = append(args, "--target-pane", o.TargetPane)
	}
	if o.Placement == "split" && o.Direction != "" {
		args = append(args, "--direction", o.Direction)
	}
	keys := make([]string, 0, len(o.Env))
	for k := range o.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--env", k+"="+o.Env[k])
	}
	if o.Focus {
		args = append(args, "--focus")
	}
	_, err := c.call(args...)
	return err
}
```

- [ ] **Step 4: Run the whole package**

Run: `go test ./internal/herdrapi/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/herdrapi/
git commit -m "feat(herdrapi): add pane rename, close, focus sequence, and pane open"
```

---

### Task 11: probe

**Files:**

- Create: `internal/probe/probe.go`
- Test: `internal/probe/probe_test.go`

- [ ] **Step 1: Write the failing test**

```go
package probe

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRunReportsReachability(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	targets := []Target{
		{Alias: "up", Addr: ln.Addr().String()},
		{Alias: "down", Addr: "127.0.0.1:1"},
		{Alias: "skipped", Addr: "127.0.0.1:1", Skip: true},
	}

	results := map[string]bool{}
	for r := range Run(context.Background(), targets, 500*time.Millisecond) {
		results[r.Alias] = r.Up
	}

	if len(results) != 2 {
		t.Fatalf("results = %v, want 2 entries (skipped host omitted)", results)
	}
	if !results["up"] {
		t.Error("listening host reported down")
	}
	if results["down"] {
		t.Error("closed port reported up")
	}
	if _, ok := results["skipped"]; ok {
		t.Error("skipped target was probed")
	}
}

func TestRunClosesChannelOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := Run(ctx, []Target{{Alias: "a", Addr: "127.0.0.1:1"}}, time.Second)
	for range ch {
		// Drain: results may or may not arrive, but the channel must close.
	}
}

func TestRunWithNoTargetsClosesImmediately(t *testing.T) {
	select {
	case _, open := <-Run(context.Background(), nil, time.Second):
		if open {
			t.Fatal("received a result for zero targets")
		}
	case <-time.After(time.Second):
		t.Fatal("channel never closed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/probe/`
Expected: FAIL — `undefined: Target`

- [ ] **Step 3: Implement the package**

```go
// Package probe checks TCP reachability of SSH hosts. Results stream so the
// picker can render before the network answers.
package probe

import (
	"context"
	"net"
	"sync"
	"time"
)

// maxInFlight bounds concurrent dials. A large ssh config should not open a
// hundred sockets at once just to draw a status dot.
const maxInFlight = 16

// Target is one host to check. Skip marks hosts that cannot be reached
// directly, such as anything behind a ProxyJump.
type Target struct {
	Alias string
	Addr  string
	Skip  bool
}

// Result reports one host's reachability.
type Result struct {
	Alias string
	Up    bool
}

// Run dials every non-skipped target and streams results. The returned channel
// always closes, including on a canceled context.
func Run(ctx context.Context, targets []Target, timeout time.Duration) <-chan Result {
	out := make(chan Result)
	sem := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup

	for _, t := range targets {
		if t.Skip {
			continue
		}
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			d := net.Dialer{Timeout: timeout}
			conn, err := d.DialContext(ctx, "tcp", t.Addr)
			if err == nil {
				conn.Close()
			}
			select {
			case out <- Result{Alias: t.Alias, Up: err == nil}:
			case <-ctx.Done():
			}
		}(t)
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/probe/ -race -v`
Expected: PASS with no race warnings

- [ ] **Step 5: Commit**

```bash
git add internal/probe/
git commit -m "feat(probe): add bounded concurrent TCP reachability checks"
```

---

### Task 12: picker — fuzzy ranking

**Files:**

- Create: `internal/picker/rank.go`
- Test: `internal/picker/rank_test.go`

- [ ] **Step 1: Write the failing test**

```go
package picker

import (
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

func aliases(hosts []sshconfig.Host) []string {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, h.Alias)
	}
	return out
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

var corpus = []sshconfig.Host{
	{Alias: "alpha", HostName: "10.0.0.1"},
	{Alias: "nixos-dev", HostName: "192.0.2.10"},
	{Alias: "devbox", HostName: "10.0.0.7"},
	{Alias: "prod-web", HostName: "dev.example.com"},
	{Alias: "dev", HostName: "10.0.0.9"},
}

func TestRankEmptyQueryPreservesOrder(t *testing.T) {
	got := aliases(Rank(corpus, ""))
	want := []string{"alpha", "nixos-dev", "devbox", "prod-web", "dev"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want config order %v", got, want)
	}
}

func TestRankOrdersExactThenPrefixThenSubstring(t *testing.T) {
	got := aliases(Rank(corpus, "dev"))
	want := []string{"dev", "devbox", "nixos-dev", "prod-web"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v", got, want)
	}
}

func TestRankIsCaseInsensitive(t *testing.T) {
	if got := aliases(Rank(corpus, "NIXOS")); len(got) == 0 || got[0] != "nixos-dev" {
		t.Fatalf("Rank = %v, want nixos-dev first", got)
	}
}

func TestRankMatchesScatteredCharacters(t *testing.T) {
	got := aliases(Rank(corpus, "nxd"))
	if len(got) != 1 || got[0] != "nixos-dev" {
		t.Fatalf("Rank = %v, want only nixos-dev", got)
	}
}

func TestRankDropsNonMatches(t *testing.T) {
	if got := Rank(corpus, "zzzz"); len(got) != 0 {
		t.Fatalf("Rank = %v, want empty", aliases(got))
	}
}

func TestRankDoesNotMutateInput(t *testing.T) {
	Rank(corpus, "dev")
	if corpus[0].Alias != "alpha" {
		t.Fatal("Rank reordered its input slice")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestRank`
Expected: FAIL — `undefined: Rank`

- [ ] **Step 3: Implement ranking**

```go
// Package picker renders the SSH host overlay.
package picker

import (
	"sort"
	"strings"

	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

// Match quality, best first. Alias matches always beat hostname matches: the
// operator typed a name they chose, not an address they were assigned.
const (
	rankExact = iota
	rankAliasPrefix
	rankAliasSubstring
	rankAliasScattered
	rankHostSubstring
	rankHostScattered
	rankNone
)

// scattered reports whether every rune of q appears in s in order.
func scattered(s, q string) bool {
	rest := []rune(s)
	for _, want := range q {
		i := 0
		for ; i < len(rest); i++ {
			if rest[i] == want {
				break
			}
		}
		if i == len(rest) {
			return false
		}
		rest = rest[i+1:]
	}
	return true
}

func score(h sshconfig.Host, q string) int {
	alias := strings.ToLower(h.Alias)
	host := strings.ToLower(h.HostName)
	switch {
	case alias == q:
		return rankExact
	case strings.HasPrefix(alias, q):
		return rankAliasPrefix
	case strings.Contains(alias, q):
		return rankAliasSubstring
	case scattered(alias, q):
		return rankAliasScattered
	case strings.Contains(host, q):
		return rankHostSubstring
	case scattered(host, q):
		return rankHostScattered
	default:
		return rankNone
	}
}

// Rank returns the hosts matching query, best first. An empty query returns
// every host in config order. Ties keep config order, so the list never
// reshuffles for reasons the operator cannot see.
func Rank(hosts []sshconfig.Host, query string) []sshconfig.Host {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]sshconfig.Host, len(hosts))
		copy(out, hosts)
		return out
	}

	type scored struct {
		host  sshconfig.Host
		rank  int
		index int
	}
	matches := make([]scored, 0, len(hosts))
	for i, h := range hosts {
		if r := score(h, q); r != rankNone {
			matches = append(matches, scored{host: h, rank: r, index: i})
		}
	}
	sort.SliceStable(matches, func(a, b int) bool {
		if matches[a].rank != matches[b].rank {
			return matches[a].rank < matches[b].rank
		}
		return matches[a].index < matches[b].index
	})

	out := make([]sshconfig.Host, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.host)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/picker/ -run TestRank -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/picker/
git commit -m "feat(picker): add fuzzy host ranking with stable ordering"
```

---

### Task 13: picker — model and keymap

**Files:**

- Create: `internal/picker/model.go`
- Test: `internal/picker/model_test.go`

Background on Bubble Tea v2 (`charm.land/bubbletea/v2`), which differs from v1:

- `Update(tea.Msg) (tea.Model, tea.Cmd)` — returns `tea.Model`, not a concrete type
- `View() tea.View` — not `string`; build one with `tea.NewView(s)`
- Key presses arrive as `tea.KeyPressMsg{Code: rune-or-tea.KeyX, Mod: tea.ModCtrl, Text: "printable text"}`
- Special codes: `tea.KeyEnter`, `tea.KeyEsc`, `tea.KeyUp`, `tea.KeyDown`, `tea.KeyBackspace`

Tests drive `Update` directly — no terminal, no golden files, and each key is one assertion.

- [ ] **Step 1: Write the failing test**

```go
package picker

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// corpus comes from rank_test.go — same package.
func newTestModel() model {
	return newModel(Options{Hosts: corpus, Theme: theme.Default(), ShowPreview: true})
}

func press(m model, k tea.KeyPressMsg) model {
	next, _ := m.Update(k)
	return next.(model)
}

func typeRunes(m model, s string) model {
	for _, r := range s {
		m = press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func ctrl(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

func TestTypingFiltersAndResetsCursor(t *testing.T) {
	m := newTestModel()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}

	m = typeRunes(m, "dev")
	if m.query != "dev" {
		t.Fatalf("query = %q, want dev", m.query)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after refiltering", m.cursor)
	}
	if len(m.view) != 4 {
		t.Errorf("view = %v, want 4 matches", aliases(m.view))
	}
}

func TestBackspaceTrimsQuery(t *testing.T) {
	m := typeRunes(newTestModel(), "dev")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "de" {
		t.Fatalf("query = %q, want de", m.query)
	}
	// Backspace on an empty query is a no-op, not a crash.
	m.query = ""
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
}

func TestCursorClampsAtBothEnds(t *testing.T) {
	m := newTestModel()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 at the top", m.cursor)
	}
	for range corpus {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.cursor != len(corpus)-1 {
		t.Errorf("cursor = %d, want %d at the bottom", m.cursor, len(corpus)-1)
	}
	m = press(m, ctrl('k'))
	if m.cursor != len(corpus)-2 {
		t.Errorf("ctrl+k did not move the cursor up: %d", m.cursor)
	}
	m = press(m, ctrl('j'))
	if m.cursor != len(corpus)-1 {
		t.Errorf("ctrl+j did not move the cursor down: %d", m.cursor)
	}
}

func TestPlacementKeys(t *testing.T) {
	tests := []struct {
		name      string
		key       tea.KeyPressMsg
		placement string
		forceNew  bool
	}{
		{"enter splits", tea.KeyPressMsg{Code: tea.KeyEnter}, "split", false},
		{"ctrl+t opens a tab", ctrl('t'), "tab", false},
		{"ctrl+z zooms", ctrl('z'), "zoomed", false},
		{"ctrl+n forces a new split", ctrl('n'), "split", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := typeRunes(newTestModel(), "nixos")
			m = press(m, tc.key)
			if m.chosen == nil {
				t.Fatal("chosen = nil, want a selection")
			}
			if m.chosen.Host.Alias != "nixos-dev" {
				t.Errorf("alias = %q, want nixos-dev", m.chosen.Host.Alias)
			}
			if m.chosen.Placement != tc.placement {
				t.Errorf("placement = %q, want %q", m.chosen.Placement, tc.placement)
			}
			if m.chosen.ForceNew != tc.forceNew {
				t.Errorf("forceNew = %v, want %v", m.chosen.ForceNew, tc.forceNew)
			}
		})
	}
}

func TestSelectingWithNoMatchesIsIgnored(t *testing.T) {
	m := typeRunes(newTestModel(), "zzzz")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chosen != nil {
		t.Fatalf("chosen = %+v, want nil when nothing matches", m.chosen)
	}
}

func TestQuitKeysLeaveNoSelection(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, ctrl('c')} {
		m := press(newTestModel(), k)
		if m.chosen != nil {
			t.Errorf("chosen = %+v after quit key, want nil", m.chosen)
		}
		if !m.quitting {
			t.Error("quitting = false, want true")
		}
	}
}

func TestCtrlOTogglesPreview(t *testing.T) {
	m := newTestModel()
	if !m.preview {
		t.Fatal("preview = false, want the configured default of true")
	}
	m = press(m, ctrl('o'))
	if m.preview {
		t.Error("preview = true, want false after toggle")
	}
}

func TestProbeResultsMarkHostsUp(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(probeMsg{Alias: "nixos-dev", Up: true})
	m = next.(model)
	if !m.up["nixos-dev"] {
		t.Error("nixos-dev not marked up")
	}
	if !m.probed["nixos-dev"] {
		t.Error("nixos-dev not marked probed")
	}
	if m.probed["alpha"] {
		t.Error("alpha marked probed without a result")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	next, _ := newTestModel().Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m := next.(model)
	if m.width != 100 || m.height != 40 {
		t.Fatalf("size = %dx%d, want 100x40", m.width, m.height)
	}
}

func TestProbeChannelDrainsWithoutBlocking(t *testing.T) {
	ch := make(chan probe.Result, 1)
	ch <- probe.Result{Alias: "alpha", Up: true}
	close(ch)

	m := newModel(Options{Hosts: corpus, Theme: theme.Default(), Probes: ch})
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init returned no command, want a probe wait")
	}
	next, _ := m.Update(probeClosedMsg{})
	if next.(model).chosen != nil {
		t.Error("probeClosedMsg produced a selection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestTyping`
Expected: FAIL — `undefined: newModel`

- [ ] **Step 3: Implement the model**

```go
package picker

import (
	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// Selection is what the operator picked and how they want it opened.
type Selection struct {
	Host      sshconfig.Host
	Placement string // split | tab | zoomed
	ForceNew  bool
}

// Options configures one picker run.
type Options struct {
	Hosts       []sshconfig.Host
	Theme       theme.Theme
	ShowPreview bool
	// OpenPanes maps alias → pane id for sessions already running, so the
	// picker can mark them and reuse them.
	OpenPanes map[string]string
	Probes    <-chan probe.Result
	Warnings  []string
}

type probeMsg probe.Result

type probeClosedMsg struct{}

type model struct {
	opts     Options
	query    string
	view     []sshconfig.Host
	cursor   int
	preview  bool
	up       map[string]bool
	probed   map[string]bool
	width    int
	height   int
	chosen   *Selection
	quitting bool
}

func newModel(o Options) model {
	return model{
		opts:    o,
		view:    Rank(o.Hosts, ""),
		preview: o.ShowPreview,
		up:      map[string]bool{},
		probed:  map[string]bool{},
	}
}

// waitProbe reads one probe result and re-arms itself, turning the probe
// channel into a stream of messages.
func waitProbe(ch <-chan probe.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return probeClosedMsg{}
		}
		return probeMsg(r)
	}
}

func (m model) Init() tea.Cmd {
	if m.opts.Probes == nil {
		return nil
	}
	return waitProbe(m.opts.Probes)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case probeMsg:
		m.probed[msg.Alias] = true
		m.up[msg.Alias] = msg.Up
		return m, waitProbe(m.opts.Probes)
	case probeClosedMsg:
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl chords are checked before printable text: ctrl+t carries Text "t" on
	// some terminals, and typing "t" must not open a tab.
	if k.Mod&tea.ModCtrl != 0 {
		switch k.Code {
		case 'c':
			m.quitting = true
			return m, tea.Quit
		case 't':
			return m.choose("tab", false)
		case 'z':
			return m.choose("zoomed", false)
		case 'n':
			return m.choose("split", true)
		case 'o':
			m.preview = !m.preview
			return m, nil
		case 'j':
			return m.moveCursor(1), nil
		case 'k':
			return m.moveCursor(-1), nil
		}
		return m, nil
	}

	switch k.Code {
	case tea.KeyEsc:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEnter:
		return m.choose("split", false)
	case tea.KeyDown:
		return m.moveCursor(1), nil
	case tea.KeyUp:
		return m.moveCursor(-1), nil
	case tea.KeyBackspace:
		if m.query != "" {
			r := []rune(m.query)
			m.query = string(r[:len(r)-1])
			m = m.refilter()
		}
		return m, nil
	}

	if k.Text != "" {
		m.query += k.Text
		m = m.refilter()
	}
	return m, nil
}

func (m model) refilter() model {
	m.view = Rank(m.opts.Hosts, m.query)
	m.cursor = 0
	return m
}

func (m model) moveCursor(delta int) model {
	next := m.cursor + delta
	if next < 0 || next >= len(m.view) {
		return m
	}
	m.cursor = next
	return m
}

func (m model) choose(placement string, forceNew bool) (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.view) {
		return m, nil
	}
	m.chosen = &Selection{Host: m.view[m.cursor], Placement: placement, ForceNew: forceNew}
	m.quitting = true
	return m, tea.Quit
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/picker/ -v`
Expected: PASS — all model and rank tests

- [ ] **Step 5: Commit**

```bash
git add internal/picker/
git commit -m "feat(picker): add bubbletea model with placement keymap"
```

---

### Task 14: picker — rendering and Run

**Files:**

- Create: `internal/picker/view.go`
- Test: `internal/picker/view_test.go`

- [ ] **Step 1: Write the failing test**

```go
package picker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// render sizes the model, then reads the rendered text. tea.View is a struct
// with a Content field — it has no String method.
func render(m model) string {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	return stripANSI(next.(model).View().Content)
}

func TestViewListsHostsAndQuery(t *testing.T) {
	out := render(typeRunes(newTestModel(), "dev"))
	for _, want := range []string{"dev", "devbox", "nixos-dev"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "alpha") {
		t.Errorf("view still shows a host that does not match %q:\n%s", "dev", out)
	}
}

func TestViewShowsOpenMarker(t *testing.T) {
	m := newModel(Options{
		Hosts:     corpus,
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"nixos-dev": "w5:pB"},
	})
	if out := render(m); !strings.Contains(out, openMarker) {
		t.Errorf("view missing the open-session marker %q:\n%s", openMarker, out)
	}
}

func TestViewMarkersDistinguishOpenUpAndDown(t *testing.T) {
	m := newModel(Options{
		Hosts:     corpus,
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"alpha": "w5:pB"},
	})
	m.probed["nixos-dev"], m.up["nixos-dev"] = true, true
	m.probed["devbox"], m.up["devbox"] = true, false

	out := render(m)
	for _, marker := range []string{openMarker, upMarker, downMarker} {
		if !strings.Contains(out, marker) {
			t.Errorf("view missing marker %q:\n%s", marker, out)
		}
	}
}

func TestViewMarksProxyJumpHostsSkipped(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "jumped", HostName: "10.9.9.9", Port: "22", ProxyJump: "bastion"}},
		Theme: theme.Default(),
	})
	out := render(m)
	if !strings.Contains(out, skipMarker) {
		t.Errorf("view missing the not-probed marker %q:\n%s", skipMarker, out)
	}
	if !strings.Contains(out, "via bastion") {
		t.Errorf("view should show the jump host instead of a direct address:\n%s", out)
	}
}

func TestViewOpenMarkerWinsOverReachability(t *testing.T) {
	m := newModel(Options{
		Hosts:     []sshconfig.Host{{Alias: "alpha", HostName: "10.0.0.1", Port: "22"}},
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"alpha": "w5:pB"},
	})
	m.probed["alpha"], m.up["alpha"] = true, false

	out := render(m)
	if !strings.Contains(out, openMarker) {
		t.Errorf("view missing %q:\n%s", openMarker, out)
	}
	if strings.Contains(out, downMarker) {
		t.Errorf("down marker shadowed the open marker:\n%s", out)
	}
}

func TestViewShowsPreviewForCursorHost(t *testing.T) {
	m := typeRunes(newTestModel(), "nixos")
	out := render(m)
	if !strings.Contains(out, "192.0.2.10") {
		t.Errorf("preview missing the resolved hostname:\n%s", out)
	}
}

func TestViewHidesPreviewWhenToggledOff(t *testing.T) {
	m := press(typeRunes(newTestModel(), "nixos"), ctrl('o'))
	if out := render(m); strings.Contains(out, "192.0.2.10") {
		t.Errorf("preview still rendered after toggle:\n%s", out)
	}
}

func TestViewEmptyStateExplainsItself(t *testing.T) {
	out := render(typeRunes(newTestModel(), "zzzz"))
	if !strings.Contains(out, "no hosts match") {
		t.Errorf("view missing an empty-state message:\n%s", out)
	}
}

func TestViewNoConfigStateExplainsItself(t *testing.T) {
	out := render(newModel(Options{Theme: theme.Default()}))
	if !strings.Contains(out, "no ~/.ssh/config — nothing to pick") {
		t.Errorf("view missing a no-config message:\n%s", out)
	}
}

func TestViewShowsSourceProvenance(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{
			Alias: "inc", HostName: "10.0.0.1", Port: "22",
			SourceFile: "/Users/operator/.config/colima/ssh_config", SourceLine: 4,
		}},
		Theme:       theme.Default(),
		ShowPreview: true,
	})
	if out := render(m); !strings.Contains(out, "source /Users/operator/.config/colima/ssh_config:4") {
		t.Errorf("preview missing source provenance:\n%s", out)
	}
}

func TestViewShowsWarningCount(t *testing.T) {
	m := newModel(Options{
		Hosts:    []sshconfig.Host{{Alias: "a", HostName: "a"}},
		Theme:    theme.Default(),
		Warnings: []string{"config:3: include unreadable: nope"},
	})
	if out := render(m); !strings.Contains(out, "1 config warning") {
		t.Errorf("view missing the warning count:\n%s", out)
	}
}

func TestViewShowsKeyHints(t *testing.T) {
	out := render(newTestModel())
	for _, hint := range []string{"enter", "^t", "^z"} {
		if !strings.Contains(out, hint) {
			t.Errorf("view missing the %q hint:\n%s", hint, out)
		}
	}
}
```

Add the ANSI stripper as a test helper in the same file:

```go
// stripANSI removes styling so assertions test content, not colors.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/picker/ -run TestView`
Expected: FAIL — `m.View undefined`

- [ ] **Step 3: Implement the view**

```go
package picker

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Markers, per the spec. "a pane is already connected" and "the host answers on
// 22" are two different facts and get two different glyphs; conflating them
// would make the reuse affordance unreadable.
const (
	openMarker  = "▪" // a session pane exists (accent)
	upMarker    = "●" // TCP answered (green)
	downMarker  = "○" // no answer
	skipMarker  = "~" // ProxyJump, deliberately not probed
	blankMarker = " " // not probed yet
	maxRows     = 12
)

func (m model) View() tea.View {
	t := m.opts.Theme
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Accent)).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	upStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Up))

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", accent.Render("ssh"), text.Render(m.query+"▏"))

	switch {
	case len(m.opts.Hosts) == 0:
		b.WriteString(muted.Render("  no ~/.ssh/config — nothing to pick") + "\n")
	case len(m.view) == 0:
		b.WriteString(muted.Render("  no hosts match") + "\n")
	default:
		b.WriteString(m.renderRows(accent, text, muted, upStyle))
	}

	if m.preview && m.cursor < len(m.view) {
		b.WriteString(m.renderPreview(muted, text))
	}

	b.WriteString(muted.Render("  enter split · ^t tab · ^z zoom · ^n new · ^o preview · esc close") + "\n")
	if n := len(m.opts.Warnings); n > 0 {
		b.WriteString(muted.Render(fmt.Sprintf("  %d config warning(s)", n)) + "\n")
	}

	// Leave AltScreen and MouseMode at their zero values: the pane is already an
	// overlay, and the picker is keyboard-only.
	return tea.NewView(b.String())
}

// window returns the visible slice of rows and the cursor's offset inside it,
// scrolling only when the cursor would fall outside.
func (m model) window() ([]sshconfig.Host, int) {
	if len(m.view) <= maxRows {
		return m.view, m.cursor
	}
	start := m.cursor - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(m.view) {
		start = len(m.view) - maxRows
	}
	return m.view[start : start+maxRows], m.cursor - start
}

func (m model) renderRows(accent, text, muted, upStyle lipgloss.Style) string {
	rows, cursor := m.window()
	var b strings.Builder
	for i, h := range rows {
		marker := blankMarker
		style := muted
		switch {
		case h.ProxyJump != "":
			marker = skipMarker
		case m.probed[h.Alias] && m.up[h.Alias]:
			marker, style = upMarker, upStyle
		case m.probed[h.Alias]:
			marker = downMarker
		}
		// "open" wins over reachability: it is the marker that changes what
		// enter does.
		if _, open := m.opts.OpenPanes[h.Alias]; open {
			marker, style = openMarker, accent
		}

		alias := text.Render(h.Alias)
		pointer := "  "
		if i == cursor {
			pointer = accent.Render("▸ ")
			alias = accent.Render(h.Alias)
		}
		detail := h.HostName
		if h.User != "" {
			detail = h.User + "@" + detail
		}
		if h.Port != "22" {
			detail += ":" + h.Port
		}
		if h.ProxyJump != "" {
			detail = "via " + h.ProxyJump
		}
		fmt.Fprintf(&b, "%s%s %s  %s\n", pointer, style.Render(marker), alias, muted.Render(detail))
	}
	if len(m.view) > len(rows) {
		fmt.Fprintf(&b, "%s\n", muted.Render(fmt.Sprintf("  … %d more", len(m.view)-len(rows))))
	}
	return b.String()
}

func (m model) renderPreview(muted, text lipgloss.Style) string {
	h := m.view[m.cursor]
	lines := []string{"HostName " + h.HostName, "Port " + h.Port}
	if h.User != "" {
		lines = append(lines, "User "+h.User)
	}
	if h.IdentityFile != "" {
		lines = append(lines, "IdentityFile "+h.IdentityFile)
	}
	if h.ProxyJump != "" {
		lines = append(lines, "ProxyJump "+h.ProxyJump)
	}
	if h.SourceFile != "" {
		// Provenance matters as soon as Include is in play: "which file did this
		// host actually come from" is otherwise unanswerable from the picker.
		lines = append(lines, fmt.Sprintf("source %s:%d", h.SourceFile, h.SourceLine))
	}
	var b strings.Builder
	b.WriteString(muted.Render("  ─────") + "\n")
	for _, l := range lines {
		b.WriteString("  " + text.Render(l) + "\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Add `Run`**

Append to `internal/picker/view.go`:

```go
// Run shows the picker and blocks until the operator selects or quits. The
// second return value is false when they quit without choosing.
func Run(o Options) (Selection, bool, error) {
	p := tea.NewProgram(newModel(o))
	final, err := p.Run()
	if err != nil {
		return Selection{}, false, err
	}
	m, ok := final.(model)
	if !ok || m.chosen == nil {
		return Selection{}, false, nil
	}
	return *m.chosen, true, nil
}
```

Note: no `tea.WithAltScreen` — the pane is already an overlay, so a second alt-screen switch just adds a flash.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/picker/ -v`
Expected: PASS. `TestViewShowsPreviewForCursorHost` requires `corpus`'s `nixos-dev` HostName of `192.0.2.10` from Task 12.

- [ ] **Step 6: Verify it builds and vet is clean**

Run: `go vet ./...`
Expected: no output

- [ ] **Step 7: Commit**

```bash
git add internal/picker/
git commit -m "feat(picker): render host list, status markers, and preview"
```

---

### Task 15: cmd — caller context handoff

**Files:**

- Create: `cmd/herdr-ssh/caller.go`
- Test: `cmd/herdr-ssh/caller_test.go`

Background: the overlay pane is not the pane the operator was working in, so it cannot know where to put the split. The `open-picker` action runs in the caller's context and writes that context to `$HERDR_PLUGIN_STATE_DIR/caller.json`; the overlay reads it back.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadCaller(t *testing.T) {
	dir := t.TempDir()
	want := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := writeCaller(dir, want); err != nil {
		t.Fatalf("writeCaller: %v", err)
	}
	if got := readCaller(dir); got != want {
		t.Fatalf("readCaller = %+v, want %+v", got, want)
	}

	info, err := os.Stat(filepath.Join(dir, "caller.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestReadCallerDegradesGracefully(t *testing.T) {
	if got := readCaller(t.TempDir()); got != (caller{}) {
		t.Errorf("readCaller on a missing file = %+v, want zero value", got)
	}
	if got := readCaller(""); got != (caller{}) {
		t.Errorf("readCaller(\"\") = %+v, want zero value", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "caller.json"), []byte("{{{"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readCaller(dir); got != (caller{}) {
		t.Errorf("readCaller on malformed JSON = %+v, want zero value", got)
	}
}

func TestCurrentCallerReadsEnv(t *testing.T) {
	t.Setenv("HERDR_PANE_ID", "w9:p1")
	t.Setenv("HERDR_TAB_ID", "w9:t1")
	t.Setenv("HERDR_WORKSPACE_ID", "w9")

	want := caller{PaneID: "w9:p1", TabID: "w9:t1", WorkspaceID: "w9"}
	if got := currentCaller(); got != want {
		t.Fatalf("currentCaller = %+v, want %+v", got, want)
	}
}

func TestWriteCallerRejectsEmptyDir(t *testing.T) {
	if err := writeCaller("", caller{PaneID: "w5:pA"}); err == nil {
		t.Fatal("err = nil, want an error for an empty state dir")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run TestCaller`
Expected: FAIL — `undefined: caller`

- [ ] **Step 3: Implement the handoff**

```go
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const callerFile = "caller.json"

// caller is the pane the operator triggered the picker from. The overlay needs
// it to know where to place a split.
type caller struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

// currentCaller reads the herdr context this process was launched with.
func currentCaller() caller {
	return caller{
		PaneID:      os.Getenv("HERDR_PANE_ID"),
		TabID:       os.Getenv("HERDR_TAB_ID"),
		WorkspaceID: os.Getenv("HERDR_WORKSPACE_ID"),
	}
}

func writeCaller(stateDir string, c caller) error {
	if stateDir == "" {
		return errors.New("HERDR_PLUGIN_STATE_DIR is not set")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, callerFile), raw, 0o600)
}

// readCaller returns the zero value when the file is missing or unusable. A
// missing caller costs the operator a default placement, not the picker.
func readCaller(stateDir string) caller {
	if stateDir == "" {
		return caller{}
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, callerFile))
	if err != nil {
		return caller{}
	}
	var c caller
	if err := json.Unmarshal(raw, &c); err != nil {
		return caller{}
	}
	return c
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): pass caller pane context to the overlay via state dir"
```

---

### Task 16: cmd — session verb

**Files:**

- Create: `cmd/herdr-ssh/session.go`
- Test: `cmd/herdr-ssh/session_test.go`

Background: the session pane renames itself before `exec`ing ssh, because `plugin pane open` does not hand the new pane's id back to the caller. After `syscall.Exec` this process is gone and ssh owns the pty — no wrapper, no extra shell, and `^d` closes the pane the way the operator expects.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

func TestSessionArgvPutsFlagsBeforeDestination(t *testing.T) {
	// ssh parses `ssh [options] destination [command]`. Flags after the
	// destination become a remote command, so order is a correctness issue.
	got := sessionArgv([]string{"-o", "ConnectTimeout=5"}, "nixos-dev")
	want := "ssh -o ConnectTimeout=5 nixos-dev"
	if strings.Join(got, " ") != want {
		t.Fatalf("argv = %v, want %q", got, want)
	}
}

func TestSessionArgvWithoutFlags(t *testing.T) {
	if got := sessionArgv(nil, "web1"); strings.Join(got, " ") != "ssh web1" {
		t.Fatalf("argv = %v", got)
	}
}

func TestSessionLabel(t *testing.T) {
	if got := sessionLabel("nixos-dev"); got != "ssh:nixos-dev" {
		t.Fatalf("sessionLabel = %q, want ssh:nixos-dev", got)
	}
}

func TestPrepareSessionRenamesOwnPane(t *testing.T) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}}

	argv, err := prepareSession(api, pluginconfig.Defaults(), "nixos-dev", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if strings.Join(argv, " ") != "ssh nixos-dev" {
		t.Errorf("argv = %v", argv)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "pane rename w5:pC ssh:nixos-dev" {
		t.Errorf("calls = %v", calls)
	}
}

func TestPrepareSessionRequiresATarget(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) { return nil, nil }}
	if _, err := prepareSession(api, pluginconfig.Defaults(), "", "w5:pC"); err == nil {
		t.Fatal("err = nil, want an error for a missing target")
	}
}

func TestPrepareSessionToleratesRenameFailure(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte("no such pane"), errRenameTest
	}}
	// A failed rename costs pane reuse, not the connection. Connect anyway.
	argv, err := prepareSession(api, pluginconfig.Defaults(), "web1", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if strings.Join(argv, " ") != "ssh web1" {
		t.Fatalf("argv = %v", argv)
	}
}
```

Add the sentinel at the top of the test file:

```go
var errRenameTest = errors.New("rename failed")
```

with `"errors"` imported. `connect_test.go` in Task 17 reuses this sentinel.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run TestSession`
Expected: FAIL — `undefined: sessionArgv`

- [ ] **Step 3: Implement the session verb**

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

const labelPrefix = "ssh:"

func sessionLabel(alias string) string { return labelPrefix + alias }

// sessionArgv builds ssh's argv. Configured flags go before the destination;
// anything after it would be sent to the remote shell as a command.
func sessionArgv(sshArgs []string, alias string) []string {
	argv := make([]string, 0, len(sshArgs)+2)
	argv = append(argv, "ssh")
	argv = append(argv, sshArgs...)
	return append(argv, alias)
}

// prepareSession labels this pane so the picker can find it again, then returns
// the argv to exec. A rename failure is logged and ignored: losing pane reuse
// is much cheaper than losing the connection the operator asked for.
func prepareSession(api herdrapi.Client, cfg pluginconfig.Config, alias, paneID string) ([]string, error) {
	if alias == "" {
		return nil, errors.New("HERDR_SSH_TARGET is not set")
	}
	if paneID != "" {
		if err := api.PaneRename(paneID, sessionLabel(alias)); err != nil {
			fmt.Fprintf(os.Stderr, "herdr-ssh: could not label pane: %v\n", err)
		}
	}
	return sessionArgv(cfg.SSHArgs, alias), nil
}

// runSession replaces this process with ssh.
func runSession() error {
	cfg, err := pluginconfig.Load(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: %v — using defaults\n", err)
		cfg = pluginconfig.Defaults()
	}

	argv, err := prepareSession(herdrapi.New(), cfg, os.Getenv("HERDR_SSH_TARGET"), os.Getenv("HERDR_PANE_ID"))
	if err != nil {
		return err
	}

	bin, err := exec.LookPath("ssh")
	if err != nil {
		// Returning here would exit immediately and take the pane with it,
		// before the operator can read why. Show the command and hold.
		fmt.Printf("herdr-ssh: cannot run %v\n%v\n\npress enter to close\n", argv, err)
		fmt.Fscanln(os.Stdin)
		return fmt.Errorf("ssh not found on PATH: %w", err)
	}
	// Exec, so ssh owns the pty: no wrapper process, and ^d closes the pane.
	return syscall.Exec(bin, argv, os.Environ())
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): add session verb that labels its pane and execs ssh"
```

---

### Task 17: cmd — reuse or open a session pane

**Files:**

- Create: `cmd/herdr-ssh/connect.go`
- Test: `cmd/herdr-ssh/connect_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

const openPanesJSON = `{"id":1,"result":{"panes":[
  {"pane_id":"w5:pA","tab_id":"w5:t1","workspace_id":"w5","label":null},
  {"pane_id":"w8:pQ","tab_id":"w8:t3","workspace_id":"w8","label":"ssh:nixos-dev"}
]}}`

func fakeAPI(out string) (herdrapi.Client, *[][]string) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), nil
	}}
	return api, &calls
}

func joined(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

var devHost = sshconfig.Host{Alias: "nixos-dev", HostName: "192.0.2.10", Port: "22"}

func TestPerformSelectionOpensASplit(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	if err := performSelection(api, cfg, sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}

	got := joined(*calls)
	if len(got) != 1 {
		t.Fatalf("calls = %v, want exactly one open", got)
	}
	want := "plugin pane open --plugin purehate.herdr-ssh --entrypoint session " +
		"--placement split --target-pane w5:pA --direction right " +
		"--env HERDR_SSH_TARGET=nixos-dev --focus"
	if got[0] != want {
		t.Fatalf("argv =\n  %q\nwant\n  %q", got[0], want)
	}
}

func TestPerformSelectionReusesAnExistingPane(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}

	want := []string{
		"pane list --json",
		"workspace focus w8",
		"tab focus w8:t3",
		"plugin pane focus w8:pQ",
	}
	got := joined(*calls)
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPerformSelectionForceNewSkipsReuse(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split", ForceNew: true}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	for _, c := range joined(*calls) {
		if strings.Contains(c, "pane focus") {
			t.Fatalf("calls = %v, want no reuse when ForceNew is set", joined(*calls))
		}
	}
}

func TestPerformSelectionTabPlacementOmitsDirection(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "tab"}

	if err := performSelection(api, cfg, sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	got := joined(*calls)[0]
	if strings.Contains(got, "--direction") {
		t.Fatalf("argv = %q, want no --direction for a tab", got)
	}
	if !strings.Contains(got, "--placement tab") {
		t.Fatalf("argv = %q, want --placement tab", got)
	}
}

func TestPerformSelectionHonorsSplitDirection(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	cfg.SplitDirection = "down"
	sel := picker.Selection{Host: devHost, Placement: "split"}

	if err := performSelection(api, cfg, sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	if got := joined(*calls)[0]; !strings.Contains(got, "--direction down") {
		t.Fatalf("argv = %q, want --direction down", got)
	}
}

func TestPerformSelectionWithoutACallerPaneStillOpens(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "split"}

	if err := performSelection(api, cfg, sel, caller{}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	if got := joined(*calls)[0]; strings.Contains(got, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane when the caller is unknown", got)
	}
}

func TestPerformSelectionToleratesAFailedPaneList(t *testing.T) {
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		if args[0] == "pane" && args[1] == "list" {
			return []byte("socket gone"), errRenameTest
		}
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	sel := picker.Selection{Host: devHost, Placement: "split"}
	// Reuse is a convenience. If the lookup fails, open a fresh pane.
	if err := performSelection(api, pluginconfig.Defaults(), sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run TestPerformSelection`
Expected: FAIL — `undefined: performSelection`

- [ ] **Step 3: Implement it**

```go
package main

import (
	"fmt"
	"os"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

const pluginID = "purehate.herdr-ssh"

// performSelection focuses an existing session for this host when there is one,
// and otherwise opens a new session pane.
func performSelection(api herdrapi.Client, cfg pluginconfig.Config, sel picker.Selection, ctx caller) error {
	if cfg.ReusePanes && !sel.ForceNew {
		if pane, ok := findSession(api, sel.Host.Alias); ok {
			return api.FocusPane(pane, ctx.WorkspaceID, ctx.TabID)
		}
	}
	return api.PluginPaneOpen(herdrapi.OpenOpts{
		Plugin:     pluginID,
		Entrypoint: "session",
		Placement:  sel.Placement,
		TargetPane: ctx.PaneID,
		Direction:  cfg.SplitDirection,
		Env:        map[string]string{"HERDR_SSH_TARGET": sel.Host.Alias},
		Focus:      true,
	})
}

// findSession looks for a pane already labeled for this host. A failed lookup
// is reported and treated as "no existing session".
func findSession(api herdrapi.Client, alias string) (herdrapi.Pane, bool) {
	panes, err := api.PaneList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: could not list panes: %v\n", err)
		return herdrapi.Pane{}, false
	}
	return herdrapi.FindLabeled(panes, sessionLabel(alias))
}

// openSessions maps alias → pane id for every live ssh session, so the picker
// can mark them.
func openSessions(api herdrapi.Client) map[string]string {
	out := map[string]string{}
	panes, err := api.PaneList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: could not list panes: %v\n", err)
		return out
	}
	for _, p := range panes {
		if p.Label == nil {
			continue
		}
		if alias, ok := trimLabel(*p.Label); ok {
			out[alias] = p.PaneID
		}
	}
	return out
}

func trimLabel(label string) (string, bool) {
	if len(label) <= len(labelPrefix) || label[:len(labelPrefix)] != labelPrefix {
		return "", false
	}
	return label[len(labelPrefix):], true
}
```

- [ ] **Step 4: Add the `openSessions` test**

Append to `cmd/herdr-ssh/connect_test.go`:

```go
func TestOpenSessionsMapsLabeledPanes(t *testing.T) {
	api, _ := fakeAPI(openPanesJSON)
	got := openSessions(api)
	if len(got) != 1 || got["nixos-dev"] != "w8:pQ" {
		t.Fatalf("openSessions = %v, want nixos-dev → w8:pQ", got)
	}
}

func TestTrimLabel(t *testing.T) {
	if alias, ok := trimLabel("ssh:web1"); !ok || alias != "web1" {
		t.Errorf("trimLabel(ssh:web1) = (%q, %v)", alias, ok)
	}
	for _, label := range []string{"ssh:", "build", "", "sshweb1"} {
		if _, ok := trimLabel(label); ok {
			t.Errorf("trimLabel(%q) matched, want no match", label)
		}
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): reuse an existing ssh pane or open a new one"
```

---

### Task 18: cmd — host loading and probe targets

**Files:**

- Create: `cmd/herdr-ssh/hosts.go`
- Test: `cmd/herdr-ssh/hosts_test.go`

- [ ] **Step 1: Write the failing test**

```go
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

func TestLoadHostsMergesExtraPathsAndHidesGlobs(t *testing.T) {
	primary := writeSSHConfig(t, "config", "Host nixos-dev\n  HostName 10.0.0.1\n\nHost colima\n  HostName 127.0.0.1\n")
	extra := writeSSHConfig(t, "work", "Host client-jump\n  HostName 10.9.9.9\n\nHost old-box\n  HostName 10.9.9.10\n")

	cfg := pluginconfig.Defaults()
	cfg.ExtraConfigPaths = []string{extra}
	cfg.Hidden = []string{"colima", "*-box"}

	hosts, warns := loadHosts(primary, cfg)
	var got []string
	for _, h := range hosts {
		got = append(got, h.Alias)
	}
	want := "nixos-dev client-jump"
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

func TestLoadHostsWarnsOnAMissingExtraPath(t *testing.T) {
	primary := writeSSHConfig(t, "config", "Host a\n")
	cfg := pluginconfig.Defaults()
	cfg.ExtraConfigPaths = []string{filepath.Join(t.TempDir(), "nope")}

	hosts, warns := loadHosts(primary, cfg)
	if len(hosts) != 1 {
		t.Fatalf("hosts = %v, want the primary host", hosts)
	}
	// An extra path the operator explicitly configured is worth complaining about.
	if len(warns) != 1 || !strings.Contains(warns[0], "nope") {
		t.Fatalf("warnings = %v, want one naming the missing path", warns)
	}
}

func TestLoadHostsDeduplicatesByAlias(t *testing.T) {
	primary := writeSSHConfig(t, "config", "Host dup\n  HostName 10.0.0.1\n")
	extra := writeSSHConfig(t, "extra", "Host dup\n  HostName 10.0.0.2\n")
	cfg := pluginconfig.Defaults()
	cfg.ExtraConfigPaths = []string{extra}

	hosts, _ := loadHosts(primary, cfg)
	if len(hosts) != 1 {
		t.Fatalf("hosts = %+v, want one entry", hosts)
	}
	if hosts[0].HostName != "10.0.0.1" {
		t.Errorf("HostName = %q, want the primary config to win", hosts[0].HostName)
	}
}

func TestTargetsForJoinsHostAndPortAndSkipsProxyJump(t *testing.T) {
	hosts := []sshconfig.Host{
		{Alias: "a", HostName: "10.0.0.1", Port: "22"},
		{Alias: "b", HostName: "10.0.0.2", Port: "2222"},
		{Alias: "c", HostName: "10.0.0.3", Port: "22", ProxyJump: "a"},
	}
	got := targetsFor(hosts)
	if len(got) != 3 {
		t.Fatalf("targets = %+v", got)
	}
	if got[0].Addr != "10.0.0.1:22" || got[1].Addr != "10.0.0.2:2222" {
		t.Errorf("addrs = %q, %q", got[0].Addr, got[1].Addr)
	}
	if got[2].Skip != true {
		t.Error("a ProxyJump host was not skipped — probing it would dial the wrong network")
	}
	if got[0].Skip || got[1].Skip {
		t.Error("direct hosts were skipped")
	}
}

func TestSSHConfigPathUsesHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/fake-home")
	if got := sshConfigPath(); got != "/tmp/fake-home/.ssh/config" {
		t.Fatalf("sshConfigPath = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run 'TestLoadHosts|TestTargetsFor|TestSSHConfigPath'`
Expected: FAIL — `undefined: loadHosts`

- [ ] **Step 3: Implement it**

```go
package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

func sshConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}

// loadHosts parses the primary config plus any extra paths, drops hidden
// aliases, and returns human-readable warnings for the picker footer. The first
// definition of an alias wins, matching ssh's own precedence.
func loadHosts(primary string, cfg pluginconfig.Config) ([]sshconfig.Host, []string) {
	var hosts []sshconfig.Host
	var warnings []string
	seen := map[string]bool{}

	add := func(found []sshconfig.Host) {
		for _, h := range found {
			if seen[h.Alias] {
				continue
			}
			seen[h.Alias] = true
			hosts = append(hosts, h)
		}
	}

	found, warns, err := sshconfig.Parse(primary)
	switch {
	case errors.Is(err, sshconfig.ErrNoConfig):
		// No ssh config is a normal state, not a problem to report.
	case err != nil:
		warnings = append(warnings, fmt.Sprintf("%s: %v", primary, err))
	default:
		add(found)
	}
	for _, w := range warns {
		warnings = append(warnings, w.String())
	}

	for _, path := range cfg.ExtraConfigPaths {
		found, warns, err := sshconfig.Parse(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		add(found)
		for _, w := range warns {
			warnings = append(warnings, w.String())
		}
	}

	return sshconfig.Exclude(hosts, cfg.Hidden), warnings
}

// targetsFor builds probe targets. Hosts behind a ProxyJump are marked Skip: a
// direct dial would test the wrong network and report a false "down".
func targetsFor(hosts []sshconfig.Host) []probe.Target {
	out := make([]probe.Target, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, probe.Target{
			Alias: h.Alias,
			Addr:  net.JoinHostPort(h.HostName, h.Port),
			Skip:  h.ProxyJump != "",
		})
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/herdr-ssh/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): load hosts from ssh config and build probe targets"
```

---

### Task 19: cmd — picker verb, open-picker verb, and dispatch

**Files:**

- Create: `cmd/herdr-ssh/main.go`
- Test: `cmd/herdr-ssh/main_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
)

func TestRunRejectsUnknownVerbs(t *testing.T) {
	for _, args := range [][]string{{}, {"wat"}, {"plugin"}, {"plugin", "wat"}} {
		err := run(args)
		if err == nil {
			t.Fatalf("run(%v) = nil, want a usage error", args)
		}
		if !strings.Contains(err.Error(), "usage") {
			t.Errorf("run(%v) error = %q, want it to mention usage", args, err)
		}
	}
}

func TestOpenPickerWritesCallerAndOpensTheOverlay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	t.Setenv("HERDR_PANE_ID", "w5:pA")
	t.Setenv("HERDR_TAB_ID", "w5:t1")
	t.Setenv("HERDR_WORKSPACE_ID", "w5")

	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}}

	if err := openPicker(api); err != nil {
		t.Fatalf("openPicker: %v", err)
	}

	if got := readCaller(dir); got.PaneID != "w5:pA" || got.WorkspaceID != "w5" {
		t.Errorf("caller.json = %+v", got)
	}
	want := "plugin pane open --plugin purehate.herdr-ssh --entrypoint picker --placement overlay --focus"
	if len(calls) != 1 || strings.Join(calls[0], " ") != want {
		t.Fatalf("argv = %v, want %q", joined(calls), want)
	}
}

func TestOpenPickerFailsWithoutAStateDir(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	api := herdrapi.Client{Run: func([]string) ([]byte, error) { return nil, nil }}
	if err := openPicker(api); err == nil {
		t.Fatal("err = nil, want an error when the state dir is unset")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/herdr-ssh/ -run 'TestRun|TestOpenPicker'`
Expected: FAIL — `undefined: run`

- [ ] **Step 3: Implement dispatch and the two picker verbs**

```go
// Command herdr-ssh is the SSH picker plugin for herdr.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

const usage = `usage: herdr-ssh <picker|session|connect <alias> [--placement split|tab|zoomed]|plugin open-picker>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "picker":
		return runPicker()
	case "session":
		return runSession()
	case "connect":
		if len(args) < 2 {
			return errors.New(usage)
		}
		return runConnect(args[1:])
	case "plugin":
		if len(args) < 2 || args[1] != "open-picker" {
			return errors.New(usage)
		}
		return openPicker(herdrapi.New())
	default:
		return errors.New(usage)
	}
}

// openPicker runs in the caller's pane: it records where the operator was, then
// opens the overlay.
func openPicker(api herdrapi.Client) error {
	if err := writeCaller(os.Getenv("HERDR_PLUGIN_STATE_DIR"), currentCaller()); err != nil {
		return err
	}
	return api.PluginPaneOpen(herdrapi.OpenOpts{
		Plugin:     pluginID,
		Entrypoint: "picker",
		Placement:  "overlay",
		Focus:      true,
	})
}

// runPicker draws the overlay and acts on the operator's choice.
func runPicker() error {
	cfg, err := pluginconfig.Load(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: %v — using defaults\n", err)
		cfg = pluginconfig.Defaults()
	}

	hosts, warnings := loadHosts(sshConfigPath(), cfg)
	api := herdrapi.New()

	opts := picker.Options{
		Hosts:       hosts,
		Theme:       theme.Load(os.Getenv("HERDR_CONFIG_PATH")),
		ShowPreview: cfg.ShowPreview,
		OpenPanes:   openSessions(api),
		Warnings:    warnings,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.Probe && len(hosts) > 0 {
		opts.Probes = probe.Run(ctx, targetsFor(hosts), time.Duration(cfg.ProbeTimeoutMS)*time.Millisecond)
	}

	sel, ok, err := picker.Run(opts)
	if err != nil {
		return err
	}

	self := os.Getenv("HERDR_PANE_ID")
	if !ok {
		closeOverlay(api, self)
		return nil
	}

	if err := performSelection(api, cfg, sel, readCaller(os.Getenv("HERDR_PLUGIN_STATE_DIR"))); err != nil {
		// Hold the overlay open with the error on screen. Closing here would
		// take the only explanation with it.
		fmt.Fprintf(os.Stderr, "\nherdr-ssh: %v\n\npress enter to close\n", err)
		fmt.Fscanln(os.Stdin)
		closeOverlay(api, self)
		return err
	}
	closeOverlay(api, self)
	return nil
}

// closeOverlay dismisses the picker pane. Best effort: if the pane is already
// gone, saying so is noise.
func closeOverlay(api herdrapi.Client, paneID string) {
	if paneID == "" {
		return
	}
	if err := api.PaneClose(paneID); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-ssh: could not close the picker pane: %v\n", err)
	}
}

// runConnect opens a session for an alias without the picker, so the plugin is
// scriptable and bindable to a single key for a favorite host.
func runConnect(args []string) error {
	alias := args[0]
	placement := "split"
	for i := 1; i < len(args); i++ {
		if args[i] != "--placement" {
			return fmt.Errorf("%s\nunknown flag %q", usage, args[i])
		}
		if i+1 >= len(args) {
			return errors.New("--placement needs a value: split, tab, or zoomed")
		}
		i++
		switch args[i] {
		case "split", "tab", "zoomed":
			placement = args[i]
		default:
			return fmt.Errorf("placement %q must be split, tab, or zoomed", args[i])
		}
	}

	cfg, err := pluginconfig.Load(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		cfg = pluginconfig.Defaults()
	}
	hosts, _ := loadHosts(sshConfigPath(), cfg)
	for _, h := range hosts {
		if h.Alias == alias {
			sel := picker.Selection{Host: h, Placement: placement}
			return performSelection(herdrapi.New(), cfg, sel, currentCaller())
		}
	}
	return fmt.Errorf("host %q not found in ssh config", alias)
}
```

- [ ] **Step 4: Add a `runConnect` test**

Append to `cmd/herdr-ssh/main_test.go`:

```go
func TestRunConnectRejectsAnUnknownAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	err := runConnect([]string{"definitely-not-a-host"})
	if err == nil {
		t.Fatal("err = nil, want a not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %q", err)
	}
}

func TestRunConnectValidatesPlacement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")

	err := runConnect([]string{"host", "--placement", "sideways"})
	if err == nil || !strings.Contains(err.Error(), "must be split, tab, or zoomed") {
		t.Fatalf("err = %v, want a placement validation error", err)
	}
	if err := runConnect([]string{"host", "--placement"}); err == nil {
		t.Fatal("err = nil, want an error for a value-less --placement")
	}
	if err := runConnect([]string{"host", "--nope"}); err == nil {
		t.Fatal("err = nil, want an error for an unknown flag")
	}
}
```

- [ ] **Step 5: Run the full suite**

Run: `go test ./... -v`
Expected: PASS across all packages

- [ ] **Step 6: Build the binary**

Run: `go build -o bin/ ./cmd/herdr-ssh && ls bin/`
Expected: `herdr-ssh`

- [ ] **Step 7: Verify vet and formatting**

```bash
go vet ./...
gofmt -l .
```

Expected: no output from either

- [ ] **Step 8: Commit**

```bash
git add cmd/herdr-ssh/
git commit -m "feat(cmd): add verb dispatch, picker, and direct connect"
```

---

### Task 20: Install, bind the key, and smoke-test live

**Files:**

- Modify: `~/.config/herdr/config.toml`

This is the first end-to-end run. Everything before this was unit-tested; this is where the real herdr socket gets involved.

- [ ] **Step 1: Link the plugin into herdr**

```bash
cd ~/DEVELOPMENT/herdr-plugin-ssh
herdr plugin link .
```

Expected: herdr reports the plugin as linked. Confirm with:

```bash
herdr plugin list --json | rg herdr-ssh
```

Expected: a line containing `purehate.herdr-ssh`

- [ ] **Step 2: Verify the action is registered**

```bash
herdr plugin action list --json | rg open-picker
```

Expected: a line containing `purehate.herdr-ssh.open-picker`

If the action is missing, the manifest failed to load. Run `herdr plugin list --json` and read the error field before continuing.

- [ ] **Step 3: Bind the key**

Append to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "purehate.herdr-ssh.open-picker"
```

`prefix+i` rather than `prefix+r`: `prefix+r` is herdr's built-in `resize_mode`, and the picker should not displace a built-in.

- [ ] **Step 4: Validate and reload the config**

```bash
herdr config check
herdr server reload-config
```

Expected: `config check` reports no diagnostics, then the reload succeeds. Note the command is `herdr server reload-config` — there is no `herdr config reload`.

- [ ] **Step 5: Smoke-test the overlay by hand**

Press `prefix+i`. Verify each of these, in order:

1. A floating box appears listing hosts from `~/.ssh/config`, in the accent color from `[ui].accent` (`#14e21a` on this machine)
2. The `colima` host from the existing `Include` is present — the include chain resolved
3. Typing filters the list; the cursor snaps back to the top
4. Status markers fill in shortly after the box opens (`●` reachable, `○` not); first paint did not wait on the network
5. `enter` splits the current pane and lands at an ssh prompt for the selected host
6. The overlay closed itself after acting
7. The new pane's title reads `ssh:<alias>`
8. `prefix+i` again shows `▪` next to that host
9. Selecting it again focuses the existing pane instead of opening a second one
10. `^n` on that same host does open a second pane
11. `^t` opens a tab, `^z` opens a zoomed pane
12. `^o` toggles the preview, and the preview shows a `source <file>:<line>` line
13. `esc` closes the overlay and changes nothing

- [ ] **Step 6: Verify the label round-trip from the CLI**

```bash
herdr pane list --json | rg 'ssh:'
```

Expected: at least one pane with `"label":"ssh:<alias>"`

- [ ] **Step 7: Commit the plugin config note**

The herdr config lives outside this repo, so record the binding in the README instead (Task 21). Nothing to commit here.

If any step failed, fix it and re-run from Step 1. Do not proceed to Task 21 with a failing smoke test.

---

### Task 21: CI, README, and publish

**Files:**

- Create: `.github/workflows/ci.yml`, `README.md`, `LICENSE`

- [ ] **Step 1: Write the CI workflow**

`.github/workflows/ci.yml`:

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.27"
      - name: Verify formatting
        run: test -z "$(gofmt -l .)"
      - name: Vet
        run: go vet ./...
      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest
      - name: Test
        run: go test -race ./...
      - name: Build
        run: go build -o bin/ ./cmd/herdr-ssh
```

Add a minimal `.golangci.yml` so the linter's defaults are explicit rather than
version-dependent:

```yaml
version: "2"
linters:
  enable:
    - errcheck
    - govet
    - ineffassign
    - staticcheck
    - unused
```

- [ ] **Step 2: Write the README**

`README.md`:

````markdown
# herdr-ssh

A floating fuzzy picker over the hosts in your `~/.ssh/config`. Pick one, get an
SSH session in a new pane, tab, or zoomed pane. Modeled on tmux's `sesh` picker.

## Install

```bash
herdr plugin install github:purehate/herdr-plugin-ssh
```

Then bind a key in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "purehate.herdr-ssh.open-picker"
```

`prefix+i` is a suggestion. Avoid `prefix+r` — that is herdr's built-in resize mode.

## Keys

| Key                    | Action                               |
| ---------------------- | ------------------------------------ |
| type                   | fuzzy filter on alias, then hostname |
| `↑` / `↓`, `^k` / `^j` | move the cursor                      |
| `enter`                | ssh in a split                       |
| `^t`                   | ssh in a new tab                     |
| `^z`                   | ssh in a zoomed pane                 |
| `^n`                   | force a new pane even if one exists  |
| `^o`                   | toggle the host preview              |
| `esc`, `^c`            | close                                |

## Markers

| Marker  | Meaning                                       |
| ------- | --------------------------------------------- |
| ▪       | a session for this host is already open       |
| ●       | port reachable                                |
| ○       | port not reachable                            |
| ~       | behind a `ProxyJump`, deliberately not probed |
| (blank) | not probed yet                                |

Hosts behind a `ProxyJump` are not probed — a direct dial would test the wrong
network and report a false "down". `▪` wins over the reachability markers,
because it is the one that changes what `enter` does.

## Configuration

Optional, at `~/.config/herdr/plugin-config/purehate.herdr-ssh/config.toml`
(the directory herdr passes as `$HERDR_PLUGIN_CONFIG_DIR`):

```toml
probe = true                  # TCP-check hosts
probe_timeout_ms = 300
split_direction = "right"     # or "down"
show_preview = true
reuse_panes = true            # focus an existing ssh:<host> pane instead of opening another
hidden = []                   # globs matched against the alias
extra_config_paths = []       # additional ssh config files to read
ssh_args = []                 # flags passed to ssh, before the destination
```

## What it understands

Reads `~/.ssh/config` the way ssh does: first value wins, `Host *` and glob
patterns supply defaults without being selectable, `!negated` patterns are
honored, and `Include` is expanded with globbing and a cycle guard. `Match`
blocks are skipped — evaluating `Match exec` would mean running commands to
build a list.

## Direct connect

Skip the picker entirely from a shell in any pane:

```bash
herdr-ssh connect nixos-dev
herdr-ssh connect nixos-dev --placement tab
```

## License

MIT
````

- [ ] **Step 3: Add the license**

Write a standard MIT `LICENSE` with `Copyright (c) 2026 purehate`.

- [ ] **Step 4: Verify CI passes locally**

```bash
test -z "$(gofmt -l .)" && go vet ./... && go test -race ./... && go build -o bin/ ./cmd/herdr-ssh
```

Expected: no output before the build, and `bin/herdr-ssh` afterward

- [ ] **Step 5: Commit**

```bash
git add .github/ .golangci.yml README.md LICENSE
git commit -m "docs: add readme, license, and CI workflow"
```

- [ ] **Step 6: Publish (operator decision — confirm before running)**

Publishing pushes to a public repo. Confirm with the operator first, then:

```bash
gh repo create purehate/herdr-plugin-ssh --public --source=. --push
gh repo edit purehate/herdr-plugin-ssh --add-topic herdr-plugin
```

The `herdr-plugin` topic plus the root `herdr-plugin.toml` is all the index
needs — there is no submission queue. There is no `herdr plugin search`, so
verify the install path directly instead:

```bash
herdr plugin unlink ~/DEVELOPMENT/herdr-plugin-ssh
herdr plugin install github:purehate/herdr-plugin-ssh
herdr plugin list --json | rg herdr-ssh
```

Expected: the plugin installs from GitHub and appears in `plugin list` with no
error field. Re-run the Task 20 smoke test against the installed copy — the
build step runs on the install host, so a missing `go` toolchain shows up here
and nowhere earlier.

---

## Verification Checklist

Run after Task 21. Every line must pass before calling this done.

- [ ] `go test -race ./...` — all packages pass
- [ ] `go vet ./...` — no output
- [ ] `gofmt -l .` — no output
- [ ] `go build -o bin/ ./cmd/herdr-ssh` — builds
- [ ] `herdr plugin list --json | rg herdr-ssh` — plugin loads with no error field
- [ ] `prefix+i` opens the overlay with real hosts from `~/.ssh/config`
- [ ] `enter` lands at an ssh prompt in a new split
- [ ] The session pane is labeled `ssh:<alias>` in `herdr pane list --json`
- [ ] Re-picking an open host focuses it; `^n` opens a second pane
- [ ] `^t` and `^z` place a tab and a zoomed pane respectively
- [ ] `esc` closes cleanly with no pane created
- [ ] A config with a broken `Include` still lists every other host, with a warning count in the footer
