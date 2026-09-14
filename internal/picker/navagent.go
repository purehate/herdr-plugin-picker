package picker

import "strings"

// navagent.go is the agents tab's preview and prompt: the output tail drawn
// under the list, and the one-line input ^p opens.

func (m navigatorModel) agentPreviewRequestLines() int {
	if m.opts.PreviewLines > 0 {
		return m.opts.PreviewLines
	}
	return defaultPreviewLines
}

// agentPreviewVisible reports whether the agents tab should reserve a preview
// block at all. The block is reserved before the first read lands, so the list
// does not jump every time the cursor moves.
func (m navigatorModel) agentPreviewVisible() bool {
	return m.section == NavAgents && m.opts.AgentRead != nil && m.selectedItemID() != ""
}

// agentPreviewHeight is the preview's body height: the requested line count,
// yielding to the pane when it is too short to hold it. The preview is the block
// that gives way, the same way the ssh preview does.
func (m navigatorModel) agentPreviewHeight() int {
	want := m.agentPreviewRequestLines()
	_, h := m.size()
	if max := h - navHeaderRows - navFooterRows - 3; want > max {
		want = max
	}
	return max(0, want)
}

// agentPreviewLines is the whole block: the separator plus the body.
func (m navigatorModel) agentPreviewLines() int {
	if !m.agentPreviewVisible() {
		return 0
	}
	if height := m.agentPreviewHeight(); height > 0 {
		return 1 + height
	}
	return 0
}

// agentPreviewBody is the text to draw and whether it is a placeholder rather
// than real output, so the caller can mute it.
func (m navigatorModel) agentPreviewBody() ([]string, bool) {
	switch {
	case m.agentPreviewErr != nil:
		return []string{"⚠ " + oneLine(m.agentPreviewErr.Error())}, true
	case m.agentPreview == "":
		return []string{"reading…"}, true
	default:
		return previewTextLines(m.agentPreview), false
	}
}

func (m navigatorModel) renderAgentPreview(s styles) []string {
	if !m.agentPreviewVisible() {
		return nil
	}
	height := m.agentPreviewHeight()
	if height == 0 {
		return nil
	}
	body, muted := m.agentPreviewBody()
	out := []string{s.muted.Render(previewSeparator)}
	for i := 0; i < height; i++ {
		text := ""
		style := s.text
		if i < len(body) {
			text = body[i]
			if muted {
				style = s.muted
			}
		}
		out = append(out, frameIndent+"  "+style.Render(text))
	}
	return out
}

// previewTextLines splits agent output into display lines, dropping trailing
// blank lines so a tail padded with newlines does not render as empty rows. It
// strips control characters first: agent output is arbitrary terminal content,
// and a stray escape sequence could move the cursor or repaint the popup.
func previewTextLines(s string) []string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < ' ' || r == 0x7f:
			return -1
		default:
			return r
		}
	}, s)
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
