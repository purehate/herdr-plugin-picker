package picker

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

// navssh.go is the ssh tab: it turns the parsed config into rows the navigator
// can rank and draw, and ports the standalone picker's reachability markers,
// reuse marker, and preview into the shared frame.

// sshNavItems ranks the hosts against query and keeps the matched rune
// positions with each row. It reuses Rank rather than rankNav so the ssh tab
// highlights the runes that earned the row, exactly as the standalone picker
// did.
func sshNavItems(hosts []sshconfig.Host, query string) []NavItem {
	matches := Rank(hosts, query)
	out := make([]NavItem, 0, len(matches))
	for _, match := range matches {
		out = append(out, NavItem{
			ID:          match.Host.Alias,
			Host:        match.Host,
			AliasPos:    match.AliasPos,
			HostNamePos: match.HostNamePos,
		})
	}
	return out
}

// sshNavItemsRegex filters hosts by a case-insensitive pattern on the alias or
// hostname, keeping config order. There are no matched-rune positions to
// highlight: a regex has no single reading to point at, so the row is drawn
// plain.
func sshNavItemsRegex(hosts []sshconfig.Host, pattern string) ([]NavItem, error) {
	re, err := compileQuery(pattern)
	if err != nil {
		return nil, err
	}
	out := make([]NavItem, 0, len(hosts))
	for _, h := range hosts {
		if re.MatchString(h.Alias) || re.MatchString(h.HostName) {
			out = append(out, NavItem{ID: h.Alias, Host: h})
		}
	}
	return out, nil
}

// markerCellWidth is the fixed width of the marker column: the glyph, a space,
// and the widest latency ("999ms"). Fixed so the alias column does not shift as
// probe results land, the same reason aliasColumnFor measures the whole set.
const markerCellWidth = 7

