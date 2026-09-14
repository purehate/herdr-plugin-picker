package main

import "os"

const (
	callerPaneEnv      = "HERDR_PICKER_CALLER_PANE_ID"
	callerTabEnv       = "HERDR_PICKER_CALLER_TAB_ID"
	callerWorkspaceEnv = "HERDR_PICKER_CALLER_WORKSPACE_ID"
)

// caller is the pane the operator triggered the picker from. The popup needs
// it to know where to place a split.
type caller struct {
	PaneID      string
	TabID       string
	WorkspaceID string
}

// currentCaller reads the herdr context this process was launched with.
func currentCaller() caller {
	return caller{
		PaneID:      os.Getenv("HERDR_PANE_ID"),
		TabID:       os.Getenv("HERDR_TAB_ID"),
		WorkspaceID: os.Getenv("HERDR_WORKSPACE_ID"),
	}
}

// callerEnv carries the action's pane context into the picker process that it
// opens. Keeping the context on that one invocation avoids a shared state file:
// two actions can race, but neither can overwrite the caller belonging to the
// other picker.
func callerEnv(c caller) map[string]string {
	env := map[string]string{}
	if c.PaneID != "" {
		env[callerPaneEnv] = c.PaneID
	}
	if c.TabID != "" {
		env[callerTabEnv] = c.TabID
	}
	if c.WorkspaceID != "" {
		env[callerWorkspaceEnv] = c.WorkspaceID
	}
	return env
}

// pickerCaller reads the invocation-scoped context forwarded by openPicker.
// A direct popup command bypasses openPicker and therefore returns the zero
// value; resolveCaller fills that from HERDR_ACTIVE_* instead.
func pickerCaller() caller {
	return caller{
		PaneID:      os.Getenv(callerPaneEnv),
		TabID:       os.Getenv(callerTabEnv),
		WorkspaceID: os.Getenv(callerWorkspaceEnv),
	}
}
