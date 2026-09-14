package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/purehate/herdr-plugin-picker/internal/herdrsock"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

// nativeVerb is one socket operation worth invoking by name. The set is
// hand-picked rather than generated from the schema: of the 128 operations the
// schema declares, most are events, getters, or plumbing, and listing them all
// would bury the handful anyone actually reaches for.
//
// Every verb here is non-destructive and works from its defaults. Closing
// panes, tabs and workspaces is deliberately absent until it can be built
// behind the broadcast confirmation — a popup that vanishes the instant it
// destroys something is the worst place to learn you picked the wrong row.
type nativeVerb struct {
	id     string
	title  string
	method string
	params map[string]any
	// paneParam names the parameter that aims the verb at a pane, because the
	// operations disagree: pane.split takes target_pane_id, pane.zoom takes
	// pane_id, and the tab and workspace verbs take neither. Empty means the
	// verb is not aimed at a pane at all.
	paneParam string
	// wantsWorkspace adds the caller's workspace, for the verbs that would
	// otherwise create their tab wherever herdr considers current.
	wantsWorkspace bool
}

var nativeVerbs = []nativeVerb{
	{id: "native:pane.split.right", title: "Split pane right", method: "pane.split",
		params: map[string]any{"direction": "right", "focus": true}, paneParam: "target_pane_id", wantsWorkspace: true},
	{id: "native:pane.split.down", title: "Split pane down", method: "pane.split",
		params: map[string]any{"direction": "down", "focus": true}, paneParam: "target_pane_id", wantsWorkspace: true},
	{id: "native:pane.zoom", title: "Zoom pane (toggle)", method: "pane.zoom",
		params: map[string]any{"mode": "toggle"}, paneParam: "pane_id"},
	{id: "native:tab.create", title: "New tab", method: "tab.create",
		params: map[string]any{"focus": true}, wantsWorkspace: true},
	{id: "native:workspace.create", title: "New workspace", method: "workspace.create",
		params: map[string]any{"focus": true}},
}

const pluginCommandPrefix = "plugin:"

// pickerContexts are the herdr contexts the picker can stand in for. It runs as
// a popup pane inside a workspace, so it has an id for both of those, and
// "global" is the context that asks for no id at all.
//
// Leaving "global" out is a quiet way to lose the best rows: on the author's
// machine it hid every pane-navigation and resize verb herdr-splits exposes —
// eleven of forty-one actions, and the ones most worth reaching by name.
var pickerContexts = []string{"pane", "workspace", "global"}

func validInAny(a herdrsock.Action, contexts []string) bool {
	for _, c := range contexts {
		if a.ValidIn(c) {
			return true
		}
	}
	return false
}

// navCommandItems builds the cmd tab: the native verbs above, then every action
// the installed plugins expose that the picker can actually satisfy.
//
// contexts is what this caller can supply, not one context it happens to be in.
// An action's `contexts` says which of herdr's own menus lists it, and the
// picker stands in for both — it is opened from a pane, inside a workspace, and
// passes ids for each. Filtering to a single context would hide the ten actions
// that declare only "workspace" from an operator who has one.
//
// selfPlugin drops this plugin's own actions. "Open Navigator" from inside the
// navigator is a no-op at best.
func navCommandItems(actions []herdrsock.Action, contexts []string, selfPlugin string) []picker.NavItem {
	items := make([]picker.NavItem, 0, len(nativeVerbs)+len(actions))
	for _, v := range nativeVerbs {
		items = append(items, picker.NavItem{
			ID:     v.id,
			Label:  v.title,
			Detail: v.method,
			Search: v.title + " " + v.method,
		})
	}

	external := make([]herdrsock.Action, 0, len(actions))
	for _, a := range actions {
		if a.PluginID == selfPlugin || !validInAny(a, contexts) {
			continue
		}
		external = append(external, a)
	}
	sort.SliceStable(external, func(i, j int) bool {
		if external[i].PluginID != external[j].PluginID {
			return external[i].PluginID < external[j].PluginID
		}
		return external[i].ActionID < external[j].ActionID
	})

	for _, a := range external {
		// Titles come out of other people's manifests, so they are stripped the
		// same way pane titles are before they reach a row.
		title := navText(a.Title)
		if title == "" {
			title = navText(a.ActionID)
		}
		items = append(items, picker.NavItem{
			ID:     pluginCommandPrefix + a.PluginID + "/" + a.ActionID,
			Label:  title,
			Detail: navText(a.PluginID),
			Search: title + " " + a.PluginID + " " + a.ActionID,
		})
	}
	return items
}

// runCommand invokes the row the operator chose. Plugin actions carry the
// caller's own position rather than the picker's, so a plugin that acts on
// "the focused pane" acts on the operator's work and not on the popup that has
// just closed over it.
func runCommand(sock herdrsock.Client, id string, ctx caller) error {
	if pluginID, actionID, ok := splitPluginCommand(id); ok {
		return sock.InvokeAction(
			herdrsock.Action{PluginID: pluginID, ActionID: actionID},
			herdrsock.InvocationContext{
				WorkspaceID:      ctx.WorkspaceID,
				TabID:            ctx.TabID,
				FocusedPaneID:    ctx.PaneID,
				InvocationSource: pluginID,
			},
		)
	}
	for _, v := range nativeVerbs {
		if v.id != id {
			continue
		}
		return sock.Invoke(v.method, v.aimedAt(ctx))
	}
	return fmt.Errorf("unknown command %q", id)
}

// splitPluginCommand undoes the "plugin:<plugin_id>/<action_id>" id. Plugin ids
// contain dots but not slashes, and action ids contain neither, so the last
// slash is the separator.
func splitPluginCommand(id string) (pluginID, actionID string, ok bool) {
	rest, found := strings.CutPrefix(id, pluginCommandPrefix)
	if !found {
		return "", "", false
	}
	i := strings.LastIndex(rest, "/")
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// aimedAt points a native verb at where the operator was standing. Without it
// every split and zoom would target whatever herdr considers current, which
// while a popup is open is the popup.
func (v nativeVerb) aimedAt(ctx caller) map[string]any {
	out := make(map[string]any, len(v.params)+2)
	for k, val := range v.params {
		out[k] = val
	}
	if v.paneParam != "" && ctx.PaneID != "" {
		out[v.paneParam] = ctx.PaneID
	}
	if v.wantsWorkspace && ctx.WorkspaceID != "" {
		out["workspace_id"] = ctx.WorkspaceID
	}
	return out
}
