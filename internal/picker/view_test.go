package picker

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// renderRaw sizes the model, then reads the rendered text with its styling
// intact. tea.View is a struct with a Content field — it has no String method.
//
// Height 30 is a pane with room to spare, so every test using this helper sees
// the full maxRows ceiling and none of them are height-sensitive. Tests about
// short panes must use renderAt, which takes the height explicitly.
//
// Two discards here, both discharged by tests rather than left implicit: the
// tea.Cmd from Update (see TestUpdateCmdMatrix, which asserts the resize arm
// returns none, in every model state) and every tea.View field except Content
// (see TestViewLeavesAltScreenAndMouseOff).
func renderRaw(m model) string { return renderSized(m, testWidth, 30) }

// renderAt renders at an explicit terminal height. renderRaw's fixed 30 rows
// cannot express the short-pane case, which is the whole point of sizing.
func renderAt(m model, height int) string { return renderSized(m, testWidth, height) }

// renderSized renders at an explicit pane width and height. renderRaw and
// renderAt both go through it at testWidth, which is the width every test that
// predates the truncation clamp was written against.
//
// The width is a parameter because the clamp is unobservable at a fixed one: no
// fixture here renders a line 90 columns wide, so at testWidth the clamp cuts
// nothing and every existing assertion in this file sees exactly the frame it
// saw before. Width behavior needs a width it can vary.
func renderSized(m model, width, height int) string {
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(model).View().Content
}

// testWidth is the pane width renderRaw and renderAt report. Wide enough that
// nothing they render is truncated — see renderSized.
const testWidth = 90

// lineCount counts the lines the terminal is asked to draw, which is exactly
// len(frameLines) and is defined as such so the two can never disagree.
//
// It was strings.Count(s, "\n"), which was right only while every frame ended
// in a newline: View trims that now, so counting separators undercounted every
// frame by one and did it silently — the tests that caught it were the ones
// asserting an exact total, and the ones asserting "taller pane draws more"
// were off by one in both terms and never noticed.
//
// It counts *logical* lines, which is exactly the blind spot the width clamp
// exists to close: a line wider than the pane counts as one here and draws as
// two on screen. Use screenRows where the assertion is about rows the terminal
// draws rather than lines View emitted.
func lineCount(s string) int { return len(frameLines(s)) }

// screenRows is lineCount corrected for soft wrap: the number of rows a
// terminal that wide actually draws the frame into. A line of w display columns
// in a pane of width columns occupies ceil(w/width) rows, so an over-wide line
// costs height that lineCount cannot see. width <= 0 means unbounded, matching
// View's own no-truncation case.
//
// Display columns, not bytes or runes: the frame is full of escape sequences
// that occupy no columns at all, and len() would count every one of them.
//
// It deliberately does not trim a trailing newline, which frameLines and
// lineCount both do. Those answer "what lines did View compose", and a phantom
// empty element is noise in that answer. This one answers "how tall is this on
// screen", and a frame ending in a newline really does occupy one more row than
// its content — a row nothing in fixedChrome budgets for. Trimming it here made
// every height-invariant test in this file blind to exactly that, which is how
// the frame came to outrun its own budget by one the moment box() stopped
// trimming the newline on View's behalf.
func screenRows(s string, width int) int {
	if s == "" {
		return 0
	}
	rows := 0
	for _, l := range strings.Split(s, "\n") {
		w := lipgloss.Width(l)
		if width <= 0 || w <= width {
			rows++
			continue
		}
		rows += (w + width - 1) / width
	}
	return rows
}

// frameLines splits a rendered frame into the lines the terminal is asked to
// draw.
//
// It does not drop the empty element after the trailing newline, and used to.
// That element was a phantom back when the newline was an accident of how the
// frame was assembled; it is the frame's bottom padding row now, put there on
// purpose and budgeted for by footerRows. Dropping it made lineCount disagree
// with screenRows about the same frame by exactly one row — the discrepancy
// that let the padding row be trimmed away with the whole suite still green.
// The two agree now: lineCount(s) == screenRows(s, 0), by construction.
func frameLines(s string) []string {
	return strings.Split(s, "\n")
}

// manyHosts builds n hosts, more than any row budget under test, so the list is
// always long enough to be truncated.
//
// What this fixture holds constant, and why: every field except Alias and
// HostName is identical, and User/IdentityFile/proxy/source fields are all
// unset. That pins the preview at its two-field minimum — a separator and two
// lines, so the chrome is frameChrome plus three and the arithmetic in these
// assertions stays checkable by hand. Stated as an offset from frameChrome
// rather than as a total, because the total is not a property of this fixture:
// it moved from 5 to 10 when the frame gained its box, and the sentence that
// named the old number went on reading as if it had been verified.
// The height tests are about the row *count*, so the preview's own
// variable height is a confound here — TestChromeLinesTracksThePreviewHeight
// varies it on purpose instead.
//
// Two fields is also a shape sshconfig.Parse cannot produce, since it resolves
// Port and records provenance on every host. That is the confound this fixture
// buys and parsedCorpus pays back: a hand-built Host understates the chrome, so
// tests written only against this fixture measured a preview shorter than any
// real one.
func manyHosts(n int) []sshconfig.Host {
	out := make([]sshconfig.Host, n)
	for i := range out {
		out[i] = sshconfig.Host{
			Alias:    fmt.Sprintf("host%02d", i),
			HostName: fmt.Sprintf("10.0.0.%d", i+1),
			Port:     "22",
		}
	}
	return out
}

// render is renderRaw without the escapes, for assertions about content.
func render(m model) string {
	return stripANSI(renderRaw(m))
}

// requireStyling skips a styling assertion in an environment where lipgloss
// emits no escapes at all. There, every style renders to bare text and the
// highlight is unobservable by construction — a failure would say nothing about
// the code. The content assertions still run.
func requireStyling(t *testing.T, s lipgloss.Style) {
	t.Helper()
	if s.Render("x") == "x" {
		t.Skip("lipgloss emitted no styling here; highlighting is unobservable")
	}
}

func TestViewListsHostsAndQuery(t *testing.T) {
	out := render(typeRunes(newTestModel(), "dev"))
	for _, want := range []string{"dev", "devbox", "nixos-dev"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "alpha") {
		t.Errorf("view still shows a host that does not match %q:\n%s", "dev", out)
	}
}

func TestViewShowsOpenMarker(t *testing.T) {
	m := newModel(Options{
		Hosts:     corpus,
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"nixos-dev": "w5:pB"},
	})
	if out := render(m); !strings.Contains(out, openMarker) {
		t.Errorf("view missing the open-session marker %q:\n%s", openMarker, out)
	}
}

func TestViewMarkersDistinguishOpenUpAndDown(t *testing.T) {
	m := newModel(Options{
		Hosts:     corpus,
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"alpha": "w5:pB"},
	})
	m.probed["nixos-dev"], m.up["nixos-dev"] = true, true
	m.probed["devbox"], m.up["devbox"] = true, false

	out := render(m)
	for _, marker := range []string{openMarker, upMarker, downMarker} {
		if !strings.Contains(out, marker) {
			t.Errorf("view missing marker %q:\n%s", marker, out)
		}
	}
}

func TestViewMarksProxyJumpHostsSkipped(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "jumped", HostName: "10.9.9.9", Port: "22", ProxyJump: "bastion"}},
		Theme: theme.Default(),
	})
	out := render(m)
	if !strings.Contains(out, skipMarker) {
		t.Errorf("view missing the not-probed marker %q:\n%s", skipMarker, out)
	}
	if !strings.Contains(out, "via bastion") {
		t.Errorf("view should show the jump host instead of a direct address:\n%s", out)
	}
}

func TestViewMarksProxyCommandHostsSkipped(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{
			Alias: "command-proxy", HostName: "10.9.9.8", Port: "22",
			ProxyCommand: "ssh gateway -W %h:%p",
		}},
		Theme: theme.Default(),
	})
	out := render(m)
	if !strings.Contains(out, skipMarker) || !strings.Contains(out, "via ProxyCommand") {
		t.Errorf("ProxyCommand host should render as deliberately unprobed:\n%s", out)
	}
}

func TestViewOpenMarkerWinsOverReachability(t *testing.T) {
	m := newModel(Options{
		Hosts:     []sshconfig.Host{{Alias: "alpha", HostName: "10.0.0.1", Port: "22"}},
		Theme:     theme.Default(),
		OpenPanes: map[string]string{"alpha": "w5:pB"},
	})
	m.probed["alpha"], m.up["alpha"] = true, false

	out := render(m)
	if !strings.Contains(out, openMarker) {
		t.Errorf("view missing %q:\n%s", openMarker, out)
	}
	if strings.Contains(out, downMarker) {
		t.Errorf("down marker shadowed the open marker:\n%s", out)
	}
}

