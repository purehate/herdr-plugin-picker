package picker

import "testing"

func TestNavNameRoundTrip(t *testing.T) {
	for i := NavSection(0); i < navSectionCount; i++ {
		name := NavName(i)
		if name == "" {
			t.Fatalf("section %d has no name", i)
		}
		got, ok := NavSectionByName(name)
		if !ok || got != i {
			t.Fatalf("round trip %d -> %q -> %v/%v", i, name, got, ok)
		}
	}
	if _, ok := NavSectionByName("gone"); ok {
		t.Fatal("an unknown tab name resolved")
	}
	if NavName(NavSection(99)) != "" {
		t.Fatal("an out-of-range section produced a name")
	}
}

// The picker opens on StartSection and reports every later switch through
// OnSection, so the caller — not the picker package — owns the file I/O.
func TestStartSectionAndOnSection(t *testing.T) {
	var seen []NavSection
	o := navFixture()
	o.StartSection = NavPanes
	o.OnSection = func(s NavSection) { seen = append(seen, s) }
	m := newNavigatorModel(o)
	if m.section != NavPanes {
		t.Fatalf("start section = %v, want panes", m.section)
	}
	if len(seen) != 0 {
		t.Fatalf("OnSection fired on init: %v", seen)
	}
	m = m.setSection(NavSSH)
	if len(seen) != 1 || seen[0] != NavSSH {
		t.Fatalf("OnSection = %v, want one ssh", seen)
	}
}
