package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const callerFile = "caller.json"

// caller is the pane the operator triggered the picker from. The overlay needs
// it to know where to place a split.
type caller struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

// currentCaller reads the herdr context this process was launched with.
func currentCaller() caller {
	return caller{
		PaneID:      os.Getenv("HERDR_PANE_ID"),
		TabID:       os.Getenv("HERDR_TAB_ID"),
		WorkspaceID: os.Getenv("HERDR_WORKSPACE_ID"),
	}
}

func writeCaller(stateDir string, c caller) error {
	if stateDir == "" {
		return errors.New("HERDR_PLUGIN_STATE_DIR is not set")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, callerFile), raw, 0o600)
}

// readCaller returns the zero value when the file is missing or unusable. A
// missing caller costs two things, neither of them the picker: the recorded
// placement, and the already-current focus skip on the reuse path — FocusPane
// compares the target's workspace and tab against "", which matches nothing, so
// both focus steps fire. Firing them is the only correct fallback with no
// caller recorded; it is a flicker, not a wrong result.
func readCaller(stateDir string) caller {
	if stateDir == "" {
		return caller{}
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, callerFile))
	if err != nil {
		return caller{}
	}
	var c caller
	if err := json.Unmarshal(raw, &c); err != nil {
		return caller{}
	}
	return c
}
