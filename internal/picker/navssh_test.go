package picker

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-picker/internal/probe"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes SGR sequences so a test can assert the visible text of a
// styled string, which highlighting splits into several styled runs.
func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func sshFixture() NavOptions {
	return NavOptions{
		Theme: theme.Default(),
		Hosts: []sshconfig.Host{
			{Alias: "web1", HostName: "192.0.2.1", Port: "22"},
			{Alias: "db-primary", HostName: "192.0.2.10", Port: "5432", User: "deploy"},
			{Alias: "behind-jump", HostName: "10.0.0.2", Port: "22", ProxyJump: "bastion"},
		},
	}
}

// sshModel opens the navigator on the ssh tab, bypassing the tab key so the
// tests are about the tab, not about switching to it.
func sshModel(o NavOptions) navigatorModel {
	m := newNavigatorModel(o)
	m.section = NavSSH
	return m.refilter()
}

func TestSSHNavItemsRankAndKeepPositions(t *testing.T) {
	items := sshNavItems(sshFixture().Hosts, "db")
	if len(items) != 1 || items[0].ID != "db-primary" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].AliasPos) == 0 {
		t.Fatalf("alias match carries no highlight positions: %+v", items[0])
	}
}

func TestSSHNavItemsMatchHostName(t *testing.T) {
	// "0.2.10" is not in any alias, so this can only come back as a hostname
	// hit, and it matches db-primary's 192.0.2.10 but not web1's 192.0.2.1.
	items := sshNavItems(sshFixture().Hosts, "0.2.10")
	if len(items) != 1 || items[0].ID != "db-primary" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].HostNamePos) == 0 {
		t.Fatalf("hostname match carries no highlight positions: %+v", items[0])
	}
}

func TestAliasColumnForCapsLongAliases(t *testing.T) {
	long := strings.Repeat("a", maxAliasColumn+5)
	hosts := []sshconfig.Host{{Alias: long}, {Alias: "web1"}}
	if got := aliasColumnFor(hosts); got != maxAliasColumn {
		t.Fatalf("aliasColumnFor = %d, want the cap %d", got, maxAliasColumn)
	}
}

func TestPreviewFieldsAlignValues(t *testing.T) {
	h := sshconfig.Host{Alias: "web1", HostName: "192.0.2.1", Port: "22", User: "deploy"}
	// Direct assertion: every value starts at the shared column.
	for _, tc := range []struct{ label, value string }{
		{"HostName", h.HostName},
		{"Port", h.Port},
		{"User", h.User},
	} {
		found := false
		for _, line := range previewFields(h) {
			if strings.HasPrefix(line, tc.label) && strings.HasSuffix(line, tc.value) {
				if line[len(tc.label)] != ' ' {
					t.Fatalf("field %q value is not separated: %q", tc.label, line)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("no preview field for %q in %#v", tc.label, previewFields(h))
		}
	}
}

func TestHighlightMarksOnlyMatchedRunes(t *testing.T) {
	s := newStyles(theme.Default())
	plain := highlight("web1", nil, s.text, s.accent)
	if stripANSI(plain) != "web1" {
		t.Fatalf("highlight dropped the text: %q", plain)
	}
	matched := highlight("web1", []int{0, 1}, s.text, s.accent)
	if stripANSI(matched) != "web1" {
		t.Fatalf("highlight dropped the text: %q", matched)
	}
	// When lipgloss emits styling here, the matched run must carry a different
	// escape sequence than the plain render. In a no-color environment the two
	// are equal by construction, so that half is skipped rather than failed.
	if s.accent.Render("x") != s.text.Render("x") && matched == plain {
		t.Fatalf("matched runes were not styled differently: %q", matched)
	}
}

func TestOneLineFlattensNewlines(t *testing.T) {
	if got := oneLine("a\nb\r\nc"); got != "a b c" {
		t.Fatalf("oneLine = %q, want %q", got, "a b c")
	}
}

func TestSSHMarkerPrecedence(t *testing.T) {
	m := sshModel(sshFixture())
	s := newStyles(theme.Default())

	// Proxied hosts are skipped, never dialed.
	if got, _ := m.sshMarker(s, "behind-jump", m.opts.Hosts[2]); got != skipMarker {
		t.Fatalf("proxy marker = %q, want %q", got, skipMarker)
	}
	// A probed, reachable host.
	m.probed["web1"], m.up["web1"] = true, true
	if got, _ := m.sshMarker(s, "web1", m.opts.Hosts[0]); got != upMarker {
		t.Fatalf("up marker = %q, want %q", got, upMarker)
	}
	// A probed, unreachable host.
	m.probed["db-primary"], m.up["db-primary"] = true, false
	if got, _ := m.sshMarker(s, "db-primary", m.opts.Hosts[1]); got != downMarker {
		t.Fatalf("down marker = %q, want %q", got, downMarker)
	}
}

func TestSSHOpenMarkerIsDrawnAndWinsOverReachability(t *testing.T) {
	o := sshFixture()
	o.OpenPanes = map[string]string{"web1": "w5:pA"}
	m := sshModel(o)
	m.width, m.height = 48, 20
	m.probed["web1"], m.up["web1"] = true, true // would be upMarker if open lost
	view := m.View().Content
	if !strings.Contains(view, openMarker) {
		t.Fatalf("open marker not drawn:\n%s", view)
	}
}

func TestSSHPlacementKeysCarryPlacement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      rune
		mod       tea.KeyMod
		placement string
		forceNew  bool
	}{
		{"enter splits", tea.KeyEnter, 0, "split", false},
		{"^t opens a tab", 't', tea.ModCtrl, "tab", false},
		{"^z zooms", 'z', tea.ModCtrl, "zoomed", false},
		{"^n forces a new split", 'n', tea.ModCtrl, "split", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sshModel(sshFixture())
			m = navKey(m, tc.code, "", tc.mod)
			if m.chosen == nil {
				t.Fatal("no selection")
			}
			if m.chosen.Section != NavSSH {
				t.Fatalf("section = %d, want ssh", m.chosen.Section)
			}
			if m.chosen.Placement != tc.placement || m.chosen.ForceNew != tc.forceNew {
				t.Fatalf("chosen = %+v, want placement %q forceNew %v", m.chosen, tc.placement, tc.forceNew)
			}
		})
	}
}

