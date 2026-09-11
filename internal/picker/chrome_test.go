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
	sgrBold      = "1"
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

// TestTheFrameDrawsNoBorderOfItsOwn guards a reversal, so it asserts an
// absence. A border was added here on the reasoning that a popup with no edge
// has nothing saying where the modal stops; the reasoning was sound and the
// premise was false. herdr already wraps a popup in its own bordered chrome, so
// this one drew a second box a single cell inside the first — two concentric
// rectangles, which the operator caught in a screenshot and which is precisely
// what the settings dialog this frame is modeled on does not look like.
//
// The corner and wall runes rather than a width check, because those are what
// made it a box and a width check would pass either way. "─" is excluded on
// purpose: the rule and the preview separator are made of it, so it is not
// evidence of a border.
func TestTheFrameDrawsNoBorderOfItsOwn(t *testing.T) {
	frame := stripANSI(renderSized(boxedModel(), 60, 20))
	for _, r := range []string{"┌", "┐", "└", "┘", "│"} {
		if strings.Contains(frame, r) {
			t.Errorf("the frame draws %q; herdr's popup chrome is the only border:\n%s", r, frame)
		}
	}
}

// bandRun reports how many of l's leading display cells are drawn with reverse
// video, and how wide the line is. They are equal exactly when the band is one
// unbroken bar from the first cell.
//
// Leading run rather than a total count of reversed cells, because the defect
// this exists for is interior. The cursor row is assembled from a pointer, a
// marker, an alias and a detail column, and the gaps between them were written
// as bare spaces: reverse video only reaches cells a style actually renders, so
// those gaps drew in the terminal's own background and the band came out as
// three green blocks with black slots between them. Every one of those cells is
// still reversed, so a count would have matched; only the run breaks.
func bandRun(l string) (run, total int) {
	on, broken := false, false
	for rest := l; rest != ""; {
		loc := sgrSeq.FindStringIndex(rest)
		text := rest
		if loc != nil {
			text = rest[:loc[0]]
		}
		if w := lipgloss.Width(text); w > 0 {
			total += w
			if on && !broken {
				run += w
			} else {
				broken = true
			}
		}
		if loc == nil {
			break
		}
		switch seq := rest[loc[0]:loc[1]]; {
		case hasReverse(seq):
			on = true
		case isSGRReset(seq):
			on = false
		}
		rest = rest[loc[1]:]
	}
	return run, total
}

