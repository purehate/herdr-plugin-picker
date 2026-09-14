package main

import (
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/herdrsock"
)

func commandIDs(t *testing.T, actions []herdrsock.Action) []string {
	t.Helper()
	items := navCommandItems(actions, pickerContexts, pluginID)
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

func hasID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// "global" is a real herdr context alongside pane and workspace, and it is the
// one a palette wants most: it is what the pane-navigation and resize verbs
// declare. Leaving it out of pickerContexts silently hid eleven of the
// forty-one actions installed on the author's machine.
func TestCommandsIncludeGlobalContextActions(t *testing.T) {
	ids := commandIDs(t, []herdrsock.Action{
		{PluginID: "p.splits", ActionID: "nav-up", Title: "Navigate up", Contexts: []string{"global"}},
		{PluginID: "p.x", ActionID: "only-pane", Title: "Pane thing", Contexts: []string{"pane"}},
		{PluginID: "p.y", ActionID: "only-ws", Title: "WS thing", Contexts: []string{"workspace"}},
		{PluginID: "p.z", ActionID: "anywhere", Title: "Anywhere"},
	})
	for _, want := range []string{
		"plugin:p.splits/nav-up",
		"plugin:p.x/only-pane",
		"plugin:p.y/only-ws",
		"plugin:p.z/anywhere",
	} {
		if !hasID(ids, want) {
			t.Errorf("%s missing from %v", want, ids)
		}
	}
}

// An action in a context the picker cannot supply is not offered, because a row
// that is listed and then refused teaches the operator to distrust the list.
func TestCommandsDropUnsatisfiableContexts(t *testing.T) {
	ids := commandIDs(t, []herdrsock.Action{
		{PluginID: "p.link", ActionID: "open-url", Title: "Open URL", Contexts: []string{"link"}},
	})
	if hasID(ids, "plugin:p.link/open-url") {
		t.Errorf("offered an action the picker cannot satisfy: %v", ids)
	}
}

// Invoking our own "Open Navigator" from inside the navigator is a no-op at
// best, so this plugin's actions are not listed in its own palette.
func TestCommandsDropOurOwnActions(t *testing.T) {
	ids := commandIDs(t, []herdrsock.Action{
		{PluginID: pluginID, ActionID: "open-navigator", Title: "Open Navigator"},
		{PluginID: "other", ActionID: "go", Title: "Go"},
	})
	if hasID(ids, "plugin:"+pluginID+"/open-navigator") {
		t.Errorf("listed our own action: %v", ids)
	}
	if !hasID(ids, "plugin:other/go") {
		t.Errorf("dropped somebody else's action: %v", ids)
	}
}

// The native verbs are always there, so the tab still works when the socket
// cannot say what else is installed.
func TestCommandsKeepNativeVerbsWithNoActions(t *testing.T) {
	items := navCommandItems(nil, pickerContexts, pluginID)
	if len(items) != len(nativeVerbs) {
		t.Fatalf("got %d rows, want the %d native verbs", len(items), len(nativeVerbs))
	}
}

// Titles come out of other people's manifests, so they are stripped before they
// reach a row, the same way pane titles are.
func TestCommandTitlesStripTerminalControls(t *testing.T) {
	items := navCommandItems([]herdrsock.Action{
		{PluginID: "p.x", ActionID: "go", Title: "run \x1b[31mred"},
	}, pickerContexts, pluginID)
	for _, it := range items {
		if strings.ContainsRune(it.Label, '\x1b') || strings.ContainsRune(it.Detail, '\x1b') {
			t.Fatalf("unsafe control in %q / %q", it.Label, it.Detail)
		}
	}
}

// An action with no title still needs a name, or it draws as a blank row.
func TestCommandFallsBackToTheActionID(t *testing.T) {
	items := navCommandItems([]herdrsock.Action{
		{PluginID: "p.x", ActionID: "do-thing", Title: "   "},
	}, pickerContexts, pluginID)
	last := items[len(items)-1]
	if last.Label != "do-thing" {
		t.Errorf("label = %q, want the action id", last.Label)
	}
}

func TestSplitPluginCommand(t *testing.T) {
	for _, tc := range []struct {
		in             string
		plugin, action string
		ok             bool
	}{
		{"plugin:a.b/open", "a.b", "open", true},
		// Plugin ids carry dots; only the last slash separates.
		{"plugin:ntindle.herdr-resurrect/save-space", "ntindle.herdr-resurrect", "save-space", true},
		{"native:pane.zoom", "", "", false},
		{"plugin:noslash", "", "", false},
		{"plugin:/open", "", "", false},
		{"plugin:a.b/", "", "", false},
		{"", "", "", false},
	} {
		p, a, ok := splitPluginCommand(tc.in)
		if ok != tc.ok || p != tc.plugin || a != tc.action {
			t.Errorf("splitPluginCommand(%q) = %q,%q,%v want %q,%q,%v", tc.in, p, a, ok, tc.plugin, tc.action, tc.ok)
		}
	}
}

// The operations disagree about which parameter aims them, so a verb that sent
// pane_id to pane.split would silently split the wrong pane — or the popup.
func TestNativeVerbsAimAtTheCallersPane(t *testing.T) {
	ctx := caller{PaneID: "w1:p9", TabID: "w1:t1", WorkspaceID: "w1"}
	byID := map[string]nativeVerb{}
	for _, v := range nativeVerbs {
		byID[v.id] = v
	}

	split := byID["native:pane.split.right"].aimedAt(ctx)
	if split["target_pane_id"] != "w1:p9" {
		t.Errorf("pane.split target = %v, want w1:p9", split["target_pane_id"])
	}
	if split["direction"] != "right" {
		t.Errorf("pane.split lost its direction: %v", split)
	}
	if split["workspace_id"] != "w1" {
		t.Errorf("pane.split workspace = %v", split["workspace_id"])
	}

	zoom := byID["native:pane.zoom"].aimedAt(ctx)
	if zoom["pane_id"] != "w1:p9" {
		t.Errorf("pane.zoom pane_id = %v, want w1:p9", zoom["pane_id"])
	}
	if _, wrong := zoom["target_pane_id"]; wrong {
		t.Errorf("pane.zoom got target_pane_id, which it does not accept: %v", zoom)
	}

	// workspace.create makes a workspace; pinning it to the current one would
	// be a contradiction.
	ws := byID["native:workspace.create"].aimedAt(ctx)
	if _, pinned := ws["workspace_id"]; pinned {
		t.Errorf("workspace.create was pinned to a workspace: %v", ws)
	}
}

// Without a caller pane there is nothing to aim at, and sending an empty id is
// worse than sending none: herdr validates ids.
func TestNativeVerbsOmitAnEmptyCaller(t *testing.T) {
	for _, v := range nativeVerbs {
		got := v.aimedAt(caller{})
		for _, k := range []string{"pane_id", "target_pane_id", "workspace_id"} {
			if val, ok := got[k]; ok && val == "" {
				t.Errorf("%s sent empty %s", v.id, k)
			}
		}
	}
}

func TestRunCommandRejectsAnUnknownID(t *testing.T) {
	err := runCommand(herdrsock.Client{Path: "/nonexistent"}, "native:nope", caller{})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("err = %v, want an unknown command", err)
	}
}
