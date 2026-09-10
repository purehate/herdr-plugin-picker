package picker

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

// Markers, per the spec. "a pane is already connected" and "the host answers on
// 22" are two different facts and get two different glyphs; conflating them
// would make the reuse affordance unreadable.
const (
	openMarker  = "▪" // a session pane exists (accent)
	upMarker    = "●" // TCP answered (green)
	downMarker  = "○" // no answer
	skipMarker  = "~" // ProxyJump, deliberately not probed
	blankMarker = " " // not probed yet
	// maxRows is the ceiling on host rows, not the count. A short pane gets
	// fewer — see visibleRows. It stays a ceiling so the picker remains the
	// floating box the spec calls it rather than growing into a full-screen
	// list in a tall terminal.
	maxRows = 12
	// maxWarnings is the ceiling on warning lines in the footer. The count is
	// unbounded — loadHosts emits one per unreadable include — and unlike the
	// preview the footer never yields, so every line here is a host row the
	// operator does not get and the list is what the picker is for.
	//
	// Three covers the shape a real failure takes: the two sources that can
	// warn at most once each (a rejected plugin config, a rejected theme) plus
	// one parse warning. Past that the overflow notice carries the rest, so the
	// block is never more than four lines however broken the config is.
	maxWarnings = 3
)

// showPreview reports whether View will draw the preview block. The cursor
// bound is load-bearing: renderPreview indexes m.view[m.cursor].
func (m model) showPreview() bool { return m.preview && m.cursor < len(m.view) }

// previewLabelWidth is the column the preview's values start at: the longest
// label, "IdentityFile" at 12, plus two spaces. It matches the column in the
// spec's own example block.
//
// Fixed rather than computed from the fields the current host happens to set.
// A per-host width would be tighter but the value edge would shift every time
// the operator moved the cursor onto a host with a different field set, and
// scanning down the list is the whole reason the panel exists — an edge that
// moves while you scan is worse than one that sits further right.
const previewLabelWidth = 14

// previewFields is the preview's content, one "Label value" line per populated
// field, with the labels padded so the values form a single edge. The
// alignment is a requirement, not presentation: the spec calls it "the whole
// reason the panel exists". It shipped ragged and no test looked at the column,
// which is why TestPreviewLabelsPadToACommonWidth now asserts the edge itself
// rather than any one line's text.
//
// previewLines budgets these and renderPreview draws them, so the row budget
// cannot disagree with what actually reaches the screen — the preview's height
// varies with how many fields the cursor host happens to set, which is why the
// chrome is computed here instead of written down as a constant.
//
// Note that sshconfig.Parse always resolves Port and sets SourceFile, so a
// parsed host has at least three fields here, never two. A hand-built Host in
// a test can have two, which understates the chrome — that discrepancy hid a
// real overflow at height 9 from two review passes.
func previewFields(h sshconfig.Host) []string {
	type field struct{ label, value string }
	fields := []field{{"HostName", h.HostName}, {"Port", h.Port}}
	if h.User != "" {
		fields = append(fields, field{"User", h.User})
	}
	if h.IdentityFile != "" {
		fields = append(fields, field{"IdentityFile", h.IdentityFile})
	}
	if h.ProxyJump != "" {
		fields = append(fields, field{"ProxyJump", h.ProxyJump})
	}
	if h.SourceFile != "" {
		// Provenance matters as soon as Include is in play: "which file did this
		// host actually come from" is otherwise unanswerable from the picker.
		fields = append(fields, field{"source", fmt.Sprintf("%s:%d", h.SourceFile, h.SourceLine)})
	}
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		lines = append(lines, fmt.Sprintf("%-*s%s", previewLabelWidth, f.label, f.value))
	}
	return lines
}

// fixedChrome counts the lines View draws whatever the height is: the query
// header, the key hints, and the warning block when there is one. These do not
// yield. The header is the operator's own typing echoed back, the hints are the
// only discoverability the picker has, and a warning is the only account of a
// host the operator can see in their own config and cannot see here.
func (m model) fixedChrome() int {
	return 2 + m.warningLines() // query header + key hints
}

// warningLines is how many footer lines the warnings occupy: one per warning up
// to maxWarnings, plus the overflow notice when there are more than that.
//
// fixedChrome reserves this and renderWarnings emits exactly it, for the reason
// previewLines and renderPreview are paired the same way: a reserve written
// down separately from the draw is a reserve that disagrees with it. Both
// directions matter — over-reserving wastes rows the host list should have had
// as surely as under-reserving overflows the pane.
func (m model) warningLines() int {
	n := len(m.opts.Warnings)
	if n > maxWarnings {
		return maxWarnings + 1 // the capped list plus the notice
	}
	return n
}

