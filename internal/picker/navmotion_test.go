package picker

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// g arms the chord and does not move, so a single g is never a jump; the second
// g is what lands on the top row.
func TestGGJumpsToTopAndGToBottom(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.cursor = 1
	m = navKey(m, 'g', "g", 0)
	if !m.pendingG || m.cursor != 1 {
		t.Fatalf("first g = pending %v cursor %d, want armed and unmoved", m.pendingG, m.cursor)
	}
	m = navKey(m, 'g', "g", 0)
	if m.pendingG || m.cursor != 0 {
		t.Fatalf("gg = pending %v cursor %d, want top", m.pendingG, m.cursor)
	}
	m = navKey(m, 'G', "G", 0)
	if m.cursor != len(m.items)-1 {
		t.Fatalf("G = cursor %d, want %d", m.cursor, len(m.items)-1)
	}
}

// A g that is not followed by another g is an ordinary query character, so a
// search can still start with the letter.
func TestLoneGCommitsToTheQuery(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, 'g', "g", 0)
	m = navKey(m, 'w', "w", 0)
	if m.pendingG || m.query != "gw" {
		t.Fatalf("g then w = pending %v query %q, want committed %q", m.pendingG, m.query, "gw")
	}
}

// Esc cancels the half-typed chord rather than closing the popup or clearing a
// query that was never committed.
func TestEscCancelsPendingG(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, 'g', "g", 0)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(navigatorModel)
	if cmd != nil {
		t.Fatal("Esc during a pending g quit the picker")
	}
	if m.pendingG || m.query != "" {
		t.Fatalf("Esc = pending %v query %q, want cancelled", m.pendingG, m.query)
	}
}

func TestHomeEndJump(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.cursor = 1
	m = navKey(m, tea.KeyHome, "", 0)
	if m.cursor != 0 {
		t.Fatalf("Home = cursor %d, want 0", m.cursor)
	}
	m = navKey(m, tea.KeyEnd, "", 0)
	if m.cursor != len(m.items)-1 {
		t.Fatalf("End = cursor %d, want %d", m.cursor, len(m.items)-1)
	}
}

// G and End clamp to the last row rather than wrapping or overshooting on an
// empty list.
func TestEndOnEmptyListIsSafe(t *testing.T) {
	o := navFixture()
	o.Spaces = nil
	m := newNavigatorModel(o)
	m = navKey(m, 'G', "G", 0)
	if m.cursor != -1 {
		t.Fatalf("G on an empty list = cursor %d, want -1", m.cursor)
	}
}

func TestCtrlWDeletesTheLastWord(t *testing.T) {
	m := newNavigatorModel(navFixture())
	for _, r := range "foo bar baz" {
		m = navKey(m, r, string(r), 0)
	}
	m = navKey(m, 'w', "", tea.ModCtrl)
	if m.query != "foo bar " {
		t.Fatalf("^w = %q, want %q", m.query, "foo bar ")
	}
	m = navKey(m, 'w', "", tea.ModCtrl)
	if m.query != "foo " {
		t.Fatalf("^w twice = %q, want %q", m.query, "foo ")
	}
}

// A ctrl chord clears a half-typed g instead of leaving the chord armed behind
// the action.
func TestCtrlChordClearsPendingG(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, 'g', "g", 0)
	m = navKey(m, 'u', "", tea.ModCtrl)
	if m.pendingG {
		t.Fatal("a ctrl chord left the g chord armed")
	}
}
