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
	skipMarker  = "~" // proxied, deliberately not probed
	blankMarker = " " // not probed yet
	// fallbackRows is how many host rows to draw before a WindowSizeMsg reports
	// a height. Only then — visibleRows otherwise fills whatever the pane gives
	// it. There is no ceiling: herdr sizes the popup, and a ceiling under it only
	// leaves the box emptier, not smaller.
	fallbackRows = 12
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
// field, with the labels padded so the values form a single edge. The alignment
// is a requirement, not presentation: scanning down the list is why the panel
// exists, and an edge that moves is worse than one further right.
//
// previewLines budgets these and renderPreview draws them, so the row budget
// cannot disagree with what reaches the screen — the preview's height varies
// with how many fields the cursor host sets.
//
// sshconfig.Parse always resolves Port and sets SourceFile, so a parsed host
// has at least three fields here, never two; a hand-built test Host can have
// two and understate the chrome.
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
	if h.ProxyCommand != "" {
		fields = append(fields, field{"ProxyCommand", h.ProxyCommand})
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

// fixedChrome counts the lines View draws at any height: the title block, the
// footer, and the warning block when there is one. These do not yield, so
// visibleRows and previewLines both subtract them.
// TestFixedChromeCountsWhatTheFrameDraws asserts the total against what the
// frame actually emits.
func (m model) fixedChrome() int {
	return headerRows + footerRows + m.warningLines()
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
// truncated. It reserves on len(m.view) > 1 rather than the exact truncation
// condition, because rows is what that condition needs and the two would be
// mutually recursive. It never under-reserves: a single-host list cannot be
// truncated.
func (m model) noticeReserve() int {
	if len(m.view) > 1 {
		return 1
	}
	return 0
}

// previewLines is how many lines the preview gets: a separator plus as many
// fields as fit, or none. The preview is what yields when the pane is too
// short — it is the only chrome whose height varies, and the only one the
// operator can dismiss with ^o. Below the threshold ^o is a no-op: in a pane
// this short the host list is the part worth keeping.
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
//
// There is no ceiling over this: the pane herdr hands the popup is the bound,
// and a second one under it only leaves the box half empty. See fallbackRows.
func (m model) visibleRows() int {
	if m.height <= 0 {
		// No WindowSizeMsg has arrived yet. Draw a plausible list rather than
		// an empty one on the first frame.
		return fallbackRows
	}
	n := m.height - m.chromeLines() - m.noticeReserve()
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
	s := newStyles(m.opts.Theme)

	// The body first, then the rule, because on the first frame — before any
	// WindowSizeMsg — the rule has no pane width to span and falls back to the
	// frame's widest line. Assembling in this order is what lets it reach both
	// edges then instead of stopping short.
	// The title is s.text rather than s.accent: the dialog's own title is plain
	// foreground, and an accent one here read as a heading competing with the
	// band rather than as the label on an input.
	title := frameIndent + fmt.Sprintf("%s %s", s.text.Render("ssh"), s.text.Render(m.query+"▏"))

	var body strings.Builder
	switch {
	case len(m.opts.Hosts) == 0:
		body.WriteString(frameIndent + s.muted.Render("  no ~/.ssh/config — nothing to pick") + "\n")
	case len(m.view) == 0:
		body.WriteString(frameIndent + s.muted.Render("  no hosts match") + "\n")
	default:
		body.WriteString(m.renderRows(s))
	}

	if m.showPreview() {
		body.WriteString(m.renderPreview(s))
	}

	w := m.innerWidth()
	if w == 0 {
		// Measure the footer in its natural, unpadded form and include it: it
		// is usually the widest line in the frame, and a rule sized without it
		// would stop short of the hints on the first paint. renderActions at
		// width 0 returns exactly that form, which is also what it will draw
		// once the measurement comes back too narrow to centre in.
		w = widestLine(title + "\n" + body.String() + "\n" + renderNavHints(s) + "\n" + renderActions(s, 0))
	}

	body.WriteString("\n")
	body.WriteString(renderNavHints(s) + "\n")
	body.WriteString(renderActions(s, w) + "\n")
	body.WriteString(m.renderWarnings(s))

	// The rule is inset on both sides to the same two columns as the title, so
	// the frame has one margin rather than a divider that outruns everything
	// above and below it.
	var frame strings.Builder
	frame.WriteString("\n")
	frame.WriteString(title + "\n")
	frame.WriteString(frameIndent + rule(w-2*len(frameIndent), s.muted) + "\n")
	frame.WriteString("\n")
	frame.WriteString(body.String())

	// The frame closes on a blank padding row, matching the one it opens with.
	// Inside herdr's border, content flush against the edge read as a pane with
	// a line round it rather than as a dialog.
	//
	// That row *is* the empty line after a trailing newline, so the frame has to
	// end in exactly one of those: trim whatever renderWarnings left and put one
	// back, rather than trusting the block above to have ended on the right
	// count. footerRows budgets for it, and so does screenRows in the tests — a
	// trailing newline really is a row the terminal draws, which is the fact
	// that went unbudgeted when box() stopped trimming it on View's behalf.
	//
	// Leave AltScreen and MouseMode at their zero values: the popup is already
	// a modal of its own, and the picker is keyboard-only.
	return tea.NewView(m.clampToWidth(strings.TrimRight(frame.String(), "\n") + "\n"))
}

// clampToWidth truncates every line of the assembled frame to the pane width,
// and is the only place a width is read on the way to the screen.
//
// Width is a height problem: the picker renders inline, so a line wider than
// the pane soft-wraps into an extra screen row the height budget counted as
// one. The key hints alone are 77 columns. One clamp on the assembled frame
// rather than a width threaded into each renderer, so a line added to View
// cannot be silently exempt.
//
// A width <= 0 means no truncation: width is 0 until the first WindowSizeMsg,
// and clamping to 0 would draw the first frame as nothing.
//
// lipgloss's MaxWidth is load-bearing over a byte or rune slice: it measures in
// display cells and copies escape sequences through even past the cut, so an
// already-styled line is not left with a half-written escape.
func (m model) clampToWidth(frame string) string {
	w := m.innerWidth()
	if w <= 0 {
		return frame
	}
	clamp := lipgloss.NewStyle().MaxWidth(w)
	// Split rather than a single Render so each line is cut on its own, and
	// rejoined so the frame's line structure comes back exactly. An empty line
	// clamps to itself, so a frame that does end in a newline is not a case to
	// handle separately.
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

// maxAliasColumn caps how far the detail column can be pushed right. One
// unusually long alias should not cost every other row the width of it; past
// this the long row goes ragged on its own and the rest stay aligned.
const maxAliasColumn = 28

// aliasColumn is the column every row's detail half starts at, measured from
// the widest alias the picker loaded.
//
// Measured over m.opts.Hosts rather than over the rows on screen, for the
// reason previewLabelWidth is a constant: an edge that moves is worse than an
// edge further right, and scanning the list is what the picker is for. The
// visible window would shift the edge on every scroll, and m.view would shift
// it on every keystroke. The whole loaded set shifts it never.
func (m model) aliasColumn() int {
	w := 0
	for _, h := range m.opts.Hosts {
		if n := lipgloss.Width(h.Alias); n > w {
			w = n
		}
	}
	if w > maxAliasColumn {
		return maxAliasColumn
	}
	return w
}

func (m model) renderRows(s styles) string {
	rows, cursor := m.window()
	col := m.aliasColumn()
	var b strings.Builder
	for i, row := range rows {
		h := row.Host
		marker := blankMarker
		style := s.muted
		switch {
		case h.ProxyJump != "" || h.ProxyCommand != "":
			marker = skipMarker
		case m.probed[h.Alias] && m.up[h.Alias]:
			marker, style = upMarker, s.up
		case m.probed[h.Alias]:
			marker = downMarker
		}
		// "open" wins over reachability: it is the marker that changes what
		// enter does.
		if _, open := m.opts.OpenPanes[h.Alias]; open {
			marker, style = openMarker, s.accent
		}

		// base, dim and hit are the row's three roles: the alias, the detail
		// column, and a rune that matched the query.
		base, dim, hit := s.text, s.muted, s.accent
		// frameIndent, then the ▸ channel: unselected rows spend it on blank
		// and the cursor row puts the marker in it, so both land their text
		// on the same column and the marker reads as a pointer into the list
		// rather than as another column of it.
		pointer := frameIndent + "  "
		selected := i == cursor
		if selected {
			// Every piece of the cursor row shares one style, because the band
			// has to be a single uninterrupted color — a muted detail column or
			// a green reachability marker inside it would punch holes in it. The
			// markers keep their glyphs, which is where the fact actually lives;
			// only the color is redundant with the glyph.
			//
			// And the query highlight switches from color to underline. The old
			// bold-only cursor existed because accent already means "this rune
			// matched", so it could not also mean "this is the cursor" — the
			// band takes over the second meaning, and accent-on-accent would
			// erase the first. Underline says it without a second color.
			base, dim, hit = s.chip, s.chip, s.chip.Underline(true)
			style = s.chip
			pointer = s.chip.Render(frameIndent + "▸ ")
		}
		alias := highlight(h.Alias, row.AliasPos, base, hit)

		// The detail column is assembled from already-styled pieces rather than
		// styled at the end, so the hostname's highlight positions stay aligned
		// with the hostname itself when a user or port is prepended.
		detail := highlight(h.HostName, row.HostNamePos, dim, hit)
		if h.User != "" {
			detail = dim.Render(h.User+"@") + detail
		}
		// Both halves matter. Parse defaults Port to "22", so the second clause
		// hides the port that every host has; the first covers a Host built
		// directly rather than parsed, where Port is "" and a lone ":" would
		// otherwise trail the hostname.
		if h.Port != "" && h.Port != "22" {
			detail += dim.Render(":" + h.Port)
		}
		if h.ProxyJump != "" {
			// A jump host replaces the address outright: the address is not what
			// the connection actually reaches.
			detail = dim.Render("via " + h.ProxyJump)
		} else if h.ProxyCommand != "" {
			// ProxyCommand can be an arbitrarily long shell command; the preview
			// shows it in full, while the row needs only the routing fact.
			detail = dim.Render("via ProxyCommand")
		}
		// The gaps between the columns are rendered rather than written as bare
		// spaces, because on the banded row a bare space is a hole. Reverse
		// video only reaches the cells a style actually renders, so an unstyled
		// separator draws in the terminal's own background — and the band came
		// out as three green blocks with black slots between them instead of
		// one bar. bar() padding the right-hand end could not show this: the
		// holes are interior.
		gap := s.text
		if selected {
			gap = s.chip
		}
		// Pad the alias out to the shared column so the detail halves form one
		// edge. Two spaces is the floor, so an alias over maxAliasColumn still
		// gets a gutter instead of running into its own hostname.
		pad := col - lipgloss.Width(alias) + 2
		if pad < 2 {
			pad = 2
		}
		line := pointer + style.Render(marker) + gap.Render(" ") + alias + gap.Render(strings.Repeat(" ", pad)) + detail
		if selected {
			line = bar(line, m.innerWidth(), s.chip)
		}
		b.WriteString(line + "\n")
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
		fmt.Fprintf(&b, "%s\n", s.muted.Render(fmt.Sprintf(frameIndent+"  … %d off screen", len(m.view)-len(rows))))
	}
	return b.String()
}

// previewSeparator is the divider above the preview's fields, indented to the
// same column as the fields under it rather than spanning the box.
//
// Named rather than written inline because it is what the tests look for when
// they ask whether the preview is drawn, and a bare run of dashes no longer
// answers that: the title's rule is dashes too, so a substring search for them
// finds the frame whether the preview is there or not. Both directions of that
// test were wrong at once — the "separator present" assertion passed off the
// title rule, and the "separator shed" assertion failed against it.
const previewSeparator = frameIndent + "  ─────"

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
func (m model) renderPreview(s styles) string {
	n := m.previewLines()
	if n < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString(s.muted.Render(previewSeparator) + "\n")
	for _, l := range previewFields(m.view[m.cursor].Host)[:n-1] {
		b.WriteString(frameIndent + "  " + s.text.Render(l) + "\n")
	}
	return b.String()
}

// renderWarnings draws the footer's warning block: the first maxWarnings
// warnings as written, then "… N more" for the rest. An empty string is the
// no-warnings case, which View writes harmlessly.
//
// The text rather than a count: a count says something is wrong and nothing
// about what, and this footer is the only place these ever appear — the picker
// is a pane entrypoint, so the stderr the connect verb uses prints nowhere the
// operator reads. "more" rather than the host list's "off screen" because this
// list is always drawn from the top.
//
// Over-wide warnings are truncated, not wrapped: a wrapped line is two screen
// rows that warningLines counted as one.
func (m model) renderWarnings(s styles) string {
	shown := m.opts.Warnings
	if len(shown) > maxWarnings {
		shown = shown[:maxWarnings]
	}
	var b strings.Builder
	for _, w := range shown {
		fmt.Fprintf(&b, "%s\n", s.muted.Render("  "+oneLine(w)))
	}
	if hidden := len(m.opts.Warnings) - len(shown); hidden > 0 {
		fmt.Fprintf(&b, "%s\n", s.muted.Render(fmt.Sprintf("  … %d more", hidden)))
	}
	return b.String()
}

// oneLine folds s onto a single line, so one warning costs the one row
// warningLines reserved for it. Warnings are opaque strings built outside this
// package, and pluginconfig's errors.Join separates multiple rejected keys with
// a newline. clampToWidth cannot catch that — by the time the frame is
// assembled, an embedded newline is indistinguishable from a line View meant to
// emit.
//
// FieldsFunc rather than a replace, so "\r\n" collapses to one space and a lone
// carriage return cannot overwrite the start of its own line.
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
