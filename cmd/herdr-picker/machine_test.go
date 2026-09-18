package main

import (
	"io"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

func TestMachineNavItems(t *testing.T) {
	got := machineNavItems([]herdrapi.Machine{
		{ID: "m1", Label: "Build", Target: "workbox", Session: "agents", Enabled: true, Selected: true},
		{ID: "m2", Target: "ssh://u@h:2222", Enabled: false},
	})
	if len(got) != 2 {
		t.Fatalf("items = %+v", got)
	}
	if got[0].ID != "m1" || got[0].Target != "workbox" || got[0].RemoteSession != "agents" || !got[0].Current {
		t.Fatalf("m1 = %+v", got[0])
	}
	if !strings.HasPrefix(got[0].Label, "● ") || !strings.Contains(got[0].Detail, "agents") {
		t.Fatalf("m1 label/detail = %q / %q", got[0].Label, got[0].Detail)
	}
	// A disabled profile still lists, marked differently, and falls back to the
	// target when it has no label.
	if !strings.HasPrefix(got[1].Label, "○ ") || !strings.Contains(got[1].Label, "ssh://u@h:2222") {
		t.Fatalf("m2 = %+v", got[1])
	}
}

func TestRemoteArgv(t *testing.T) {
	if got := remoteArgv("workbox", ""); strings.Join(got, " ") != "herdr --remote workbox" {
		t.Fatalf("argv = %v", got)
	}
	if got := remoteArgv("workbox", "agents"); strings.Join(got, " ") != "herdr --remote workbox --session agents" {
		t.Fatalf("argv = %v", got)
	}
}

func TestRunRemoteRequiresTarget(t *testing.T) {
	t.Setenv("HERDR_PICKER_TARGET", "")
	var out strings.Builder
	err := runRemoteWith(&out, strings.NewReader("\n"))
	if err == nil {
		t.Fatal("a missing target did not error")
	}
	if !strings.Contains(out.String(), "HERDR_PICKER_TARGET") {
		t.Fatalf("out = %q, want the missing-variable name", out.String())
	}
}

func TestOpenMachineOpensTheRemoteEntrypoint(t *testing.T) {
	t.Setenv("HERDR_ACTIVE_PANE_ID", "")
	t.Setenv(callerPaneEnv, "")
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"result":{"panes":[]}}`), nil
	}}
	item := picker.NavItem{ID: "m1", Target: "workbox", RemoteSession: "agents"}
	if err := openMachine(io.Discard, api, item); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(calls[len(calls)-1], " ")
	for _, want := range []string{
		"plugin pane open", "--entrypoint remote", "--placement split",
		"HERDR_PICKER_TARGET=workbox", "HERDR_PICKER_SESSION=agents",
	} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv = %q, missing %q", argv, want)
		}
	}
}

// The remote entrypoint must be declared in the manifest or herdr refuses the
// open, exactly like the session entrypoint the ssh tab uses.
func TestOpenMachineEntrypointIsDeclared(t *testing.T) {
	m := manifestLoad(t)
	t.Setenv("HERDR_ACTIVE_PANE_ID", "")
	t.Setenv(callerPaneEnv, "")
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"result":{"panes":[]}}`), nil
	}}
	if err := openMachine(io.Discard, api, picker.NavItem{Target: "workbox"}); err != nil {
		t.Fatal(err)
	}
	if got := manifestOpenEntrypoint(t, calls); got != "remote" || !m.declaresPane(got) {
		t.Fatalf("remote entrypoint %q is not declared in the manifest", got)
	}
}
