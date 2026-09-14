package herdrapi

import "testing"

// A pane row carries more than the id and label the reuse lookup needs: the
// panes tab draws the agent, its status, the title, and the directory.
func TestPaneListDecodesTheDisplayFields(t *testing.T) {
	c := Client{Run: func([]string) ([]byte, error) {
		return []byte(`{"id":"cli:pane:list","result":{"panes":[
			{"pane_id":"w4:p1","tab_id":"w4:t1","workspace_id":"w4",
			 "agent":"codex","agent_status":"done","focused":true,
			 "terminal_title_stripped":"trustedsec",
			 "cwd":"/home/a","foreground_cwd":"/home/a/sub"}]}}`), nil
	}}

	panes, err := c.PaneList()
	if err != nil {
		t.Fatalf("PaneList: %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("got %d panes, want 1", len(panes))
	}
	p := panes[0]
	if p.PaneID != "w4:p1" || p.TabID != "w4:t1" || p.WorkspaceID != "w4" {
		t.Errorf("ids = %q/%q/%q", p.PaneID, p.TabID, p.WorkspaceID)
	}
	if p.Agent != "codex" || p.Status != "done" {
		t.Errorf("agent = %q status = %q", p.Agent, p.Status)
	}
	if p.Title != "trustedsec" {
		t.Errorf("Title = %q", p.Title)
	}
	if !p.Focused {
		t.Error("Focused = false, want true")
	}
}

// The foreground directory is where a command would actually run, so it wins
// over the pane's launch directory when herdr reports both.
func TestPaneDirPrefersTheForegroundDirectory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cwd, fg string
		want    string
	}{
		{"foreground wins", "/home/a", "/home/a/sub", "/home/a/sub"},
		{"falls back to cwd", "/home/a", "", "/home/a"},
		{"neither", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{CWD: tc.cwd, ForegroundCWD: tc.fg}
			if got := p.Dir(); got != tc.want {
				t.Errorf("Dir() = %q, want %q", got, tc.want)
			}
		})
	}
}
