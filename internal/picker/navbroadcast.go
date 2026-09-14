package picker

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// navbroadcast.go is the panes tab and its one destructive verb: type a command
// once and send it to every marked pane. Ported from the author's tmux picker,
// where the same key ran one tool across a whole engagement's worth of shells.

// broadcastAction is the sentinel that tells the shared input and confirm flow
// this text is a broadcast rather than a row action. It is not an action ID the
// caller ever sees: RunAction is not involved, Broadcast is.
const broadcastAction = "\x00broadcast"

// markedPanes returns the marked panes in inventory order, so the send order
// matches the list the operator was looking at rather than map iteration. It
// reads the full inventory, not the filtered rows, so a mark survives a query
// that hides it — the same rule the ssh tab follows.
func (m navigatorModel) markedPanes() []NavItem {
	if len(m.marked) == 0 {
		return nil
	}
	out := make([]NavItem, 0, len(m.marked))
	for _, item := range m.opts.Panes {
		if m.marked[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// broadcastTargets is what ^b would send to: the marks when there are any,
// otherwise the row under the cursor, so the single-pane case needs no marking.
func (m navigatorModel) broadcastTargets() []NavItem {
	if marked := m.markedPanes(); len(marked) > 0 {
		return marked
	}
	if item, ok := m.cursorItem(); ok {
		return []NavItem{item}
	}
	return nil
}

// markAllListed marks every row currently on screen, or clears the marks when
// they are already all marked — one key that both widens and undoes. It is
// bounded by the filter because the filter is how a broadcast gets narrowed.
func (m navigatorModel) markAllListed() navigatorModel {
	if m.section != NavPanes || len(m.items) == 0 {
		return m
	}
	marked := make(map[string]bool, len(m.items))
	for _, item := range m.items {
		if m.marked[item.ID] {
			continue
		}
		marked[item.ID] = true
	}
	if len(marked) == 0 {
		m.marked = map[string]bool{}
		return m
	}
	for id := range marked {
		m.marked[id] = true
	}
	return m
}

// openBroadcastInput starts ^b. The targets are resolved when the confirm is
// answered rather than here, so a refresh landing mid-typing cannot leave the
// operator confirming a count that no longer matches.
func (m navigatorModel) openBroadcastInput() (tea.Model, tea.Cmd) {
	if m.section != NavPanes || m.opts.Broadcast == nil {
		return m, nil
	}
	if len(m.broadcastTargets()) == 0 {
		return m, nil
	}
	m.menuActions = nil
	m.inputOpen = true
	m.inputLabel = "broadcast"
	m.inputText = ""
	m.inputTarget = ""
	m.inputAction = broadcastAction
	m.inputItem = NavItem{}
	m.inputSection = m.section
	m.note = ""
	return m, nil
}

// confirmBroadcast turns the typed command into a y/n. Sending to many live
// shells is many writes that nothing undoes, so it is the one thing here that
// always asks, whatever the count.
func (m navigatorModel) confirmBroadcast(text string) (tea.Model, tea.Cmd) {
	targets := m.broadcastTargets()
	if len(targets) == 0 {
		return m, nil
	}
	m.confirmOpen = true
	m.confirmAction = NavAction{ID: broadcastAction}
	m.confirmItem = NavItem{}
	m.confirmSection = m.section
	m.confirmText = text
	m.confirmQuestion = fmt.Sprintf("send %q to %s?", oneLine(text), paneCount(len(targets)))
	return m, nil
}

func paneCount(n int) string {
	if n == 1 {
		return "1 pane"
	}
	return fmt.Sprintf("%d panes", n)
}

// broadcastCmd hands the targets and the text as typed to the configured sink.
// The newline that submits a command belongs to the sink, not here — see the
// Broadcast field's comment.
func (m navigatorModel) broadcastCmd(text string) tea.Cmd {
	send := m.opts.Broadcast
	targets := m.broadcastTargets()
	ids := make([]string, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, t.ID)
	}
	return func() tea.Msg {
		status, err := send(ids, text)
		if err != nil {
			return navActionErrMsg{forID: broadcastAction, err: err}
		}
		return navActionMsg{forID: broadcastAction, status: status}
	}
}

// paneMarker is the glyph in a pane row's first column: the focused pane, an
// agent's status, or nothing for a plain shell.
func (m navigatorModel) paneMarker(s styles, item NavItem) (string, lipgloss.Style) {
	switch {
	case item.Current:
		return openMarker, s.accent
	case item.Status == "blocked":
		return "◉", s.accent
	case item.Status == "working":
		return upMarker, s.up
	case item.Status != "":
		return downMarker, s.muted
	default:
		return blankMarker, s.muted
	}
}

func (m navigatorModel) renderPaneRows(s styles, selected lipgloss.Style, start, rows, w int) []string {
	out := make([]string, 0, rows)
	for i := start; i < len(m.items) && i < start+rows; i++ {
		item := m.items[i]
		marker, markerStyle := m.paneMarker(s, item)

		base, dim := s.text, s.muted
		pointer := frameIndent + "  "
		if m.marked[item.ID] {
			pointer = frameIndent + s.accent.Render("▣ ")
		}
		if i == m.cursor {
			base, dim = selected, selected
			markerStyle = selected
			pointer = selected.Render(frameIndent + "▸ ")
		}

		line := pointer + markerStyle.Render(marker) + base.Render(" "+oneLine(item.Label))
		if item.Detail != "" {
			line += dim.Render("  " + oneLine(item.Detail))
		}
		if i == m.cursor {
			line += selected.Render(strings.Repeat(" ", max(0, w-lipgloss.Width(line))))
		}
		out = append(out, line)
	}
	return out
}
