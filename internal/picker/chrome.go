package picker

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// chrome.go is the frame the picker sits in: the title's rule, the footer's key
// hints, and the band under the cursor row. view.go stays about the content that
// goes inside it. The shape is herdr's own settings dialog: a title, a rule, a
// list with a full-width band on the selected row, and a footer whose primary
// action is an inverted chip.
//
// It draws no border of its own: herdr already draws the popup a bordered chrome
// in the accent color, and a second one produced two concentric boxes. Each line
// here carries indentation to stay off that border; the band is the one thing
// that deliberately runs the full width and touches it.

const (
	// headerRows is the blank padding row, the title, the rule under it, and
	// the blank line that separates the rule from the first host row.
	headerRows = 4
	// footerRows is the blank line above the hints, the two hint lines, and
	// the blank padding row under them.
	//
	// Two hint lines rather than one because the dialog this frame copies has
	// two: the keys that move around the list, and then the keys that act,
	// centred under them with the primary action as a chip. One line of seven
	// hints separated by interpuncts was denser than anything herdr draws.
	footerRows = 4
	// frameIndent is the column the title, the rule and the hints start at: two
	// columns, matching the settings dialog's inset. Host rows and the preview
	// add their own gutter, so the ▸ marker sits in the channel between. Not
	// applied to the band, which runs the full inner width.
	frameIndent = "  "
)

// styles is the palette one frame renders with, resolved from the theme once
// rather than rebuilt by each renderer. Passed rather than stored on the model
// because it is derived from opts.Theme and nothing mutates it.
type styles struct {
	// accent is the "ssh" label and, off the cursor row, a rune that matched
	// the query.
	accent lipgloss.Style
	text   lipgloss.Style
	muted  lipgloss.Style
	// up is the reachability marker for a host that answered.
	up lipgloss.Style
	// chip is the inverted band: the cursor row and the footer's primary
	// action. Reverse rather than an explicit Background, so the accent becomes
	// the band and the terminal's own background becomes the text — legible
	// under any theme, and the theme loader only reads [theme].name and
	// [ui].accent anyway.
	chip lipgloss.Style
}

func newStyles(t theme.Theme) styles {
	accent := lipgloss.Color(t.Accent)
	return styles{
		accent: lipgloss.NewStyle().Foreground(accent).Bold(true),
		text:   lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text)),
		muted:  lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted)),
		up:     lipgloss.NewStyle().Foreground(lipgloss.Color(t.Up)),
		chip:   lipgloss.NewStyle().Foreground(accent).Reverse(true),
	}
}

// innerWidth is the width available to content, which is the whole pane: the
// frame draws inside herdr's popup border, so no columns are spent on a border
// here.
//
// Zero means "no width reported yet": clampToWidth does not clamp, bar does not
// pad, and the rule spans its widest line. No floor under this — a floor would
// make the frame wider than the pane, and the soft-wrap that follows is a height
// problem the height budget cannot see.
func (m model) innerWidth() int {
	if m.width < 0 {
		return 0
	}
	return m.width
}

// rule is the divider under the title. It spans the whole content width because
// a separator shorter than the frame reads as a piece of content rather than a
// division.
func rule(w int, muted lipgloss.Style) string {
	if w < 1 {
		return ""
	}
	return muted.Render(strings.Repeat("─", w))
}

// bar fills row out to w with the row's own style, so the cursor reads as a band
// across the box rather than a colored word. The padding carries the style:
// lipgloss pads every short line anyway, so bare spaces would reach the right
// edge and still look like a word. An over-wide row is returned untouched —
// clampToWidth is what cuts one, and it runs after this.
func bar(row string, w int, style lipgloss.Style) string {
	pad := w - lipgloss.Width(row)
	if pad <= 0 {
		return row
	}
	return row + style.Render(strings.Repeat(" ", pad))
}

// navHints are the keys that move around inside the picker, and actionKeys the
// keys that do something and leave. Splitting the footer on that line says which
// keys are safe to press while you are still looking. Two spaces rather than
// interpuncts, matching the dialog.
const (
	navHints   = "↑↓ select   ^o preview   ^u clear"
	actionKeys = "^t tab   ^z zoom   ^n new"
)

// renderNavHints is the footer's first line: the keys that move.
func renderNavHints(s styles) string {
	return frameIndent + s.muted.Render(navHints)
}

// renderActions is the footer's second line: the keys that act, with enter as an
// inverted chip, centred under the list. "↵ split" rather than "enter split" for
// the same reason the dialog says "↵ apply": the glyph is the key and it buys
// back the columns the chip's padding spends.
//
// Centring falls back to the plain indent at width 0 — the frame's first paint,
// before any WindowSizeMsg — rather than against a width nobody reported.
func renderActions(s styles, w int) string {
	line := s.muted.Render(actionKeys) + "   " + s.chip.Render(" ↵ split ") + s.muted.Render("   esc close")
	pad := (w - lipgloss.Width(line)) / 2
	if pad < len(frameIndent) {
		return frameIndent + line
	}
	return strings.Repeat(" ", pad) + line
}

// widestLine measures the longest line in s, in display cells.
//
// It is the width the box will size itself to when no pane width has been
// reported, which is what the rule has to match on that first frame. Cells
// rather than bytes or runes: these lines are full of escape sequences that
// occupy no columns at all.
func widestLine(s string) int {
	w := 0
	for _, l := range strings.Split(s, "\n") {
		if n := lipgloss.Width(l); n > w {
			w = n
		}
	}
	return w
}
