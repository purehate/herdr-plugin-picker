package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/herdrsock"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
	"github.com/purehate/herdr-plugin-picker/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-picker/internal/probe"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
	"github.com/purehate/herdr-plugin-picker/internal/sshusage"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

func openNavigator(api herdrapi.Client) error {
	return api.PluginPaneOpen(herdrapi.OpenOpts{
		Plugin:     pluginID,
		Entrypoint: "navigator",
		Placement:  "popup",
		// Forward the caller for the same reason openPicker did: an ssh tab
		// selection splits the pane that triggered the picker, and this process
		// — unlike the picker's — is running in that pane's context.
		Env:   callerEnv(currentCaller()),
		Focus: true,
	})
}

// Labels come from running programs and user-edited workspace names. Strip
// terminal controls before drawing them inside the picker.
func navText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s))
}

func statusMark(status string) string {
	switch status {
	case "blocked":
		return "!"
	case "working":
		return "●"
	case "done":
		return "✓"
	case "idle":
		return "○"
	default:
		return "·"
	}
}

func countLabel(n int, singular string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

// navItems builds the server-backed lists the picker shows. It is kept separate
// from navigatorOptions so the live-refresh tick can rebuild them from a fresh
// read without re-reading the disk-backed hosts or the warnings. panes comes
// from a second herdr call because the snapshot carries no pane inventory;
// selfPane is this process's own pane, which navPaneItems drops.
func navItems(snapshot herdrapi.Snapshot, panes []herdrapi.Pane, selfPane string) picker.NavRefresh {
	spaceLabels := make(map[string]string, len(snapshot.Workspaces))
	spaces := make([]picker.NavItem, 0, len(snapshot.Workspaces))
	for _, w := range snapshot.Workspaces {
		label := navText(w.Label)
		spaceLabels[w.ID] = label
		spaces = append(spaces, picker.NavItem{
			ID:          w.ID,
			Label:       statusMark(w.Status) + " " + label,
			Detail:      countLabel(w.TabCount, "tab") + " · " + countLabel(w.PaneCount, "pane"),
			Current:     w.Focused,
			WorkspaceID: w.ID,
		})
	}
	agents := make([]picker.NavItem, 0, len(snapshot.Agents))
	for _, a := range blockedFirst(snapshot.Agents) {
		title := navText(a.Title)
		if title == "" {
			title = navText(a.CWD)
		}
		space := spaceLabels[a.WorkspaceID]
		agents = append(agents, picker.NavItem{
			ID:          a.PaneID,
			Label:       statusMark(a.Status) + " " + navText(a.Name) + "  " + title,
			Detail:      space + " · " + a.Status,
			Search:      navText(a.CWD),
			Current:     a.Focused,
			WorkspaceID: a.WorkspaceID,
			CWD:         a.CWD,
		})
	}
	sessions := make([]picker.NavItem, 0, len(snapshot.Tabs))
	for _, t := range snapshot.Tabs {
		space := spaceLabels[t.WorkspaceID]
		sessions = append(sessions, picker.NavItem{
			ID:          t.ID,
			Label:       statusMark(t.Status) + " " + navText(t.Label),
			Detail:      space + " · " + countLabel(t.PaneCount, "pane"),
			Current:     t.Focused,
			WorkspaceID: t.WorkspaceID,
		})
	}
	return picker.NavRefresh{
		Spaces:   spaces,
		Agents:   agents,
		Sessions: sessions,
		Panes:    navPaneItems(panes, spaceLabels, selfPane),
	}
}

// blockedFirst orders agents so the ones waiting on the operator sort above the
// rest, keeping the API's order within each group. The picker ranks equal
// scores stably, so this order survives a query.
func blockedFirst(agents []herdrapi.AgentInfo) []herdrapi.AgentInfo {
	out := append([]herdrapi.AgentInfo(nil), agents...)
	sort.SliceStable(out, func(i, j int) bool {
		return agentBlockedRank(out[i].Status) < agentBlockedRank(out[j].Status)
	})
	return out
}

func agentBlockedRank(status string) int {
	if status == "blocked" {
		return 0
	}
	return 1
}

func navigatorOptions(snapshot herdrapi.Snapshot, th theme.Theme, hosts []sshconfig.Host, panes []herdrapi.Pane, selfPane string) picker.NavOptions {
	items := navItems(snapshot, panes, selfPane)
	return picker.NavOptions{
		Theme:    th,
		Hosts:    hosts,
		Spaces:   items.Spaces,
		Agents:   items.Agents,
		Sessions: items.Sessions,
		Panes:    items.Panes,
	}
}

// openHosts opens the marked hosts, or the cursor host when nothing is marked.
// Every host is recorded, so a multi-open updates frecency for all of them. The
// caller context is resolved once by the caller and reused: it is the pane the
// picker was launched from, which does not move between opens.
func openHosts(out io.Writer, api herdrapi.Client, cfg pluginconfig.Config, sel picker.NavSelection, ctx caller) error {
	items := sel.Marked
	if len(items) == 0 {
		items = []picker.NavItem{sel.Item}
	}
	for _, item := range items {
		recordHostUse(out, item.Host.Alias)
		if err := performSelection(out, api, cfg, picker.Selection{
			Host:      item.Host,
			Placement: sel.Placement,
			ForceNew:  sel.ForceNew,
		}, ctx); err != nil {
			return err
		}
	}
	return nil
}

// focusNavigatorSelection moves the operator to the chosen row. ctx is the pane
// the picker was launched from, which is what makes a pane jump skip the
// workspace and tab steps it is already in.
func focusNavigatorSelection(api herdrapi.Client, sel picker.NavSelection, ctx caller) error {
	switch sel.Section {
	case picker.NavSpaces:
		return api.FocusWorkspace(sel.Item.ID)
	case picker.NavAgents:
		return api.FocusAgent(sel.Item.ID)
	case picker.NavSessions:
		return api.FocusTab(sel.Item.ID)
	case picker.NavPanes:
		return api.FocusPane(herdrapi.Pane{
			PaneID:      sel.Item.ID,
			TabID:       sel.Item.TabID,
			WorkspaceID: sel.Item.WorkspaceID,
		}, ctx.WorkspaceID, ctx.TabID)
	default:
		return fmt.Errorf("unknown navigator section %d", sel.Section)
	}
}

type navigatorFn func(picker.NavOptions) (picker.NavSelection, bool, error)

func runNavigator() error {
	return runNavigatorWith(os.Stdout, os.Stdin, picker.RunNavigator, herdrapi.New())
}

func runNavigatorWith(out io.Writer, in io.Reader, pick navigatorFn, api herdrapi.Client) error {
	// Keep the returned Config; only the rejected keys were reset. See runSession.
	cfg, cfgErr := pluginconfig.LoadDir(resolvePluginConfigDir())
	// theme.LoadFile always returns a usable Theme, so th is safe to render with
	// even when themeErr is non-nil. The error is reported, not acted on.
	th, themeErr := theme.LoadFile(resolveHerdrConfigPath())

	snapshot, err := api.Snapshot()
	if err != nil {
		return fatalInPane(out, in, err)
	}
	// One pane read serves both the panes tab and the ssh tab's ▪ markers. A
	// failure is not fatal: the other four tabs are still worth showing, the
	// panes tab opens empty, and the first refresh tick either fills it or flags
	// the footer — where every other stale-inventory case lands.
	panes, paneErr := api.PaneList()
	if paneErr != nil {
		_, _ = fmt.Fprintf(out, "herdr-picker: could not list panes: %v\n", paneErr)
	}
	selfPane := currentCaller().PaneID
	hosts, warnings := loadHosts(sshConfigPath(), cfg)
	// Pins and frecency reorder the ssh tab only. Rank keeps this order for ties,
	// so a query still decides while typing and usage breaks the draws.
	hosts = sshusage.Order(hosts, sshusage.Load(sshUsagePath()), cfg.Pinned, time.Now())

	// Same footer policy as the standalone picker: the popup is where the
	// operator is looking, so config problems render under the ssh tab rather
	// than being written above it. Built in a fixed order so the two
	// once-each warnings never reverse.
	var loadWarnings []string
	if cfgErr != nil {
		loadWarnings = append(loadWarnings, fmt.Sprintf("%v — ignoring the rejected keys", cfgErr))
	}
	if themeErr != nil {
		loadWarnings = append(loadWarnings, fmt.Sprintf("%v — using the default palette", themeErr))
	}
	warnings = append(loadWarnings, warnings...)

	opts := navigatorOptions(snapshot, th, hosts, panes, selfPane)
	opts.ShowPreview = cfg.ShowPreview
	opts.Warnings = warnings
	// The inventory on screen drifts the moment it is read — agents block and
	// finish while the popup is open — so hand the picker a way to re-read it.
	// Hosts and warnings stay as built: they come from disk, not the server.
	opts.Refresh = func() (picker.NavRefresh, error) {
		fresh, err := api.Snapshot()
		if err != nil {
			return picker.NavRefresh{}, err
		}
		freshPanes, err := api.PaneList()
		if err != nil {
			return picker.NavRefresh{}, err
		}
		return navItems(fresh, freshPanes, selfPane), nil
	}
	// Both of the socket's affordances are opt-in by presence, like the agent
	// callbacks below. Without the socket there is no way to type into a plain
	// shell and no way to learn what the other plugins expose, so ^b stays dead
	// and the cmd tab lists only the native verbs rather than failing after the
	// operator has committed.
	sock := herdrsock.New()
	if sock.Available() {
		opts.Broadcast = func(paneIDs []string, text string) (string, error) {
			return broadcastText(sock, paneIDs, text)
		}
	}
	// One read, at open: the action list changes when a plugin is installed,
	// not while the popup is up. A failure costs the plugin actions and keeps
	// the native verbs, which is the same bargain the pane list makes.
	var actions []herdrsock.Action
	if sock.Available() {
		var actErr error
		if actions, actErr = sock.Actions(); actErr != nil {
			_, _ = fmt.Fprintf(out, "herdr-picker: could not list plugin actions: %v\n", actErr)
		}
	}
	opts.Commands = navCommandItems(actions, pickerContexts, pluginID)
	// The agents tab previews the selected agent's output and ^p prompts it.
	// Both are opt-in by presence: a nil callback hides the affordance.
	opts.AgentRead = func(paneID string, lines int) (string, error) {
		return api.AgentRead(paneID, "recent-unwrapped", lines)
	}
	opts.AgentPrompt = func(paneID, text string) (string, error) {
		info, err := api.AgentPrompt(paneID, text)
		if err != nil {
			return "", err
		}
		return info.Status, nil
	}
	// ^x builds the row's action menu; the picker owns the menu UI and this owns
	// what each action does.
	opts.Actions = navActions
	opts.RunAction = func(section picker.NavSection, item picker.NavItem, actionID, text string) (string, error) {
		return runNavAction(api, section, item, actionID, text)
	}
	// Only ask herdr for the session panes when reuse is on, for the reason the
	// standalone picker gated it: the ▪ marker promises enter focuses the
	// existing session, a promise only the reuse branch can keep.
	if cfg.ReusePanes {
		opts.OpenPanes = sessionsFrom(panes)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.Probe && len(hosts) > 0 {
		opts.Probes = probe.Run(ctx, targetsFor(hosts), time.Duration(cfg.ProbeTimeoutMS)*time.Millisecond)
	}

	sel, ok, err := pick(opts)
	if err != nil {
		return fatalInPane(out, in, err)
	}
	if !ok {
		return nil
	}
	if sel.Section == picker.NavSSH {
		return openHosts(out, api, cfg, sel, resolveCaller(pickerCaller()))
	}
	// A command acts where the operator was, so it takes the resolved caller
	// like a jump does, rather than the popup's own ids.
	if sel.Section == picker.NavCommands {
		if err := runCommand(sock, sel.Item.ID, resolveCaller(pickerCaller())); err != nil {
			_, _ = fmt.Fprintln(out)
			return fatalInPane(out, in, err)
		}
		return nil
	}
	if err := focusNavigatorSelection(api, sel, resolveCaller(pickerCaller())); err != nil {
		_, _ = fmt.Fprintln(out)
		return fatalInPane(out, in, err)
	}
	return nil
}
