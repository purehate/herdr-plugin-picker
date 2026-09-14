package herdrapi

// actions.go is the mutating calls behind the picker's ^x menu: rename, close,
// create, and worktree open. Each returns only an error; the picker shows a
// short status of its own, not the server's echo.

func (c Client) RenameWorkspace(id, label string) error {
	_, err := c.call("workspace", "rename", id, label)
	return err
}

func (c Client) CloseWorkspace(id string) error {
	_, err := c.call("workspace", "close", id)
	return err
}

func (c Client) CreateWorkspace(cwd, label string, focus bool) error {
	args := []string{"workspace", "create"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if label != "" {
		args = append(args, "--label", label)
	}
	if focus {
		args = append(args, "--focus")
	}
	_, err := c.call(args...)
	return err
}

func (c Client) RenameTab(id, label string) error {
	_, err := c.call("tab", "rename", id, label)
	return err
}

func (c Client) CloseTab(id string) error {
	_, err := c.call("tab", "close", id)
	return err
}

func (c Client) CreateTab(workspaceID, cwd, label string, focus bool) error {
	args := []string{"tab", "create"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if label != "" {
		args = append(args, "--label", label)
	}
	if focus {
		args = append(args, "--focus")
	}
	_, err := c.call(args...)
	return err
}

// ClosePane closes an ordinary pane. It is not PaneClose, which closes a pane
// this plugin opened.
func (c Client) ClosePane(id string) error {
	_, err := c.call("pane", "close", id)
	return err
}

func (c Client) RenameAgent(paneID, name string) error {
	_, err := c.call("agent", "rename", paneID, name)
	return err
}

// WorktreeOpen opens the Git worktree that cwd sits in.
func (c Client) WorktreeOpen(cwd string, focus bool) error {
	args := []string{"worktree", "open"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if focus {
		args = append(args, "--focus")
	}
	_, err := c.call(args...)
	return err
}
