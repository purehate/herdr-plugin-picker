package main

// herdr-plugin.toml is the only description of this plugin herdr ever reads:
// it learns the plugin's id from it, which entrypoints exist, and what argv to
// exec for each one. No Go file reads it back, so every value in it is
// duplicated as a literal on the Go side — pluginID against the manifest's id,
// the Entrypoint strings against the [[panes]] ids, and the binary the commands
// exec against wherever [[build]] writes it. Nothing compiled here breaks when
// the two halves drift; the plugin simply fails to launch, or asks herdr for an
// entrypoint that does not exist, at runtime on the operator's machine.
//
// These tests are that missing read. They assert the manifest against the
// production values rather than against restated literals, so a test literal
// cannot pin only the half herdr does not read.
//
// STILL UNGUARDED, deliberately: that each command's argv verb — "navigator",
// "session", "plugin open-navigator" — is one run() dispatches. run() cannot be
// called for it. `run(["navigator"])` draws the picker, which needs a tty, and
// `run(["plugin","open-navigator"])` builds a real herdr client against
// HERDR_BIN_PATH. Making either observable means a new seam in main.go, and a
// test that settled for matching the verbs against the usage string would be
// asserting on a help message, not on dispatch. So a verb renamed in run()
// without the manifest following still compiles, still passes, and still fails
// at launch.

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
	"github.com/purehate/herdr-plugin-picker/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

// manifestEntry is one [[panes]] or [[actions]] table. Only the two fields with
// a counterpart in the Go code are modelled; herdr reads more (title, contexts,
// placement) and go-toml ignores what is not declared here.
type manifestEntry struct {
	ID      string   `toml:"id"`
	Command []string `toml:"command"`
}

type manifestBuild struct {
	Command []string `toml:"command"`
}

type manifestDoc struct {
	ID      string          `toml:"id"`
	Build   []manifestBuild `toml:"build"`
	Actions []manifestEntry `toml:"actions"`
	Panes   []manifestEntry `toml:"panes"`
}

func (m manifestDoc) paneIDs() []string {
	ids := make([]string, 0, len(m.Panes))
	for _, p := range m.Panes {
		ids = append(ids, p.ID)
	}
	return ids
}

func (m manifestDoc) declaresPane(id string) bool {
	for _, p := range m.Panes {
		if p.ID == id {
			return true
		}
	}
	return false
}

// binary is where [[build]] leaves the plugin executable, derived rather than
// restated so that moving the output directory fails the commands test instead
// of quietly passing it.
func (m manifestDoc) binary(t *testing.T) string {
	t.Helper()
	var out []string
	for _, b := range m.Build {
		if p, ok := manifestBuildOutput(b.Command); ok {
			out = append(out, p)
		}
	}
	if len(out) != 1 {
		t.Fatalf("want exactly one [[build]] command with a -o target, got %v from %+v", out, m.Build)
	}
	return out[0]
}

// manifestBuildOutput reproduces `go build -o`'s own rule: a target ending in a
// separator is a directory, and the binary inside it takes the name of the
// package directory being built; any other target is itself the binary path.
// Reported as (path, false) rather than fatally, so a [[build]] stanza that is
// not a `go build` — a codegen step, say — is skipped instead of failing.
func manifestBuildOutput(argv []string) (string, bool) {
	if len(argv) < 2 {
		return "", false
	}
	// `go build [flags] packages`: the package is the last argument, and a
	// leading dash there means there is no package argument at all.
	pkg := argv[len(argv)-1]
	if len(pkg) > 0 && pkg[0] == '-' {
		return "", false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] != "-o" {
			continue
		}
		target := argv[i+1]
		if len(target) > 0 && target[len(target)-1] == '/' {
			return path.Join(target, path.Base(pkg)), true
		}
		return path.Clean(target), true
	}
	return "", false
}