func TestViewShowsPreviewForCursorHost(t *testing.T) {
	m := typeRunes(newTestModel(), "nixos")
	out := render(m)
	// "HostName" is the preview's label. Asserting on the hostname alone would
	// pass with no preview at all, because the host row renders the bare
	// hostname in its detail column.
	//
	// Derived from previewLabelWidth rather than spelled out, because this test
	// is about the preview showing the resolved hostname at all. The column
	// itself is TestPreviewLabelsPadToACommonWidth's job, and that test pins it
	// against the spec's literal rather than against this constant.
	want := fmt.Sprintf("%-*s%s", previewLabelWidth, "HostName", "192.0.2.10")
	if !strings.Contains(out, want) {
		t.Errorf("preview missing the resolved hostname (want %q):\n%s", want, out)
	}
}

func TestViewHidesPreviewWhenToggledOff(t *testing.T) {
	m := press(typeRunes(newTestModel(), "nixos"), ctrl('o'))
	// Assert the preview's label is gone, not the hostname: the row keeps
	// rendering the hostname whether the preview is up or not, so asserting the
	// hostname is absent is a test that can never pass.
	if out := render(m); strings.Contains(out, "HostName ") {
		t.Errorf("preview still rendered after toggle:\n%s", out)
	}
}

func TestViewOmitsThePortSuffixWhenPortIsUnset(t *testing.T) {
	// sshconfig.Parse always resolves Port, but Host is an exported struct that
	// anything can build directly. An unset Port must read as "no port shown",
	// not as a bare ":" hanging off the hostname.
	m := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "bare", HostName: "10.0.0.5"}},
		Theme: theme.Default(),
	})
	if out := render(m); strings.Contains(out, "10.0.0.5:") {
		t.Errorf("rendered a bare port separator:\n%s", out)
	}
}

func TestViewEmptyStateExplainsItself(t *testing.T) {
	out := render(typeRunes(newTestModel(), "zzzz"))
	if !strings.Contains(out, "no hosts match") {
		t.Errorf("view missing an empty-state message:\n%s", out)
	}
}

func TestViewNoConfigStateExplainsItself(t *testing.T) {
	out := render(newModel(Options{Theme: theme.Default()}))
	if !strings.Contains(out, "no ~/.ssh/config — nothing to pick") {
		t.Errorf("view missing a no-config message:\n%s", out)
	}
}

func TestViewShowsSourceProvenance(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{
			Alias: "inc", HostName: "10.0.0.1", Port: "22",
			SourceFile: "/Users/operator/.config/colima/ssh_config", SourceLine: 4,
		}},
		Theme:       theme.Default(),
		ShowPreview: true,
	})
	want := fmt.Sprintf("%-*s%s", previewLabelWidth, "source", "/Users/operator/.config/colima/ssh_config:4")
	if out := render(m); !strings.Contains(out, want) {
		t.Errorf("preview missing source provenance (want %q):\n%s", want, out)
	}
}

// The preview's two-column alignment is a spec requirement, not presentation:
// "labels pad to a common width so the values form a single scannable edge.
// That is the whole reason the panel exists." It shipped ragged — every line
// was label plus one space — and survived to the final whole-feature review
// because every existing assertion was a strings.Contains on a single line,
// which passes just as well on a ragged column as on an aligned one.
//
// So this asserts the edge as a property: every value starts at the same
// column. It also pins that column to the literal 14 from the spec's own
// example block, deliberately NOT to previewLabelWidth — comparing the
// constant to itself would pass at any width, including one that no longer
// matched the spec, and comparing the lines only to each other would pass on
// any consistent width. The fixture sets every field so the widest label is in
// play; a common width proves nothing if the longest label never has to fit it.
func TestPreviewLabelsPadToACommonWidth(t *testing.T) {
	const specColumn = 14 // IdentityFile (12) + 2, per the spec's example block

	lines := previewFields(sshconfig.Host{
		Alias: "all", HostName: "10.0.0.1", Port: "2222", User: "root",
		IdentityFile: "/keys/id_ed25519", ProxyJump: "bastion",
		ProxyCommand: "ssh gateway -W %h:%p",
		SourceFile:   "/cfg/config", SourceLine: 41,
	})
	want := []struct{ label, value string }{
		{"HostName", "10.0.0.1"},
		{"Port", "2222"},
		{"User", "root"},
		{"IdentityFile", "/keys/id_ed25519"},
		{"ProxyJump", "bastion"},
		{"ProxyCommand", "ssh gateway -W %h:%p"},
		{"source", "/cfg/config:41"},
	}
	if len(lines) != len(want) {
		t.Fatalf("previewFields returned %d lines, want %d: %q", len(lines), len(want), lines)
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w.label) {
			t.Fatalf("line %d = %q, want it to start with the label %q", i, lines[i], w.label)
		}
		if at := strings.Index(lines[i], w.value); at != specColumn {
			t.Errorf("line %d %q: value starts at column %d, want %d — labels must pad to a common width",
				i, lines[i], at, specColumn)
		}
	}
}

// columnHosts builds hosts whose aliases differ in width and whose hostnames
// share no substring with any alias, so a plain-text index of the hostname is
// unambiguously where the detail column starts.
func columnHosts(aliases ...string) []sshconfig.Host {
	names := []string{"one.invalid", "two.invalid", "six.invalid", "ten.invalid", "own.invalid"}
	hosts := make([]sshconfig.Host, 0, len(aliases))
	for i, a := range aliases {
		hosts = append(hosts, sshconfig.Host{Alias: a, HostName: names[i%len(names)], Port: "22"})
	}
	return hosts
}

// detailColumns is the display column each host row's detail half starts at,
// keyed by alias.
//
// Rows are located by hostname rather than by alias: columnHosts gives every
// host a unique one, whereas a short alias is a substring of longer aliases and
// of most text on the frame, and differing alias widths is the whole point of
// the fixture.
func detailColumns(t *testing.T, frame string, hosts []sshconfig.Host) map[string]int {
	t.Helper()
	at := make(map[string]int, len(hosts))
	for _, h := range hosts {
		for _, l := range frameLines(frame) {
			p := stripANSI(l)
			i := strings.Index(p, h.HostName)
			if i < 0 {
				continue
			}
			if !strings.Contains(p[:i], h.Alias) {
				t.Fatalf("row %q holds hostname %q but not its alias %q", p, h.HostName, h.Alias)
			}
			at[h.Alias] = lipgloss.Width(p[:i])
			break
		}
	}
	if len(at) != len(hosts) {
		t.Fatalf("found %d of %d host rows in:\n%s", len(at), len(hosts), stripANSI(frame))
	}
	return at
}

// TestHostRowsPadToACommonDetailColumn is the list's half of the argument
// TestPreviewLabelsPadToACommonWidth makes about the preview. The detail column
// shipped ragged — every row was alias plus two spaces — so the hostnames
// stepped in and out as the operator scanned, which is the one thing scanning a
// list is for.
//
// The fixture's aliases differ in width on purpose: a common column proves
// nothing if every alias is already the same length, which is exactly why the
// existing manyHosts fixture (host00..hostNN) could never have caught this.
func TestHostRowsPadToACommonDetailColumn(t *testing.T) {
	hosts := columnHosts("aa", "bbbb", "cccccccccc")
	m := newModel(Options{Hosts: hosts, Theme: theme.Default()})
	at := detailColumns(t, renderSized(m, 80, 20), hosts)

	// The widest alias is 10, plus the "  ▸ " gutter and the two-space gap.
	want := at["cccccccccc"]
	for _, h := range hosts {
		if at[h.Alias] != want {
			t.Errorf("alias %q puts its detail at column %d, want %d — the detail column is ragged:\n%s",
				h.Alias, at[h.Alias], want, stripANSI(renderSized(m, 80, 20)))
		}
	}
}

