package herdrapi

// Snapshot is the current server's navigation inventory. The picker needs
// metadata only; pane contents and agent conversations are never fetched.
type Snapshot struct {
	Workspaces []WorkspaceInfo `json:"workspaces"`
	Tabs       []TabInfo       `json:"tabs"`
	Agents     []AgentInfo     `json:"agents"`
}

type WorkspaceInfo struct {
	ID        string `json:"workspace_id"`
	Label     string `json:"label"`
	Status    string `json:"agent_status"`
	TabCount  int    `json:"tab_count"`
	PaneCount int    `json:"pane_count"`
	Focused   bool   `json:"focused"`
}

type TabInfo struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Status      string `json:"agent_status"`
	PaneCount   int    `json:"pane_count"`
	Focused     bool   `json:"focused"`
}

type AgentInfo struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Name        string `json:"agent"`
	Status      string `json:"agent_status"`
	Title       string `json:"terminal_title_stripped"`
	CWD         string `json:"foreground_cwd"`
	Focused     bool   `json:"focused"`
}

func (c Client) Snapshot() (Snapshot, error) {
	var result struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.callJSON(&result, "api", "snapshot"); err != nil {
		return Snapshot{}, err
	}
	return result.Snapshot, nil
}

func (c Client) FocusWorkspace(id string) error {
	_, err := c.call("workspace", "focus", id)
	return err
}

func (c Client) FocusTab(id string) error {
	_, err := c.call("tab", "focus", id)
	return err
}

func (c Client) FocusAgent(paneID string) error {
	_, err := c.call("agent", "focus", paneID)
	return err
}
