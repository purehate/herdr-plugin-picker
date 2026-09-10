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
	// Create path only: os.WriteFile applies its mode when it creates the file,
	// not when it truncates an existing one, and t.TempDir() is fresh every run.
	// So this pins the mode we ask for, not a durable guarantee about the file.
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