// TestAnOverlongAliasDoesNotPushEveryOtherRow covers the cap. Without it a
// single pathological alias costs every row in the list the width of it, and
// the detail column is the part with the addresses in it.
//
// The long alias goes ragged alone: it still gets its two-space gutter, and it
// is the only row past the shared column.
func TestAnOverlongAliasDoesNotPushEveryOtherRow(t *testing.T) {
	long := strings.Repeat("x", maxAliasColumn+12)
	hosts := columnHosts("aa", "bbbb", long)
	m := newModel(Options{Hosts: hosts, Theme: theme.Default()})
	at := detailColumns(t, renderSized(m, 120, 20), hosts)

	if at["aa"] != at["bbbb"] {
		t.Errorf("the short rows disagree on the detail column: %d vs %d", at["aa"], at["bbbb"])
	}
	// Capped, so the shared column is set by maxAliasColumn and not by the long
	// alias. Spelled as maxAliasColumn+2 relative to the "a" row's own alias
	// start, which is what makes this an assertion about the cap rather than
	// about whatever the rows happen to agree on.
	gutter := at["aa"] - (maxAliasColumn + 2)
	if gutter < 0 {
		t.Fatalf("the short rows' detail column is %d, which is inside the %d-column cap", at["aa"], maxAliasColumn)
	}
	if want := gutter + len(long) + 2; at[long] != want {
		t.Errorf("the overlong alias puts its detail at column %d, want %d — it should go ragged with a two-space gutter",
			at[long], want)
	}
}

// warnCap is the number of warnings the footer lists before it stops listing
// them and counts the rest.
//
// Spelled as a literal here rather than read from maxWarnings, for the reason
// TestPreviewLabelsPadToACommonWidth spells its column out: a test that compares
// the implementation's constant to itself passes at every value it could ever
// hold, including one that leaves no room for the host list the picker exists
// to draw.
const warnCap = 3

// longWarnings builds n warnings in the shape the loaders really produce — a
// source position, what failed, and what was done about it.
//
// Long on purpose. The shortest of these is past 40 columns, so a pane of any
// ordinary width truncates it: the width sweeps need a footer line with
// something to cut, and a fixture of short warnings would make every assertion
// about truncation vacuous.
func longWarnings(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("/cfg/alpha/ssh_config:%d: include unreadable: "+
			"/cfg/alpha/fragment_%02d.conf — skipping the hosts it defined", i+1, i)
	}
	return out
}

// oneWarnedHost is a single-host fixture for the footer tests. One host so the
// list can never overflow, which leaves "…" unambiguous: any ellipsis in the
// frame is the footer's own overflow notice and not the host list's.
func oneWarnedHost(warnings []string) model {
	return newModel(Options{
		Hosts:    []sshconfig.Host{{Alias: "a", HostName: "10.0.0.1", Port: "22"}},
		Theme:    theme.Default(),
		Warnings: warnings,
	})
}

// TestViewFooterShowsTheWarningTextNotACount is the defect. The footer rendered
// "%d config warning(s)" — enough to tell the operator something is wrong and
// nothing about what, so the only way to act on it was to re-run the plugin from
// a shell and read stderr, which for the picker verb prints into a pane nobody
// reads.
//
// The text was never missing. main.go builds it and hands it over in
// Options.Warnings; the footer discarded it at the last step.
//
// Width 200 so the clamp cuts nothing. Truncation is its own concern, and a
// containment assertion against a truncated line would fail for the wrong
// reason.
func TestViewFooterShowsTheWarningTextNotACount(t *testing.T) {
	warn := longWarnings(1)
	out := stripANSI(renderSized(oneWarnedHost(warn), 200, 30))
	if !strings.Contains(out, warn[0]) {
		t.Errorf("the footer does not carry the warning text %q:\n%s", warn[0], out)
	}
	if strings.Contains(out, "config warning") {
		t.Errorf("the footer still renders a count instead of the text:\n%s", out)
	}
}

// TestViewFooterShowsEveryWarningUpToTheCap is the other half of the cap. The
// overflow test below is satisfied by a footer that lists one warning and counts
// everything else, which would be a worse bug than the count it replaced.
func TestViewFooterShowsEveryWarningUpToTheCap(t *testing.T) {
	warn := longWarnings(warnCap)
	out := stripANSI(renderSized(oneWarnedHost(warn), 200, 30))
	for i, w := range warn {
		if !strings.Contains(out, w) {
			t.Errorf("warning %d of %d is inside the cap but missing (%q):\n%s", i, len(warn), w, out)
		}
	}
	// Nothing was dropped, so there is nothing to count. The fixture has one
	// host, so the host list cannot be the source of an ellipsis here.
	if strings.Contains(out, "…") {
		t.Errorf("the footer counted an overflow it does not have:\n%s", out)
	}
}

// TestViewFooterCountsTheWarningsItCouldNotShow caps the block. The warning
// count is unbounded — loadHosts emits one per unreadable include — and the
// footer is chrome that never yields, so an uncapped list pushes the host list
// off the bottom of a short pane one broken include at a time.
//
// This is the host list's own "… N off screen" notice applied to the footer,
// rather than a second overflow idiom. "more" is accurate here where it was
// wrong there: the footer list never scrolls, so the warnings it dropped are
// always the ones after the ones it drew.
func TestViewFooterCountsTheWarningsItCouldNotShow(t *testing.T) {
	const dropped = 4
	warn := longWarnings(warnCap + dropped)
	out := stripANSI(renderSized(oneWarnedHost(warn), 200, 30))
	for i, w := range warn[:warnCap] {
		if !strings.Contains(out, w) {
			t.Errorf("warning %d is inside the cap but was not drawn:\n%s", i, out)
		}
	}
	for i, w := range warn[warnCap:] {
		if strings.Contains(out, w) {
			t.Errorf("warning %d is past the cap and should have been counted, not drawn:\n%s", warnCap+i, out)
		}
	}
	if want := fmt.Sprintf("… %d more", dropped); !strings.Contains(out, want) {
		t.Errorf("the footer is missing %q:\n%s", want, out)
	}
}

// TestViewFooterIsAbsentWithoutWarnings pins the zero case in both directions.
// fixedChrome reserves the block only when there is one, so anything the footer
// draws on an empty slice — a blank line, a "0 warnings" — costs a host row and
// has nothing reserved for it.
//
// The exact line count, not just the absence of a warning: a blank line at the
// bottom is invisible to every content assertion in this file.
func TestViewFooterIsAbsentWithoutWarnings(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(3), Theme: theme.Default()})
	out := stripANSI(renderSized(m, 200, 30))
	lines := frameLines(renderSized(m, 200, 30))
	// The frame draws no bottom border, so below the hints there is exactly the
	// blank padding row and nothing else. Both are asserted: the padding row is
	// deliberate and footerRows budgets for it, and a warning block leaking out
	// on an empty slice would land between the two.
	if last := stripANSI(lines[len(lines)-1]); last != "" {
		t.Errorf("the frame's last line is %q, want the blank padding row:\n%s", last, out)
	}
	if hints := stripANSI(lines[len(lines)-2]); !strings.Contains(hints, "esc close") {
		t.Errorf("the line above the padding row is %q, want the key hints; something was drawn below them:\n%s", hints, out)
	}
	if got, want := len(lines), frameChrome+3; got != want {
		t.Errorf("frame is %d lines, want %d — the title block, three hosts, and the footer:\n%s", got, want, out)
	}
}

// TestViewNeverExceedsTheReportedHeightWithWarnings is the height invariant with
// the footer block in play. Turning one count line into N text lines changes the
// line count, so what fixedChrome reserves has to match what the footer emits in
// both directions — over-reserving wastes rows the host list should have had,
// and under-reserving overflows the pane.
//
// Counted in screen rows rather than logical lines, and swept at a width the
// warnings do not fit: a footer line wider than the pane wraps into rows no
// newline arithmetic can see, which is the failure commit 58c9762 closed.
func TestViewNeverExceedsTheReportedHeightWithWarnings(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name  string
		warn  []string
		floor int
	}{
		// The floor is the frame that cannot shrink: frameChrome, the warning
		// block, one host row, and the list's overflow notice. The preview yields
		// all the way to nothing; the warning block does not.
		{"one warning", longWarnings(1), frameChrome + 1 + 1 + 1},
		{"the cap exactly", longWarnings(warnCap), frameChrome + warnCap + 1 + 1},
		{"past the cap, so the notice as well", longWarnings(warnCap + 3), frameChrome + warnCap + 1 + 1 + 1},
	} {
		m := newModel(Options{Hosts: wideHosts(30), Theme: th, ShowPreview: true, Warnings: tc.warn})
		for _, w := range []int{40, 200} {
			// The guard that keeps the narrow sweep from asserting nothing: a
			// warning that already fits cannot wrap, and screenRows would agree
			// with lineCount for the whole pass.
			if w == 40 && lipgloss.Width("  "+tc.warn[0]) <= w {
				t.Fatalf("%s: the warning is %d columns and fits a %d-column pane; nothing here can wrap",
					tc.name, lipgloss.Width("  "+tc.warn[0]), w)
			}
			for h := 1; h <= sweepHeight; h++ {
				want := h
				if want < tc.floor {
					want = tc.floor
				}
				if got := screenRows(renderSized(m, w, h), w); got > want {
					t.Errorf("%s: pane %dx%d drew %d screen rows, want <= %d:\n%s",
						tc.name, w, h, got, want, stripANSI(renderSized(m, w, h)))
				}
			}
		}
	}
}

