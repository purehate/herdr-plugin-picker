package main

import (
	"os"
	"path/filepath"
)

// The picker has two launch modes, and herdr hands them different environments.
//
// A plugin pane — `herdr plugin pane open --entrypoint picker`, which is what
// the `plugin open-picker` action triggers — gets HERDR_PLUGIN_CONFIG_DIR,
// HERDR_PLUGIN_STATE_DIR and HERDR_PANE_ID. A popup keybinding
// (`type = "popup"`) gets none of them: a popup runs a shell command, so herdr
// exports only the session context — HERDR_ACTIVE_PANE_ID, HERDR_ACTIVE_TAB_ID,
// HERDR_ACTIVE_WORKSPACE_ID, HERDR_BIN_PATH, HERDR_SOCKET_PATH, HERDR_ENV.
// Measured on herdr 0.9.0 rather than assumed.
//
// That matters because the popup is the mode that renders a floating box; the
// overlay placement calls layout.set_split_ratio and docks. Reading the
// plugin-pane variables alone meant the floating mode silently ran on built-in
// defaults: no operator accent, and no plugin config at all. Silently, because
// both loaders treat an empty path as "no location was supplied" and return
// defaults without an error — the honest answer when nothing is set, and the
// wrong one when something is set somewhere else.
//
// So each resolver prefers the variable and falls back to the location herdr
// itself documents. Nothing here invents a path.

// resolveHerdrConfigPath returns the herdr config to read the theme from.
// `herdr --help` documents both halves: "Config: ~/.config/herdr/config.toml"
// and "Env: HERDR_CONFIG_PATH overrides config file path".
func resolveHerdrConfigPath() string {
	if p := os.Getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Same degrade as sshConfigPath: an empty path makes theme.LoadFile
		// return the default palette, which is exactly the right answer when we
		// cannot tell where the operator's config lives.
		return ""
	}
	return filepath.Join(home, ".config", "herdr", "config.toml")
}

// resolvePluginConfigDir returns this plugin's config directory.
// `herdr plugin config-dir purehate.herdr-ssh` answers
// ~/.config/herdr/plugins/config/purehate.herdr-ssh on 0.9.0. pluginID supplies
// the last element so the path cannot drift from the manifest.
func resolvePluginConfigDir() string {
	if d := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herdr", "plugins", "config", pluginID)
}

// resolveCaller fills in context that openPicker did not forward. In direct
// popup mode openPicker does not run — the popup execs the picker verb itself —
// so pickerCaller returns the zero value. HERDR_ACTIVE_PANE_ID names the pane
// the operator triggered that popup from and is the right substitute.
//
// Field by field, not all-or-nothing: a forwarded caller always wins, because
// inside a plugin popup pane the HERDR_ACTIVE_* variables may name the popup
// itself. Filling only the empty fields also keeps a partial
// invocation-scoped context useful — FocusPane compares the target's workspace
// and tab against these, and an absent value costs a redundant focus while a
// wrong one costs a jump to the wrong place.
func resolveCaller(c caller) caller {
	if c.PaneID == "" {
		c.PaneID = os.Getenv("HERDR_ACTIVE_PANE_ID")
	}
	if c.TabID == "" {
		c.TabID = os.Getenv("HERDR_ACTIVE_TAB_ID")
	}
	if c.WorkspaceID == "" {
		c.WorkspaceID = os.Getenv("HERDR_ACTIVE_WORKSPACE_ID")
	}
	return c
}

// pickerSelfPane returns the pane the picker is drawing into, which it closes on
// the way out — and it deliberately has no HERDR_ACTIVE_PANE_ID fallback.
//
// That variable names the operator's pane, never the picker's. Falling back to
// it here would hand closeOverlay the pane the operator was working in and
// close that instead. In popup mode the correct answer is the empty string:
// closeOverlay no-ops on it, and a popup closes itself when its command exits.
// Kept as a named function rather than an inline Getenv so the absence is
// something a reader can see, and so a test can hold it.
func pickerSelfPane() string {
	return os.Getenv("HERDR_PANE_ID")
}
