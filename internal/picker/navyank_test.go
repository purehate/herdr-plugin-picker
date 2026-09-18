package picker

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

func TestYankCopiesTheRowIdentity(t *testing.T) {
	m := newNavigatorModel(navFixture()) // cursor sits on w2, the current space
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = next.(navigatorModel)
	if m.note != "copied id" || m.noteFor != "w2" || m.noteErr {
		t.Fatalf("yank = note %q for %q err %v", m.note, m.noteFor, m.noteErr)
	}
	if cmd == nil {
		t.Fatal("yank did not return a clipboard command")
	}
}

// What "identity" means is per tab: an ssh alias and a machine's ssh target are
// not herdr ids.
func TestYankUsesTheTabIdentity(t *testing.T) {
	o := navFixture()
	o.Hosts = []sshconfig.Host{{Alias: "web1", HostName: "10.0.0.1"}}
	m := newNavigatorModel(o).setSection(NavSSH)
	m = navKey(m, 'y', "", tea.ModCtrl)
	if m.note != "copied alias" || m.noteFor != "web1" {
		t.Fatalf("ssh yank = note %q for %q", m.note, m.noteFor)
	}

	mo := navFixture()
	mo.Machines = []NavItem{{ID: "m1", Label: "box", Target: "workbox"}}
	mm := newNavigatorModel(mo).setSection(NavMachines)
	mm = navKey(mm, 'y', "", tea.ModCtrl)
	if mm.note != "copied ssh target" || mm.noteFor != "m1" {
		t.Fatalf("machine yank = note %q for %q", mm.note, mm.noteFor)
	}
}

func TestYankOnEmptyListIsSafe(t *testing.T) {
	o := navFixture()
	o.Spaces = nil
	m := newNavigatorModel(o)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = next.(navigatorModel)
	if cmd != nil || m.note != "" {
		t.Fatalf("yank on an empty list = cmd %v note %q", cmd, m.note)
	}
}