// manifestPath resolves the manifest from this source file rather than from the
// working directory. `go test` does set the working directory to the package
// directory, but a check that silently reads some other tree — or reports a
// pass about one — is the exact failure this file exists to prevent, so the
// resolved root is confirmed to be the module root before it is trusted.
func manifestPath(t *testing.T) string {
	t.Helper()
	dir := ""
	if _, self, _, ok := runtime.Caller(0); ok {
		if _, err := os.Stat(self); err == nil {
			dir = filepath.Dir(self)
		}
	}
	if dir == "" {
		// -trimpath rewrites runtime.Caller's path to a module-relative one that
		// does not exist on disk. Fall back to the working directory, which
		// `go test` sets to the package directory either way.
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("cannot locate the manifest: no usable source path and getwd failed: %v", err)
		}
		dir = wd
	}
	root := filepath.Clean(filepath.Join(dir, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved %s as the module root, but it holds no go.mod: %v", root, err)
	}
	return filepath.Join(root, "herdr-plugin.toml")
}

// manifestLoad reads and parses the manifest. A missing or unparseable manifest
// is fatal, never a skip: this file is the only reader of the manifest in the
// repo, so a skip here restores the unguarded state while still reporting green.
func manifestLoad(t *testing.T) manifestDoc {
	t.Helper()
	p := manifestPath(t)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("cannot read the plugin manifest at %s: %v", p, err)
	}
	var m manifestDoc
	if err := toml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("cannot parse the plugin manifest at %s: %v", p, err)
	}
	return m
}

// manifestRecorder is a herdr client that records argv and answers every call
// with an empty result envelope, so a pane list on the way to the open decodes
// cleanly instead of writing a diagnostic. Declared here rather than reused
// from a neighbouring test file: these tests must keep working while the files
// around them are being edited.
func manifestRecorder(calls *[][]string) herdrapi.Client {
	return herdrapi.Client{Run: func(args []string) ([]byte, error) {
		*calls = append(*calls, args)
		return []byte(`{"id":1,"result":{"panes":[]}}`), nil
	}}
}

