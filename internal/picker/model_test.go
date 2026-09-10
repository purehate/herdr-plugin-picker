package picker

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// corpus comes from rank_test.go — same package.
func newTestModel() model {
	return newModel(Options{Hosts: corpus, Theme: theme.Default(), ShowPreview: true})
}

func press(m model, k tea.KeyPressMsg) model {
	next, _ := m.Update(k)
	return next.(model)
}

func typeRunes(m model, s string) model {
	for _, r := range s {
		m = press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func ctrl(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

// pressCmd, assertQuit and isQuit live in update_cmd_test.go, with the rest of
// the Cmd-disposition assertions.

func TestTypingFiltersAndResetsCursor(t *testing.T) {
	m := newTestModel()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}

	m = typeRunes(m, "dev")
	if m.query != "dev" {
		t.Fatalf("query = %q, want dev", m.query)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after refiltering", m.cursor)
	}
	if len(m.view) != 4 {
		t.Errorf("view = %v, want 4 matches", aliases(m.view))
	}
}

func TestBackspaceTrimsQuery(t *testing.T) {
	m := typeRunes(newTestModel(), "dev")
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "de" {
		t.Fatalf("query = %q, want de", m.query)
	}
	// "de" matches the same 4 hosts as "dev" (devbox and dev both still match),
	// so a bare count would stay 4 even if backspace forgot to refilter. Only
	// the order tells them apart: "de" drops dev's exact-match tier, so devbox
	// (still an alias-prefix match) sorts ahead of it.
	if want := []string{"devbox", "dev", "nixos-dev", "prod-web"}; !equal(aliases(m.view), want) {
		t.Fatalf("view = %v, want %v", aliases(m.view), want)
	}
	// Backspace on an empty query is a no-op, not a crash.
	m.query = ""
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
}

func TestCursorClampsAtBothEnds(t *testing.T) {
	m := newTestModel()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 at the top", m.cursor)
	}
	for range corpus {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.cursor != len(corpus)-1 {
		t.Errorf("cursor = %d, want %d at the bottom", m.cursor, len(corpus)-1)
	}
	m = press(m, ctrl('k'))
	if m.cursor != len(corpus)-2 {
		t.Errorf("ctrl+k did not move the cursor up: %d", m.cursor)
	}
	m = press(m, ctrl('j'))
	if m.cursor != len(corpus)-1 {
		t.Errorf("ctrl+j did not move the cursor down: %d", m.cursor)
	}
}

func TestPlacementKeys(t *testing.T) {
	tests := []struct {
		name      string
		key       tea.KeyPressMsg
		placement string
		forceNew  bool
	}{
		{"enter splits", tea.KeyPressMsg{Code: tea.KeyEnter}, "split", false},
		{"ctrl+t opens a tab", ctrl('t'), "tab", false},
		{"ctrl+z zooms", ctrl('z'), "zoomed", false},
		{"ctrl+n forces a new split", ctrl('n'), "split", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := typeRunes(newTestModel(), "nixos")
			m, cmd := pressCmd(m, tc.key)
			if m.chosen == nil {
				t.Fatal("chosen = nil, want a selection")
			}
			if m.chosen.Host.Alias != "nixos-dev" {
				t.Errorf("alias = %q, want nixos-dev", m.chosen.Host.Alias)
			}
			if m.chosen.Placement != tc.placement {
				t.Errorf("placement = %q, want %q", m.chosen.Placement, tc.placement)
			}
			if m.chosen.ForceNew != tc.forceNew {
				t.Errorf("forceNew = %v, want %v", m.chosen.ForceNew, tc.forceNew)
			}
			assertQuit(t, cmd)
		})
	}
}

func TestSelectingWithNoMatchesIsIgnored(t *testing.T) {
	m := typeRunes(newTestModel(), "zzzz")
	m, cmd := pressCmd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chosen != nil {
		t.Fatalf("chosen = %+v, want nil when nothing matches", m.chosen)
	}
	if cmd != nil {
		t.Fatal("cmd != nil, want no Cmd (and no quit) when nothing matches")
	}
}

func TestQuitKeysLeaveNoSelection(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, ctrl('c')} {
		m, cmd := pressCmd(newTestModel(), k)
		if m.chosen != nil {
			t.Errorf("chosen = %+v after quit key, want nil", m.chosen)
		}
		if !m.quitting {
			t.Error("quitting = false, want true")
		}
		assertQuit(t, cmd)
	}
}

// The opposite direction — that a non-quit key returns no Cmd — is asserted in
// update_cmd_test.go, across every key and model state rather than the four
// keys and one state this file used to check.