// TestViewFillsThePaneWithTheWarningBlockPresent is the other direction of the
// same arithmetic. Every assertion above is satisfied by a footer that reserves
// four lines and draws one: the frame fits, and the rows the host list should
// have had are simply never drawn. A reserve is only right when it equals what
// is emitted, so this pins equality rather than a bound.
//
// The preview is off and the sweep stops before the maxRows ceiling binds,
// which are the two ways a frame is allowed to come in under the pane height.
// Inside that band the frame is the pane.
func TestViewFillsThePaneWithTheWarningBlockPresent(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name string
		warn []string
		// lines is the footer block's height: one per warning, plus the notice
		// once there are more of them than the cap.
		lines int
	}{
		{"no warnings", nil, 0},
		{"one warning", longWarnings(1), 1},
		{"the cap exactly", longWarnings(warnCap), warnCap},
		{"past the cap", longWarnings(warnCap + 3), warnCap + 1},
	} {
		m := newModel(Options{Hosts: manyHosts(30), Theme: th, Warnings: tc.warn})
		// frameChrome, the warning block, one host row, and the list's overflow
		// notice: the frame that cannot shrink.
		floor := frameChrome + tc.lines + 1 + 1
		// Past this the 12-row ceiling binds and the frame stops growing with
		// the pane, so falling short there is the ceiling working rather than a
		// row the footer took and did not use.
		ceiling := frameChrome + tc.lines + 12 + 1
		for h := floor; h <= ceiling; h++ {
			if got := screenRows(renderSized(m, 200, h), 200); got != h {
				t.Errorf("%s: a %d-line pane drew %d rows; the pane is not being filled:\n%s",
					tc.name, h, got, stripANSI(renderSized(m, 200, h)))
			}
		}
	}
}

// TestViewFlattensAWarningThatCarriesItsOwnNewline covers the shape the footer
// cannot budget for. Warnings arrive as opaque strings built elsewhere, and one
// of them is multi-line today: pluginconfig returns errors.Join when more than
// one key is rejected, and errors.Join's Error() separates them with a newline.
//
// A newline inside a warning draws two rows where the reserve counted one, and
// clampToWidth cannot save it the way it saves an over-wide line: by the time
// the frame is assembled, an embedded newline is indistinguishable from a line
// View meant to emit.
func TestViewFlattensAWarningThatCarriesItsOwnNewline(t *testing.T) {
	warn := []string{
		`invalid plugin config: split_direction must be "right" or "down", got "sideways"` + "\n" +
			`invalid plugin config: probe_timeout_ms must be positive, got -1 — ignoring the rejected keys`,
	}
	if !strings.Contains(warn[0], "\n") {
		t.Fatal("the fixture lost its newline; there is nothing here to flatten")
	}
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default(), Warnings: warn})

	// One warning is one reserved line whatever it happens to contain.
	if got, want := m.fixedChrome(), frameChrome+1; got != want {
		t.Errorf("fixedChrome() = %d, want %d", got, want)
	}
	// Width 400 so nothing here is truncated: the failure under test is a row
	// the frame gained, and a clamp would hide it by cutting the line instead.
	for h := 1; h <= sweepHeight; h++ {
		want := h
		// frameChrome + the warning + one host row + the notice.
		if floor := frameChrome + 1 + 1 + 1; want < floor {
			want = floor
		}
		if got := screenRows(renderSized(m, 400, h), 400); got > want {
			t.Errorf("height %d drew %d screen rows, want <= %d:\n%s",
				h, got, want, stripANSI(renderSized(m, 400, h)))
		}
	}

	// Flattened rather than cut: both halves stay on the one line the footer
	// reserved. Dropping everything after the newline would satisfy the row
	// count above and lose the second rejected key silently.
	out := stripANSI(renderSized(m, 400, 30))
	var line string
	for _, l := range frameLines(renderSized(m, 400, 30)) {
		if strings.Contains(stripANSI(l), "split_direction") {
			line = stripANSI(l)
		}
	}
	if line == "" {
		t.Fatalf("no footer line carries the warning:\n%s", out)
	}
	if !strings.Contains(line, "probe_timeout_ms") {
		t.Errorf("the second half of the warning did not survive the flattening: %q", line)
	}
}

func TestViewHighlightsMatchedRunes(t *testing.T) {
	// These assertions build the expected styles with lipgloss rather than
	// hardcoding escape bytes, so they test the behavior and not the encoding.
	// View() builds its accent style with Bold(true); match that here.
	th := theme.Default()
	raw := renderRaw(typeRunes(newModel(Options{Hosts: corpus, Theme: th}), "dev"))

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(th.Accent)).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(th.Text))
	requireStyling(t, accent)

	// devbox matched on its first three runes and is not the cursor row, so
	// "dev" carries the accent while "box" keeps the plain text color.
	if want := accent.Render("dev"); !strings.Contains(raw, want) {
		t.Errorf("matched runes are not accented; missing %q in:\n%q", want, raw)
	}
	if want := text.Render("box"); !strings.Contains(raw, want) {
		t.Errorf("unmatched runes lost the text color; missing %q in:\n%q", want, raw)
	}
	if bare := accent.Render("devbox"); strings.Contains(raw, bare) {
		t.Errorf("the whole alias was accented, so the highlight distinguishes nothing:\n%q", raw)
	}
}

// bandStyles is the palette the cursor row renders with: the band itself, and
// the shape a query match takes on top of it.
//
// Built here rather than read from newStyles, so these assertions test the
// behavior and not the encoding — the same reason the accent style is rebuilt
// in every other styling test in this file. A helper that asked the production
// code what it drew would agree with any change to it.
func bandStyles(th theme.Theme) (band, hit lipgloss.Style) {
	band = lipgloss.NewStyle().Foreground(lipgloss.Color(th.Accent)).Reverse(true)
	return band, band.Underline(true)
}

func TestViewHighlightsHostNameOnAHostNameMatch(t *testing.T) {
	// prod-web matches only through its HostName "dev.example.com". Marking the
	// alias would point the operator at the column that did not match.
	//
	// It is also the only match, so it is the cursor row, and the highlight
	// there is an underline on the band rather than the accent — accent on an
	// accent band would erase it. The column the highlight lands on is what this
	// test is about and that is unchanged; only which style carries it moved.
	th := theme.Default()
	raw := renderRaw(typeRunes(newModel(Options{Hosts: corpus, Theme: th}), "example"))

	band, hit := bandStyles(th)
	requireStyling(t, band)

	if want := hit.Render("example"); !strings.Contains(raw, want) {
		t.Errorf("the hostname match is not highlighted; missing %q in:\n%q", want, raw)
	}
	if bad := hit.Render("prod-web"); strings.Contains(raw, bad) {
		t.Errorf("the alias was highlighted for a hostname-only match:\n%q", raw)
	}
}

func TestViewShowsKeyHints(t *testing.T) {
	out := render(newTestModel())
	// "↵" rather than "enter", matching herdr's own dialogs — the glyph is the
	// key, and the columns it saves are what let the chip's padding fit without
	// the line truncating any sooner than it did.
	for _, hint := range []string{"↵", "^t", "^z", "^u"} {
		if !strings.Contains(out, hint) {
			t.Errorf("view missing the %q hint:\n%s", hint, out)
		}
	}
}

// stripANSI removes styling so assertions test content, not colors.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// tallPreviewHosts is manyHosts with every optional field set on the host the
// cursor starts on, so the preview renders its maximum six fields instead of
// the minimum two. Only host00 is populated: the preview shows the cursor host,
// so it is the only one whose fields affect the chrome.
func tallPreviewHosts(n int) []sshconfig.Host {
	hosts := manyHosts(n)
	hosts[0].User = "root"
	hosts[0].IdentityFile = "/keys/id_ed25519"
	hosts[0].ProxyJump = "bastion"
	hosts[0].SourceFile = "/Users/operator/.ssh/config"
	hosts[0].SourceLine = 12
	return hosts
}

