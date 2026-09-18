package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

// lasttab.go remembers which tab the operator was on, so the next popup opens
// there. It stores the tab's name, not its index: adding or reordering tabs must
// not silently move the operator to a different one.

// lastTabPath is the state file, alongside the ssh tab's frecency file. An empty
// state dir — no HERDR_PLUGIN_STATE_DIR and no resolvable home — disables the
// feature rather than writing somewhere arbitrary.
func lastTabPath() string {
	dir := resolvePluginStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "last-tab")
}

// loadLastTab returns the saved tab name, or "" when there is none. A missing
// file is the normal first run, not an error worth reporting.
func loadLastTab(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// saveLastTab writes the tab name. Best effort: an unwritable state directory is
// not worth failing a picker over, so the error is dropped rather than surfaced.
func saveLastTab(path string, s picker.NavSection) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(picker.NavName(s)), 0o644)
}
