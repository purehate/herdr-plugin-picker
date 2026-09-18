package picker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMachinesTabListsAndSelects(t *testing.T) {
	o := navFixture()
	o.Machines = []NavItem{
		{ID: "m1", Label: "● build", Detail: "workbox", Current: true},
		{ID: "m2", Label: "○ lab", Detail: "ssh://u@h:2222"},
	}
	m := newNavigatorModel(o).setSection(NavMachines)
	if m.section != NavMachines || len(m.items) != 2 || m.cursor != 0 {
		t.Fatalf("machines tab = section %d items %v cursor %d", m.section, m.items, m.cursor)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if m.chosen == nil || m.chosen.Section != NavMachines || m.chosen.Item.ID != "m1" {
		t.Fatalf("chosen = %+v", m.chosen)
	}
}

func TestMachinesTabEmptyStates(t *testing.T) {
	m := newNavigatorModel(navFixture()).setSection(NavMachines)
	if got := m.emptyMessage(); got != "no saved machines" {
		t.Fatalf("empty message = %q", got)
	}
	o := navFixture()
	o.MachineNote = "could not list machines: boom"
	m = newNavigatorModel(o).setSection(NavMachines)
	if got := m.emptyMessage(); !strings.Contains(got, "boom") {
		t.Fatalf("note message = %q", got)
	}
}

// The machines tab carries no marks and no preview, so space is an ordinary
// query character there like the other non-marking tabs.
func TestMachinesTabDoesNotMark(t *testing.T) {
	m := newNavigatorModel(navFixture()).setSection(NavMachines)
	if m.marksRows() {
		t.Fatal("machines tab claims to mark rows")
	}
	if got := m.previewBlockLines(); got != 0 {
		t.Fatalf("machines preview lines = %d, want 0", got)
	}
}
