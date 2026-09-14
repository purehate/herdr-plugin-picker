package picker

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// agentOptions is navFixture opened on the agents tab with a preview read that
// answers synchronously, so a test can drive the debounce by running the
// returned command rather than sleeping.
func agentOptions() NavOptions {
	o := navFixture()
	o.Agents = []NavItem{{ID: "a1", Label: "one"}, {ID: "a2", Label: "two"}}
	o.AgentRead = func(paneID string, lines int) (string, error) {
		return "output for " + paneID, nil
	}
	o.PreviewDebounce = time.Millisecond
	return o
}

func agentsModel(o NavOptions) navigatorModel {
	m := newNavigatorModel(o)
	m.section = NavAgents
	return m.refilter()
}

func TestAgentPreviewDebouncesAndRenders(t *testing.T) {
	m := agentsModel(agentOptions())
	m.width, m.height = 60, 28
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // onto a2
	m = next.(navigatorModel)
	if cmd == nil {
		t.Fatal("cursor move did not schedule a preview read")
	}
	msg, ok := cmd().(navAgentReadMsg)
	if !ok || msg.target != "a2" || msg.text != "output for a2" {
		t.Fatalf("read = %#v", msg)
	}
	next, _ = m.Update(msg)
	m = next.(navigatorModel)
	if !strings.Contains(m.View().Content, "output for a2") {
		t.Fatalf("preview not drawn:\n%s", m.View().Content)
	}
}

func TestAgentPreviewDiscardsStaleRead(t *testing.T) {
	m := agentsModel(agentOptions())
	m.width, m.height = 60, 28
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // a2
	m = next.(navigatorModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // back to a1
	m = next.(navigatorModel)
	next, _ = m.Update(navAgentReadMsg{target: "a2", text: "stale a2"})
	m = next.(navigatorModel)
	if strings.Contains(m.View().Content, "stale a2") {
		t.Fatalf("stale read drawn:\n%s", m.View().Content)
	}
}

func TestAgentPreviewFrameFitsAndReservesSpace(t *testing.T) {
	o := agentOptions()
	o.Agents = nil
	for i := 0; i < 20; i++ {
		o.Agents = append(o.Agents, NavItem{ID: fmt.Sprint(i), Label: fmt.Sprintf("agent %d", i)})
	}
	m := agentsModel(o)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 28})
	m = next.(navigatorModel)
	// The block is reserved before any read lands, so the list does not jump.
	if got := m.previewBlockLines(); got != 1+defaultPreviewLines {
		t.Fatalf("reserved preview lines = %d, want %d", got, 1+defaultPreviewLines)
	}
	next, _ = m.Update(navAgentReadMsg{target: m.selectedItemID(), text: "line1\nline2\nline3"})
	m = next.(navigatorModel)
	view := m.View().Content
	if got := len(strings.Split(view, "\n")); got != 28 {
		t.Fatalf("frame height = %d, want 28", got)
	}
	for i, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 60 {
			t.Errorf("line %d width = %d", i, lipgloss.Width(line))
		}
	}
}