// noticeReserve is the line the "… N off screen" notice needs once the list is
// truncated.
//
// It reserves on len(m.view) > 1 rather than on the exact truncation condition
// (len(m.view) > rows), because rows is what this feeds into and the two would
// be mutually recursive. The cost is at most one over-reserved line, and only
// for a list short enough to fit entirely in a pane tight enough for the
// preview to be shedding fields. Never under-reserves: a single-host list
// cannot be truncated at all.
func (m model) noticeReserve() int {
	if len(m.view) > 1 {
		return 1
	}
	return 0
}

// previewLines is how many lines the preview gets: a separator plus as many
// fields as fit, or none.
//
// The preview is what yields when the pane is too short, for two reasons: it is
// the only chrome whose height varies — it grows with the cursor host's field
// count, which is what made the old overflow depend on which row the cursor was
// on — and it is the only one the operator can already dismiss with ^o.
//
// Below the threshold ^o becomes a no-op. That is a deliberate trade rather
// than a bug: in a pane this short the preview cannot fit whatever the toggle
// says, and the host list is the part worth keeping.
func (m model) previewLines() int {
	if !m.showPreview() {
		return 0
	}
	full := 1 + len(previewFields(m.view[m.cursor].Host)) // separator + fields
	if m.height <= 0 {
		// No WindowSizeMsg yet. Assume room, matching visibleRows' fallback, so
		// the first frame is the one a roomy pane would draw.
		return full
	}
	// What is left after the lines that never yield, including one host row.
	budget := m.height - m.fixedChrome() - 1 - m.noticeReserve()
	if budget < 2 {
		// Not even a separator plus one field. Drop the block rather than draw
		// a divider with nothing under it.
		return 0
	}
	if full > budget {
		return budget
	}
	return full
}

// chromeLines is every line that is not a host row and not the overflow notice.
// The notice is excluded because whether it appears depends on the row budget
// this feeds, which is what noticeReserve exists to break.
func (m model) chromeLines() int { return m.fixedChrome() + m.previewLines() }

// visibleRows is how many host rows fit in the terminal height the last
// WindowSizeMsg reported, once chrome has taken its share. The picker is a
// floating overlay, so an oversized frame is not a scroll the operator can use.
func (m model) visibleRows() int {
	if m.height <= 0 {
		// No WindowSizeMsg has arrived yet. Fall back to the ceiling rather
		// than rendering an empty list on the first frame.
		return maxRows
	}
	n := m.height - m.chromeLines() - m.noticeReserve()
	if n > maxRows {
		n = maxRows
	}
	if n < 1 {
		// A picker showing zero hosts is worse than one that overflows, so the
		// floor wins over the fit. fixedChrome + one row + the notice is the
		// irreducible frame; below that height the frame stops shrinking and
		// stays at it rather than growing.
		n = 1
	}
	return n
}

func (m model) View() tea.View {
	t := m.opts.Theme
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Accent)).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	upStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Up))

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", accent.Render("ssh"), text.Render(m.query+"▏"))

	switch {
	case len(m.opts.Hosts) == 0:
		b.WriteString(muted.Render("  no ~/.ssh/config — nothing to pick") + "\n")
	case len(m.view) == 0:
		b.WriteString(muted.Render("  no hosts match") + "\n")
	default:
		b.WriteString(m.renderRows(accent, text, muted, upStyle))
	}

	if m.showPreview() {
		b.WriteString(m.renderPreview(muted, text))
	}

	b.WriteString(muted.Render("  enter split · ^t tab · ^z zoom · ^n new · ^o preview · ^u clear · esc close") + "\n")
	b.WriteString(m.renderWarnings(muted))

	// Leave AltScreen and MouseMode at their zero values: the pane is already an
	// overlay, and the picker is keyboard-only.
	return tea.NewView(m.clampToWidth(b.String()))
}

