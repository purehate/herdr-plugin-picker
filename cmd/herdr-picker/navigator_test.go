package main

import (
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
	"github.com/purehate/herdr-plugin-picker/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

func TestNavigatorOptionsFromSnapshot(t *testing.T) {
	s := herdrapi.Snapshot{
		Workspaces: []herdrapi.WorkspaceInfo{{ID: "w1", Label: "my\x1b[31mspace", Status: "working", TabCount: 2, PaneCount: 3, Focused: true}},
		Tabs:       []herdrapi.TabInfo{{ID: "w1:t1", WorkspaceID: "w1", Label: "build", PaneCount: 2}},
		Agents:     []herdrapi.AgentInfo{{PaneID: "w1:p1", WorkspaceID: "w1", Name: "codex", Status: "blocked", Title: "review", CWD: "/tmp/review"}},
	}
	o := navigatorOptions(s, theme.Default(), nil, nil, "")
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
	o := navigatorOptions(herdrapi.Snapshot{}, theme.Default(), hosts, nil, "")
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
		if err := focusNavigatorSelection(api, sel, caller{}); err != nil {
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

// TestOpenHostsOpensEveryMarkedHost pins the multi-open: one session per marked
// host, not just the cursor row.
func TestOpenHostsOpensEveryMarkedHost(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return nil, nil
	}}
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.NavSelection{
		Section:   picker.NavSSH,
		Item:      picker.NavItem{Host: sshconfig.Host{Alias: "web1"}},
		Placement: "split",
		Marked: []picker.NavItem{
			{Host: sshconfig.Host{Alias: "web1"}},
			{Host: sshconfig.Host{Alias: "db"}},
		},
	}
	if err := openHosts(io.Discard, api, cfg, sel, caller{}); err != nil {
		t.Fatal(err)
	}
	opens := 0
	for _, c := range calls {
		if len(c) > 0 && c[0] == "plugin" {
			opens++
		}
	}
	if opens != 2 {
		t.Fatalf("plugin opens = %d, want 2 (calls %v)", opens, calls)
	}
}

func TestRunNavigatorUsesOneSnapshotAndFocusesSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_CONFIG_PATH", t.TempDir()+"/absent.toml")
	// Reuse off and probing off, so the log is only the two inventory reads the
	// picker always makes and the focus the selection produces.
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir(t, "reuse_panes = false\nprobe = false\n"))
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		if reflect.DeepEqual(args, []string{"api", "snapshot"}) {
			return []byte(`{"result":{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"project"}]}}}`), nil
		}
		return []byte(`{"result":{"panes":[]}}`), nil
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
	want := [][]string{{"api", "snapshot"}, {"pane", "list"}, {"workspace", "focus", "w1"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestNavigatorOptionsSortsBlockedAgentsFirst(t *testing.T) {
	s := herdrapi.Snapshot{Agents: []herdrapi.AgentInfo{
		{PaneID: "p1", Status: "working", Name: "one"},
		{PaneID: "p2", Status: "blocked", Name: "two"},
		{PaneID: "p3", Status: "done", Name: "three"},
		{PaneID: "p4", Status: "blocked", Name: "four"},
	}}
	o := navigatorOptions(s, theme.Default(), nil, nil, "")
	var got []string
	for _, a := range o.Agents {
		got = append(got, a.ID)
	}
	want := []string{"p2", "p4", "p1", "p3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agent order = %v, want %v", got, want)
	}
}

// TestRunNavigatorRefreshRereadsSnapshot pins the live-refresh wiring: the
// picker must be handed a function that reads the server again, not a closure
// over the first snapshot.
func TestRunNavigatorRefreshRereadsSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_CONFIG_PATH", t.TempDir()+"/absent.toml")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir(t, "reuse_panes = false\nprobe = false\n"))
	var snapshots int
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		if reflect.DeepEqual(args, []string{"api", "snapshot"}) {
			snapshots++
			return []byte(`{"result":{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"project"}]}}}`), nil
		}
		return []byte(`{"result":{"panes":[]}}`), nil
	}}
	var refresh func() (picker.NavRefresh, error)
	pick := func(o picker.NavOptions) (picker.NavSelection, bool, error) {
		refresh = o.Refresh
		return picker.NavSelection{}, false, nil
	}
	if err := runNavigatorWith(io.Discard, strings.NewReader(""), pick, api); err != nil {
		t.Fatal(err)
	}
	if refresh == nil {
		t.Fatal("navigator was not given a refresh function")
	}
	if snapshots != 1 {
		t.Fatalf("initial snapshots = %d, want 1", snapshots)
	}
	r, err := refresh()
	if err != nil {
		t.Fatal(err)
	}
	if snapshots != 2 || len(r.Spaces) != 1 || r.Spaces[0].ID != "w1" {
		t.Fatalf("refresh read %d snapshots, spaces %+v", snapshots, r.Spaces)
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

// TestNavigatorPaneSelectionWalksToThePane pins what the panes tab's enter does.
// herdr's `pane focus` only takes a direction, so reaching an arbitrary pane
// means walking workspace → tab → pane, which needs the route the row carries.
func TestNavigatorPaneSelectionWalksToThePane(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  caller
		want [][]string
	}{
		{
			name: "from another workspace, the full walk",
			ctx:  caller{WorkspaceID: "w9", TabID: "w9:t1"},
			want: [][]string{
				{"workspace", "focus", "w1"},
				{"tab", "focus", "w1:t2"},
				{"plugin", "pane", "focus", "w1:p3"},
			},
		},
		{
			// Refocusing the workspace and tab the operator is already in is a
			// visible flicker for no gain, so those steps are skipped.
			name: "from the same tab, the pane alone",
			ctx:  caller{WorkspaceID: "w1", TabID: "w1:t2"},
			want: [][]string{{"plugin", "pane", "focus", "w1:p3"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
				calls = append(calls, args)
				return nil, nil
			}}
			sel := picker.NavSelection{
				Section: picker.NavPanes,
				Item:    picker.NavItem{ID: "w1:p3", TabID: "w1:t2", WorkspaceID: "w1"},
			}
			if err := focusNavigatorSelection(api, sel, tc.ctx); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("calls = %v, want %v", calls, tc.want)
			}
		})
	}
}

// TestNavigatorBroadcastNeedsTheSocket pins ^b's one precondition. pane.send_text
// is a socket-only operation — the CLI's `agent send-keys` reaches agents, not
// plain shells — so without HERDR_SOCKET_PATH there is nothing to send with, and
// the picker must be handed no Broadcast rather than one that fails per pane
// after the operator has typed a command and confirmed it.
func TestNavigatorBroadcastNeedsTheSocket(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"no socket, no broadcast", "", false},
		// The path is not dialed until a send, so a plausible one is enough to
		// prove the wiring is presence-gated and not doing its own probe.
		{"socket present, broadcast wired", filepath.Join(t.TempDir(), "herdr.sock"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			navigatorEnv(t, t.TempDir())
			t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir(t, "probe = false\nreuse_panes = false\n"))
			t.Setenv("HERDR_SOCKET_PATH", tc.path)

			api, _ := fakeAPI(openPanesJSON)
			var opts picker.NavOptions
			pick := func(o picker.NavOptions) (picker.NavSelection, bool, error) {
				opts = o
				return picker.NavSelection{}, false, nil
			}
			if err := runNavigatorWith(io.Discard, strings.NewReader(""), pick, api); err != nil {
				t.Fatal(err)
			}
			if got := opts.Broadcast != nil; got != tc.want {
				t.Fatalf("Broadcast wired = %v, want %v", got, tc.want)
			}
		})
	}
}