func TestPromptOpensTypesAndSends(t *testing.T) {
	var gotTarget, gotText string
	o := navFixture()
	o.Agents = []NavItem{{ID: "a1", Label: "one"}}
	o.AgentPrompt = func(paneID, text string) (string, error) {
		gotTarget, gotText = paneID, text
		return "working", nil
	}
	m := agentsModel(o)
	m.width, m.height = 60, 28
	m = navKey(m, 'p', "", tea.ModCtrl)
	if !m.inputOpen {
		t.Fatal("^p did not open the prompt")
	}
	for _, r := range "go" {
		m = navKey(m, r, string(r), 0)
	}
	if m.query != "" {
		t.Fatalf("prompt typing leaked into the search query: %q", m.query)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if m.inputOpen || cmd == nil {
		t.Fatalf("enter did not submit: inputOpen=%v cmd=%v", m.inputOpen, cmd)
	}
	msg := cmd().(navPromptMsg)
	if msg.target != "a1" || gotTarget != "a1" || gotText != "go" || msg.status != "working" {
		t.Fatalf("sent %q to %q, msg %#v", gotText, gotTarget, msg)
	}
	next, _ = m.Update(msg)
	m = next.(navigatorModel)
	if !strings.Contains(m.View().Content, "sent · working") {
		t.Fatalf("status not shown:\n%s", m.View().Content)
	}
}

func TestPromptEscCancels(t *testing.T) {
	o := navFixture()
	o.Agents = []NavItem{{ID: "a1", Label: "one"}}
	o.AgentPrompt = func(string, string) (string, error) { return "working", nil }
	m := agentsModel(o)
	m = navKey(m, 'p', "", tea.ModCtrl)
	m = navKey(m, 'x', "x", 0)
	m = navKey(m, tea.KeyEsc, "", 0)
	if m.inputOpen || m.inputText != "" {
		t.Fatalf("esc left inputOpen=%v input=%q", m.inputOpen, m.inputText)
	}
}

func TestPromptEmptyInputDoesNotSend(t *testing.T) {
	called := false
	o := navFixture()
	o.Agents = []NavItem{{ID: "a1", Label: "one"}}
	o.AgentPrompt = func(string, string) (string, error) { called = true; return "working", nil }
	m := agentsModel(o)
	m = navKey(m, 'p', "", tea.ModCtrl)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	if cmd != nil || called {
		t.Fatalf("empty prompt sent: cmd=%v called=%v", cmd, called)
	}
	if m.inputOpen {
		t.Fatal("empty prompt left the input open")
	}
}

func TestPromptInertOffAgentsTabAndWithoutCallback(t *testing.T) {
	o := navFixture()
	o.AgentPrompt = func(string, string) (string, error) { return "working", nil }
	m := newNavigatorModel(o) // spaces
	m = navKey(m, 'p', "", tea.ModCtrl)
	if m.inputOpen {
		t.Fatal("^p opened the prompt on the spaces tab")
	}

	o2 := navFixture()
	o2.Agents = []NavItem{{ID: "a1", Label: "one"}}
	m2 := agentsModel(o2) // AgentPrompt nil
	m2 = navKey(m2, 'p', "", tea.ModCtrl)
	if m2.inputOpen {
		t.Fatal("^p opened the prompt with no AgentPrompt callback")
	}
}

func TestPromptErrorIsSurfaced(t *testing.T) {
	o := navFixture()
	o.Agents = []NavItem{{ID: "a1", Label: "one"}}
	o.AgentPrompt = func(string, string) (string, error) {
		return "", errors.New("agent a1 is blocked")
	}
	m := agentsModel(o)
	m.width, m.height = 90, 28
	m = navKey(m, 'p', "", tea.ModCtrl)
	for _, r := range "hi" {
		m = navKey(m, r, string(r), 0)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	next, _ = m.Update(cmd().(navPromptErrMsg))
	m = next.(navigatorModel)
	if !m.noteErr || !strings.Contains(m.View().Content, "agent a1 is blocked") {
		t.Fatalf("error not surfaced: %v\n%s", m.note, m.View().Content)
	}
}

func TestPromptNoteIsScopedToItsAgent(t *testing.T) {
	o := navFixture()
	o.Agents = []NavItem{{ID: "a1", Label: "one"}, {ID: "a2", Label: "two"}}
	o.AgentPrompt = func(string, string) (string, error) { return "working", nil }
	m := agentsModel(o)
	m.width, m.height = 90, 28
	m = navKey(m, 'p', "", tea.ModCtrl) // targets a1
	for _, r := range "hi" {
		m = navKey(m, r, string(r), 0)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(navigatorModel)
	next, _ = m.Update(navPromptMsg{target: "a1", status: "working"})
	m = next.(navigatorModel)
	if !strings.Contains(m.View().Content, "sent · working") {
		t.Fatalf("note not shown on its own agent:\n%s", m.View().Content)
	}
	// Move to a2: a1's note must not follow.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(navigatorModel)
	if strings.Contains(m.View().Content, "sent · working") {
		t.Fatalf("a1's note shown under a2:\n%s", m.View().Content)
	}
	// Back to a1: the note is there again.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = next.(navigatorModel)
	if !strings.Contains(m.View().Content, "sent · working") {
		t.Fatalf("a1's note did not return on a1:\n%s", m.View().Content)
	}
}

func TestPreviewTextLinesStripsControlsAndTrailingBlanks(t *testing.T) {
	got := previewTextLines("a\x1b[31mb\nc\r\nd\x07\n\n")
	want := []string{"a[31mb", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("previewTextLines = %q, want %q", got, want)
	}
}