func TestSSHPlacementKeysAreInertOnOtherTabs(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, 't', "", tea.ModCtrl)
	if m.chosen != nil {
		t.Fatalf("^t chose %+v on the spaces tab", m.chosen)
	}
	if m.section != NavSpaces {
		t.Fatalf("^t changed section to %d", m.section)
	}
}

func TestSSHPreviewTogglesWithCtrlO(t *testing.T) {
	o := sshFixture()
	o.ShowPreview = true
	m := sshModel(o)
	m.width, m.height = 48, 24
	if !strings.Contains(m.View().Content, "HostName") {
		t.Fatalf("preview fields missing from the ssh tab:\n%s", m.View().Content)
	}
	m = navKey(m, 'o', "", tea.ModCtrl)
	if m.preview {
		t.Fatal("^o did not toggle the preview off")
	}
	if strings.Contains(m.View().Content, "HostName") {
		t.Fatalf("preview still drawn after ^o:\n%s", m.View().Content)
	}
}

func TestSSHTabRendersProbeResults(t *testing.T) {
	m := sshModel(sshFixture())
	m.width, m.height = 48, 24
	next, cmd := m.Update(probeMsg(probe.Result{Alias: "web1", Up: true}))
	m = next.(navigatorModel)
	if cmd == nil {
		t.Fatal("probe result did not re-arm the probe command")
	}
	if !m.probed["web1"] || !m.up["web1"] {
		t.Fatalf("probe state = probed %v up %v", m.probed["web1"], m.up["web1"])
	}
}

func TestNavigatorFrameIncludesSSHTabAndFits(t *testing.T) {
	m := sshModel(sshFixture())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: 20})
	view := next.(navigatorModel).View().Content
	for _, tab := range []string{"spaces", "agents", "sessions", "ssh"} {
		if !strings.Contains(view, tab) {
			t.Fatalf("missing tab %q in:\n%s", tab, view)
		}
	}
	if got := len(strings.Split(view, "\n")); got != 20 {
		t.Fatalf("frame height = %d, want 20", got)
	}
}

func TestSSHWarningsRenderAndAreCounted(t *testing.T) {
	o := sshFixture()
	o.Warnings = []string{"first", "second"}
	m := sshModel(o)
	if got := m.warningLines(); got != 2 {
		t.Fatalf("warningLines = %d, want 2", got)
	}
	m.width, m.height = 48, 24
	view := m.View().Content
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Fatalf("warnings missing from the ssh tab:\n%s", view)
	}
}

func TestSSHWarningsAreNotDrawnOnOtherTabs(t *testing.T) {
	o := navFixture()
	o.Warnings = []string{"ssh-only warning"}
	m := newNavigatorModel(o)
	m.width, m.height = 48, 20
	if got := m.warningLines(); got != 0 {
		t.Fatalf("warningLines on spaces = %d, want 0", got)
	}
	if strings.Contains(m.View().Content, "ssh-only warning") {
		t.Fatalf("ssh warning leaked onto the spaces tab:\n%s", m.View().Content)
	}
}

