package main

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

func TestNavigatorOptionsFromSnapshot(t *testing.T) {
	s := herdrapi.Snapshot{
		Workspaces: []herdrapi.WorkspaceInfo{{ID: "w1", Label: "my\x1b[31mspace", Status: "working", TabCount: 2, PaneCount: 3, Focused: true}},
		Tabs:       []herdrapi.TabInfo{{ID: "w1:t1", WorkspaceID: "w1", Label: "build", PaneCount: 2}},
		Agents:     []herdrapi.AgentInfo{{PaneID: "w1:p1", WorkspaceID: "w1", Name: "codex", Status: "blocked", Title: "review", CWD: "/tmp/review"}},
	}
	o := navigatorOptions(s, theme.Default(), nil)
	if len(o.Spaces) != 1 || o.Spaces[0].ID != "w1" || !o.Spaces[0].Current {
		t.Fatalf("spaces = %+v", o.Spaces)
	}
	if strings.ContainsRune(o.Spaces[0].Label, '\x1b') {
		t.Fatalf("unsafe control in %q", o.Spaces[0].Label)
	}
	if len(o.Agents) != 1 || !strings.Contains(o.Agents[0].Detail, "blocked") {
		t.Fatalf("agents = %+v", o.Agents)
	}
	if len(o.Sessions) != 1 || o.Sessions[0].ID != "w1:t1" {
		t.Fatalf("sessions = %+v", o.Sessions)
	}
}

func TestNavigatorOptionsCarriesHosts(t *testing.T) {
	hosts := []sshconfig.Host{{Alias: "web1", HostName: "192.0.2.1", Port: "22"}}
	o := navigatorOptions(herdrapi.Snapshot{}, theme.Default(), hosts)
	if !reflect.DeepEqual(o.Hosts, hosts) {
		t.Fatalf("Hosts = %+v, want %+v", o.Hosts, hosts)
	}
}

func TestNavigatorSelectionsFocusTheRightHerdrObject(t *testing.T) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return nil, nil
	}}
	for _, sel := range []picker.NavSelection{
		{Section: picker.NavSpaces, Item: picker.NavItem{ID: "w1"}},
		{Section: picker.NavAgents, Item: picker.NavItem{ID: "w1:p2"}},
		{Section: picker.NavSessions, Item: picker.NavItem{ID: "w1:t3"}},
	} {
		if err := focusNavigatorSelection(api, sel); err != nil {
			t.Fatal(err)
		}
	}
	want := [][]string{{"workspace", "focus", "w1"}, {"agent", "focus", "w1:p2"}, {"tab", "focus", "w1:t3"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestNavigatorLaunchAndManifestAgree(t *testing.T) {
	m := manifestLoad(t)
	var calls [][]string
	if err := openNavigator(manifestRecorder(&calls)); err != nil {
		t.Fatal(err)
	}
	if got := manifestOpenEntrypoint(t, calls); got != "navigator" || !m.declaresPane(got) {
		t.Fatalf("navigator entrypoint %q not in manifest", got)
	}
	if got := manifestOpenPlugin(t, calls); got != m.ID {
		t.Fatalf("navigator plugin %q, manifest %q", got, m.ID)
	}
}

func TestRunNavigatorUsesOneSnapshotAndFocusesSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_CONFIG_PATH", t.TempDir()+"/absent.toml")
	// Reuse off, so the pane list the marker would need is not fetched and the
	// call log is just the snapshot and the focus.
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir(t, "reuse_panes = false\nprobe = false\n"))
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		if reflect.DeepEqual(args, []string{"api", "snapshot"}) {
			return []byte(`{"result":{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"project"}]}}}`), nil
		}
		return nil, nil
	}}
	pick := func(o picker.NavOptions) (picker.NavSelection, bool, error) {
		if len(o.Spaces) != 1 || o.Spaces[0].ID != "w1" {
			t.Fatalf("picker received %+v", o)
		}
		return picker.NavSelection{Section: picker.NavSpaces, Item: o.Spaces[0]}, true, nil
	}
	if err := runNavigatorWith(io.Discard, strings.NewReader(""), pick, api); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"api", "snapshot"}, {"workspace", "focus", "w1"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

// TestRunNavigatorSSHSelectionOpensASession pins the ssh tab's whole point: a
// NavSSH choice must leave through performSelection as a session open, carrying
// the host and placement the row was chosen with.
func TestRunNavigatorSSHSelectionOpensASession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_CONFIG_PATH", t.TempDir()+"/absent.toml")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir(t, "reuse_panes = false\nprobe = false\n"))
	api, calls := fakeAPI(openPanesJSON)

	host := sshconfig.Host{Alias: "web1", HostName: "192.0.2.1", Port: "22"}
	pick := func(picker.NavOptions) (picker.NavSelection, bool, error) {
		return picker.NavSelection{
			Section:   picker.NavSSH,
			Item:      picker.NavItem{ID: host.Alias, Host: host},
			Placement: "tab",
		}, true, nil
	}
	if err := runNavigatorWith(io.Discard, strings.NewReader(""), pick, api); err != nil {
		t.Fatal(err)
	}
	argv := openArgv(t, *calls)
	if !strings.Contains(argv, "--entrypoint session") {
		t.Fatalf("argv = %q, want a session open", argv)
	}
	if !strings.Contains(argv, "--placement tab") {
		t.Fatalf("argv = %q, want the chosen placement", argv)
	}
	if !strings.Contains(argv, "--env HERDR_PICKER_TARGET=web1") {
		t.Fatalf("argv = %q, want the chosen host forwarded", argv)
	}
}