// clampToWidth truncates every line of the finished frame to m.width display
// columns. It is the only place the pane width is read.
//
// Width is a height problem. The picker renders inline rather than in an
// alternate screen, so the renderer sizes the frame to its content instead of
// clipping it to the terminal: a line wider than the pane soft-wraps into an
// extra screen row, and the frame occupies more rows than fixedChrome,
// noticeReserve and previewLines budgeted. All of that arithmetic counts logical
// lines, so none of it can see the overflow. The key hints alone are 77 columns,
// which is an ordinary split. Truncation is what makes the height invariant a
// fact rather than an aspiration.
//
// One clamp on the assembled frame rather than a width argument threaded into
// View, renderRows and renderPreview: the invariant is about every line the
// frame emits, and five call sites are five places for the next line added to
// View to be silently exempt. The clamp cannot know what it is cutting, which is
// what the fix wants — truncate, never re-layout.
//
// m.width <= 0 means no truncation. Width is 0 until the first
// tea.WindowSizeMsg arrives, and a clamp to 0 would draw the first frame as
// nothing at all.
//
// The guard is defensive rather than load-bearing today, and deleting it is the
// one change to this function no test catches: lipgloss applies MaxWidth only
// when it is > 0, so the clamp below is already a no-op at a zero or negative
// width. Keep it anyway. It states the contract where the width is read instead
// of borrowing it from a library, and it is the difference between a correct
// first frame and an empty one the moment the truncation primitive changes.
//
// The cutting is lipgloss's MaxWidth, which per line delegates to
// ansi.Truncate. Two properties are load-bearing and neither is true of a byte
// or rune slice: it measures in display cells, and it copies escape sequences
// through even past the cut. renderRows hands this function alias and detail
// strings that have already been through highlight() and style.Render(), so
// slicing one would emit a half-written escape sequence and corrupt every line
// after it.
//
// Rendering line by line rather than handing the whole frame to a single Render
// call is deliberate: lipgloss also runs horizontal alignment on a multi-line
// render, which pads every short line out to the longest one.
func (m model) clampToWidth(frame string) string {
	if m.width <= 0 {
		return frame
	}
	clamp := lipgloss.NewStyle().MaxWidth(m.width)
	// Every line View emits is newline-terminated, so the final element here is
	// the empty string after the trailing newline. Clamping it yields itself,
	// and rejoining restores the frame's exact line structure.
	lines := strings.Split(frame, "\n")
	for i, l := range lines {
		lines[i] = clamp.Render(l)
	}
	return strings.Join(lines, "\n")
}

// highlight renders s with the runes at pos in the hit style and everything else
// in base. Runs of same-styled runes are batched into one Render call, so the
// output carries one escape pair per run rather than one per rune.
//
// pos holds rune indices, so s is converted once and indexed as runes. Using
// byte offsets here would slice multi-byte runes in half.
func highlight(s string, pos []int, base, hit lipgloss.Style) string {
	if len(pos) == 0 {
		return base.Render(s)
	}
	matched := make(map[int]bool, len(pos))
	for _, p := range pos {
		matched[p] = true
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); {
		j := i
		for j < len(rs) && matched[j] == matched[i] {
			j++
		}
		style := base
		if matched[i] {
			style = hit
		}
		b.WriteString(style.Render(string(rs[i:j])))
		i = j
	}
	return b.String()
}

// window returns the visible slice of rows and the cursor's offset inside it,
// scrolling only when the cursor would fall outside.
func (m model) window() ([]Match, int) {
	rows := m.visibleRows()
	if len(m.view) <= rows {
		return m.view, m.cursor
	}
	start := m.cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start+rows > len(m.view) {
		start = len(m.view) - rows
	}
	return m.view[start : start+rows], m.cursor - start
}

func (m model) renderRows(accent, text, muted, upStyle lipgloss.Style) string {
	rows, cursor := m.window()
	var b strings.Builder
	for i, row := range rows {
		h := row.Host
		marker := blankMarker
		style := muted
		switch {
		case h.ProxyJump != "":
			marker = skipMarker
		case m.probed[h.Alias] && m.up[h.Alias]:
			marker, style = upMarker, upStyle
		case m.probed[h.Alias]:
			marker = downMarker
		}
		// "open" wins over reachability: it is the marker that changes what
		// enter does.
		if _, open := m.opts.OpenPanes[h.Alias]; open {
			marker, style = openMarker, accent
		}

		base := text
		pointer := "  "
		if i == cursor {
			// The cursor row goes bold rather than fully accented. The accent
			// color now means "this rune matched the query", so it cannot also
			// mean "this is the cursor" without swallowing the highlight.
			pointer = accent.Render("▸ ")
			base = text.Bold(true)
		}
		alias := highlight(h.Alias, row.AliasPos, base, accent)

		// The detail column is assembled from already-styled pieces rather than
		// styled at the end, so the hostname's highlight positions stay aligned
		// with the hostname itself when a user or port is prepended.
		detail := highlight(h.HostName, row.HostNamePos, muted, accent)
		if h.User != "" {
			detail = muted.Render(h.User+"@") + detail
		}
		// Both halves matter. Parse defaults Port to "22", so the second clause
		// hides the port that every host has; the first covers a Host built
		// directly rather than parsed, where Port is "" and a lone ":" would
		// otherwise trail the hostname.
		if h.Port != "" && h.Port != "22" {
			detail += muted.Render(":" + h.Port)
		}
		if h.ProxyJump != "" {
			// A jump host replaces the address outright: the address is not what
			// the connection actually reaches.
			detail = muted.Render("via " + h.ProxyJump)
		}
		fmt.Fprintf(&b, "%s%s %s  %s\n", pointer, style.Render(marker), alias, detail)
	}
	if len(m.view) > len(rows) {
		// "off screen" rather than "more", because the count is every match the
		// window does not include — the ones scrolled off above it as well as
		// the ones below. "N more" reads as "N further down", which is wrong
		// wherever the window is not at the top of the list, and at the bottom
		// of a long list it is wrong about every host it counts. Splitting the
		// count into two directions would say more, but direction is not
		// actionable here: there is no jump-to-end, so the operator's next move
		// is ^j/^k or a narrower query either way, and one number is one thing
		// to read.
		fmt.Fprintf(&b, "%s\n", muted.Render(fmt.Sprintf("  … %d off screen", len(m.view)-len(rows))))
	}
	return b.String()
}

