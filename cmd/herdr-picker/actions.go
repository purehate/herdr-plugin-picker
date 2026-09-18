package main

import (
	"fmt"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

// actions.go builds and runs the ^x menu. The picker owns the menu, input, and
// confirm UI; this is only what the actions are and what they do, so adding one
// is a case in navActions and a case in the matching runner.

// navActions is the menu for a row. It is per section because the objects
// differ: a workspace can spawn a tab, an agent can be renamed and previewed,
// and an ssh host is not a herdr object at all, so it gets no menu.
func navActions(section picker.NavSection, item picker.NavItem) []picker.NavAction {
	switch section {
	case picker.NavSpaces:
		return []picker.NavAction{
			{ID: "rename", Label: "rename", Input: "rename workspace"},
			{ID: "new-tab", Label: "new tab here"},
			{ID: "new-workspace", Label: "new workspace"},
			{ID: "close", Label: "close workspace", Confirm: "close this workspace?"},
			{ID: "copy-id", Label: "copy id", Copy: item.ID},
		}
	case picker.NavAgents:
		actions := []picker.NavAction{
			{ID: "rename", Label: "rename agent", Input: "rename agent"},
			{ID: "new-tab", Label: "new tab here"},
			{ID: "close", Label: "close pane", Confirm: "close this pane?"},
			{ID: "copy-id", Label: "copy pane id", Copy: item.ID},
		}
		// cwd-backed actions only where there is a cwd: a workspace row has none,
		// and a copy of "" or a worktree open at "" would both be nonsense.
		if item.CWD != "" {
			actions = append(actions,
				picker.NavAction{ID: "copy-cwd", Label: "copy cwd", Copy: item.CWD},
				picker.NavAction{ID: "worktree", Label: "open git worktree"},
			)
		}
		return actions
	case picker.NavSessions:
		return []picker.NavAction{
			{ID: "rename", Label: "rename tab", Input: "rename tab"},
			{ID: "new-tab", Label: "new tab here"},
			{ID: "close", Label: "close tab", Confirm: "close this tab?"},
			{ID: "copy-id", Label: "copy id", Copy: item.ID},
		}
	case picker.NavMachines:
		// Read-only: the profile catalog belongs to `herdr machine`, so the menu
		// only copies. Copy is handled by the picker, so runNavAction never sees
		// these.
		return []picker.NavAction{
			{ID: "copy-id", Label: "copy id", Copy: item.ID},
			{ID: "copy-target", Label: "copy ssh target", Copy: item.Target},
		}
	default:
		return nil
	}
}

// runNavAction executes one action and returns the footer's status line.
func runNavAction(api herdrapi.Client, section picker.NavSection, item picker.NavItem, actionID, text string) (string, error) {
	switch section {
	case picker.NavSpaces:
		return runWorkspaceAction(api, item, actionID, text)
	case picker.NavAgents:
		return runAgentAction(api, item, actionID, text)
	case picker.NavSessions:
		return runTabAction(api, item, actionID, text)
	}
	return "", fmt.Errorf("no actions for section %d", section)
}

func runWorkspaceAction(api herdrapi.Client, item picker.NavItem, actionID, text string) (string, error) {
	switch actionID {
	case "rename":
		return "renamed", api.RenameWorkspace(item.ID, text)
	case "new-tab":
		return "opened a tab", api.CreateTab(item.ID, "", "", true)
	case "new-workspace":
		return "opened a workspace", api.CreateWorkspace("", "", true)
	case "close":
		return "closed", api.CloseWorkspace(item.ID)
	}
	return "", fmt.Errorf("unknown workspace action %q", actionID)
}

func runAgentAction(api herdrapi.Client, item picker.NavItem, actionID, text string) (string, error) {
	switch actionID {
	case "rename":
		return "renamed", api.RenameAgent(item.ID, text)
	case "new-tab":
		return "opened a tab", api.CreateTab(item.WorkspaceID, item.CWD, "", true)
	case "close":
		return "closed", api.ClosePane(item.ID)
	case "worktree":
		return "opened worktree", api.WorktreeOpen(item.CWD, true)
	}
	return "", fmt.Errorf("unknown agent action %q", actionID)
}

func runTabAction(api herdrapi.Client, item picker.NavItem, actionID, text string) (string, error) {
	switch actionID {
	case "rename":
		return "renamed", api.RenameTab(item.ID, text)
	case "new-tab":
		return "opened a tab", api.CreateTab(item.WorkspaceID, item.CWD, "", true)
	case "close":
		return "closed", api.CloseTab(item.ID)
	}
	return "", fmt.Errorf("unknown tab action %q", actionID)
}
