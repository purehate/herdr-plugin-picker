package picker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestQuestionMarkTogglesHelp(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, '?', "?", 0)
	if !m.helpOpen {
		t.Fatal("? did not open the help overlay")
	}
	if !strings.Contains(m.View().Content, "keys — spaces") {
		t.Fatalf("help not drawn:\n%s", m.View().Content)
	}
	m = navKey(m, '?', "?", 0)
	if m.helpOpen {
		t.Fatal("? did not close the help overlay")
	}
}

func TestHelpClosesOnEscAndSwallowsTyping(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, '?', "?", 0)
	// A printable key must not leak into the query behind the overlay.
	m = navKey(m, 'x', "x", 0)
	if !m.helpOpen || m.query != "" {
		t.Fatalf("typing under help = open %v query %q, want swallowed", m.helpOpen, m.query)
	}
	m = navKey(m, tea.KeyEsc, "", 0)
	if m.helpOpen {
		t.Fatal("esc did not close the help overlay")
	}
}

// The overlay is section-aware: it must not advertise a key that does nothing
// where the operator is standing.
func TestHelpIsSectionAware(t *testing.T) {
	cases := []struct {
		section NavSection
		want    string
	}{
		{NavSSH, "^t / ^z"},
		{NavPanes, "^b"},
		{NavAgents, "^p"},
		{NavMachines, "herdr --remote"},
	}
	for _, tc := range cases {
		m := newNavigatorModel(navFixture()).setSection(tc.section)
		m.helpOpen = true
		if got := m.View().Content; !strings.Contains(got, tc.want) {
			t.Fatalf("help on %s missing %q:\n%s", navNames[tc.section], tc.want, got)
		}
	}
}

// Scrolling is clamped: the offset cannot run past the end or below the top.
func TestHelpScrollClamps(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, '?', "?", 0)
	for range 100 {
		m = navKey(m, 'j', "", 0)
	}
	m = navKey(m, 'G', "", 0)
	if m.helpOffset > len(m.helpKeys()) {
		t.Fatalf("helpOffset ran away: %d", m.helpOffset)
	}
	m = navKey(m, 'g', "g", 0)
	if m.helpOffset != 0 {
		t.Fatalf("g did not return to the top: %d", m.helpOffset)
	}
}