// TestTheCursorBandSpansThePane is the other half of matching the settings
// dialog, whose selected row is one solid bar touching both edges.
//
// bar() padding the row out to the pane cannot show this on its own, which is
// why the assertion walks cells: a row can reach the right edge and still be
// full of holes.
func TestTheCursorBandSpansThePane(t *testing.T) {
	requireStyling(t, lipgloss.NewStyle().Reverse(true))
	for _, width := range []int{40, 60, 90, 120} {
		found := false
		for _, l := range frameLines(renderSized(boxedModel(), width, 20)) {
			if !strings.Contains(stripANSI(l), "▸") {
				continue
			}
			found = true
			run, total := bandRun(l)
			if total != width {
				t.Errorf("at pane width %d the cursor row is %d cells:\n%q", width, total, stripANSI(l))
			}
			if run != total {
				t.Errorf("at pane width %d the band covers the first %d of %d cells, so it is broken by a gap:\n%q",
					width, run, total, stripANSI(l))
			}
			break
		}
		if !found {
			t.Fatalf("at pane width %d no row carries the cursor marker", width)
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

// stylingLeaksPastEOL reports whether l ends with a style still open, so the
// terminal would carry it into the rest of the screen row and the line below.
//
// This replaced a check on whether the last printable cell was reversed, and the
// premise inverted rather than the code being wrong. That cell used to be the
// box's right-hand wall, so a band reaching it meant the band had leaked onto
// the border. There is no border now — herdr draws the popup's, and the band is
// *supposed* to run edge to edge the way the settings dialog's selected row does
// — so reaching the last cell is the correct rendering and the old check failed
// on it.
//
// The old doc rejected "ends in a reset" on the grounds that the border style
// closed itself and so every line ended in one regardless. That was true, and it
// is what the removal of the border undid: nothing supplies a trailing reset for
// free any more, so the line ending in one is exactly the property, and the
// failure it is watching for — ansi.Truncate cutting a banded row and copying
// the opener through without its closer — shows up as a line whose last SGR
// sequence is not a reset.
func stylingLeaksPastEOL(l string) bool {
	open := false
	for rest := l; ; {
		loc := sgrSeq.FindStringIndex(rest)
		if loc == nil {
			return open
		}
		open = !isSGRReset(rest[loc[0]:loc[1]])
		rest = rest[loc[1]:]
	}
}

// TestABarRowClosesItsStyling guards the interaction between the bar and the
// width clamp. clampToWidth cuts with ansi.Truncate, which copies escape
// sequences through past the cut; a reverse-video row cut mid-line and left
// open would paint the rest of the terminal row, and then the line below it,
// in accent — inside herdr's popup, where the picker does not own the pixels.
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
			if stylingLeaksPastEOL(l) {
				t.Errorf("at width %d, line %d leaves the band open past end of line:\n%q", width, i, l)
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

// TestTheRuleSpansTheContentWidth keeps the rule from being a stub, and keeps
// it inset. A separator shorter than the frame reads as a piece of content
// rather than a division; one that runs the full pane while the title and the
// hints sit two columns in reads as a line drawn across a dialog rather than as
// the dialog's own divider.
//
// The rule is found by position rather than by content: it is ruleLine, under
// the padding row and the title. Searching for a run of dashes would find the
// preview separator too, and that one is deliberately short.
func TestTheRuleSpansTheContentWidth(t *testing.T) {
	for _, width := range []int{40, 90} {
		lines := frameLines(renderSized(boxedModel(), width, 20))
		if len(lines) <= ruleLine {
			t.Fatalf("at width %d the frame has %d lines, too few to hold a rule", width, len(lines))
		}
		rule := stripANSI(lines[ruleLine])
		if !strings.Contains(rule, "───") {
			t.Fatalf("at width %d the line under the title is not a rule: %q", width, rule)
		}
		if got, want := strings.Count(rule, "─"), width-2*len(frameIndent); got != want {
			t.Errorf("at pane width %d the rule is %d cells, want %d", width, got, want)
		}
		if !strings.HasPrefix(rule, frameIndent+"─") {
			t.Errorf("at pane width %d the rule is not inset to the title's column: %q", width, rule)
		}
	}
}

// Where the frame's fixed lines sit, counting from the top. The frame opens on
// a blank padding row, then the title, then the rule. By index rather than by
// search — see TestTheRuleSpansTheContentWidth.
const (
	titleLine = 1
	ruleLine  = 2
)

// footerLines picks the frame's last three lines apart: navigation hints,
// action hints, and the blank padding row that closes the frame.
//
// Counted from the end rather than searched for, for the same reason ruleLine
// is: "the line with esc close on it" finds the footer by the thing under test.
func footerLines(t *testing.T, frame string) (nav, actions, pad string) {
	t.Helper()
	lines := frameLines(frame)
	if len(lines) < 3 {
		t.Fatalf("the frame has %d lines, too few to hold a footer:\n%s", len(lines), stripANSI(frame))
	}
	n := len(lines)
	return stripANSI(lines[n-3]), stripANSI(lines[n-2]), stripANSI(lines[n-1])
}

// TestTheFrameOpensAndClosesOnAPaddingRow is the inset herdr's own dialog has
// and the picker did not. The popup draws a border one cell off the content;
// with the title on the first row and the hints on the last, the box read as a
// pane with a line around it rather than as a dialog.
//
// Swept over heights because the top row is fixed and the bottom row is not:
// the bottom one is whatever survives the warning block, the preview yielding,
// and the trailing-newline normalisation, and those all move with height.
func TestTheFrameOpensAndClosesOnAPaddingRow(t *testing.T) {
	for _, height := range []int{minFrame, 12, 20, 30} {
		lines := frameLines(renderSized(boxedModel(), 90, height))
		if got := stripANSI(lines[0]); strings.TrimSpace(got) != "" {
			t.Errorf("at height %d the frame opens on %q, want a blank padding row", height, got)
		}
		if got := stripANSI(lines[len(lines)-1]); strings.TrimSpace(got) != "" {
			t.Errorf("at height %d the frame closes on %q, want a blank padding row", height, got)
		}
	}
}

// TestTheTitleIsNotAccented pins the title to plain foreground. An accent title
// reads as a heading competing with the cursor band for the eye; the dialog
// this frame copies renders its own title in the same color as its body text,
// because the title here is the label on an input rather than a section head.
func TestTheTitleIsNotAccented(t *testing.T) {
	th := theme.Default()
	accent, text := fgParams(t, th.Accent), fgParams(t, th.Text)
	if accent == text {
		t.Fatalf("the theme renders accent and text identically (%q), so this test cannot see the difference", accent)
	}

	title := frameLines(renderSized(newModel(Options{Hosts: manyHosts(4), Theme: th}), 90, 20))[titleLine]
	if strings.Contains(title, accent) {
		t.Errorf("the title is drawn in the accent color:\n%q", title)
	}
	if !strings.Contains(title, text) {
		t.Errorf("the title is not drawn in the theme's text color:\n%q", title)
	}
	if hasSGRParam(title, sgrBold) {
		t.Errorf("the title is bold; the dialog's own title is plain:\n%q", title)
	}
}

// TestTheFooterSplitsNavigationFromActions covers the split itself. One line of
// seven hints chained together reads as a string to scan rather than as a set
// of keys to pick from, and it buries the distinction the dialog makes on
// purpose: the keys that move around the list, then the keys that act and
// leave. Asserted in both directions so a footer that drew everything twice, or
// put the whole set on either line, fails.
func TestTheFooterSplitsNavigationFromActions(t *testing.T) {
	nav, actions, _ := footerLines(t, renderSized(boxedModel(), 90, 20))

	for _, want := range []string{"↑↓ select", "^o preview"} {
		if !strings.Contains(nav, want) {
			t.Errorf("the navigation line is missing %q:\n%q", want, nav)
		}
	}
	for _, notWant := range []string{"↵ split", "esc close"} {
		if strings.Contains(nav, notWant) {
			t.Errorf("the navigation line carries the action %q, so the split says nothing:\n%q", notWant, nav)
		}
	}
	for _, want := range []string{"↵ split", "esc close"} {
		if !strings.Contains(actions, want) {
			t.Errorf("the action line is missing %q:\n%q", want, actions)
		}
	}
	if strings.Contains(actions, "↑↓ select") {
		t.Errorf("the action line carries a navigation hint:\n%q", actions)
	}
}

// TestTheActionLineIsCentredUnderTheList covers the placement the dialog gives
// its primary action: centred under the list rather than left-aligned with
// everything else, which is what makes it read as the footer's one row of
// buttons instead of a third column of hints.
//
// Both directions, because the fallback is the interesting one. Centring in a
// pane too narrow to hold the line would compute a negative pad; the line goes
// to the frame's own indent there, not to column zero and not off the left
// edge.
func TestTheActionLineIsCentredUnderTheList(t *testing.T) {
	const wide = 90
	_, actions, _ := footerLines(t, renderSized(boxedModel(), wide, 20))
	lead, body := indentOf(actions), lipgloss.Width(strings.TrimLeft(actions, " "))
	if want := (wide - body) / 2; lead != want {
		t.Errorf("the action line starts at column %d in a %d-column pane, want %d:\n%q", lead, wide, want, actions)
	}
	// The control: at this width centring has to actually move the line. Without
	// it the assertion above is satisfied by a frame indent that happens to land
	// on the centre of some pane nobody is rendering into.
	if lead <= len(frameIndent) {
		t.Errorf("the action line is still at the frame indent in a %d-column pane, so nothing was centred:\n%q", wide, actions)
	}

	_, narrow, _ := footerLines(t, renderSized(boxedModel(), 40, 20))
	if got := indentOf(narrow); got != len(frameIndent) {
		t.Errorf("in a pane too narrow to centre in, the action line starts at column %d, want the frame indent (%d):\n%q", got, len(frameIndent), narrow)
	}
}

// TestTheCursorMarkerSitsInTheRowGutter is the alignment the ▸ channel exists
// for. The cursor row and an unselected row build their left edge in different
// branches — one spends the gutter on the marker, the other on blanks — so
// nothing but an assertion keeps the two from drifting apart, and a list whose
// text shifts a column as the cursor passes over it is the most visible defect
// the frame can have.
func TestTheCursorMarkerSitsInTheRowGutter(t *testing.T) {
	frame := renderSized(boxedModel(), 90, 20)

	var cursor, plain string
	for _, l := range frameLines(frame) {
		p := stripANSI(l)
		switch {
		case strings.Contains(p, "host00"):
			cursor = p
		case strings.Contains(p, "host01"):
			plain = p
		}
	}
	if cursor == "" || plain == "" {
		t.Fatalf("could not find both a cursor row and a plain row:\n%s", stripANSI(frame))
	}
	if !strings.HasPrefix(cursor, frameIndent+"▸ ") {
		t.Errorf("the cursor marker is not in the gutter at the frame indent:\n%q", cursor)
	}
	if got, want := columnOf(cursor, "host00"), columnOf(plain, "host01"); got != want {
		t.Errorf("the cursor row starts its alias at column %d and a plain row at %d, so the list shifts as the cursor moves:\n%q\n%q", got, want, cursor, plain)
	}
}

// columnOf is the display column sub starts at in line, or -1.
//
// Display columns rather than strings.Index's byte offset, which is what this
// first compared: the ▸ on the cursor row is one column wide and three bytes
// wide, so the byte offsets differ by two on rows that line up perfectly on
// screen, and the assertion reported a drift that was not there.
func columnOf(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return lipgloss.Width(line[:i])
}

// indentOf is how many leading blank columns a stripped line has.
func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// fgParams is the SGR parameter run a bare foreground of c renders to, e.g.
// "38;2;137;180;250".
//
// Comparing against this rather than against a whole escape sequence, because
// lipgloss merges a style's attributes into one: the accent's own sequence
// carries its bold alongside the color, so a line painted accent-but-not-bold
// contains the color run and not the sequence.
func fgParams(t *testing.T, c string) string {
	t.Helper()
	seq := sgrSeq.FindString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("x"))
	if seq == "" {
		t.Skip("lipgloss emitted no color here; the title's color is unobservable")
	}
	return strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
}

// TestFixedChromeCountsWhatTheFrameDraws is the reserve-against-draw pairing
// this package already insists on for the preview and the warnings, applied to
// the title block and the footer. fixedChrome is what visibleRows subtracts, so
// a rule and a blank line that draw more lines than it counts overflow the pane
// by exactly the difference — and an overflowing popup is the failure the
// height budget exists to prevent.
func TestFixedChromeCountsWhatTheFrameDraws(t *testing.T) {
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

// TestTheDialogFrameStillFitsThePane re-runs the overflow guard now that the
// rule and the two blank lines have been added to the frame. The bound is on
// rows the terminal draws, not lines emitted, so an over-wide line that
// soft-wraps is counted as the two rows it costs.
//
// The sweep starts at minFrame because that is where fitting begins to be
// possible: the title block and the footer cost the irreducible frame three
// more lines than it had before, and below that the picker overflows on purpose
// rather than hiding the host list or the keys that dismiss it. minFrame itself
// is in the sweep, so a frame that grew by one more line than the budget counts
// still fails here.
func TestTheDialogFrameStillFitsThePane(t *testing.T) {
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

// TestAnUnsizedFrameStillRulesToItsWidestLine covers the first frame, before
// any WindowSizeMsg. There is no pane width to span then, so the rule falls
// back to the frame's widest line — and a rule that stopped at zero, or ran
// past the content, is visible for that one frame and reads as a crash.
//
// This is why View assembles the body before the header: the fallback width is
// not known until the body exists.
func TestAnUnsizedFrameStillRulesToItsWidestLine(t *testing.T) {
	frame := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "alpha", HostName: "alpha.example", Port: "22"}},
		Theme: theme.Default(),
	}).View().Content

	lines := frameLines(frame)
	if len(lines) <= ruleLine {
		t.Fatalf("the unsized frame has %d lines, too few to hold a rule", len(lines))
	}
	// Inset on both sides, the same as at a known width: the fallback stands in
	// for the pane width, so everything downstream of it has to behave
	// identically or the first frame is laid out differently from the second.
	want := widestLine(frame) - 2*len(frameIndent)
	if got := strings.Count(stripANSI(lines[ruleLine]), "─"); got != want {
		t.Errorf("the unsized rule is %d cells and the widest line inset is %d:\n%s", got, want, stripANSI(frame))
	}
}