// TestChromeLinesTracksThePreviewHeight pins the chrome arithmetic directly.
// The end-to-end fit tests below would pass with a hardcoded constant in any
// terminal tall enough for the ceiling to bind instead, so the computation
// needs its own assertion. The numbers are hand-checkable: frameChrome, plus
// one line per extra element.
//
// The extras are written out as sums rather than folded into a total, because
// what this test is for is that chromeLines adds up the same terms View draws —
// a total would just be the answer copied out of the code.
func TestChromeLinesTracksThePreviewHeight(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name string
		opts Options
		want int
	}{
		{"preview off", Options{Hosts: manyHosts(3), Theme: th}, frameChrome},
		// A separator plus HostName and Port, the two fields manyHosts sets.
		{"minimal preview", Options{Hosts: manyHosts(3), Theme: th, ShowPreview: true}, frameChrome + 1 + 2},
		// A separator plus all six.
		{"six-field preview", Options{Hosts: tallPreviewHosts(3), Theme: th, ShowPreview: true}, frameChrome + 1 + 6},
		{
			"warning line",
			Options{Hosts: manyHosts(3), Theme: th, Warnings: []string{"config:3: bad"}},
			frameChrome + 1,
		},
	} {
		if got := newModel(tc.opts).chromeLines(); got != tc.want {
			t.Errorf("%s: chromeLines() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// parsedCorpus writes a config and runs it through sshconfig.Parse, so at least
// one fixture here has the shape a real config produces rather than the shape a
// convenient literal has. Parse resolves Port and sets SourceFile on every host,
// so a parsed preview never has fewer than three fields; manyHosts has two.
// That gap is not cosmetic — it understated the chrome by enough to hide an
// overflow at height 9 from both the fix and its review, because every fixture
// in the package agreed with the code about a preview height no real config ever
// renders.
//
// Odd-numbered hosts set User and IdentityFile, so field counts alternate 3, 5,
// 3, 5 down the list. That variation is the fixture's whole point: the chrome
// depends on the *cursor* host's field count, so a fixture where every host had
// the same number of fields could not distinguish a height that fits one row
// from a height that fits the next.
//
// Warnings are asserted empty rather than discarded: a warning would add a
// fixedChrome line and silently shift every number computed from this fixture.
func parsedCorpus(t *testing.T, n int) []sshconfig.Host {
	t.Helper()
	var cfg strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&cfg, "Host host%02d\n  HostName 10.0.0.%d\n", i, i+1)
		if i%2 == 1 {
			cfg.WriteString("  User root\n  IdentityFile /keys/id_ed25519\n")
		}
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(cfg.String()), 0o600); err != nil {
		t.Fatalf("writing the fixture config: %v", err)
	}
	hosts, warnings, err := sshconfig.Parse(path)
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}
	if len(warnings) > 0 {
		t.Fatalf("fixture config produced warnings %v; they would change the chrome", warnings)
	}
	if len(hosts) != n {
		t.Fatalf("Parse returned %d hosts, want %d", len(hosts), n)
	}
	if a, b := len(previewFields(hosts[0])), len(previewFields(hosts[1])); a == b {
		t.Fatalf("hosts 0 and 1 both have %d preview fields; "+
			"this fixture exists to vary that", a)
	}
	return hosts
}

// frameChrome is what View draws at every height before any warnings: the
// title block's three lines — title, rule, blank — and the footer's two. It is
// fixedChrome with no warnings, spelled out from the same constants rather than
// written down as a number, because every floor and every discriminating height
// in this file is derived from it and they all have to move together when the
// frame changes shape. They did not, once: the frame gained five lines and
// nineteen tests here failed at the same time, which is what these constants
// exist to stop happening twice.
const frameChrome = headerRows + footerRows

// minFrame is the frame that cannot shrink: frameChrome, one host row, and the
// overflow notice. The preview yields all the way to nothing, but those lines
// have nowhere left to go, so below this height the frame stops shrinking and
// stays put rather than growing. Showing the operator zero hosts, hiding the
// keys that dismiss the picker would both be worse than a line of overflow in
// a pane this small.
//
// It has no warning term because every case below has more than one host and no
// warnings. A fixture with warnings needs warningLines() on top.
const minFrame = frameChrome + 1 + 1

// sweepHeight is the top of every height sweep in this file: twenty rows above
// the floor, which is the span they covered when the floor was four. A fixed 24
// would now spend a third of each sweep on heights that all clamp to minFrame
// and assert the same frame over and over.
const sweepHeight = minFrame + 20

// TestViewNeverExceedsTheReportedHeight is the invariant, swept rather than
// sampled: at no height does the frame render more lines than the pane has. The
// sweep matters more than any single case, because the boundary is not a fixed
// height — it is chrome + 2, and the chrome varies with the cursor host. Sampled
// heights chosen from one fixture's chrome therefore step straight over the
// overflow in another's, which is how the last round of this test passed while
// a real config overflowed.
//
// The parsed case is the one that would have caught that. It is here so the
// sweep is anchored to a config Parse actually produced, not to three literals
// that happen to agree with the code.
func TestViewNeverExceedsTheReportedHeight(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name    string
		hosts   []sshconfig.Host
		preview bool
	}{
		{"preview off", manyHosts(30), false},
		{"minimal preview", manyHosts(30), true},
		{"six-field preview", tallPreviewHosts(30), true},
		{"parsed config", parsedCorpus(t, 30), true},
	} {
		m := newModel(Options{Hosts: tc.hosts, Theme: th, ShowPreview: tc.preview})
		for h := 1; h <= sweepHeight; h++ {
			want := h
			if want < minFrame {
				want = minFrame
			}
			if got := lineCount(renderAt(m, h)); got > want {
				t.Errorf("%s: height %d rendered %d lines, want <= %d:\n%s",
					tc.name, h, got, want, stripANSI(renderAt(m, h)))
			}
		}
	}
}

// TestViewFitsAtEveryCursorPositionInAShortPane holds the pane height fixed and
// walks the cursor, which is the case a height sweep cannot reach: the chrome is
// a function of the cursor host, so one height is simultaneously fitting and
// overflowing depending on which row the operator has arrowed to.
//
// frameChrome+7 is the discriminating value for this fixture. Before the
// preview learned to yield, a pane that tall rendered exactly its height on
// host00 (3 preview fields) and one line more on host01 (5), so the frame
// overflowed on every other keypress while scrolling and was not reproducible
// from the pane size alone. Both are in the sweep below; the neighbouring
// heights are there so a fix that merely special-cased the one would still
// fail.
//
// Written as offsets from frameChrome rather than as the heights they came to,
// because what makes a height discriminating is how much room is left after the
// chrome — the box moved every one of them by five.
func TestViewFitsAtEveryCursorPositionInAShortPane(t *testing.T) {
	hosts := parsedCorpus(t, 12)
	for _, h := range []int{frameChrome + 5, frameChrome + 6, frameChrome + 7, frameChrome + 8, frameChrome + 10} {
		m := newModel(Options{Hosts: hosts, Theme: theme.Default(), ShowPreview: true})
		for i := range hosts {
			cursor := m.view[m.cursor].Host
			if got := lineCount(renderAt(m, h)); got > h {
				t.Errorf("height %d, cursor at row %d on %s (%d preview fields): "+
					"rendered %d lines, overflows by %d:\n%s",
					h, i, cursor.Alias, len(previewFields(cursor)), got, got-h,
					stripANSI(renderAt(m, h)))
			}
			m = press(m, ctrl('j'))
		}
	}
}

// TestViewPreviewShedsFieldsBeforeItDisappears pins the shape of the yield, not
// just the fit. A clamp that truncated the finished frame would satisfy the
// height invariant too — but it would cut from the bottom, taking the key hints
// and leaving the preview whole. These assertions distinguish the two: the
// preview must lose its own trailing fields first, in declaration order, so
// provenance goes before the hostname and the hints never go at all.
func TestViewPreviewShedsFieldsBeforeItDisappears(t *testing.T) {
	m := newModel(Options{Hosts: parsedCorpus(t, 30), Theme: theme.Default(), ShowPreview: true})
	if n := len(previewFields(m.view[m.cursor].Host)); n != 3 {
		t.Fatalf("cursor host has %d preview fields, want 3; "+
			"the heights below are computed from that", n)
	}
	// shedBase is the height at which the preview's budget is zero: frameChrome,
	// one host row and the overflow notice, with nothing left over. Each case
	// below adds the lines it expects the preview to get, so the heights say
	// what they are testing instead of being four numbers that moved when the
	// box did.
	const shedBase = frameChrome + 1 + 1
	for _, tc := range []struct {
		height  int
		present []string
		absent  []string
	}{
		{shedBase + 4, []string{"HostName ", "Port ", "source "}, nil},
		{shedBase + 3, []string{"HostName ", "Port "}, []string{"source "}},
		{shedBase + 2, []string{"HostName "}, []string{"Port ", "source "}},
		// One line short of a separator plus a field, so the block goes rather
		// than drawing a divider with nothing under it. The host row stays.
		{shedBase + 1, []string{"host00", "esc close"}, []string{"HostName ", previewSeparator}},
	} {
		out := stripANSI(renderAt(m, tc.height))
		for _, want := range tc.present {
			if !strings.Contains(out, want) {
				t.Errorf("height %d: %q missing:\n%s", tc.height, want, out)
			}
		}
		for _, unwanted := range tc.absent {
			if strings.Contains(out, unwanted) {
				t.Errorf("height %d: %q should have been shed:\n%s", tc.height, unwanted, out)
			}
		}
	}
}

