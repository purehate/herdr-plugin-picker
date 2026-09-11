package picker

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// chrome.go is the frame the picker sits in: the border, the title's rule, the
// footer's key hints, and the band under the cursor row. view.go stays about
// the content that goes inside it.
//
// The shape is herdr's own settings dialog, which is the floating box the
// operator already recognises on this terminal: a single-line border, a title,
// a rule under it, the list, and a footer whose primary action is an inverted
// chip. Matching it is not decoration. The picker is a popup keybinding now,
// and a popup with no border has no edge — nothing on screen says where the
// modal stops and the pane behind it starts.

const (
	// boxRows is what the border costs the pane's height: its top and bottom.
	boxRows = 2
	// boxCols is what it costs the width: two border columns plus the one
	// column of padding inside each of them.
	boxCols = 4
	// headerRows is the title line, the rule under it, and the blank line that
	// separates the rule from the first host row.
	headerRows = 3
	// footerRows is the blank line above the key hints plus the hints.
	footerRows = 2
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
	// border is the box's edge, a color rather than a style because lipgloss
	// takes it through BorderForeground.
	border color.Color
}

func newStyles(t theme.Theme) styles {
	accent := lipgloss.Color(t.Accent)
	return styles{
		accent: lipgloss.NewStyle().Foreground(accent).Bold(true),
		text:   lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text)),
		muted:  lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted)),
		up:     lipgloss.NewStyle().Foreground(lipgloss.Color(t.Up)),
		chip:   lipgloss.NewStyle().Foreground(accent).Reverse(true),
		border: lipgloss.Color(t.Muted),
	}
}

// innerWidth is the width available to content: the pane less the border and
// its padding.
//
// Zero means "no usable content width", and every caller reads it the same way
// — clampToWidth as "do not clamp", bar as "do not pad", and box as "size
// yourself to your widest line". That is the convention m.width already used
// for the frame before the first WindowSizeMsg, kept rather than replaced with
// a guessed default, because a guess would be wrong for exactly one frame and
// visibly so.
//
// A pane of four columns or fewer resolves to it as well, and there the
// fallback is not cosmetic: the border and its padding are those four columns,
// so there is no content width left to clamp to. box's own clamp is what keeps
// the frame inside a pane that small. There is deliberately no floor under
// this. A floor would be a frame wider than the pane, every line of it would
// soft-wrap into a second screen row, and the height budget counts logical
// lines and cannot see that — the width invariant is a height invariant.
func (m model) innerWidth() int {
	if m.width <= boxCols {
		return 0
	}
	return m.width - boxCols
}

// box draws the border around the assembled content.
//
// Width is handed the whole pane width, not the content width: in lipgloss v2 it
// is the width of the finished block, border columns and padding included, so
// the content ends up boxCols narrower than whatever is passed. Handing it the
// content width draws a box two columns short of the pane and — worse —
// soft-wraps every line clampToWidth cut to the wider figure, doubling the
// frame's height. Measured against the library rather than reasoned from the
// name, and pinned by TestTheBoxFillsThePaneExactly.
func (m model) box(content string, border color.Color) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(border).
		Padding(0, 1)
	if w := m.innerWidth(); w > 0 {
		style = style.Width(w + boxCols)
	}
	// Every line the content builder emits is newline-terminated, and lipgloss
	// draws the empty string after the last newline as a row of its own — an
	// empty line inside the box, above the bottom border.
	out := style.Render(strings.TrimSuffix(content, "\n"))
	// The box is the one thing clampToWidth cannot cover, because it runs before
	// the border exists. It needs its own clamp for the panes too narrow to hold
	// a box at all: lipgloss ignores a Width that leaves no room for the border
	// and the padding — measured, every value from 1 to 4 renders content-sized
	// — so at five columns or fewer the frame comes back wider than the pane and
	// would wrap. Cutting the right-hand border off is the honest outcome there;
	// a wrapped frame would double the height instead, and the height is what
	// the operator loses the host list to.
	return clampLines(out, m.width) + "\n"
}

// rule is the divider under the title. It spans the whole content width because
// a separator shorter than the box reads as a piece of content rather than a
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

// hintText is the footer's key list, minus the primary action that leads it.
// Unchanged from what shipped: the chip is a change of emphasis, not of
// content, and every key here is still the only discoverability the picker has.
const hintText = "  ^t tab · ^z zoom · ^n new · ^o preview · ^u clear · esc close"

// renderHints draws the footer, with enter as an inverted chip.
//
// Eight equally dim hints say every key matters the same amount, and enter is
// the key the picker exists for — it was indistinguishable from "^u clear".
// "↵ split" rather than "enter split" for the same reason herdr's dialog says
// "↵ apply": the glyph is the key, and it buys back the columns the chip's
// padding spends, so the line truncates no sooner than before on a narrow pane.
func renderHints(s styles) string {
	return s.chip.Render(" ↵ split ") + s.muted.Render(hintText)
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