func TestCtrlUClearsTheQuery(t *testing.T) {
	// "nixos" matches exactly one host, so the cursor is clamped to 0 before
	// ctrl+u even runs — a broken reset would pass trivially. "dev" matches
	// several, so moving Down first gives the reset something to prove.
	m := typeRunes(newTestModel(), "dev")
	if len(m.view) < 2 {
		t.Fatalf("test setup needs a query matching at least 2 hosts, got %v", aliases(m.view))
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor == 0 {
		t.Fatal("cursor did not move off 0 before ctrl+u, so the reset below proves nothing")
	}

	m = press(m, ctrl('u'))
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
	if len(m.view) != len(corpus) {
		t.Errorf("view = %v, want the whole list back", aliases(m.view))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestCtrlWDeletesTheLastWord(t *testing.T) {
	m := typeRunes(newTestModel(), "nixos-dev")
	m = press(m, ctrl('w'))
	if m.query != "nixos-" {
		t.Fatalf("query = %q, want \"nixos-\" — the separator stays, as in fzf", m.query)
	}
	if want := []string{"nixos-dev"}; !equal(aliases(m.view), want) {
		t.Errorf("view = %v, want %v", aliases(m.view), want)
	}
	// A second press eats the separator and the word in front of it.
	m = press(m, ctrl('w'))
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
	// "nixos-" also matches only nixos-dev, so this transition (1 match -> the
	// whole corpus) is the one that actually proves refilter ran — the "nixos-"
	// checkpoint above would look identical whether or not it did.
	if len(m.view) != len(corpus) {
		t.Errorf("view = %v, want the whole list back", aliases(m.view))
	}
	// On an already-empty query it is a no-op, not a panic.
	m = press(m, ctrl('w'))
	if m.query != "" {
		t.Fatalf("query = %q, want empty", m.query)
	}
}

func TestCtrlOTogglesPreview(t *testing.T) {
	m := newTestModel()
	if !m.preview {
		t.Fatal("preview = false, want the configured default of true")
	}
	m = press(m, ctrl('o'))
	if m.preview {
		t.Error("preview = true, want false after toggle")
	}
	m = press(m, ctrl('o'))
	if !m.preview {
		t.Error("preview = false, want true after a second toggle")
	}
}

func TestProbeResultsMarkHostsUp(t *testing.T) {
	m := newTestModel()
	next, _ := m.Update(probeMsg{Alias: "nixos-dev", Up: true})
	m = next.(model)
	if !m.up["nixos-dev"] {
		t.Error("nixos-dev not marked up")
	}
	if !m.probed["nixos-dev"] {
		t.Error("nixos-dev not marked probed")
	}
	if m.probed["alpha"] {
		t.Error("alpha marked probed without a result")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	next, _ := newTestModel().Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m := next.(model)
	if m.width != 100 || m.height != 40 {
		t.Fatalf("size = %dx%d, want 100x40", m.width, m.height)
	}
}

func TestProbeChannelDrainsWithoutBlocking(t *testing.T) {
	ch := make(chan probe.Result, 1)
	ch <- probe.Result{Alias: "alpha", Up: true}
	close(ch)

	m := newModel(Options{Hosts: corpus, Theme: theme.Default(), Probes: ch})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command, want a probe wait")
	}

	// Run the Cmd Init actually handed back, rather than hand-constructing the
	// message we expect — that would pass even if waitProbe never read ch.
	msg := cmd()
	pm, ok := msg.(probeMsg)
	if !ok {
		t.Fatalf("Init's cmd produced %T, want probeMsg", msg)
	}
	if pm != (probeMsg{Alias: "alpha", Up: true}) {
		t.Fatalf("probeMsg = %+v, want {Alias:alpha Up:true}", pm)
	}

	next, cmd := m.Update(pm)
	m = next.(model)
	if !m.up["alpha"] || !m.probed["alpha"] {
		t.Fatal("probeMsg did not mark alpha up and probed")
	}
	if cmd == nil {
		t.Fatal("Update did not re-arm the probe wait")
	}

	// The channel is now empty and closed, so running the re-armed Cmd for
	// real — not just asserting a hand-built probeClosedMsg{} — is what proves
	// waitProbe actually notices the close instead of blocking forever.
	msg = cmd()
	if _, ok := msg.(probeClosedMsg); !ok {
		t.Fatalf("re-armed cmd produced %T, want probeClosedMsg now the channel is closed", msg)
	}

	next, _ = m.Update(probeClosedMsg{})
	if next.(model).chosen != nil {
		t.Error("probeClosedMsg produced a selection")
	}
}

// TestQueryEditingIsRuneSafe uses a host of its own rather than adding a
// multi-byte alias to the shared corpus in rank_test.go — that corpus backs
// count assertions elsewhere (e.g. len(m.view) == len(corpus)) that a new
// fixture would silently change.
func TestQueryEditingIsRuneSafe(t *testing.T) {
	hosts := []sshconfig.Host{{Alias: "café-dëv", HostName: "10.0.0.1", Port: "22"}}
	m := newModel(Options{Hosts: hosts, Theme: theme.Default()})

	m = typeRunes(m, "café-dëv")
	if m.query != "café-dëv" {
		t.Fatalf("query = %q, want café-dëv", m.query)
	}

	// v is one byte, so dropping it proves nothing about rune-vs-byte slicing —
	// a buggy byte-slice backspace would look identical here.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "café-dë" {
		t.Fatalf("query = %q, want café-dë", m.query)
	}

	// ë is two bytes. A byte-slice backspace would drop only ë's second byte,
	// leaving a mangled "café-d\xc3" instead of the whole rune gone.
	m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "café-d" {
		t.Fatalf("query = %q, want café-d — backspace must drop the whole rune ë, not one byte of it", m.query)
	}

	m = press(m, ctrl('w'))
	if m.query != "café-" {
		t.Fatalf("query = %q, want café-", m.query)
	}
}