// TestViewSpendsTheYieldedPreviewLinesOnHostRows is the other half of the yield,
// the way TestViewShowsMoreRowsInATallerPane is the other half of the fit. Every
// assertion above is satisfied by a preview that gives its lines up to nothing:
// the frame would fit, and the preview would be correctly absent, while the pane
// sat one row short of full. The lines have to actually reach the host list.
//
// frameChrome+3 is the shortest pane where the preview is gone and there is
// still slack to spend: the chrome plus the notice leaves two rows, so host01
// is present exactly when the yielded line was reused rather than dropped.
func TestViewSpendsTheYieldedPreviewLinesOnHostRows(t *testing.T) {
	const height = frameChrome + 3
	m := newModel(Options{Hosts: parsedCorpus(t, 30), Theme: theme.Default(), ShowPreview: true})
	out := stripANSI(renderAt(m, height))
	if got := lineCount(out); got != height {
		t.Errorf("a %d-line pane rendered %d lines; the pane is not being filled:\n%s", height, got, out)
	}
	for _, want := range []string{"host00", "host01"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from a %d-line pane with the preview shed:\n%s", want, height, out)
		}
	}
}

// TestViewShowsMoreRowsInATallerPane is the other half of the fit assertion.
// Fitting alone is satisfied by always rendering a single row, which would be a
// different bug; the output has to actually track the height.
func TestViewShowsMoreRowsInATallerPane(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default(), ShowPreview: true})
	short, tall := lineCount(renderAt(m, 10)), lineCount(renderAt(m, 20))
	if short >= tall {
		t.Errorf("10-line pane rendered %d lines and 20-line pane %d; "+
			"the row count is not tracking the height", short, tall)
	}
}

// TestViewInATallPaneShowsEveryHost is the operator-visible half of removing the
// row ceiling. A 200-line pane holding 30 hosts has room for all of them, so all
// of them are drawn and there is nothing left to put a notice about.
//
// This test used to assert the opposite, and did it as "a tall pane matches the
// unsized first frame" — true only because the ceiling made both frames 12 rows,
// which is the same coincidence that let two thirds of the operator's popup
// render as void. The two facts it yoked together are now separate, and
// TestViewUnsizedFallsBackToAPlausibleList owns the other one.
//
// Asserted through boxedRows so the width is discarded: a tall frame is padded
// out to testWidth and the host count is what this is about.
func TestViewInATallPaneShowsEveryHost(t *testing.T) {
	const hosts = 30
	m := newModel(Options{Hosts: manyHosts(hosts), Theme: theme.Default(), ShowPreview: true})
	tall := renderAt(m, 200)

	drawn := 0
	for _, l := range boxedRows(tall) {
		if strings.Contains(l, "host") {
			drawn++
		}
	}
	if drawn != hosts {
		t.Errorf("a 200-line pane drew %d of %d hosts; the pane is not being filled:\n%s",
			drawn, hosts, stripANSI(tall))
	}
	if strings.Contains(stripANSI(tall), "off screen") {
		t.Errorf("a 200-line pane fits every host but still drew an overflow notice:\n%s", stripANSI(tall))
	}
}

// TestViewUnsizedFallsBackToAPlausibleList covers the first frame, before any
// WindowSizeMsg has reported a height. There is no pane to fill yet, so
// visibleRows draws fallbackRows and the rest of the list overflows — 30 hosts
// less 12 drawn is 18.
//
// The alternative was drawing nothing until the size arrives, which is one
// frame of empty picker on every launch.
func TestViewUnsizedFallsBackToAPlausibleList(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default(), ShowPreview: true})
	unsized := stripANSI(m.View().Content) // no WindowSizeMsg yet, so height is 0
	if !strings.Contains(unsized, "… 18 off screen") {
		t.Errorf("unsized first frame did not draw fallbackRows rows:\n%s", unsized)
	}
}

// boxedRows is a frame's content lines with everything the frame's width
// decides discarded: the rule, the blank lines, and the trailing padding a
// banded row carries out to the right edge.
//
// What is left is the line the picker composed, which is what two frames of
// different widths can be compared on. The styling goes with it — these are
// plain text — so a caller comparing two frames through this is asserting about
// their rows and not about their colors.
func boxedRows(frame string) []string {
	var out []string
	for _, l := range frameLines(frame) {
		p := strings.TrimRight(stripANSI(l), " ")
		if strings.Trim(p, "─ ") == "" {
			continue // the rule, or a blank line: nothing but width
		}
		out = append(out, p)
	}
	return out
}

// TestViewOverflowNoticeCountsHostsInBothDirections is why the notice names no
// direction. The count is len(m.view) - len(rows) — every match the window
// leaves out, including the ones scrolled off above it — so at the bottom of a
// long list every host it counts is behind the operator. "N more" read as "N
// further down", which was ambiguous everywhere the window was not at the top
// and wrong about every host it counted here.
//
// The cursor is walked to the last host rather than the fixture being trimmed,
// because the count is not a property of the list: it is a property of where
// the window sits in it, and only moving the cursor moves the window.
func TestViewOverflowNoticeCountsHostsInBothDirections(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default()})
	for i := 1; i < len(m.view); i++ {
		m = press(m, ctrl('j'))
	}
	if m.cursor != 29 {
		t.Fatalf("cursor = %d, want 29; the window is not at the end of the list", m.cursor)
	}

	// The pane spends frameChrome on the title block and the footer and one more
	// on the notice itself; the rest are host rows, and every host that does not
	// get one is what the notice counts. Derived rather than written down: the
	// number moved when the frame gained its padding rows, and a hardcoded one
	// would have made this test fail for a reason it is not about.
	const height = 30
	hidden := len(m.view) - (height - frameChrome - 1)
	out := stripANSI(renderAt(m, height))
	if want := fmt.Sprintf("… %d off screen", hidden); !strings.Contains(out, want) {
		t.Errorf("notice missing or miscounted, want %q:\n%s", want, out)
	}
	// The two assertions that make the count's meaning observable: the last host
	// is on screen, so nothing is below the window, and the first is not, so
	// every hidden host is above it. A notice reading "N more" would be pointing
	// down at nothing.
	if !strings.Contains(out, "host29") {
		t.Errorf("the cursor host is not on screen, so the window did not follow it:\n%s", out)
	}
	if strings.Contains(out, "host00") {
		t.Errorf("the top of the list is still on screen, so the hidden hosts are not all above:\n%s", out)
	}
}

// TestViewKeepsARowInAPaneTooShortForChrome covers the floor. In a pane this
// short the frame cannot fit, and showing the operator zero hosts would be the
// worse failure, so one row survives.
func TestViewKeepsARowInAPaneTooShortForChrome(t *testing.T) {
	m := newModel(Options{Hosts: manyHosts(30), Theme: theme.Default(), ShowPreview: true})
	if out := stripANSI(renderAt(m, 3)); !strings.Contains(out, "host00") {
		t.Errorf("no host row survived a 3-line pane; the one-row floor is gone:\n%s", out)
	}
}

// wideHosts builds n hosts whose every column overflows a narrow pane: a long
// alias, a long hostname, a user prefix, a non-default port, and every optional
// preview field, so the host row, the detail column and the preview are all
// over-wide at once. host01 sets ProxyJump so the "via" form of the detail
// column is in the frame too — the cursor starts on host00, so the preview's
// height still comes from a host with every field set.
//
// Every name here is synthetic. .invalid is reserved by RFC 2606 and never
// resolves, so nothing in this fixture is a host anything could connect to.
func wideHosts(n int) []sshconfig.Host {
	out := make([]sshconfig.Host, n)
	for i := range out {
		out[i] = sshconfig.Host{
			Alias:        fmt.Sprintf("alpha-%02d-bastion-relay-gateway", i),
			HostName:     fmt.Sprintf("beta%02d.dept.division.example.invalid", i),
			Port:         "2222",
			User:         "operator-service-account",
			IdentityFile: "/keys/alpha/id_ed25519_operator_service_account",
			SourceFile:   "/cfg/alpha/included_fragment_ssh_config",
			SourceLine:   41,
		}
	}
	if n > 1 {
		out[1].ProxyJump = "gamma-jump-host.dept.example.invalid"
	}
	return out
}

