package main

import (
	"fmt"
	"io"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

const pluginID = "purehate.herdr-ssh"

// performSelection focuses an existing session for this host when there is one,
// and otherwise opens a new session pane. Diagnostics go to out rather than
// straight to os.Stderr, because the two callers report to different places: the
// picker pane writes to the screen it owns, and connect writes to stderr so a
// script's stdout stays clean. Passing the writer in also lets a test read what
// the operator would have been shown, which is the only way to tell a tolerated
// failure from a silent one.
func performSelection(out io.Writer, api herdrapi.Client, cfg pluginconfig.Config, sel picker.Selection, ctx caller) error {
	var panes []herdrapi.Pane
	listed := false
	if cfg.ReusePanes && !sel.ForceNew {
		panes = listPanes(out, api)
		listed = true
		if pane, ok := herdrapi.FindLabeled(panes, sessionLabel(sel.Host.Alias)); ok {
			return api.FocusPane(pane, ctx.WorkspaceID, ctx.TabID)
		}
	}
	// Only the placements PlacementTargetsPane names hand ctx.PaneID to herdr,
	// so only they make it worth resolving: PluginPaneOpen drops the id on the
	// rest, and listPanes reports a failed lookup to out, so checking it for a
	// tab would risk warning the operator about a pane this open was never
	// going to reference. A spurious diagnostic costs more than the wasted
	// call, because nothing on screen marks it as spurious. Asking the shared
	// predicate rather than restating the set keeps herdrapi the only place
	// that has to change when a placement is added.
	//
	// Where the id is used, resolving it is a precondition of the open and not
	// a part of the reuse scan. The caller can close while the picker is open,
	// and the two paths that skip the scan — reuse_panes = false, and the
	// force-new key — are no less exposed to that than the one that does.
	// ForceNew is the sharper case: the more explicitly the operator asks for a
	// fresh pane, the more certainly they would get a dead target.
	//
	// The scan has already paid for a list when it ran, so reuse costs nothing
	// extra; the other paths pay for one only when there is an id worth checking.
	if ctx.PaneID != "" && herdrapi.PlacementTargetsPane(sel.Placement) {
		if !listed {
			panes = listPanes(out, api)
		}
		ctx.PaneID = livePaneID(panes, ctx.PaneID)
	}
	return api.PluginPaneOpen(herdrapi.OpenOpts{
		Plugin:     pluginID,
		Entrypoint: "session",
		Placement:  sel.Placement,
		TargetPane: ctx.PaneID,
		Direction:  cfg.SplitDirection,
		Env:        map[string]string{"HERDR_SSH_TARGET": sel.Host.Alias},
		Focus:      true,
	})
}

// listPanes reports a failed lookup and returns no panes, so that both
// questions performSelection asks of the list degrade the same way: no session
// to reuse, and no confirmation that the caller's pane is still there. Both
// answers cost placement rather than the connection, which is the trade the
// whole caller path is built on.
func listPanes(out io.Writer, api herdrapi.Client) []herdrapi.Pane {
	panes, err := api.PaneList()
	if err != nil {
		_, _ = fmt.Fprintf(out, "herdr-ssh: could not list panes: %v\n", err)
		return nil
	}
	return panes
}

// livePaneID returns id only while a pane still carries it, and "" once that
// pane is gone. herdr rejects an open whose --target-pane no longer exists, so
// passing a dead id through costs the operator the session they asked for;
// dropping it costs them the split's position and nothing else.
func livePaneID(panes []herdrapi.Pane, id string) string {
	for _, p := range panes {
		if p.PaneID == id {
			return id
		}
	}
	return ""
}

// openSessions maps alias → pane id for every live ssh session, so the picker
// can mark them.
func openSessions(out io.Writer, api herdrapi.Client) map[string]string {
	sessions := map[string]string{}
	panes, err := api.PaneList()
	if err != nil {
		_, _ = fmt.Fprintf(out, "herdr-ssh: could not list panes: %v\n", err)
		return sessions
	}
	for _, p := range panes {
		if p.Label == nil {
			continue
		}
		if alias, ok := trimLabel(*p.Label); ok {
			sessions[alias] = p.PaneID
		}
	}
	return sessions
}

func trimLabel(label string) (string, bool) {
	if len(label) <= len(labelPrefix) || label[:len(labelPrefix)] != labelPrefix {
		return "", false
	}
	return label[len(labelPrefix):], true
}