// renderPreview draws the separator plus the fields previewLines budgeted, in
// declaration order — so a short pane sheds provenance before it sheds the
// hostname. An empty string is the "did not fit" case, which View writes
// harmlessly.
//
// The guard reads n < 2 rather than n < 1 even though n == 1 is unreachable:
// previewFields always yields at least HostName and Port, so a non-zero
// previewLines is at least 2. The stricter bound is not testable — no input
// reaches it — and weakening it to n < 1 changes nothing today. It stays
// because the two differ if that contract ever slips: n < 2 degrades to no
// block, n < 1 degrades to a divider with nothing under it. Only n == 0 must be
// caught at all, since [:n-1] would panic on it.
func (m model) renderPreview(muted, text lipgloss.Style) string {
	n := m.previewLines()
	if n < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString(muted.Render("  ─────") + "\n")
	for _, l := range previewFields(m.view[m.cursor].Host)[:n-1] {
		b.WriteString("  " + text.Render(l) + "\n")
	}
	return b.String()
}

// renderWarnings draws the footer's warning block: the first maxWarnings
// warnings as they were written, then "… N more" for the ones that did not fit.
// An empty string is the no-warnings case, which View writes harmlessly.
//
// The text rather than the count that shipped. A count tells the operator that
// something is wrong and nothing about what, and this footer is the only place
// these ever appear: the picker is a pane entrypoint, so the stderr the connect
// verb writes its half of the same policy to prints nowhere the operator reads.
//
// "more" rather than the host list's "off screen", against the same count of
// what is not shown. The two notices differ because the two lists do: window()
// scrolls, so its hidden rows lie in both directions, while this list is always
// drawn from the top and the ones it drops are always the ones after it.
//
// Over-wide warnings are truncated by clampToWidth like every other line,
// rather than wrapped. A wrapped footer line is two screen rows that
// warningLines counted as one — the overflow commit 58c9762 closed — and a
// truncated warning still leads with the file and position the operator needs,
// which is more than the count it replaces carried at any width.
func (m model) renderWarnings(muted lipgloss.Style) string {
	shown := m.opts.Warnings
	if len(shown) > maxWarnings {
		shown = shown[:maxWarnings]
	}
	var b strings.Builder
	for _, w := range shown {
		fmt.Fprintf(&b, "%s\n", muted.Render("  "+oneLine(w)))
	}
	if hidden := len(m.opts.Warnings) - len(shown); hidden > 0 {
		fmt.Fprintf(&b, "%s\n", muted.Render(fmt.Sprintf("  … %d more", hidden)))
	}
	return b.String()
}

// oneLine folds s onto a single line, so one warning costs the one row
// warningLines reserved for it.
//
// Warnings are opaque strings built outside this package, and one of them is
// multi-line today: pluginconfig returns errors.Join when more than one key is
// rejected, and errors.Join's Error() separates them with a newline. clampToWidth
// cannot cover this the way it covers an over-wide line — by the time the frame
// is assembled, an embedded newline is indistinguishable from a line View meant
// to emit, so the row is already spent.
//
// FieldsFunc rather than a replace, so a "\r\n" pair collapses to one space
// instead of two and a lone carriage return cannot send the rest of the warning
// back over the start of its own line.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r'
	}), " ")
}

// Run shows the picker and blocks until the operator selects or quits. The
// second return value is false when they quit without choosing.
func Run(o Options) (Selection, bool, error) {
	p := tea.NewProgram(newModel(o))
	final, err := p.Run()
	if err != nil {
		return Selection{}, false, err
	}
	m, ok := final.(model)
	if !ok || m.chosen == nil {
		return Selection{}, false, nil
	}
	return *m.chosen, true, nil
}
