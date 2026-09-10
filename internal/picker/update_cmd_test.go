package picker

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// This file asserts on the tea.Cmd half of Update's return. The rest of the
// package's tests take the model and discard the Cmd.
//
// model.go returns a literal Cmd from eighteen sites. Three are tea.Quit — esc,
// ctrl+c, and a choose that found a row — and the other fifteen are nil or the
// probe re-arm. Line coverage cannot tell a correct one from a wrong one,
// because at every site the line runs; only its second value is thrown away. The
// bug that hides in the gap is a stray tea.Quit on a keystroke that should be
// inert: the picker would exit mid-typing, and every model-level assertion in
// model_test.go would stay green while it happened.
//
// The matrix crosses every key the picker handles with every model state its
// branches turn on, so a Cmd that appears where none belongs is caught whichever
// key and whichever state grew it.
//
// One site is deliberately not here: the probe re-arm (Update's probeMsg arm)
// is the only Cmd that is neither nil nor tea.Quit, it needs a channel fixture
// to be runnable, and TestProbeChannelDrainsWithoutBlocking already runs it and
// identifies what it produces. Every state below has a nil Probes channel.

// unhandledMsg is a message type Update has no arm for, so it reaches the
// trailing return that every unrecognised message falls through to.
type unhandledMsg struct{}

// cmdDisposition is what Update's second return value should be, at the only
// granularity that is honestly decidable. Cmds are closures: two of them cannot
// be compared for equality, so the answerable questions are "is there one at
// all" and "what does running it produce".
type cmdDisposition int

const (
	cmdNone cmdDisposition = iota // Update returned no command
	cmdQuit                       // ... returned one that produces tea.QuitMsg
)

func (d cmdDisposition) String() string {
	if d == cmdQuit {
		return "a Cmd producing tea.QuitMsg"
	}
	return "no Cmd"
}

// quitPolicy declares, per key, when that key is meant to end the program. It is
// written down as data rather than derived from the model, because a matrix that
// computes its expectations the way the implementation does cannot disagree with
// the implementation when the implementation is wrong.
type quitPolicy int

const (
	neverQuits quitPolicy = iota
	alwaysQuits
	quitsIfRowUnderCursor
)

func (p quitPolicy) want(rowUnderCursor bool) cmdDisposition {
	switch p {
	case alwaysQuits:
		return cmdQuit
	case quitsIfRowUnderCursor:
		if rowUnderCursor {
			return cmdQuit
		}
	}
	return cmdNone
}

// pressCmd is press, but it also hands back the Cmd Update returned instead of
// discarding it — tests that care whether a key quits the program need the
// actual Cmd, not just the model's quitting field.
func pressCmd(m model, k tea.KeyPressMsg) (model, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(model), cmd
}

// assertQuit fails unless cmd, when run, produces tea.QuitMsg. A key that only
// sets quitting=true without returning tea.Quit would leave bubbletea's own
// runtime loop running forever — quitting is a UI flag, tea.Quit is what
// actually stops the program.
func assertQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd = nil, want tea.Quit")
	}
	if msg := cmd(); !isQuit(msg) {
		t.Fatalf("cmd produced %T, want tea.QuitMsg", msg)
	}
}

func isQuit(msg tea.Msg) bool {
	_, ok := msg.(tea.QuitMsg)
	return ok
}

