package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
	"github.com/purehate/herdr-plugin-picker/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-picker/internal/probe"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
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

func navigatorOptions(snapshot herdrapi.Snapshot, th theme.Theme, hosts []sshconfig.Host) picker.NavOptions {
	o := picker.NavOptions{Theme: th, Hosts: hosts}
	spaceLabels := make(map[string]string, len(snapshot.Workspaces))
	for _, w := range snapshot.Workspaces {
		label := navText(w.Label)
		spaceLabels[w.ID] = label
		o.Spaces = append(o.Spaces, picker.NavItem{
			ID:      w.ID,
			Label:   statusMark(w.Status) + " " + label,
			Detail:  countLabel(w.TabCount, "tab") + " · " + countLabel(w.PaneCount, "pane"),
			Current: w.Focused,
		})
	}
	for _, a := range snapshot.Agents {
		title := navText(a.Title)
		if title == "" {
			title = navText(a.CWD)
		}
		space := spaceLabels[a.WorkspaceID]
		o.Agents = append(o.Agents, picker.NavItem{
			ID:      a.PaneID,
			Label:   statusMark(a.Status) + " " + navText(a.Name) + "  " + title,
			Detail:  space + " · " + a.Status,
			Search:  navText(a.CWD),
			Current: a.Focused,
		})
	}
	for _, t := range snapshot.Tabs {
		space := spaceLabels[t.WorkspaceID]
		o.Sessions = append(o.Sessions, picker.NavItem{
			ID:      t.ID,
			Label:   statusMark(t.Status) + " " + navText(t.Label),
			Detail:  space + " · " + countLabel(t.PaneCount, "pane"),
			Current: t.Focused,
		})
	}
	return o
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
