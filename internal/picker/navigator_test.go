package picker

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

func navFixture() NavOptions {
	return NavOptions{
		Theme: theme.Default(),
		Spaces: []NavItem{
			{ID: "w1", Label: "project alpha", Detail: "2 tabs"},
			{ID: "w2", Label: "nixos-dev", Current: true},
		},
		Agents:   []NavItem{{ID: "w2:p1", Label: "codex review", Search: "/tmp/review"}},
		Sessions: []NavItem{{ID: "w2:t1", Label: "build", Detail: "nixos-dev"}},
	}
}

func navKey(m navigatorModel, code rune, text string, mod tea.KeyMod) navigatorModel {
	next, _ := m.Update(tea.KeyPressMsg{Code: code, Text: text, Mod: mod})
	return next.(navigatorModel)
}

func navClick(m navigatorModel, x, y int) (navigatorModel, tea.Cmd) {
	next, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return next.(navigatorModel), cmd
}

func TestNavigatorTabsFilterAndChoose(t *testing.T) {
	m := newNavigatorModel(navFixture())
	if m.cursor != 1 {
		t.Fatalf("current space should be preselected, cursor = %d", m.cursor)
	}
	m = navKey(m, tea.KeyTab, "", 0)
	if m.section != NavAgents || len(m.items) != 1 {
		t.Fatalf("agent tab = section %d, items %v", m.section, m.items)
	}
	for _, r := range "rvw" {
		m = navKey(m, r, string(r), 0)
	}
	if len(m.items) != 1 || m.items[0].ID != "w2:p1" {
		t.Fatalf("fuzzy agent match = %v", m.items)
	}
	m = navKey(m, tea.KeyRight, "", 0)
	if m.section != NavSessions || m.query != "" {
		t.Fatalf("section switch = %d, query %q", m.section, m.query)
	}
	m = navKey(m, tea.KeyEnter, "", 0)
	if m.chosen == nil || m.chosen.Section != NavSessions || m.chosen.Item.ID != "w2:t1" {
		t.Fatalf("chosen = %+v", m.chosen)
	}
}

func TestNavigatorFilterSearchesMetadataAndClears(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, tea.KeyTab, "", 0)
	for _, r := range "tmp" {
		m = navKey(m, r, string(r), 0)
	}
	if len(m.items) != 1 {
		t.Fatalf("cwd search found %v", m.items)
	}
	m = navKey(m, 'u', "", tea.ModCtrl)
	if m.query != "" {
		t.Fatalf("^u left query %q", m.query)
	}
	m = navKey(m, tea.KeyTab, "", tea.ModShift)
	if m.section != NavSpaces {
		t.Fatalf("shift-tab moved to %d", m.section)
	}
}

func TestNavigatorViewFitsPopupAndHasSettingsTabs(t *testing.T) {
	m := newNavigatorModel(navFixture())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: 20})
	v := next.(navigatorModel).View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %d, want cell motion", v.MouseMode)
	}
	view := v.Content
	if !strings.Contains(view, "spaces") || !strings.Contains(view, "agents") || !strings.Contains(view, "sessions") {
		t.Fatalf("missing tabs: %q", view)
	}
	lines := strings.Split(view, "\n")
	if len(lines) != 20 {
		t.Fatalf("frame height = %d, want 20", len(lines))
	}
	for i, line := range lines {
		if lipgloss.Width(line) > 48 {
			t.Errorf("line %d width = %d", i, lipgloss.Width(line))
		}
	}
}

func TestNavigatorMouseTabsRowsAndFooter(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.width, m.height = 48, 20
	m, _ = navClick(m, 13, 1) // agents tab
	if m.section != NavAgents {
		t.Fatalf("clicked agents tab, got section %d", m.section)
	}
	m, cmd := navClick(m, 5, 5) // first visible agent
	if m.chosen == nil || m.chosen.Item.ID != "w2:p1" || cmd == nil {
		t.Fatalf("clicked agent row: chosen = %+v, quit = %v", m.chosen, cmd)
	}

	m = newNavigatorModel(navFixture())
	m.width, m.height = 48, 20
	m, _ = navClick(m, 22, 1) // sessions tab
	if m.section != NavSessions {
		t.Fatalf("clicked sessions tab, got section %d", m.section)
	}
	m, cmd = navClick(m, 3, 18) // jump footer
	if m.chosen == nil || m.chosen.Item.ID != "w2:t1" || cmd == nil {
		t.Fatalf("clicked jump: chosen = %+v, quit = %v", m.chosen, cmd)
	}

	m = newNavigatorModel(navFixture())
	m.width, m.height = 48, 20
	m, cmd = navClick(m, 14, 18) // close footer
	if m.chosen != nil || cmd == nil {
		t.Fatalf("clicked close: chosen = %+v, quit = %v", m.chosen, cmd)
	}
}

func TestNavigatorMouseWheelAndScrolledRow(t *testing.T) {
	o := navFixture()
	o.Spaces = nil
	for i := 0; i < 30; i++ {
		o.Spaces = append(o.Spaces, NavItem{ID: fmt.Sprint(i), Label: fmt.Sprintf("space %d", i)})
	}
	m := newNavigatorModel(o)
	m.width, m.height = 48, 20 // 12 visible rows
	for i := 0; i < 15; i++ {
		next, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		m = next.(navigatorModel)
	}
	if m.cursor != 15 {
		t.Fatalf("wheel cursor = %d, want 15", m.cursor)
	}
	start, _ := m.listWindow()
	if start != 4 {
		t.Fatalf("scroll start = %d, want 4", start)
	}
	m, _ = navClick(m, 5, 6)
	if m.chosen == nil || m.chosen.Item.ID != "5" {
		t.Fatalf("scrolled click chose %+v, want space 5", m.chosen)
	}
	next, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if got := next.(navigatorModel).cursor; got != 4 {
		t.Fatalf("wheel up cursor = %d, want 4", got)
	}
}

func TestNavigatorMouseIgnoresOutsideAndOtherButtons(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.width, m.height = 48, 20
	for _, click := range []tea.MouseClickMsg{
		{X: -1, Y: 6, Button: tea.MouseLeft},
		{X: 50, Y: 6, Button: tea.MouseLeft},
		{X: 5, Y: 4, Button: tea.MouseLeft},
		{X: 5, Y: 6, Button: tea.MouseRight},
	} {
		next, cmd := m.Update(click)
		m = next.(navigatorModel)
		if m.chosen != nil || cmd != nil || m.section != NavSpaces {
			t.Fatalf("outside click %v changed navigator: %+v", click, m)
		}
	}
}

func TestNavigatorSelectionUsesRGBAccentBackground(t *testing.T) {
	th := theme.Default()
	th.Accent = "#14e21a"
	style := navigatorSelectedStyle(th, lipgloss.NewStyle().Reverse(true))
	if got := style.GetBackground(); !reflect.DeepEqual(got, lipgloss.Color("#14e21a")) {
		t.Fatalf("selected background = %v", got)
	}
	if rendered := style.Render(" selected "); !strings.Contains(rendered, "48;2;20;226;26") {
		t.Fatalf("selected row emitted no green background: %q", rendered)
	}
}
