package picker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// boxedModel is a model with enough hosts to have a cursor row and a row under
// it, so the bar tests can compare the two.
func boxedModel() model {
	return newModel(Options{Hosts: manyHosts(4), Theme: theme.Default()})
}

// SGR attribute codes these assertions look for. Reverse is the band — the
// cursor row and the footer's chip — and underline is how a query match stays
// visible on top of one.
const (
	sgrReverse   = "7"
	sgrUnderline = "4"
)

// hasSGRParam reports whether any escape sequence in s sets the SGR attribute
// param.
//
// Parsed rather than matched against "\x1b[7m", which is what every assertion
// here first looked for and what none of them would ever have found: lipgloss
// merges a style's attributes into one sequence, so a reversed row with a
// foreground color comes out as "\x1b[7;38;2;137;180;250m". The literal search
// reported "there is no bar" about a frame that was entirely banded.
//
// The 38/48/58 skip is the other half. Their operands are color data, not
// attributes, so an accent whose red channel happened to be 7 — "38;2;7;g;b" —
// would otherwise read as reverse video and every one of these tests would pass
// on the wrong evidence.
func hasSGRParam(s, param string) bool {
	for _, seq := range sgrSeq.FindAllString(s, -1) {
		params := strings.Split(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m"), ";")
		for i := 0; i < len(params); i++ {
			switch params[i] {
			case param:
				return true
			case "38", "48", "58":
				if i+1 < len(params) && params[i+1] == "2" {
					i += 4 // 2;r;g;b
				} else {
					i += 2 // 5;n
				}
			}
		}
	}
	return false
}

func hasReverse(s string) bool   { return hasSGRParam(s, sgrReverse) }
func hasUnderline(s string) bool { return hasSGRParam(s, sgrUnderline) }

// TestSGRParamsAreReadFromMergedSequences is the control on the helper above.
// Both halves of it are answers to a check that reported a result about
// something other than what it appeared to check, so both get a case here
// rather than being trusted.
func TestSGRParamsAreReadFromMergedSequences(t *testing.T) {
	reverse := lipgloss.NewStyle().Reverse(true)
	requireStyling(t, reverse)

	for _, tc := range []struct {
		name  string
		in    string
		param string
		want  bool
	}{
		{"plain text", "host00", sgrReverse, false},
		{"reverse alone", reverse.Render("x"), sgrReverse, true},
		{"reverse merged with a foreground", "\x1b[7;38;2;137;180;250mx\x1b[m", sgrReverse, true},
		{"underline merged with reverse", "\x1b[4;7;38;2;137;180;250mx\x1b[m", sgrUnderline, true},
		{"a foreground whose red channel is 7", "\x1b[38;2;7;180;250mx\x1b[m", sgrReverse, false},
		{"a 256-color foreground numbered 7", "\x1b[38;5;7mx\x1b[m", sgrReverse, false},
		{"a green channel of 4", "\x1b[38;2;20;4;250mx\x1b[m", sgrUnderline, false},
	} {
		if got := hasSGRParam(tc.in, tc.param); got != tc.want {
			t.Errorf("%s: hasSGRParam(%q, %q) = %v, want %v", tc.name, tc.in, tc.param, got, tc.want)
		}
	}
}

// TestTheFrameIsBoxed is the whole point of the file: a popup with no border
// has no edge, so nothing on screen says where the modal stops and the pane
// behind it starts. Asserted on the corner runes rather than on a screenshot,
// because those are what make it a box.
func TestTheFrameIsBoxed(t *testing.T) {
	lines := frameLines(renderSized(boxedModel(), 60, 20))
	if len(lines) < 3 {
		t.Fatalf("frame has %d lines, too few to be a box:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	first, last := stripANSI(lines[0]), stripANSI(lines[len(lines)-1])
	if !strings.HasPrefix(first, "┌") || !strings.HasSuffix(first, "┐") {
		t.Errorf("first line %q is not the top of a box", first)
	}
	if !strings.HasPrefix(last, "└") || !strings.HasSuffix(last, "┘") {
		t.Errorf("last line %q is not the bottom of a box", last)
	}
	for i, l := range lines[1 : len(lines)-1] {
		if s := stripANSI(l); !strings.HasPrefix(s, "│") || !strings.HasSuffix(s, "│") {
			t.Errorf("line %d %q is not walled on both sides", i+1, s)
		}
	}
}

// TestTheBoxFillsThePaneExactly pins the width arithmetic, which is the part
// that cannot be eyeballed: lipgloss's Width sets the block width including
// padding, so the value handed to it is not the width the text gets. A box one
// column too wide soft-wraps every line and doubles the frame's height; one
// column too narrow leaves a ragged gap down the right of the popup.
//
// Every line, not just the border rows, because the content lines are the ones
// lipgloss pads and they are where an off-by-one shows up.
func TestTheBoxFillsThePaneExactly(t *testing.T) {
	for _, width := range []int{40, 60, 90, 120} {
		frame := renderSized(boxedModel(), width, 20)
		for i, l := range frameLines(frame) {
			if got := lipgloss.Width(l); got != width {
				t.Errorf("at pane width %d, line %d is %d cells:\n%q", width, i, got, stripANSI(l))
			}
		}
	}
}

// TestBarFillsTheRowWithItsOwnStyle tests the mechanism directly, because at
// frame level it is nearly untestable: lipgloss's Width pads every short line
// out to the box anyway, so a cursor row that reached the right edge would
// measure correct whether the bar filled it or lipgloss did. The difference is
// only in who owns the trailing cells — plain spaces read as a short colored
// word, styled ones as a band — so that is what this asserts.
func TestBarFillsTheRowWithItsOwnStyle(t *testing.T) {
	style := lipgloss.NewStyle().Reverse(true)
	requireStyling(t, style)

	got := bar("ab", 10, style)
	if w := lipgloss.Width(got); w != 10 {
		t.Errorf("bar(%q, 10) is %d cells, want 10", "ab", w)
	}
	// The padding is the point: it has to carry the style, not be bare spaces
	// appended after the styled text closed. bar styles only what it adds, so
	// with a plain row in, the one reverse run in the output is the padding.
	if pad := strings.TrimPrefix(got, "ab"); !hasReverse(pad) {
		t.Errorf("bar's padding is unstyled, so the bar stops at the text: %q", pad)
	}
	if plain := stripANSI(got); plain != "ab        " {
		t.Errorf("bar's plain text is %q, want %q", plain, "ab        ")
	}
	// A row already at or past the width is returned untouched rather than
	// padded negatively; clampToWidth is what cuts an over-wide one.
	if got := bar("abcdefghijkl", 10, style); got != "abcdefghijkl" {
		t.Errorf("bar padded an over-wide row: %q", got)
	}
}

// TestOnlyTheCursorRowIsABar is the frame-level half: exactly one row reads as
// selected. A cursor marked only by bold text is a word; a cursor marked by a
// band across the box is a position, which is what a list needs to say — but
// two bands say nothing at all.
func TestOnlyTheCursorRowIsABar(t *testing.T) {
	requireStyling(t, lipgloss.NewStyle().Reverse(true))

	frame := renderSized(boxedModel(), 60, 20)
	cursor := cursorRow(t, frame)
	if !hasReverse(cursor) {
		t.Errorf("cursor row carries no reverse-video attribute, so there is no bar:\n%q", cursor)
	}

	for _, l := range frameLines(frame) {
		if l == cursor {
			continue
		}
		// The footer's chip is reversed on purpose; it is not a row.
		if strings.Contains(stripANSI(l), "↵ split") {
			continue
		}
		if hasReverse(l) {
			t.Errorf("a non-cursor line is reversed, so more than one row reads as selected:\n%q", stripANSI(l))
		}
	}
}

// isSGRReset reports whether seq is a full reset. lipgloss closes every style
// with one — it never emits 27, the reverse-off code — so this plus hasReverse
// is the whole state machine the frame exercises.
func isSGRReset(seq string) bool {
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
	return body == "" || body == "0"
}

// lastCellReversed reports whether reverse video was on when the terminal drew
// the last printable cell of l.
//
// On a boxed line that cell is the right-hand wall, which is what makes this
// the assertion below wants: a band left open runs past the content and paints
// the wall in accent. Checking that the line *ends* in a reset would not — the
// border style closes itself, so every line ends in one whether the band leaked
// onto the wall or not.
func lastCellReversed(l string) bool {
	on, last := false, false
	rest := l
	for {
		loc := sgrSeq.FindStringIndex(rest)
		if loc == nil {
			break
		}
		if loc[0] > 0 {
			last = on // printable cells preceded this sequence
		}
		switch seq := rest[loc[0]:loc[1]]; {
		case hasReverse(seq):
			on = true
		case isSGRReset(seq):
			on = false
		}
		rest = rest[loc[1]:]
	}
	if rest != "" {
		last = on
	}
	return last
}

// TestABarRowClosesItsStyling guards the interaction between the bar and the
// width clamp. clampToWidth cuts with ansi.Truncate, which copies escape
// sequences through past the cut; a reverse-video row cut mid-line and left
// open would paint the rest of the terminal row, and then the border on the
// next line, in accent.
//
// Widths 30 and 40 are narrower than these rows, so the cut lands inside the
// band rather than after it, which is the case the copy-through has to survive.
func TestABarRowClosesItsStyling(t *testing.T) {
	requireStyling(t, lipgloss.NewStyle().Reverse(true))

	m := newModel(Options{Hosts: wideHosts(4), Theme: theme.Default()})
	for _, width := range []int{30, 40, 60} {
		frame := renderSized(m, width, 20)
		banded := 0
		for i, l := range frameLines(frame) {
			if !hasReverse(l) {
				continue
			}
			banded++
			if lastCellReversed(l) {
				t.Errorf("at width %d, line %d leaves the band open across the box's wall:\n%q", width, i, l)
			}
		}
		// Without this the loop above passes by finding nothing to check, which
		// is exactly what it did while hasReverse was a literal string match.
		if banded == 0 {
			t.Errorf("at width %d no line in the frame is banded; there is nothing here to leave open", width)
		}
	}
}

// TestAQueryMatchStaysVisibleOnTheBar covers the collision the bar creates.
// renderRows used to argue that the cursor row could not be accented because
// accent already means "this rune matched the query" — true, and the reason the
// cursor was only bold. The bar keeps both meanings by changing which one uses
// color: the row's band is the accent, and a matched rune on it is underlined
// rather than recolored. Recoloring it accent-on-accent would erase it.
func TestAQueryMatchStaysVisibleOnTheBar(t *testing.T) {
	requireStyling(t, lipgloss.NewStyle().Underline(true))

	m := typeRunes(newModel(Options{Hosts: manyHosts(4), Theme: theme.Default()}), "host0")
	cursor := cursorRow(t, renderSized(m, 60, 20))
	if !hasUnderline(cursor) {
		t.Errorf("the matched runes on the cursor row are not underlined, so the query highlight is lost under the bar:\n%q", cursor)
	}
	// The control. With no query there is no match, so nothing on the cursor row
	// is underlined; without this the assertion above is satisfied by a band that
	// underlines the whole row and says nothing about which runes matched.
	if hasUnderline(cursorRow(t, renderSized(boxedModel(), 60, 20))) {
		t.Error("the cursor row is underlined with no query typed, so the underline says nothing about a match")
	}
}

// TestThePrimaryActionIsAChip asserts the footer's one emphasis. Eight equally
// dim hints say that every key matters the same amount, and enter is the key
// the picker exists for.
func TestThePrimaryActionIsAChip(t *testing.T) {
	requireStyling(t, lipgloss.NewStyle().Reverse(true))

	var hint string
	for _, l := range frameLines(renderSized(boxedModel(), 90, 20)) {
		if strings.Contains(stripANSI(l), "esc close") {
			hint = l
		}
	}
	if hint == "" {
		t.Fatal("no hint line in the frame")
	}
	if !hasReverse(hint) {
		t.Errorf("the hint line has no chip, so enter reads like every other key:\n%q", hint)
	}
	if plain := stripANSI(hint); !strings.Contains(plain, "↵ split") {
		t.Errorf("hint line %q does not name the primary action", plain)
	}
}

// TestTheRuleSpansTheContentWidth keeps the rule from being a stub. A separator
// shorter than the box reads as a piece of content rather than a division.
func TestTheRuleSpansTheContentWidth(t *testing.T) {
	for _, width := range []int{40, 90} {
		var rule string
		for _, l := range frameLines(renderSized(boxedModel(), width, 20)) {
			// Walled, so the top border — which is nothing but ─ runes — cannot
			// be mistaken for the rule and pass this by drawing itself.
			plain := stripANSI(l)
			if strings.HasPrefix(plain, "│") && strings.Contains(plain, "───") {
				rule = plain
				break
			}
		}
		if rule == "" {
			t.Fatalf("at width %d there is no rule under the title", width)
		}
		if got, want := strings.Count(rule, "─"), width-boxCols; got != want {
			t.Errorf("at pane width %d the rule is %d cells, want %d", width, got, want)
		}
	}
}

// TestFixedChromeCountsWhatTheBoxDraws is the reserve-against-draw pairing this
// package already insists on for the preview and the warnings, applied to the
// box. fixedChrome is what visibleRows subtracts, so a border and a rule that
// draw more lines than it counts overflow the pane by exactly the difference —
// and an overflowing popup is the failure the height budget exists to prevent.
func TestFixedChromeCountsWhatTheBoxDraws(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default()})
	const height = 20

	frame := renderSized(m, 90, height)
	lines := frameLines(frame)

	rows := 0
	for _, l := range lines {
		if strings.Contains(stripANSI(l), "10.0.0.") {
			rows++
		}
	}
	if rows == 0 {
		t.Fatalf("no host rows in the frame:\n%s", stripANSI(frame))
	}
	// Every line that is not a host row is chrome, and with a list this long the
	// off-screen notice is drawn too.
	if got, want := len(lines)-rows, height-rows; got != want {
		t.Errorf("frame has %d chrome lines in a %d-row pane holding %d hosts, want %d", got, height, rows, want)
	}
}

// TestTheBoxedFrameStillFitsThePane re-runs the overflow guard now that the
// border, the rule and the two blank lines have been added to the frame. The
// bound is on rows the terminal draws, not lines emitted, so an over-wide line
// that soft-wraps is counted as the two rows it costs.
//
// The sweep starts at minFrame because that is where fitting begins to be
// possible: the box cost the irreducible frame five more lines than it had
// before, and below it the picker overflows on purpose rather than hiding the
// host list or the keys that dismiss it. minFrame itself is in the sweep, so a
// frame that grew by one more line than the budget counts still fails here.
func TestTheBoxedFrameStillFitsThePane(t *testing.T) {
	hosts := manyHosts(40)
	for _, height := range []int{minFrame, 10, 12, 16, 20, 30} {
		for _, width := range []int{40, 60, 90} {
			m := newModel(Options{Hosts: hosts, Theme: theme.Default(), ShowPreview: true})
			if got := screenRows(renderSized(m, width, height), width); got > height {
				t.Errorf("at %dx%d the frame draws %d rows", width, height, got)
			}
		}
	}
}

// TestAnUnsizedFrameIsStillABox covers the first frame, before any
// WindowSizeMsg. There is no pane width to fill then, so the box sizes itself
// to its widest line — but it is still a box, and the rule still reaches both
// walls. Getting this wrong is visible for one frame and looks like a crash.
func TestAnUnsizedFrameIsStillABox(t *testing.T) {
	frame := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "alpha", HostName: "alpha.example", Port: "22"}},
		Theme: theme.Default(),
	}).View().Content

	lines := frameLines(frame)
	width := lipgloss.Width(lines[0])
	for i, l := range lines {
		if got := lipgloss.Width(l); got != width {
			t.Errorf("unsized line %d is %d cells, want %d (the box is ragged):\n%q", i, got, width, stripANSI(l))
		}
	}
	if got, want := strings.Count(stripANSI(frame), "─"), 2*(width-2)+(width-boxCols); got != want {
		t.Errorf("unsized frame has %d horizontal runes, want %d (border top and bottom plus the rule)", got, want)
	}
}