// manifestOpenFlag returns the value of flag on the one `plugin pane open` call
// in calls. Scanning for the call rather than indexing calls[0], because a path
// that gained a preceding `pane list` would otherwise be checked against argv
// that cannot contain the flag at all.
func manifestOpenFlag(t *testing.T, calls [][]string, flag string) string {
	t.Helper()
	var found []string
	for _, c := range calls {
		if len(c) < 3 || c[0] != "plugin" || c[1] != "pane" || c[2] != "open" {
			continue
		}
		for i := 0; i < len(c)-1; i++ {
			if c[i] == flag {
				found = append(found, c[i+1])
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one %s on a `plugin pane open` call, got %v from argv %v", flag, found, calls)
	}
	return found[0]
}

func manifestOpenEntrypoint(t *testing.T, calls [][]string) string {
	t.Helper()
	return manifestOpenFlag(t, calls, "--entrypoint")
}

// manifestOpenPlugin exists because TestManifestIDMatchesPluginID pins the
// manifest against the pluginID *constant*, which says nothing about whether a
// call site uses it. A literal inlined at an open would satisfy that test and
// still name a plugin herdr has not registered, so the name is read back out of
// the argv the call actually sent.
func manifestOpenPlugin(t *testing.T, calls [][]string) string {
	t.Helper()
	return manifestOpenFlag(t, calls, "--plugin")
}

// TestManifestIDMatchesPluginID pins the name every `plugin pane open` is sent
// under against the name herdr registers the plugin as. They disagree and herdr
// rejects the open: the plugin the argv names is not installed.
func TestManifestIDMatchesPluginID(t *testing.T) {
	m := manifestLoad(t)
	if m.ID != pluginID {
		t.Errorf("herdr-plugin.toml id = %q, but connect.go's pluginID = %q; herdr resolves the open by this name", m.ID, pluginID)
	}
}

// TestOpenNavigatorUsesAManifestPaneID asserts the coupling behaviourally: it
// takes the entrypoint out of the argv openNavigator actually sends, so the test
// keeps checking the real value if the literal in main.go moves or is computed.
func TestOpenNavigatorUsesAManifestPaneID(t *testing.T) {
	m := manifestLoad(t)

	var calls [][]string
	if err := openNavigator(manifestRecorder(&calls)); err != nil {
		t.Fatalf("openNavigator: %v", err)
	}

	got := manifestOpenEntrypoint(t, calls)
	if !m.declaresPane(got) {
		t.Errorf("openNavigator opens entrypoint %q, which herdr-plugin.toml does not declare as a [[panes]] id; declared: %v", got, m.paneIDs())
	}
	if got := manifestOpenPlugin(t, calls); got != pluginID {
		t.Errorf("openNavigator opens plugin %q, not pluginID %q; the manifest is pinned against the constant, so a literal here is unchecked", got, pluginID)
	}
}

// TestPerformSelectionUsesAManifestPaneID is the same assertion for the pane
// the operator's chosen host is connected in — the one that carries the whole
// point of the plugin.
func TestPerformSelectionUsesAManifestPaneID(t *testing.T) {
	m := manifestLoad(t)

	cfg := pluginconfig.Defaults()
	// Reuse off, and no caller pane id below, so the open is the only call: this
	// test is about the entrypoint, not about the pane-list paths around it.
	cfg.ReusePanes = false
	sel := picker.Selection{Host: sshconfig.Host{Alias: "manifest-contract.invalid"}, Placement: "split"}

	var calls [][]string
	if err := performSelection(io.Discard, manifestRecorder(&calls), cfg, sel, caller{}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}

	got := manifestOpenEntrypoint(t, calls)
	if !m.declaresPane(got) {
		t.Errorf("performSelection opens entrypoint %q, which herdr-plugin.toml does not declare as a [[panes]] id; declared: %v", got, m.paneIDs())
	}
	if got := manifestOpenPlugin(t, calls); got != pluginID {
		t.Errorf("performSelection opens plugin %q, not pluginID %q; the manifest is pinned against the constant, so a literal here is unchecked", got, pluginID)
	}
}

// TestManifestCommandsPointAtTheBuildOutput checks the other end of the launch:
// herdr execs these argv directly, so a command naming a path [[build]] does
// not write is a plugin that installs and then cannot start.
func TestManifestCommandsPointAtTheBuildOutput(t *testing.T) {
	m := manifestLoad(t)
	want := m.binary(t)

	check := func(kind string, entries []manifestEntry) {
		for _, e := range entries {
			if len(e.Command) == 0 {
				continue // reported by the well-formedness test
			}
			// Clean both sides: "./bin/herdr-picker" and "bin/herdr-picker" name the
			// same file, and the assertion is about the directory and the binary
			// name, not about the leading-dot spelling.
			if got := path.Clean(e.Command[0]); got != want {
				t.Errorf("%s %q execs %q, but [[build]] writes the binary to %q", kind, e.ID, e.Command[0], want)
			}
		}
	}
	check("pane", m.Panes)
	check("action", m.Actions)
}

// TestManifestPanesAndActionsAreWellFormed catches a truncated or half-edited
// manifest: an entry herdr cannot resolve or cannot exec, and a duplicate pane
// id, which makes the entrypoint the Go side asks for ambiguous.
func TestManifestPanesAndActionsAreWellFormed(t *testing.T) {
	m := manifestLoad(t)
	if len(m.Panes) == 0 {
		t.Error("manifest declares no [[panes]]; the plugin has no pane entrypoint to open")
	}
	if len(m.Actions) == 0 {
		t.Error("manifest declares no [[actions]]; the operator has no way to invoke the plugin")
	}

	seen := map[string]int{}
	for i, p := range m.Panes {
		if p.ID == "" {
			t.Errorf("[[panes]] #%d has an empty id", i)
		}
		if len(p.Command) == 0 {
			t.Errorf("pane %q (#%d) has no command; herdr has nothing to exec", p.ID, i)
		}
		if prev, dup := seen[p.ID]; dup {
			t.Errorf("pane id %q is declared twice (#%d and #%d); the entrypoint it names is ambiguous", p.ID, prev, i)
		}
		seen[p.ID] = i
	}
	for i, a := range m.Actions {
		if a.ID == "" {
			t.Errorf("[[actions]] #%d has an empty id", i)
		}
		if len(a.Command) == 0 {
			t.Errorf("action %q (#%d) has no command; herdr has nothing to exec", a.ID, i)
		}
	}
}