// latencyLabel formats a probe's round-trip time for the marker column, or ""
// when the host was not reached. At most five characters, which is what
// markerCellWidth reserves.
func (m navigatorModel) latencyLabel(alias string) string {
	if !m.probed[alias] || !m.up[alias] {
		return ""
	}
	switch d := m.latency[alias]; {
	case d <= 0:
		return ""
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

// toggleMark flips the cursor host's mark and steps down, the way fzf's
// multi-select does, so several hosts can be marked without moving the cursor
// back to the keyboard.
func (m navigatorModel) toggleMark() navigatorModel {
	item, ok := m.cursorItem()
	if !ok {
		return m
	}
	if m.marked[item.ID] {
		delete(m.marked, item.ID)
	} else {
		m.marked[item.ID] = true
	}
	return m.move(1)
}

// markedHosts returns the marked hosts in the ssh tab's order, or nil when
// nothing is marked. It reads the full host list rather than the filtered rows,
// so a mark survives a query that hides it.
func (m navigatorModel) markedHosts() []NavItem {
	if len(m.marked) == 0 {
		return nil
	}
	out := make([]NavItem, 0, len(m.marked))
	for _, item := range sshNavItems(m.opts.Hosts, "") {
		if m.marked[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// aliasColumnFor is the column every ssh row's detail half starts at, measured
// from the widest alias loaded. Measured over the whole set rather than the rows
// on screen, for the reason previewLabelWidth is a constant: an edge that moves
// on every scroll or keystroke is worse than one further right.
func aliasColumnFor(hosts []sshconfig.Host) int {
	w := 0
	for _, h := range hosts {
		if n := lipgloss.Width(h.Alias); n > w {
			w = n
		}
	}
	if w > maxAliasColumn {
		return maxAliasColumn
	}
	return w
}

// sshMarker is the glyph and style for a host's row: a live session outranks
// reachability because it changes what enter does, and a proxied host is marked
// as deliberately unprobed rather than down.
func (m navigatorModel) sshMarker(s styles, alias string, h sshconfig.Host) (string, lipgloss.Style) {
	switch {
	case h.ProxyJump != "" || h.ProxyCommand != "":
		return skipMarker, s.muted
	case m.probed[alias] && m.up[alias]:
		return upMarker, s.up
	case m.probed[alias]:
		return downMarker, s.muted
	default:
		return blankMarker, s.muted
	}
}

func (m navigatorModel) renderSSHRows(s styles, selected lipgloss.Style, start, rows, w int) []string {
	col := aliasColumnFor(m.opts.Hosts)
	out := make([]string, 0, rows)
	for i := start; i < len(m.items) && i < start+rows; i++ {
		item := m.items[i]
		h := item.Host
		marker, markerStyle := m.sshMarker(s, h.Alias, h)
		if _, open := m.opts.OpenPanes[h.Alias]; open {
			marker, markerStyle = openMarker, s.accent
		}

		base, dim, hit := s.text, s.muted, s.accent
		pointer := frameIndent + "  "
		if m.marked[h.Alias] {
			pointer = frameIndent + s.accent.Render("▣ ")
		}
		if i == m.cursor {
			// One style for the whole band: a muted detail or a green marker
			// inside it would punch a hole in the highlight. The markers keep
			// their glyphs; only the color is redundant with them.
			base, dim, hit = selected, selected, selected.Underline(true)
			markerStyle = selected
			pointer = selected.Render(frameIndent + "▸ ")
		}

		alias := highlight(h.Alias, item.AliasPos, base, hit)
		detail := highlight(h.HostName, item.HostNamePos, dim, hit)
		if h.User != "" {
			detail = dim.Render(h.User+"@") + detail
		}
		// Both halves matter: Parse defaults Port to "22", so the second clause
		// hides the port every host has; the first covers a hand-built Host
		// where Port is "" and a lone ":" would trail the hostname.
		if h.Port != "" && h.Port != "22" {
			detail += dim.Render(":" + h.Port)
		}
		if h.ProxyJump != "" {
			detail = dim.Render("via " + h.ProxyJump)
		} else if h.ProxyCommand != "" {
			detail = dim.Render("via ProxyCommand")
		}

		gap := s.text
		if i == m.cursor {
			gap = selected
		}
		pad := col - lipgloss.Width(alias) + 2
		if pad < 2 {
			pad = 2
		}
		cell := marker
		if lat := m.latencyLabel(h.Alias); lat != "" {
			cell += " " + lat
		}
		cellPad := markerCellWidth - lipgloss.Width(cell)
		if cellPad < 0 {
			cellPad = 0
		}
		line := pointer + markerStyle.Render(cell) + gap.Render(strings.Repeat(" ", cellPad+1)) +
			alias + gap.Render(strings.Repeat(" ", pad)) + detail
		if i == m.cursor {
			line += selected.Render(strings.Repeat(" ", max(0, w-lipgloss.Width(line))))
		}
		out = append(out, line)
	}
	return out
}

// sshPreviewLines is how many lines the preview block gets: a separator plus as
// many fields as fit. The preview is the chrome that yields first, and it is the
// only one the operator can dismiss with ^o.
func (m navigatorModel) sshPreviewLines() int {
	if m.section != NavSSH || !m.preview || m.cursor < 0 || m.cursor >= len(m.items) {
		return 0
	}
	return 1 + len(previewFields(m.items[m.cursor].Host))
}

// renderSSHPreview draws the separator plus the fields sshPreviewLines budgeted,
// in declaration order — a short pane sheds provenance before it sheds the
// hostname.
func (m navigatorModel) renderSSHPreview(s styles) []string {
	n := m.sshPreviewLines()
	if n < 2 {
		return nil
	}
	out := []string{s.muted.Render(previewSeparator)}
	for _, l := range previewFields(m.items[m.cursor].Host)[:n-1] {
		out = append(out, frameIndent+"  "+s.text.Render(l))
	}
	return out
}

// warningLines is how many footer lines the config warnings occupy: one per
// warning up to maxWarnings, plus the overflow notice when there are more.
func (m navigatorModel) warningLines() int {
	if m.section != NavSSH {
		return 0
	}
	n := len(m.opts.Warnings)
	if n > maxWarnings {
		return maxWarnings + 1
	}
	return n
}

// renderWarnings draws the first maxWarnings warnings, then "… N more". An empty
// slice is the no-warnings case, which View writes harmlessly. Only the ssh tab
// shows these: they are all about the ssh config, and the other tabs have
// nothing to do with it.
func (m navigatorModel) renderWarnings(s styles) []string {
	if m.section != NavSSH || m.helpOpen {
		return nil
	}
	shown := m.opts.Warnings
	if len(shown) > maxWarnings {
		shown = shown[:maxWarnings]
	}
	out := make([]string, 0, len(shown)+1)
	for _, w := range shown {
		out = append(out, s.muted.Render(frameIndent+oneLine(w)))
	}
	if hidden := len(m.opts.Warnings) - len(shown); hidden > 0 {
		out = append(out, s.muted.Render(fmt.Sprintf(frameIndent+"… %d more", hidden)))
	}
	return out
}
