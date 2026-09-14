package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain makes this package hermetic, which it stopped being the moment the
// resolvers gained fallbacks.
//
// Before them, an unset HERDR_CONFIG_PATH meant theme.LoadFile("") and the
// built-in palette — a fixed answer on every machine. Now it means
// $HOME/.config/herdr/config.toml, so any test that calls runPickerWith without
// setting the variable reads the herdr config of whoever is running the suite,
// and resolvePluginConfigDir does the same for the plugin config. Several tests
// do exactly that: they are about closing the overlay and about holding the pane
// on failure, so they set HERDR_PANE_ID and nothing else.
//
// Those tests pass either way, which is the problem — they would keep passing
// while silently testing against a config nobody wrote down, and the first
// operator config to disagree with the defaults would fail a test about pane
// closing for reasons found nowhere near it.
//
// So: HOME points at an empty directory, and every HERDR_* variable this package
// reads is cleared. Both loaders return their defaults for an absent file, so an
// empty home is the same fixed answer the tests used to get. A test that wants
// one of these set still calls t.Setenv and wins, because t.Setenv is scoped
// tighter than this.
//
// os.Setenv rather than t.Setenv: there is no *testing.T here, and the
// assignment has to be in place before the first test runs.
//
// The mainProbeEnv branch is not an exemption from any of that. runMain re-execs
// this same binary as the process under test, so TestMain runs a second time
// inside the subprocess — and it runs before the test body, which means it would
// clear the HERDR_BIN_PATH the parent put in cmd.Env and point HOME at a second
// temp directory. The parent built that environment on purpose, out of an
// already-pinned os.Environ(), so the subprocess is hermetic by inheritance and
// the only thing left to do here is not overwrite it.
func TestMain(m *testing.M) {
	if os.Getenv(mainProbeEnv) != "" {
		os.Exit(m.Run())
	}

	home, err := os.MkdirTemp("", "herdr-picker-test-home")
	if err != nil {
		// Nothing has run yet, so there is no test to fail. Say why and stop
		// rather than continue with the real home and report a result about the
		// developer's machine.
		panic("cannot create a test home: " + err.Error())
	}

	for k, v := range map[string]string{
		"HOME":                      home,
		"HERDR_CONFIG_PATH":         "",
		"HERDR_PLUGIN_CONFIG_DIR":   "",
		"HERDR_PANE_ID":             "",
		"HERDR_TAB_ID":              "",
		"HERDR_WORKSPACE_ID":        "",
		"HERDR_ACTIVE_PANE_ID":      "",
		"HERDR_ACTIVE_TAB_ID":       "",
		"HERDR_ACTIVE_WORKSPACE_ID": "",
		callerPaneEnv:               "",
		callerTabEnv:                "",
		callerWorkspaceEnv:          "",
		"HERDR_PICKER_TARGET":       "",
	} {
		if err := os.Setenv(k, v); err != nil {
			panic("cannot pin " + k + ": " + err.Error())
		}
	}

	code := m.Run()
	// Not deferred: os.Exit does not run deferred functions.
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// clearHerdrEnv unsets every variable the resolvers below consult, so a test
// states its whole environment rather than inheriting whatever the shell that
// started `go test` happened to export. Without this a developer running the
// suite from inside herdr would have HERDR_ACTIVE_PANE_ID set and the
// "nothing set" cases would silently test something else.
func clearHerdrEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"HERDR_CONFIG_PATH",
		"HERDR_PLUGIN_CONFIG_DIR",
		"HERDR_PANE_ID",
		"HERDR_TAB_ID",
		"HERDR_WORKSPACE_ID",
		"HERDR_ACTIVE_PANE_ID",
		"HERDR_ACTIVE_TAB_ID",
		"HERDR_ACTIVE_WORKSPACE_ID",
		callerPaneEnv,
		callerTabEnv,
		callerWorkspaceEnv,
	} {
		t.Setenv(k, "")
	}
}

// TestTheSuiteDoesNotReadTheDevelopersOwnConfig asserts TestMain's pin from
// inside a test, which is the only place it matters. Every other test in this
// package trusts that pin implicitly: it is what makes an unset
// HERDR_CONFIG_PATH mean "the defaults" instead of "whatever this machine's
// operator configured". If the pin ever stopped taking effect, nothing else here
// would fail — the suite would just quietly start reading ~/.config/herdr.
//
// Asserted as "not the real home" rather than by re-deriving the temp path,
// because the claim is about isolation and that is the property to state.
func TestTheSuiteDoesNotReadTheDevelopersOwnConfig(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("no home at all during the suite: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "herdr", "config.toml")); err == nil {
		t.Errorf("the suite's HOME (%q) contains a real herdr config; TestMain's pin is not in effect and these tests are reading it", home)
	}
	for _, k := range []string{"HERDR_CONFIG_PATH", "HERDR_PLUGIN_CONFIG_DIR", "HERDR_ACTIVE_PANE_ID"} {
		if v := os.Getenv(k); v != "" {
			t.Errorf("%s = %q during the suite; TestMain did not clear it", k, v)
		}
	}
}

// TestHerdrConfigPathFallsBackToHerdrsOwnDefault holds the reason this resolver
// exists: a popup keybinding is a shell command, and herdr sets none of the
// plugin-pane variables for one. Measured against the live 0.9.0 popup env,
// which carries HERDR_ACTIVE_*, HERDR_BIN_PATH and HERDR_SOCKET_PATH and not
// HERDR_CONFIG_PATH. Reading the env var alone meant theme.LoadFile("") and the
// built-in palette, so the operator's accent was silently discarded in the one
// launch mode that renders a floating box.
func TestHerdrConfigPathFallsBackToHerdrsOwnDefault(t *testing.T) {
	clearHerdrEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".config", "herdr", "config.toml")
	if got := resolveHerdrConfigPath(); got != want {
		t.Errorf("resolveHerdrConfigPath() = %q, want %q", got, want)
	}
}