// assertCmd checks Update's second return value against want.
//
// The nil check is exact, and worth being explicit about. tea.Cmd is a defined
// func type (`type Cmd func() Msg`), not an interface, so comparing one to nil
// asks precisely "does this hold a function". cmd has to stay at type tea.Cmd
// all the way to the comparison: widening it to any — or to tea.Msg, which is an
// alias for an interface — would wrap a nil Cmd in a non-nil interface, and the
// check would stop discriminating, reporting "there is a Cmd" for every cell
// whatever Update returned. TestAssertCmdTakesAConcreteCmdType pins the
// parameter's type, so that refactor fails once, in a place that names the
// helper, instead of in a hundred cells that all accuse model.go.
//
// The cmdNone failure does not run the Cmd it is complaining about. Naming what
// it produced would read better, but a Cmd is arbitrary code — the probe re-arm
// blocks on a channel — and a test that hangs on failure is worse than one with
// a vaguer message.
func assertCmd(t *testing.T, cmd tea.Cmd, want cmdDisposition) {
	t.Helper()
	if want == cmdNone {
		if cmd != nil {
			t.Fatalf("Update returned a Cmd, want %s — a stray tea.Quit here quits the picker mid-keystroke", want)
		}
		return
	}
	if cmd == nil {
		t.Fatalf("Update returned no Cmd, want %s — without it bubbletea's loop never stops", want)
	}
	if msg := cmd(); !isQuit(msg) {
		t.Fatalf("Update's Cmd produced %T, want tea.QuitMsg", msg)
	}
}

// cmdState is a starting model plus the shape the matrix reasons about. rows and
// cursor are asserted before the state is used, so a fixture that drifts — a
// corpus edit, a ranking change — fails loudly instead of quietly testing a
// different state than it claims to.
type cmdState struct {
	name           string
	build          func() model
	rows           int
	cursor         int
	rowUnderCursor bool
}

func cmdStates() []cmdState {
	return []cmdState{{
		// The picker as it opens: no query, nothing filtered, cursor at the top.
		name:  "fresh, cursor at the top",
		build: newTestModel,
		rows:  len(corpus), cursor: 0, rowUnderCursor: true,
	}, {
		// A non-empty query with the cursor moved off zero. The only state where
		// backspace, ctrl+u and ctrl+w take their query-is-non-empty branch and
		// actually refilter.
		name: "query typed, cursor moved down",
		build: func() model {
			return press(typeRunes(newTestModel(), "dev"), tea.KeyPressMsg{Code: tea.KeyDown})
		},
		rows: 4, cursor: 1, rowUnderCursor: true,
	}, {
		// The bottom of the list, where moveCursor(1) hits its clamp and hands
		// back the model untouched. A clamp that returned tea.Quit rather than
		// nil would close the picker on a down-arrow at the last row.
		name: "cursor on the last row",
		build: func() model {
			m := newTestModel()
			for range corpus {
				m = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
			}
			return m
		},
		rows: len(corpus), cursor: len(corpus) - 1, rowUnderCursor: true,
	}, {
		// A query that filters everything out. This is the state that flips the
		// placement keys from tea.Quit to nil — choose finds no row under the
		// cursor and declines — and it clamps cursor movement at both ends,
		// since there is nothing to move over.
		name:  "query matches nothing",
		build: func() model { return typeRunes(newTestModel(), "zzzz") },
		rows:  0, cursor: 0, rowUnderCursor: false,
	}, {
		// ctrl+o's other branch. The toggle is the one key whose behaviour
		// depends on a field nothing else in the matrix reads, so without this
		// state half of it is never exercised.
		name:  "preview toggled off",
		build: func() model { return press(newTestModel(), ctrl('o')) },
		rows:  len(corpus), cursor: 0, rowUnderCursor: true,
	}, {
		// No ssh config at all. The view is empty for a different reason than
		// "query matches nothing" — there is nothing to rank, rather than nothing
		// that ranked — so refilter takes Rank's empty-query path instead of its
		// scoring loop.
		name:  "no hosts configured",
		build: func() model { return newModel(Options{Theme: theme.Default()}) },
		rows:  0, cursor: 0, rowUnderCursor: false,
	}}
}

