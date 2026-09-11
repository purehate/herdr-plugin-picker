package main

import (
	"os"
	"path/filepath"
)

// The picker has two launch modes, and herdr hands them different environments.
//
// A plugin pane — `herdr plugin pane open --entrypoint picker`, which the
// `plugin open-picker` action triggers — gets HERDR_PLUGIN_CONFIG_DIR,
// HERDR_PLUGIN_STATE_DIR and HERDR_PANE_ID. A popup keybinding
// (`type = "popup"`) gets none of them: it runs a shell command, so herdr
// exports only the session context (HERDR_ACTIVE_*, HERDR_BIN_PATH,
// HERDR_SOCKET_PATH, HERDR_ENV). Measured on herdr 0.9.0 rather than assumed.
//
// That matters because the popup is the mode that renders a floating box.
// Reading the plugin-pane variables alone meant it silently ran on built-in
// defaults — no operator accent, no plugin config — because both loaders treat
// an empty path as "no location supplied" and return defaults without an error.
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
// so pickerCaller returns the zero value, and HERDR_ACTIVE_PANE_ID names the
// pane the operator triggered the popup from.
//
// Field by field, not all-or-nothing: a forwarded caller always wins, because
// inside a plugin popup pane HERDR_ACTIVE_* may name the popup itself. An
// absent value costs a redundant focus; a wrong one jumps to the wrong place.
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
// the way out. It deliberately has no HERDR_ACTIVE_PANE_ID fallback: that
// variable names the operator's pane, never the picker's, and falling back would
// close the pane the operator was working in. In popup mode the answer is "",
// on which closeOverlay no-ops — a popup closes itself when its command exits.
// A named function rather than an inline Getenv, so the absence is visible.
func pickerSelfPane() string {
	return os.Getenv("HERDR_PANE_ID")
}