// TestWideHostsFixtureRendersEveryKindOfLine guards the width sweeps below
// against passing for the wrong reason. The clamp is one call on the assembled
// frame, so a sweep proves it covers every kind of line only if every kind of
// line is in the frame being swept — a fixture that quietly stopped rendering a
// preview, or a list short enough not to overflow, would sweep a frame with
// nothing at stake and pass.
//
// Width 0 is the unclamped render, so these are the lines as the builders emit
// them, before truncation.
func TestWideHostsFixtureRendersEveryKindOfLine(t *testing.T) {
	m := newModel(Options{
		Hosts: wideHosts(30), Theme: theme.Default(), ShowPreview: true,
		// One past the cap, so the footer contributes both kinds of line it can:
		// a warning and its own overflow notice.
		Warnings: longWarnings(warnCap + 1),
	})
	frame := renderSized(m, 0, 24)
	out := stripANSI(frame)
	for _, want := range []struct{ kind, text string }{
		{"the query header", "ssh ▏"},
		{"a host row", "alpha-00-bastion-relay-gateway"},
		{"the detail column", "operator-service-account@beta00.dept.division.example.invalid:2222"},
		{"a ProxyJump row", "via gamma-jump-host.dept.example.invalid"},
		// The ellipsis rather than the notice's wording: the count's phrasing is
		// a separate change and this assertion is about the line existing.
		{"the overflow notice", "…"},
		{"the preview separator", previewSeparator},
		{"a preview field", "HostName"},
		{"the key hints", "esc close"},
		{"a warning line", "/cfg/alpha/fragment_00.conf"},
		{"the warning overflow notice", "… 1 more"},
	} {
		if !strings.Contains(out, want.text) {
			t.Errorf("%s is missing (%q) from:\n%s", want.kind, want.text, out)
		}
	}
	if got := lipgloss.Width(frame); got <= 60 {
		t.Errorf("the widest line is %d columns; this fixture exists to overflow a narrow pane", got)
	}
}

// TestViewNeverExceedsThePaneWidth is the width invariant: no line the frame
// emits is wider than the pane. It is a height invariant in disguise. The
// picker renders inline rather than in an alternate screen, so an over-wide
// line is not clipped — it wraps, and the frame draws into more screen rows
// than fixedChrome, noticeReserve and previewLines budgeted, none of which can
// see it because all of them count logical lines.
//
// Swept over widths rather than sampled at one, because the line that overflows
// first depends on the width: the key hints are a fixed 77 columns and the host
// rows vary with the alias, so a single narrow width exercises whichever of
// them that width happens to cut. 1 and 2 are in the sweep because every line
// is over-wide there, including the notice and the separator that no realistic
// width touches.
func TestViewNeverExceedsThePaneWidth(t *testing.T) {
	th := theme.Default()
	// Past the cap, so the footer's own overflow notice is swept too, and long
	// enough that the warning lines are over-wide at the realistic widths in the
	// sweep rather than only at 1, 2 and 3.
	warn := longWarnings(warnCap + 1)
	for _, tc := range []struct {
		name string
		m    model
	}{
		{"wide rows, preview and a warning", newModel(Options{
			Hosts: wideHosts(30), Theme: th, ShowPreview: true, Warnings: warn,
		})},
		{"preview off", newModel(Options{Hosts: wideHosts(30), Theme: th})},
		{"a long query in the header", typeRunes(
			newModel(Options{Hosts: wideHosts(30), Theme: th, ShowPreview: true}),
			"alpha-00-bastion-relay-gateway",
		)},
		{"no hosts match", typeRunes(newModel(Options{Hosts: wideHosts(30), Theme: th}), "zzzz")},
		{"no config at all", newModel(Options{Theme: th})},
	} {
		for _, w := range []int{1, 2, 3, 7, 20, 40, 77, 80} {
			for _, h := range []int{4, 9, 24} {
				for i, l := range frameLines(renderSized(tc.m, w, h)) {
					if got := lipgloss.Width(l); got > w {
						t.Errorf("%s: pane %dx%d: line %d is %d columns wide: %q",
							tc.name, w, h, i, got, stripANSI(l))
					}
				}
			}
		}
	}
}

// TestViewDoesNotTruncateBeforeTheFirstWindowSizeMsg pins the behavior the
// m.width <= 0 guard exists for. Width is 0 until the first tea.WindowSizeMsg
// arrives, and an unconditional clamp would draw that frame as nothing:
// truncating to 0 columns keeps a line's escape sequences and drops every
// printable cell, so the picker's first frame would be a box of blank lines
// that still counted as lines.
//
// What it does not pin, stated rather than left to be discovered: deleting the
// guard itself keeps this green. lipgloss applies MaxWidth only when it is > 0,
// so with the current primitive the guarded and unguarded paths render the same
// frame. This asserts the frame, which is the contract; the guard's own comment
// in view.go carries the reason it stays.
func TestViewDoesNotTruncateBeforeTheFirstWindowSizeMsg(t *testing.T) {
	m := newModel(Options{Hosts: wideHosts(30), Theme: theme.Default(), ShowPreview: true})
	if m.width != 0 {
		t.Fatalf("m.width = %d before any WindowSizeMsg, want 0; there is no guard here to pin", m.width)
	}
	frame := m.View().Content
	out := stripANSI(frame)
	for _, want := range []string{"alpha-00-bastion-relay-gateway", "HostName", "esc close"} {
		if !strings.Contains(out, want) {
			t.Errorf("the unsized first frame is missing %q, so it was truncated at width 0:\n%s", want, out)
		}
	}
	// Whole, not merely present: any clamp narrow enough to matter would have
	// cut these lines, and the widest of them is far past one.
	if got := lipgloss.Width(frame); got <= 60 {
		t.Errorf("the unsized frame's widest line is %d columns; something truncated it", got)
	}
}

// sgrSeq matches one complete SGR escape sequence. A truncation that cut one in
// half leaves behind an ESC this cannot match, which is what escapesIntact
// looks for.
var sgrSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// escapesIntact reports whether every ESC in s begins a complete SGR sequence.
// Removing the complete ones must leave no ESC behind; anything left is a
// sequence that was cut, and a terminal reading it swallows the text that
// follows for the rest of the session.
func escapesIntact(s string) bool {
	return !strings.ContainsRune(sgrSeq.ReplaceAllString(s, ""), 0x1b)
}

// TestViewTruncationKeepsEscapeSequencesIntact is the hazard the clamp is built
// around. renderRows assembles its alias and detail strings out of highlight()
// and Style.Render() output, so by the time they reach the frame they carry SGR
// escape sequences: len() is not their display width, and slicing them by byte
// or by rune cuts a sequence in half.
//
// Every plain-text assertion in this file is blind to that failure by
// construction, and stripANSI is why: it skips from an ESC to the next 'm', so
// a half-written sequence merely eats the text after it and the content
// assertions stay green. So this asserts on the styled bytes.
//
// The width is 12: four columns of row chrome plus eight of the alias, which
// lands inside the run of unmatched runes after the highlighted "alpha" — a cut
// through styled text, not between two styled pieces. The frame spends no
// columns on a border, so the pane width and the content width are the same
// number, and it is what clampToWidth cuts to.
func TestViewTruncationKeepsEscapeSequencesIntact(t *testing.T) {
	const width = 12
	th := theme.Default()
	// The cursor row is banded, so its highlight is an underline on the band.
	_, hit := bandStyles(th)
	requireStyling(t, hit)

	m := typeRunes(newModel(Options{Hosts: wideHosts(3), Theme: th, ShowPreview: true}), "alpha")
	for i, l := range frameLines(renderSized(m, width, 24)) {
		if !escapesIntact(l) {
			t.Errorf("line %d was cut inside an escape sequence: %q", i, l)
		}
		if got := lipgloss.Width(l); got > width {
			t.Errorf("line %d is %d columns wide, want <= %d: %q", i, got, width, stripANSI(l))
		}
	}

	row := cursorRow(t, renderSized(m, width, 24))
	// The highlighted run survives the cut whole. A truncation that dropped
	// escapes rather than copying them through would leave the same plain text
	// with no styling on it, which stripANSI cannot tell apart.
	if want := hit.Render("alpha"); !strings.Contains(row, want) {
		t.Errorf("the truncated cursor row lost the highlight on the matched runes; missing %q in %q", want, row)
	}
	// And it does not leave the terminal styled: the escapes past the cut are
	// copied through, so the closing reset is still there. Checked on the line's
	// final sequence — see stylingLeaksPastEOL for why that became the sharp
	// test once the frame stopped drawing a border of its own.
	if stylingLeaksPastEOL(row) {
		t.Errorf("the truncated cursor row leaves the band open past end of line: %q", row)
	}

	// The control. A byte slice of the same row at the same number is what a
	// width-as-len implementation would produce, and it fails the check above —
	// which is what makes these assertions assertions about the hazard rather
	// than about a line that happened to carry no escapes.
	unclamped := cursorRow(t, renderSized(m, 0, 24))
	if len(unclamped) <= width {
		t.Fatalf("the unclamped cursor row is only %d bytes; it cannot demonstrate the cut", len(unclamped))
	}
	if escapesIntact(unclamped[:width]) {
		t.Errorf("a %d-byte slice of the cursor row kept its escapes intact; "+
			"this fixture no longer reaches the hazard: %q", width, unclamped[:width])
	}
}

