package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

func TestSaveAndLoadLastTab(t *testing.T) {
	// A subdirectory that does not exist yet: saveLastTab must create it.
	path := filepath.Join(t.TempDir(), "sub", "last-tab")
	if got := loadLastTab(path); got != "" {
		t.Fatalf("missing file = %q, want empty", got)
	}
	saveLastTab(path, picker.NavPanes)
	if got := loadLastTab(path); got != "panes" {
		t.Fatalf("round trip = %q, want panes", got)
	}
	// Trailing whitespace from an editor or a partial write is trimmed.
	if err := os.WriteFile(path, []byte("ssh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadLastTab(path); got != "ssh" {
		t.Fatalf("trimmed load = %q, want ssh", got)
	}
	// An empty path disables the feature rather than writing somewhere else.
	if loadLastTab("") != "" {
		t.Fatal("empty path read something")
	}
	saveLastTab("", picker.NavSSH) // must not panic or write
}

// The real run opens on the saved tab and writes a later switch back, so the
// next popup lands where the operator left off.
func TestRunNavigatorOpensOnTheLastTabAndPersistsChanges(t *testing.T) {
	stateDir := t.TempDir()
	navigatorEnv(t, t.TempDir())
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	if err := os.WriteFile(filepath.Join(stateDir, "last-tab"), []byte("ssh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api, _ := fakeAPI(openPanesJSON)
	pick := func(opts picker.NavOptions) (picker.NavSelection, bool, error) {
		if opts.StartSection != picker.NavSSH {
			t.Fatalf("StartSection = %v, want ssh", opts.StartSection)
		}
		if opts.OnSection == nil {
			t.Fatal("OnSection was not wired")
		}
		opts.OnSection(picker.NavPanes) // what the model does on a tab switch
		return picker.NavSelection{}, false, nil
	}
	if err := runNavigatorWith(io.Discard, strings.NewReader(""), pick, api); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "last-tab"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != "panes" {
		t.Fatalf("saved tab = %q, want panes", got)
	}
}

// A saved tab that no longer exists — renamed or removed — must not strand the
// operator; the picker opens on the default.
func TestUnknownSavedTabOpensOnSpaces(t *testing.T) {
	stateDir := t.TempDir()
	navigatorEnv(t, t.TempDir())
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	if err := os.WriteFile(filepath.Join(stateDir, "last-tab"), []byte("gone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api, _ := fakeAPI(openPanesJSON)
	pick := func(opts picker.NavOptions) (picker.NavSelection, bool, error) {
		if opts.StartSection != picker.NavSpaces {
			t.Fatalf("StartSection = %v, want spaces", opts.StartSection)
		}
		return picker.NavSelection{}, false, nil
	}
	if err := runNavigatorWith(io.Discard, strings.NewReader(""), pick, api); err != nil {
		t.Fatal(err)
	}
}