func TestHerdrConfigPathPrefersTheEnvVar(t *testing.T) {
	clearHerdrEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_CONFIG_PATH", "/somewhere/else/herdr.toml")

	if got := resolveHerdrConfigPath(); got != "/somewhere/else/herdr.toml" {
		t.Errorf("resolveHerdrConfigPath() = %q, want the env var to win", got)
	}
}

// TestPluginConfigDirFallsBackToTheDocumentedLayout pins the path against
// herdr's own answer. `herdr plugin config-dir purehate.herdr-picker` reports
// ~/.config/herdr/plugins/config/purehate.herdr-picker on 0.9.0, and pluginID is
// the single source of the last element so the two cannot drift.
func TestPluginConfigDirFallsBackToTheDocumentedLayout(t *testing.T) {
	clearHerdrEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".config", "herdr", "plugins", "config", pluginID)
	if got := resolvePluginConfigDir(); got != want {
		t.Errorf("resolvePluginConfigDir() = %q, want %q", got, want)
	}
}

func TestPluginConfigDirPrefersTheEnvVar(t *testing.T) {
	clearHerdrEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "/run/herdr/plugincfg")

	if got := resolvePluginConfigDir(); got != "/run/herdr/plugincfg" {
		t.Errorf("resolvePluginConfigDir() = %q, want the env var to win", got)
	}
}

// TestResolveCallerUsesTheActivePaneWhenNoCallerWasForwarded is the direct
// popup path. That launch bypasses openPicker, so no HERDR_PICKER_CALLER_* values
// exist; herdr exports the operator's pane as HERDR_ACTIVE_PANE_ID instead.
func TestResolveCallerUsesTheActivePaneWhenNoCallerWasForwarded(t *testing.T) {
	clearHerdrEnv(t)
	t.Setenv("HERDR_ACTIVE_PANE_ID", "wD:p2")
	t.Setenv("HERDR_ACTIVE_TAB_ID", "wD:t2")
	t.Setenv("HERDR_ACTIVE_WORKSPACE_ID", "wD")

	got := resolveCaller(caller{})
	want := caller{PaneID: "wD:p2", TabID: "wD:t2", WorkspaceID: "wD"}
	if got != want {
		t.Errorf("resolveCaller(zero) = %+v, want %+v", got, want)
	}
}

// A forwarded caller is authoritative. The action captures it before opening
// the picker; HERDR_ACTIVE_* read inside the picker could name the picker.
func TestResolveCallerKeepsAForwardedCaller(t *testing.T) {
	clearHerdrEnv(t)
	t.Setenv("HERDR_ACTIVE_PANE_ID", "wD:pOVERLAY")
	t.Setenv("HERDR_ACTIVE_TAB_ID", "wD:t9")
	t.Setenv("HERDR_ACTIVE_WORKSPACE_ID", "wD")

	forwarded := caller{PaneID: "w4:p3", TabID: "w4:t1", WorkspaceID: "w4"}
	if got := resolveCaller(forwarded); got != forwarded {
		t.Errorf("resolveCaller(%+v) = %+v, want the forwarded caller untouched", forwarded, got)
	}
}

// TestResolveCallerFillsOnlyWhatIsMissing covers the partial case rather than
// treating "no pane id" as "no caller at all": forwarded context carrying a
// pane id but an empty workspace should keep the id and gain the workspace.
func TestResolveCallerFillsOnlyWhatIsMissing(t *testing.T) {
	clearHerdrEnv(t)
	t.Setenv("HERDR_ACTIVE_PANE_ID", "wD:pACTIVE")
	t.Setenv("HERDR_ACTIVE_TAB_ID", "wD:t2")
	t.Setenv("HERDR_ACTIVE_WORKSPACE_ID", "wD")

	got := resolveCaller(caller{PaneID: "w4:p3"})
	want := caller{PaneID: "w4:p3", TabID: "wD:t2", WorkspaceID: "wD"}
	if got != want {
		t.Errorf("resolveCaller = %+v, want %+v", got, want)
	}
}

// TestResolveCallerStaysEmptyOutsideHerdr guards the degrade: with nothing set
// the result must stay the zero value, which performSelection already handles
// by omitting --target-pane rather than sending a bogus one.
func TestResolveCallerStaysEmptyOutsideHerdr(t *testing.T) {
	clearHerdrEnv(t)

	if got := resolveCaller(caller{}); got != (caller{}) {
		t.Errorf("resolveCaller(zero) = %+v, want the zero value", got)
	}
}

// TestPickerSelfPaneIsNotTheActivePane is the dangerous one, and it is asserted
// on the production call rather than on a resolver. `self` is the pane the
// picker closes on the way out. HERDR_ACTIVE_PANE_ID names the operator's pane,
// never the picker's, so falling back to it here would make the picker close
// the pane the operator was working in. The fallback is deliberately absent
// from this one read, and this test is what stops a later reader from
// "completing" the pattern.
func TestPickerSelfPaneIsNotTheActivePane(t *testing.T) {
	clearHerdrEnv(t)
	t.Setenv("HERDR_ACTIVE_PANE_ID", "wD:pOPERATOR")

	if got := pickerSelfPane(); got != "" {
		t.Errorf("pickerSelfPane() = %q with only HERDR_ACTIVE_PANE_ID set; the picker would close the operator's own pane", got)
	}

	t.Setenv("HERDR_PANE_ID", "wD:pPICKER")
	if got := pickerSelfPane(); got != "wD:pPICKER" {
		t.Errorf("pickerSelfPane() = %q, want the picker's own pane", got)
	}
}
