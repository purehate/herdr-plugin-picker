package picker

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

type bcastCall struct {
	panes []string
	text  string
}

// paneFixture opens the navigator on the panes tab with a recording Broadcast,
// so a test can assert exactly which panes were written to and with what.
func paneFixture(t *testing.T) (navigatorModel, *[]bcastCall) {
	t.Helper()
	calls := &[]bcastCall{}
	o := NavOptions{
		Theme: theme.Default(),
		Panes: []NavItem{
			{ID: "w1:p1", Label: "shell", Detail: "~/src"},
			{ID: "w1:p2", Label: "codex", Detail: "~/src/api"},
			{ID: "w2:p1", Label: "build", Detail: "~/other"},
		},
		Broadcast: func(ids []string, text string) (string, error) {
			*calls = append(*calls, bcastCall{panes: append([]string(nil), ids...), text: text})
			return "sent", nil
		},
	}
	m := newNavigatorModel(o)
	m.section = NavPanes
	return m.refilter(), calls
}

// run drains a tea.Cmd so the recorded Broadcast actually fires.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run")
	}
	return cmd()
}

func TestSpaceMarksAPane(t *testing.T) {
	m, _ := paneFixture(t)
	m = navKey(m, tea.KeySpace, " ", 0)
	if !m.marked["w1:p1"] {
		t.Fatalf("space did not mark the cursor pane: %v", m.marked)
	}
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (space steps down)", m.cursor)
	}
}

func TestCtrlAMarksEveryListedPane(t *testing.T) {
	m, _ := paneFixture(t)
	m = navKey(m, 'a', "", tea.ModCtrl)
	if len(m.marked) != 3 {
		t.Fatalf("marked %d panes, want 3: %v", len(m.marked), m.marked)
	}
}

// ^a marks what is on screen, not the whole inventory: the filter is how the
// operator narrows a broadcast, so it must bound the select-all too.
func TestCtrlAOnlyMarksTheFilteredRows(t *testing.T) {
	m, _ := paneFixture(t)
	for _, r := range "build" {
		m = navKey(m, r, string(r), 0)
	}
	m = navKey(m, 'a', "", tea.ModCtrl)
	if len(m.marked) != 1 || !m.marked["w2:p1"] {
		t.Fatalf("marked = %v, want only w2:p1", m.marked)
	}
}

// A second ^a clears, so the same key undoes an over-broad selection.
func TestCtrlATogglesOff(t *testing.T) {
	m, _ := paneFixture(t)
	m = navKey(m, 'a', "", tea.ModCtrl)
	m = navKey(m, 'a', "", tea.ModCtrl)
	if len(m.marked) != 0 {
		t.Fatalf("second ^a left %v marked", m.marked)
	}
}

