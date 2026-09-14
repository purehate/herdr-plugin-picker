package picker

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

// chrome.go is the palette the frame renders with and the rule under its title.
// The frame itself is assembled in navigator.go; this file is what both the
// tabs and the ssh rows draw with.
//
// It draws no border of its own: herdr already draws the popup a bordered chrome
// in the accent color, and a second one produced two concentric boxes. Each line
// carries indentation to stay off that border; the band is the one thing that
// deliberately runs the full width and touches it.

// frameIndent is the column the title, the rule and the hints start at: two
// columns, matching the settings dialog's inset. Rows and the preview add their
// own gutter, so the ▸ marker sits in the channel between. Not applied to the
// band, which runs the full inner width.
const frameIndent = "  "

// styles is the palette one frame renders with, resolved from the theme once
// rather than rebuilt by each renderer. Passed rather than stored on the model
// because it is derived from opts.Theme and nothing mutates it.
type styles struct {
	// accent is a rune that matched the query, off the cursor row.
	accent lipgloss.Style
	text   lipgloss.Style
	muted  lipgloss.Style
	// up is the reachability marker for a host that answered.
	up lipgloss.Style
	// chip is the inverted band the Settings fallback uses: the cursor row and
	// the footer's primary action. Reverse rather than an explicit Background,
	// so the accent becomes the band and the terminal's own background becomes
	// the text — legible under any theme, and the theme loader only reads
	// [theme].name and [ui].accent anyway.
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

// rule is the divider under the title. It spans the whole content width because
// a separator shorter than the frame reads as a piece of content rather than a
// division.
func rule(w int, muted lipgloss.Style) string {
	if w < 1 {
		return ""
	}
	return muted.Render(strings.Repeat("─", w))
}
