package picker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// actionOptions is navFixture with a four-action menu that exercises each
// dispatch path: an input, a confirm, a copy, and a plain run.
func actionOptions() NavOptions {
	o := navFixture()
	o.Actions = func(section NavSection, item NavItem) []NavAction {
		return []NavAction{
			{ID: "rename", Label: "rename", Input: "rename it"},
			{ID: "close", Label: "close", Confirm: "close it?"},
			{ID: "copy-id", Label: "copy id", Copy: item.ID},
			{ID: "new-tab", Label: "new tab"},
		}
	}
	o.RunAction = func(section NavSection, item NavItem, actionID, text string) (string, error) {
		return actionID + ":" + item.ID + ":" + text, nil
	}
	return o
}

func TestMenuNavigatesAndCopies(t *testing.T) {
	m := newNavigatorModel(actionOptions())
	m.width, m.height = 60, 28
	m = navKey(m, 'x', "", tea.ModCtrl)
	if len(m.menuActions) != 4 {
		t.Fatalf("^x menu = %+v", m.menuActions)
	}
	view := m.View().Content
	if !strings.Contains(view, "actions ·") || !strings.Contains(view, "rename") {
		t.Fatalf("menu not drawn:\n%s", view)
	}
	if got := len(strings.Split(view, "\n")); got != 28 {
		t.Fatalf("menu frame height = %d, want 28", got)
	}
	// Down twice lands on copy-id.
	m = navKey(m, tea.KeyDown, "", 0)
	m = navKey(m, tea.KeyDown, "", 0)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if m.menuActions != nil || cmd == nil {
		t.Fatalf("enter did not run the action: menu=%v cmd=%v", m.menuActions, cmd)
	}
	if m.note != "copied copy id" || m.noteErr {
		t.Fatalf("copy note = %q (err %v)", m.note, m.noteErr)
	}
	_ = cmd() // the OSC 52 clipboard command
}

func TestMenuRenameCollectsText(t *testing.T) {
	var gotAction, gotText string
	o := actionOptions()
	o.RunAction = func(section NavSection, item NavItem, actionID, text string) (string, error) {
		gotAction, gotText = actionID, text
		return "renamed", nil
	}
	m := newNavigatorModel(o)
	m.width, m.height = 60, 28
	m = navKey(m, 'x', "", tea.ModCtrl)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // rename is first
	m = next.(navigatorModel)
	if !m.inputOpen || m.inputLabel != "rename it" {
		t.Fatalf("rename did not open the input: open=%v label=%q", m.inputOpen, m.inputLabel)
	}
	for _, r := range "new name" {
		m = navKey(m, r, string(r), 0)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if m.inputOpen || cmd == nil {
		t.Fatalf("rename did not submit: open=%v cmd=%v", m.inputOpen, cmd)
	}
	msg := cmd().(navActionMsg)
	if gotAction != "rename" || gotText != "new name" || msg.status != "renamed" {
		t.Fatalf("rename sent %q %q, msg %#v", gotAction, gotText, msg)
	}
}

func TestMenuCloseConfirms(t *testing.T) {
	var ran string
	o := actionOptions()
	o.RunAction = func(section NavSection, item NavItem, actionID, text string) (string, error) {
		ran = actionID
		return "closed", nil
	}
	m := newNavigatorModel(o)
	m.width, m.height = 60, 28
	m = navKey(m, 'x', "", tea.ModCtrl)
	m = navKey(m, tea.KeyDown, "", 0) // close is second
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if !m.confirmOpen || m.confirmQuestion != "close it?" {
		t.Fatalf("close did not ask: open=%v q=%q", m.confirmOpen, m.confirmQuestion)
	}
	m = navKey(m, 'n', "n", 0)
	if m.confirmOpen || ran != "" {
		t.Fatalf("n did not cancel: open=%v ran=%q", m.confirmOpen, ran)
	}
	// Reopen and confirm.
	m = navKey(m, 'x', "", tea.ModCtrl)
	m = navKey(m, tea.KeyDown, "", 0)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = next.(navigatorModel)
	if m.confirmOpen || cmd == nil {
		t.Fatalf("y did not run: open=%v cmd=%v", m.confirmOpen, cmd)
	}
	cmd()
	if ran != "close" {
		t.Fatalf("ran = %q, want close", ran)
	}
}

func TestMenuEscCancels(t *testing.T) {
	m := newNavigatorModel(actionOptions())
	m = navKey(m, 'x', "", tea.ModCtrl)
	m = navKey(m, tea.KeyEsc, "", 0)
	if m.menuActions != nil {
		t.Fatal("esc left the menu open")
	}
}

func TestMenuInertOnSSHAndWithoutActions(t *testing.T) {
	o := actionOptions()
	m := newNavigatorModel(o)
	m.section = NavSSH
	m = m.refilter()
	m = navKey(m, 'x', "", tea.ModCtrl)
	if m.menuActions != nil {
		t.Fatal("^x opened a menu on the ssh tab")
	}
	m2 := newNavigatorModel(navFixture()) // no Actions callback
	m2 = navKey(m2, 'x', "", tea.ModCtrl)
	if m2.menuActions != nil {
		t.Fatal("^x opened a menu with no Actions callback")
	}
}

// A refresh can reorder rows while the menu is open. The action must still
// target the row the menu was opened on, not wherever the cursor landed.
func TestMenuActionTargetsTheRowItOpenedOn(t *testing.T) {
	var gotItem string
	o := actionOptions()
	o.RunAction = func(section NavSection, item NavItem, actionID, text string) (string, error) {
		gotItem = item.ID
		return "ok", nil
	}
	m := newNavigatorModel(o)
	m.width, m.height = 60, 28
	m = m.move(-1)                      // navFixture preselects the Current row; step onto w1
	m = navKey(m, 'x', "", tea.ModCtrl) // opened on w1
	next, _ := m.Update(navRefreshMsg(NavRefresh{
		Spaces:   []NavItem{{ID: "w2", Label: "nixos-dev"}, {ID: "w1", Label: "project alpha"}},
		Agents:   o.Agents,
		Sessions: o.Sessions,
	}))
	m = next.(navigatorModel)
	m.menuCursor = len(m.menuActions) - 1 // new-tab, a plain run
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if cmd == nil {
		t.Fatal("no action command")
	}
	cmd()
	if gotItem != "w1" {
		t.Fatalf("action targeted %q, want w1", gotItem)
	}
}