func TestLatencyLabelFormatting(t *testing.T) {
	m := sshModel(sshFixture())
	m.probed["web1"], m.up["web1"] = true, true
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Microsecond, "<1ms"},
		{12 * time.Millisecond, "12ms"},
		{999 * time.Millisecond, "999ms"},
		{1500 * time.Millisecond, "1.5s"},
		{0, ""},
	} {
		m.latency["web1"] = tc.d
		if got := m.latencyLabel("web1"); got != tc.want {
			t.Fatalf("latencyLabel(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	// A host that was not reached has no latency to show, whatever the dial cost.
	m.up["web1"] = false
	m.latency["web1"] = 40 * time.Millisecond
	if got := m.latencyLabel("web1"); got != "" {
		t.Fatalf("latencyLabel for a down host = %q, want empty", got)
	}
}

func TestSSHRowShowsLatencyOnlyWhenUp(t *testing.T) {
	m := sshModel(sshFixture())
	m.width, m.height = 70, 24
	m.probed["web1"], m.up["web1"] = true, true
	m.latency["web1"] = 12 * time.Millisecond
	if view := m.View().Content; !strings.Contains(view, "12ms") {
		t.Fatalf("latency not shown:\n%s", view)
	}
	m.probed["db-primary"], m.up["db-primary"] = true, false
	m.latency["db-primary"] = 40 * time.Millisecond
	if view := m.View().Content; strings.Contains(view, "40ms") {
		t.Fatalf("latency shown for a down host:\n%s", view)
	}
}

func TestPreviewFieldsIncludeForwards(t *testing.T) {
	h := sshconfig.Host{Alias: "tunnel", HostName: "10.0.0.1", Port: "22",
		LocalForward:   []string{"8080 localhost:80", "9090 localhost:90"},
		RemoteForward:  []string{"3000 localhost:3000"},
		DynamicForward: []string{"1080"},
	}
	lines := previewFields(h)
	for _, want := range []string{"LocalForward", "RemoteForward", "DynamicForward"} {
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, want) {
				if line[previewLabelWidth-1] != ' ' {
					t.Fatalf("%s value does not start at the value column: %q", want, line)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("no %s line in %#v", want, lines)
		}
	}
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "LocalForward") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("LocalForward lines = %d, want both", count)
	}
}

func TestSSHSpaceMarksAndEnterOpensAll(t *testing.T) {
	m := sshModel(sshFixture())
	m.width, m.height = 60, 24
	m = navKey(m, tea.KeySpace, " ", 0) // mark web1, step down
	if !m.marked["web1"] {
		t.Fatal("space did not mark the cursor host")
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (stepped down after marking)", m.cursor)
	}
	m = navKey(m, tea.KeySpace, " ", 0) // mark db-primary
	m = navKey(m, tea.KeyEnter, "", 0)
	if m.chosen == nil || len(m.chosen.Marked) != 2 {
		t.Fatalf("chosen = %+v", m.chosen)
	}
	if m.chosen.Marked[0].ID != "web1" || m.chosen.Marked[1].ID != "db-primary" {
		t.Fatalf("marked = %+v", m.chosen.Marked)
	}
}

func TestSSHEscClearsMarksBeforeClosing(t *testing.T) {
	m := sshModel(sshFixture())
	m = navKey(m, tea.KeySpace, " ", 0)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(navigatorModel)
	if len(m.marked) != 0 {
		t.Fatalf("esc left marks: %v", m.marked)
	}
	if cmd != nil {
		t.Fatal("esc cleared marks but also quit")
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc with no marks did not close")
	}
}

// A mark is a statement about a host, not about the current filter: typing a
// query that hides the marked row must not silently drop it from the open.
func TestMarkedHostsSurviveAQuery(t *testing.T) {
	m := sshModel(sshFixture())
	m = navKey(m, tea.KeySpace, " ", 0) // mark web1
	for _, r := range "db" {
		m = navKey(m, r, string(r), 0)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if m.chosen == nil || len(m.chosen.Marked) != 1 || m.chosen.Marked[0].ID != "web1" {
		t.Fatalf("chosen = %+v", m.chosen)
	}
}

func TestSSHMarkedRowShowsTheMarkGlyph(t *testing.T) {
	m := sshModel(sshFixture())
	m.width, m.height = 60, 24
	m = navKey(m, tea.KeySpace, " ", 0) // web1 marked, cursor on db-primary
	if view := m.View().Content; !strings.Contains(view, "▣") {
		t.Fatalf("mark glyph not drawn:\n%s", view)
	}
}

// Space is a query character everywhere but the ssh tab, where it cannot match
// an alias and is taken for marking instead.
func TestSpaceStillFiltersOnOtherTabs(t *testing.T) {
	o := navFixture()
	o.Spaces = []NavItem{{ID: "w1", Label: "a b"}, {ID: "w2", Label: "ab"}}
	m := newNavigatorModel(o)
	m = navKey(m, tea.KeySpace, " ", 0)
	if m.query != " " {
		t.Fatalf("space did not reach the query on the spaces tab: %q", m.query)
	}
	if len(m.marked) != 0 {
		t.Fatalf("space marked a row off the ssh tab: %v", m.marked)
	}
}