// cursorRow returns the frame line carrying the cursor pointer, found rather
// than indexed so the assertions on it do not move when the chrome does.
func cursorRow(t *testing.T, frame string) string {
	t.Helper()
	for _, l := range frameLines(frame) {
		if strings.Contains(stripANSI(l), "▸") {
			return l
		}
	}
	t.Fatalf("no cursor row in:\n%s", stripANSI(frame))
	return ""
}

// TestViewNeverExceedsTheReportedHeightInANarrowPane is
// TestViewNeverExceedsTheReportedHeight counted in screen rows instead of
// logical lines. That test cannot fail on width: renderAt reports testWidth,
// which no fixture there overflows, and lineCount counts a wrapped line once
// however wide it is. This one renders in a pane narrow enough that most lines
// would wrap and counts what the terminal actually draws, which is the
// invariant the budget arithmetic is trying to hold.
//
// The fixture guard is the load-bearing part: if nothing in the frame were
// over-wide at a given width, screenRows and lineCount would agree and this
// would assert nothing beyond what the height sweep already covers.
func TestViewNeverExceedsTheReportedHeightInANarrowPane(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		name    string
		preview bool
	}{
		{"wide rows, preview off", false},
		{"wide rows and a preview", true},
	} {
		m := newModel(Options{Hosts: wideHosts(30), Theme: th, ShowPreview: tc.preview})
		for _, w := range []int{20, 40, 60} {
			for h := 1; h <= sweepHeight; h++ {
				if widest := lipgloss.Width(renderSized(m, 0, h)); widest <= w {
					t.Fatalf("%s: at height %d the widest line is %d columns, which fits a "+
						"%d-column pane; nothing here can wrap", tc.name, h, widest, w)
				}
				want := h
				if want < minFrame {
					want = minFrame
				}
				if got := screenRows(renderSized(m, w, h), w); got > want {
					t.Errorf("%s: pane %dx%d drew %d screen rows, want <= %d:\n%s",
						tc.name, w, h, got, want, stripANSI(renderSized(m, w, h)))
				}
			}
		}
	}
}

// TestViewRendersUserAndNonDefaultPortInTheDetailColumn varies the two fields
// every other fixture in this package holds constant. Every host in corpus,
// manyHosts and tallPreviewHosts has Port "22", and only tallPreviewHosts sets
// User at all — and nothing asserted it. So both detail-column branches did no
// work under test: deleting either the ":"+Port suffix or the User+"@" prefix
// left the whole suite green. A real config sets both, and the detail column is
// where the operator confirms what they are about to connect to.
//
// Port "2222" rather than something like "220" on purpose: it shares a prefix
// with the "22" default, so a prefix comparison in place of the equality check
// would render nothing here and fail.
func TestViewRendersUserAndNonDefaultPortInTheDetailColumn(t *testing.T) {
	m := newModel(Options{
		Hosts: []sshconfig.Host{{Alias: "gw", HostName: "10.0.0.3", User: "root", Port: "2222"}},
		Theme: theme.Default(),
	})
	out := render(m)
	if !strings.Contains(out, "root@10.0.0.3") {
		t.Errorf("detail column missing the user prefix:\n%s", out)
	}
	if !strings.Contains(out, "10.0.0.3:2222") {
		t.Errorf("detail column missing the non-default port suffix:\n%s", out)
	}
}

// TestViewKeepsTheHostNameHighlightAlignedUnderAUserPrefix pins the hazard
// renderRows names at view.go:264-266 — that the detail column is assembled
// from already-styled pieces so the hostname's highlight positions stay aligned
// when a user is prepended. The comment is correct and the code depends on it,
// but nothing tested it: inverting the construction (join the text first, style
// the result) left the whole suite green.
//
// The defect it hides is entirely silent. Both arms emit the same characters in
// the same order; only the escapes move. With this fixture the accent sits on
// "example" when the pieces are styled first and slides five runes left onto
// "@dev.ex" when they are not.
//
// Two conditions made it invisible, and both are asserted here rather than
// assumed, because a fixture that quietly lost either one would pass while
// pinning nothing:
//
//   - The match must land on the HostName, not the alias. positions are nil for
//     an alias match, and highlight returns early on nil — the misalignment
//     cannot arise at all. The suite's only other User fixture renders with an
//     empty query, which is exactly this case.
//   - The host must set User. Without the prefix there is nothing to shift by.
//
// The assertions go through renderRaw, with the escapes intact. render() strips
// them, and every content assertion in this file is blind to this by
// construction — the last check below is that blindness, stated as an
// assertion: the plain text is identical in both arms, so the styled channel is
// the only one that can see the defect. requireStyling means this test is
// skipped where lipgloss emits nothing, so it is not a guarantee off a styled
// terminal.
func TestViewKeepsTheHostNameHighlightAlignedUnderAUserPrefix(t *testing.T) {
	th := theme.Default()
	// Port "22" so the default-port suffix stays out of the detail column; the
	// User prefix is the shift under test.
	hosts := []sshconfig.Host{{Alias: "gw", HostName: "dev.example.com", User: "root", Port: "22"}}

	// Guard both invisibility conditions before asserting anything about
	// styling. "gw" holds no "e", so it cannot match "example" under any rule
	// and the match can only come from the hostname.
	if hosts[0].User == "" {
		t.Fatal("fixture lost its User; the prefix is what shifts the positions")
	}
	ranked := Rank(hosts, "example")
	if len(ranked) != 1 || ranked[0].HostNamePos == nil || ranked[0].AliasPos != nil {
		t.Fatalf("fixture no longer produces a hostname-only match: %+v", ranked)
	}

	raw := renderRaw(typeRunes(newModel(Options{Hosts: hosts, Theme: th}), "example"))
	// The one host is the cursor row, so the match is underlined on the band
	// rather than accented. Where it lands is the subject here either way.
	band, hit := bandStyles(th)
	requireStyling(t, band)

	if want := hit.Render("example"); !strings.Contains(raw, want) {
		t.Errorf("the matched hostname runes are not highlighted; missing %q in:\n%q", want, raw)
	}
	// The shifted reading, named exactly: seven runes starting five to the left
	// of "example" once "root@" is counted as part of the styled string.
	if bad := hit.Render("@dev.ex"); strings.Contains(raw, bad) {
		t.Errorf("the highlight is offset by the user prefix; %q is highlighted in:\n%q", bad, raw)
	}
	if plain := stripANSI(raw); !strings.Contains(plain, "root@dev.example.com") {
		t.Errorf("detail column text changed; want root@dev.example.com in:\n%s", plain)
	}
}

// TestViewLeavesAltScreenAndMouseOff discharges the other discard in the render
// helpers: they read tea.View.Content and ignore its eleven other fields.
// View's comment claims AltScreen and MouseMode are deliberately zero, and Run
// omits tea.WithAltScreen to match. Setting either would flash or grab the
// terminal underneath what is meant to be a keyboard-only overlay.
//
// The nine remaining fields — OnMouse, Cursor, Background/ForegroundColor,
// WindowTitle, ProgressBar, ReportFocus, DisableBracketedPasteMode and
// KeyboardEnhancements — get no assertion deliberately. View reaches the
// terminal through tea.NewView and one Content string and never names them, so
// a test on those would assert the absence of code rather than any behavior.
// Add one the moment View starts setting one.
func TestViewLeavesAltScreenAndMouseOff(t *testing.T) {
	next, _ := newTestModel().Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	v := next.(model).View()
	if v.AltScreen {
		t.Error("AltScreen is set; the picker is an overlay, not a full-screen app")
	}
	if v.MouseMode != 0 {
		t.Errorf("MouseMode = %d, want 0; the picker is keyboard-only", v.MouseMode)
	}
}
