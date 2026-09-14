package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/herdrsock"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

// panes.go is the panes tab and the broadcast behind ^b: one command typed once
// and sent to every marked pane, which is the `setw synchronize-panes` habit
// this plugin exists to keep after moving off tmux.

// navPaneItems turns herdr's pane inventory into rows. selfPane is this
// process's own pane, dropped from the list: it is the popup drawing the picker,
// so it is never somewhere to jump to and never somewhere to send a command.
func navPaneItems(panes []herdrapi.Pane, spaceLabels map[string]string, selfPane string) []picker.NavItem {
	out := make([]picker.NavItem, 0, len(panes))
	for _, p := range panes {
		if p.PaneID == selfPane {
			continue
		}
		dir := shortenHome(p.Dir())
		out = append(out, picker.NavItem{
			ID:          p.PaneID,
			Label:       paneLabel(p),
			Detail:      joinDetail(spaceLabels[p.WorkspaceID], dir),
			Search:      strings.Join([]string{p.PaneID, navText(p.Title), dir, paneLabelOf(p)}, " "),
			Current:     p.Focused,
			Status:      p.Status,
			WorkspaceID: p.WorkspaceID,
			TabID:       p.TabID,
			CWD:         p.Dir(),
		})
	}
	return out
}

// paneLabel is the row's name: the agent when one is running, else the label the
// picker gave an ssh session, else the terminal title, else the directory. A
// pane always has an id, so a row is never blank.
func paneLabel(p herdrapi.Pane) string {
	title := navText(p.Title)
	if agent := navText(p.Agent); agent != "" {
		if title != "" {
			return agent + "  " + title
		}
		return agent
	}
	if own := paneLabelOf(p); own != "" {
		return own
	}
	if title != "" {
		return title
	}
	if dir := p.Dir(); dir != "" {
		return filepath.Base(dir)
	}
	return p.PaneID
}

func paneLabelOf(p herdrapi.Pane) string {
	if p.Label == nil {
		return ""
	}
	return navText(*p.Label)
}

func joinDetail(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

// shortenHome renders a path the way a shell prompt does, so a row's directory
// costs a few columns instead of most of the popup's width.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if path == "" || err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

// broadcastText sends one command to every pane. The newline is appended here,
// at the edge that knows the operator asked to run the command rather than stage
// it. This goes over the socket rather than the CLI because it has to: `agent
// send-keys` reaches agents only, and most of these panes are plain shells.
func broadcastText(sock herdrsock.Client, paneIDs []string, text string) (string, error) {
	line := text + "\n"
	failed := 0
	var first error
	for _, id := range paneIDs {
		if err := sock.SendText(id, line); err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	if failed == 0 {
		return "sent to " + countLabel(len(paneIDs), "pane"), nil
	}
	// Reporting the count with one reason: a broadcast fails the same way for
	// every pane when the socket is down, and per-pane when one has closed.
	return "", fmt.Errorf("%d of %d panes: %w", failed, len(paneIDs), first)
}