func TestUpdateCmdMatrix(t *testing.T) {
	keys := []struct {
		name  string
		key   tea.KeyPressMsg
		quits quitPolicy
	}{
		{"esc", tea.KeyPressMsg{Code: tea.KeyEsc}, alwaysQuits},
		{"ctrl+c", ctrl('c'), alwaysQuits},

		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, quitsIfRowUnderCursor},
		{"ctrl+t", ctrl('t'), quitsIfRowUnderCursor},
		{"ctrl+z", ctrl('z'), quitsIfRowUnderCursor},
		{"ctrl+n", ctrl('n'), quitsIfRowUnderCursor},

		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, neverQuits},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, neverQuits},
		{"ctrl+j", ctrl('j'), neverQuits},
		{"ctrl+k", ctrl('k'), neverQuits},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, neverQuits},
		{"printable rune", tea.KeyPressMsg{Code: 'd', Text: "d"}, neverQuits},
		{"ctrl+o", ctrl('o'), neverQuits},
		{"ctrl+u", ctrl('u'), neverQuits},
		{"ctrl+w", ctrl('w'), neverQuits},
		{"unmapped ctrl chord", ctrl('x'), neverQuits},
		// A special key carrying no Text falls past every arm of handleKey's
		// switch and then past the k.Text != "" guard, reaching the same return
		// as a printable rune does by a different route.
		{"unmapped special key", tea.KeyPressMsg{Code: tea.KeyTab}, neverQuits},
	}

	// Update's non-key arms return a Cmd too, and none of them branches on model
	// state — which is the claim, so they run against every state rather than
	// one.
	msgs := []struct {
		name string
		msg  tea.Msg
	}{
		{"window resize", tea.WindowSizeMsg{Width: 100, Height: 40}},
		{"probes finished", probeClosedMsg{}},
		{"unhandled message", unhandledMsg{}},
	}

	for _, st := range cmdStates() {
		t.Run(st.name, func(t *testing.T) {
			assertStateShape(t, st)
			for _, k := range keys {
				t.Run(k.name, func(t *testing.T) {
					_, cmd := pressCmd(st.build(), k.key)
					assertCmd(t, cmd, k.quits.want(st.rowUnderCursor))
				})
			}
			for _, mc := range msgs {
				t.Run(mc.name, func(t *testing.T) {
					_, cmd := st.build().Update(mc.msg)
					assertCmd(t, cmd, cmdNone)
				})
			}
		})
	}
}

// assertStateShape fails unless the fixture really is the state it advertises.
// Without it a state could quietly become a duplicate of another — a corpus edit
// that made "dev" match nothing would turn "query typed" into a second copy of
// "query matches nothing" — and the matrix would report full coverage of a state
// it had stopped visiting.
func assertStateShape(t *testing.T, st cmdState) {
	t.Helper()
	m := st.build()
	if len(m.view) != st.rows {
		t.Fatalf("fixture has %d rows, declares %d", len(m.view), st.rows)
	}
	if m.cursor != st.cursor {
		t.Fatalf("fixture has cursor %d, declares %d", m.cursor, st.cursor)
	}
}

// TestAssertCmdTakesAConcreteCmdType pins the property assertCmd's nil check
// actually rests on: its parameter's static type.
//
// `cmd == nil` is exact for tea.Cmd because tea.Cmd is a defined func type. Widen
// that parameter to any — or to tea.Msg, an alias for an interface — and a nil
// Cmd arrives wrapped in a non-nil interface, so the check stops discriminating
// and reports "there is a Cmd" for every cell, whatever Update returned. The
// suite does go red — but as a hundred matrix cells accusing model.go of a stray
// tea.Quit it never returned. This test is the one that fails naming the cause.
//
// It asserts the signature rather than the language rule. A test that a zero
// func value compares nil, or that a nil func widened to an interface does not,
// cannot fail on any build — those are facts about Go, true before and after the
// refactor, so checking them would guard nothing while looking like it did.
// (staticcheck agrees, and proves the widened comparison never-true statically:
// SA4023.)
func TestAssertCmdTakesAConcreteCmdType(t *testing.T) {
	ft := reflect.TypeOf(assertCmd)
	want := reflect.TypeOf(tea.Cmd(nil))
	found := false
	for i := range ft.NumIn() {
		if ft.In(i) == want {
			found = true
		}
		if ft.In(i).Kind() == reflect.Interface {
			t.Errorf("assertCmd parameter %d is the interface %v; a nil tea.Cmd widened into it compares non-nil, so cmdNone would stop asserting anything",
				i, ft.In(i))
		}
	}
	if !found {
		t.Errorf("assertCmd takes no tea.Cmd parameter; signature is %v", ft)
	}
}