func TestBroadcastAsksBeforeSending(t *testing.T) {
	m, calls := paneFixture(t)
	m = navKey(m, tea.KeySpace, " ", 0)
	m = navKey(m, tea.KeySpace, " ", 0)
	m = navKey(m, 'b', "", tea.ModCtrl)
	if !m.inputOpen {
		t.Fatal("^b did not open the command input")
	}
	for _, r := range "id" {
		m = navKey(m, r, string(r), 0)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if cmd != nil {
		t.Fatal("enter sent the broadcast without confirming")
	}
	if !m.confirmOpen {
		t.Fatal("enter did not open the confirm")
	}
	if !strings.Contains(m.confirmQuestion, "2") {
		t.Errorf("confirm %q does not name the pane count", m.confirmQuestion)
	}
	if len(*calls) != 0 {
		t.Fatalf("broadcast fired before confirmation: %v", *calls)
	}

	next, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = next.(navigatorModel)
	run(t, cmd)
	if len(*calls) != 1 {
		t.Fatalf("got %d broadcasts, want 1", len(*calls))
	}
	got := (*calls)[0]
	if strings.Join(got.panes, ",") != "w1:p1,w1:p2" {
		t.Errorf("panes = %v, want [w1:p1 w1:p2]", got.panes)
	}
	if got.text != "id" {
		t.Errorf("text = %q, want %q", got.text, "id")
	}
}

func TestBroadcastDeclinedSendsNothing(t *testing.T) {
	m, calls := paneFixture(t)
	m = navKey(m, tea.KeySpace, " ", 0)
	m = navKey(m, 'b', "", tea.ModCtrl)
	m = navKey(m, 'x', "x", 0)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = next.(navigatorModel)
	if cmd != nil {
		t.Fatal("declining the confirm still ran a command")
	}
	if m.confirmOpen {
		t.Error("confirm stayed open after n")
	}
	if len(*calls) != 0 {
		t.Fatalf("broadcast fired after n: %v", *calls)
	}
}

// With nothing marked, ^b targets the row under the cursor — the single-pane
// case should not require marking first.
func TestBroadcastWithNoMarksUsesTheCursorRow(t *testing.T) {
	m, calls := paneFixture(t)
	m = m.move(1)
	m = navKey(m, 'b', "", tea.ModCtrl)
	m = navKey(m, 'x', "x", 0)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, cmd := next.(navigatorModel).Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_ = next
	run(t, cmd)
	if len(*calls) != 1 || strings.Join((*calls)[0].panes, ",") != "w1:p2" {
		t.Fatalf("calls = %v, want one to w1:p2", *calls)
	}
}

// An empty command is a slip, not a request to send a bare newline to every
// marked shell.
func TestBroadcastIgnoresAnEmptyCommand(t *testing.T) {
	m, calls := paneFixture(t)
	m = navKey(m, tea.KeySpace, " ", 0)
	m = navKey(m, 'b', "", tea.ModCtrl)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if cmd != nil || m.confirmOpen {
		t.Fatal("an empty command reached the confirm")
	}
	if len(*calls) != 0 {
		t.Fatalf("empty command sent: %v", *calls)
	}
}

// A mark is about the pane, not the query — the same rule the ssh tab follows.
func TestMarkedPanesSurviveAQuery(t *testing.T) {
	m, calls := paneFixture(t)
	m = navKey(m, tea.KeySpace, " ", 0) // mark w1:p1
	for _, r := range "build" {
		m = navKey(m, r, string(r), 0)
	}
	m = navKey(m, 'b', "", tea.ModCtrl)
	m = navKey(m, 'x', "x", 0)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, cmd := next.(navigatorModel).Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_ = next
	run(t, cmd)
	if len(*calls) != 1 || strings.Join((*calls)[0].panes, ",") != "w1:p1" {
		t.Fatalf("calls = %v, want the marked pane w1:p1 despite the query", *calls)
	}
}

func TestBroadcastReportsAFailure(t *testing.T) {
	calls := 0
	o := NavOptions{
		Theme: theme.Default(),
		Panes: []NavItem{{ID: "w1:p1", Label: "shell"}},
		Broadcast: func([]string, string) (string, error) {
			calls++
			return "", errors.New("pane is gone")
		},
	}
	m := newNavigatorModel(o)
	m.section = NavPanes
	m = m.refilter()
	m = navKey(m, 'b', "", tea.ModCtrl)
	m = navKey(m, 'x', "x", 0)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, cmd := next.(navigatorModel).Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = next.(navigatorModel)
	msg := run(t, cmd)
	next, _ = m.Update(msg)
	m = next.(navigatorModel)
	if !m.noteErr || !strings.Contains(m.note, "pane is gone") {
		t.Fatalf("note = %q err = %v, want the failure surfaced", m.note, m.noteErr)
	}
}

// Broadcast is a panes-tab verb. On the other tabs ^b must do nothing rather
// than write a command into whatever rows happen to be listed.
func TestBroadcastIsPanesTabOnly(t *testing.T) {
	m, calls := paneFixture(t)
	m.section = NavSpaces
	m = m.refilter()
	m = navKey(m, 'b', "", tea.ModCtrl)
	if m.inputOpen {
		t.Fatal("^b opened an input off the panes tab")
	}
	if len(*calls) != 0 {
		t.Fatalf("broadcast ran off the panes tab: %v", *calls)
	}
}

// Marks belong to the tab they were made on. Carrying them across would let a
// broadcast target rows the operator marked for a different purpose.
func TestSwitchingTabsClearsMarks(t *testing.T) {
	m, _ := paneFixture(t)
	m = navKey(m, tea.KeySpace, " ", 0)
	m = navKey(m, tea.KeyTab, "", 0)
	if len(m.marked) != 0 {
		t.Fatalf("marks survived a tab switch: %v", m.marked)
	}
}

func TestEscClearsPaneMarksBeforeClosing(t *testing.T) {
	m, _ := paneFixture(t)
	m = navKey(m, tea.KeySpace, " ", 0)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(navigatorModel)
	if len(m.marked) != 0 {
		t.Fatalf("esc left marks: %v", m.marked)
	}
	if cmd != nil {
		t.Fatal("esc cleared marks but also quit")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc with no marks did not close")
	}
}

// Enter on a pane row focuses it; that is the tab's value even when nothing is
// being broadcast.
func TestEnterChoosesThePane(t *testing.T) {
	m, _ := paneFixture(t)
	m = m.move(1)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if cmd == nil {
		t.Fatal("enter did not close the picker")
	}
	if m.chosen == nil || m.chosen.Section != NavPanes || m.chosen.Item.ID != "w1:p2" {
		t.Fatalf("chosen = %+v", m.chosen)
	}
}

// Broadcast needs a sink. With none wired, ^b must not open an input the picker
// cannot act on.
func TestBroadcastNeedsASink(t *testing.T) {
	m := newNavigatorModel(NavOptions{
		Theme: theme.Default(),
		Panes: []NavItem{{ID: "w1:p1", Label: "shell"}},
	})
	m.section = NavPanes
	m = m.refilter()
	m = navKey(m, 'b', "", tea.ModCtrl)
	if m.inputOpen {
		t.Fatal("^b opened an input with no Broadcast configured")
	}
}
