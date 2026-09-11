package picker

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// chrome.go is the frame the picker sits in: the title's rule, the footer's key
// hints, and the band under the cursor row. view.go stays about the content that
// goes inside it.
//
// The shape is herdr's own settings dialog, which is the floating box the
// operator already recognises on this terminal: a title, a rule under it, the
// list with the selected row as a full-width band, and a footer whose primary
// action is an inverted chip.
//
// It deliberately draws no border of its own, which is a reversal. A border was
// added here on the reasoning that a popup with no edge has nothing saying where
// the modal stops — and that reasoning was sound and the premise was wrong.
// herdr already draws the popup its own bordered chrome, labelled "popup", in
// the accent color. Drawing a second one inside it produced two concentric
// boxes a single cell apart, which is what the settings dialog does not look
// like. There is no setting to suppress herdr's: `herdr --default-config`
// documents `type`, `command`, `width` and `height` for a popup keybinding and
// nothing about its frame.
//
// So the border below is herdr's, and everything here draws inside it. The
// indentation each line carries is what keeps content off that border; the
// band is the one thing that deliberately runs the full width and touches it,
// the way the settings dialog's selected row does.

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
	// frameIndent is the column the title, the rule and the hints start at.
	//
	// Two columns, matching the settings dialog's inset. Host rows and the
	// preview carry their own gutter on top of this, so a row's text lands two
	// columns further in again and the ▸ marker sits in the channel between —
	// which is where the dialog puts its own.
	//
	// Not applied to the band: the dialog's selected row runs the full inner
	// width, past the inset on both sides, and that contrast is what makes it
	// read as a band rather than as a highlighted word.
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
	// action. Reverse rather than an explicit Background, so the accent
	// becomes the band and the terminal's own background becomes the text.
	// That keeps both legible under any herdr theme without this package
	// having to learn what the operator's background color is — and the theme
	// loader only reads [theme].name and [ui].accent, so it could not tell it.
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
// frame draws inside herdr's popup border rather than one of its own, so no
// columns are spent on a border here.
//
// Zero means "no width reported yet", and every caller reads it the same way —
// clampToWidth as "do not clamp", bar as "do not pad", and the rule as "span
// your widest line". That is the convention m.width already used before the
// first WindowSizeMsg, kept rather than replaced with a guessed default,
// because a guess would be wrong for exactly one frame and visibly so.
//
// There is deliberately no floor under this. A floor would be a frame wider
// than the pane, every line of it would soft-wrap into a second screen row, and
// the height budget counts logical lines and cannot see that — the width
// invariant is a height invariant.
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

// bar fills row out to w with the row's own style, so the cursor reads as a
// band across the box rather than a colored word.
//
// The padding has to carry the style rather than be appended after it: lipgloss
// pads every short line out to the box anyway, so bare spaces here would still
// reach the right edge and would still look like a word. Only who owns the
// trailing cells differs.
//
// An over-wide row is returned untouched. Padding it negatively is not a case
// to handle — clampToWidth is what cuts one, and it runs after this.
func bar(row string, w int, style lipgloss.Style) string {
	pad := w - lipgloss.Width(row)
	if pad <= 0 {
		return row
	}
	return row + style.Render(strings.Repeat(" ", pad))
}

// navHints are the keys that move around inside the picker, and actionKeys the
// keys that do something and leave. The dialog splits its footer on exactly
// that line — "↑↓ select   tab section" over "↵ apply   esc close" — and the
// split is worth copying for its own sake: it says which keys are safe to
// press while you are still looking.
//
// Separated by two spaces rather than interpuncts, again matching the dialog.
// Seven hints chained with "·" read as one long string to scan rather than as
// a set of keys to pick from.
const (
	navHints   = "↑↓ select   ^o preview   ^u clear"
	actionKeys = "^t tab   ^z zoom   ^n new"
)

// renderNavHints is the footer's first line: the keys that move.
func renderNavHints(s styles) string {
	return frameIndent + s.muted.Render(navHints)
}

// renderActions is the footer's second line: the keys that act, with enter as
// an inverted chip, centred under the list.
//
// Equally dim hints say every key matters the same amount, and enter is the key
// the picker exists for — it was indistinguishable from "^u clear". "↵ split"
// rather than "enter split" for the same reason the dialog says "↵ apply": the
// glyph is the key, and it buys back the columns the chip's padding spends.
//
// Centring falls back to the plain indent at width 0, which is the frame's
// first paint before any WindowSizeMsg. Centring against a width nobody
// reported would put the line in a place the next frame moves it out of.
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
