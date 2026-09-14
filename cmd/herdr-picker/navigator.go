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

// navItems builds the three server-backed lists the picker shows. It is kept
// separate from navigatorOptions so the live-refresh tick can rebuild them from
// a fresh snapshot without re-reading the disk-backed hosts or the warnings.
func navItems(snapshot herdrapi.Snapshot) picker.NavRefresh {
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
	return picker.NavRefresh{Spaces: spaces, Agents: agents, Sessions: sessions}
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

func navigatorOptions(snapshot herdrapi.Snapshot, th theme.Theme, hosts []sshconfig.Host) picker.NavOptions {
	items := navItems(snapshot)
	return picker.NavOptions{
		Theme:    th,
		Hosts:    hosts,
		Spaces:   items.Spaces,
		Agents:   items.Agents,
		Sessions: items.Sessions,
	}
}

func focusNavigatorSelection(api herdrapi.Client, sel picker.NavSelection) error {
	switch sel.Section {
	case picker.NavSpaces:
		return api.FocusWorkspace(sel.Item.ID)
	case picker.NavAgents:
		return api.FocusAgent(sel.Item.ID)
	case picker.NavSessions:
		return api.FocusTab(sel.Item.ID)
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

	opts := navigatorOptions(snapshot, th, hosts)
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
		return navItems(fresh), nil
	}
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
		opts.OpenPanes = openSessions(out, api)
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
		recordHostUse(out, sel.Item.Host.Alias)
		return performSelection(out, api, cfg, picker.Selection{
			Host:      sel.Item.Host,
			Placement: sel.Placement,
			ForceNew:  sel.ForceNew,
		}, resolveCaller(pickerCaller()))
	}
	if err := focusNavigatorSelection(api, sel); err != nil {
		_, _ = fmt.Fprintln(out)
		return fatalInPane(out, in, err)
	}
	return nil
}
