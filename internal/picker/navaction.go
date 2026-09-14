package picker

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// navaction.go is the ^x action menu: the list of actions for a row, the
// one-line input a rename collects, and the y/n a close asks for. What the
// actions are and what they do lives with the caller; the picker owns only the
// flow, so a new action needs no change here.

// NavAction is one row of the ^x menu. Input, Confirm, and Copy each change how
// the picker runs it; the zero value runs immediately.
type NavAction struct {
	ID    string
	Label string
	// Input, when non-empty, is the label of a one-line text prompt shown before
	// the action runs; the typed text is passed to RunAction.
	Input string
	// Confirm, when non-empty, is the question asked before the action runs.
	Confirm string
	// Copy, when non-empty, is placed on the system clipboard with OSC 52
	// instead of calling RunAction.
	Copy string
}

func (m navigatorModel) cursorItem() (NavItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return NavItem{}, false
	}
	return m.items[m.cursor], true
}

// selectedItemID is the id of the row under the cursor, or "" when there is
// none. It is what scopes a footer note to the row it belongs to.
func (m navigatorModel) selectedItemID() string {
	item, ok := m.cursorItem()
	if !ok {
		return ""
	}
	return item.ID
}

// openMenu builds the ^x menu for the cursor row. The row is captured here, so
// a live refresh moving the cursor cannot retarget an action the operator is
// about to run.
func (m navigatorModel) openMenu() (tea.Model, tea.Cmd) {
	if m.opts.Actions == nil || m.section == NavSSH {
		return m, nil
	}
	item, ok := m.cursorItem()
	if !ok {
		return m, nil
	}
	actions := m.opts.Actions(m.section, item)
	if len(actions) == 0 {
		return m, nil
	}
	m.menuActions = actions
	m.menuCursor = 0
	m.menuItem = item
	m.menuSection = m.section
	m.note = ""
	return m, nil
}

func (m navigatorModel) menuKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyEsc:
		m.menuActions = nil
		return m, nil
	case tea.KeyUp:
		if m.menuCursor > 0 {
			m.menuCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.menuCursor < len(m.menuActions)-1 {
			m.menuCursor++
		}
		return m, nil
	case tea.KeyEnter:
		return m.runMenuAction()
	}
	return m, nil
}

// runMenuAction dispatches the highlighted action to whichever mode it needs.
// The row and section were captured when the menu opened, so the action applies
// to the row the operator saw.
func (m navigatorModel) runMenuAction() (tea.Model, tea.Cmd) {
	action := m.menuActions[m.menuCursor]
	item := m.menuItem
	section := m.menuSection
	m.menuActions = nil
	switch {
	case action.Copy != "":
		m.note = "copied " + action.Label
		m.noteFor = item.ID
		m.noteErr = false
		return m, tea.SetClipboard(action.Copy)
	case action.Confirm != "":
		m.confirmOpen = true
		m.confirmAction = action
		m.confirmItem = item
		m.confirmSection = section
		m.confirmQuestion = action.Confirm
		return m, nil
	case action.Input != "":
		m.inputOpen = true
		m.inputLabel = action.Input
		m.inputText = ""
		m.inputTarget = item.ID
		m.inputAction = action.ID
		m.inputItem = item
		m.inputSection = section
		return m, nil
	default:
		return m, m.actionCmd(section, item, action.ID, "")
	}
}

func (m navigatorModel) confirmKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if k.Mod&tea.ModCtrl != 0 {
		if k.Code == 'c' {
			return m, tea.Quit
		}
		return m, nil
	}
	switch k.Text {
	case "y", "Y":
		action := m.confirmAction
		item := m.confirmItem
		section := m.confirmSection
		m.confirmOpen = false
		return m, m.actionCmd(section, item, action.ID, "")
	case "n", "N":
		m.confirmOpen = false
		return m, nil
	}
	if k.Code == tea.KeyEsc {
		m.confirmOpen = false
	}
	return m, nil
}

// inputKey routes keys into the one-line input while it is open. Esc cancels and
// ^c still quits; every other printable rune is appended, so the search query
// and the section keys stay out of the way until the input closes.
func (m navigatorModel) inputKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyEsc:
		m.inputOpen = false
		m.inputText = ""
		return m, nil
	case tea.KeyEnter:
		return m.submitInput()
	case tea.KeyBackspace:
		if r := []rune(m.inputText); len(r) > 0 {
			m.inputText = string(r[:len(r)-1])
		}
		return m, nil
	}
	if k.Mod&tea.ModCtrl != 0 {
		if k.Code == 'c' {
			return m, tea.Quit
		}
		return m, nil
	}
	if k.Text != "" {
		m.inputText += k.Text
	}
	return m, nil
}

// submitInput runs whatever the open input was for. An empty input is a no-op
// rather than a cancel: ^p then enter by accident must not send a blank line or
// rename a row to nothing.
func (m navigatorModel) submitInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.inputText)
	target := m.inputTarget
	action := m.inputAction
	item := m.inputItem
	section := m.inputSection
	m.inputOpen = false
	if text == "" {
		return m, nil
	}
	if action == "" {
		m.note = "sending…"
		m.noteFor = target
		m.noteErr = false
		return m, m.promptCmd(target, text)
	}
	return m, m.actionCmd(section, item, action, text)
}

// openPromptInput starts the ^p agent prompt. The target is captured now, so
// moving the cursor while typing cannot redirect the text to a different agent.
func (m navigatorModel) openPromptInput() (tea.Model, tea.Cmd) {
	if m.section != NavAgents || m.opts.AgentPrompt == nil {
		return m, nil
	}
	target := m.selectedItemID()
	if target == "" {
		return m, nil
	}
	m.menuActions = nil
	m.inputOpen = true
	m.inputLabel = "prompt"
	m.inputText = ""
	m.inputTarget = target
	m.inputAction = ""
	m.inputItem = NavItem{}
	m.inputSection = m.section
	m.note = ""
	return m, nil
}

// navActionMsg carries an executed action's status, and navActionErrMsg its
// failure, both tagged with the row so the note is shown under that row.
type navActionMsg struct {
	forID  string
	status string
}
type navActionErrMsg struct {
	forID string
	err   error
}

func (m navigatorModel) actionCmd(section NavSection, item NavItem, actionID, text string) tea.Cmd {
	run := m.opts.RunAction
	forID := item.ID
	return func() tea.Msg {
		status, err := run(section, item, actionID, text)
		if err != nil {
			return navActionErrMsg{forID: forID, err: err}
		}
		return navActionMsg{forID: forID, status: status}
	}
}

// renderMenu draws the action list in place of the rows. It returns at most rows
// lines, so a menu taller than the pane is clipped rather than pushing the
// footer off the bottom.
func (m navigatorModel) renderMenu(s styles, selected lipgloss.Style, rows, w int) []string {
	out := []string{frameIndent + s.muted.Render("actions · ") + s.text.Render(oneLine(m.menuItem.Label))}
	for i, a := range m.menuActions {
		if i >= rows-1 {
			break
		}
		label := a.Label
		switch {
		case a.Input != "":
			label += "…"
		case a.Confirm != "":
			label += " (confirm)"
		}
		pointer := frameIndent + "  "
		if i == m.menuCursor {
			pointer = frameIndent + "▸ "
		}
		line := pointer + label
		if i == m.menuCursor {
			line += strings.Repeat(" ", max(0, w-lipgloss.Width(line)))
			out = append(out, selected.Render(line))
		} else {
			out = append(out, s.text.Render(line))
		}
	}
	return out
}
