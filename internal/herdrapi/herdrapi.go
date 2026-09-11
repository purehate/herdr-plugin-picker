// Package herdrapi wraps the herdr CLI. Every call goes through one Runner so
// the whole plugin is testable without a live herdr socket.
package herdrapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Runner executes a herdr subcommand and returns its stdout.
type Runner func(args []string) ([]byte, error)

// Client talks to herdr through a Runner.
type Client struct {
	Run Runner
}

// CLIError carries the argv and output of a failed herdr call. The output is
// the only useful diagnostic when the socket or a flag is wrong.
type CLIError struct {
	Args   []string
	Output string
	Err    error
}

func (e *CLIError) Error() string {
	return fmt.Sprintf("herdr %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Output))
}

func (e *CLIError) Unwrap() error { return e.Err }

// New returns a Client bound to the herdr binary the plugin was launched with.
func New() Client {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	return Client{Run: func(args []string) ([]byte, error) {
		cmd := exec.Command(bin, args...)
		out, err := cmd.Output()
		if err == nil {
			return out, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return append(out, exitErr.Stderr...), err
		}
		return out, err
	}}
}

// Pane is the subset of herdr's PaneInfo the picker needs; the other fields are
// ignored. Label is a pointer so "no label" stays distinguishable from a label
// of "": herdr omits the key on an unlabeled pane, and a plain string would
// collapse the two cases and make FindLabeled("") match every unlabeled pane.
type Pane struct {
	PaneID      string  `json:"pane_id"`
	TabID       string  `json:"tab_id"`
	WorkspaceID string  `json:"workspace_id"`
	Label       *string `json:"label"`
}

type envelope struct {
	Result json.RawMessage `json:"result"`
}

func (c Client) call(args ...string) ([]byte, error) {
	out, err := c.Run(args)
	if err != nil {
		return nil, &CLIError{Args: args, Output: string(out), Err: err}
	}
	return out, nil
}

func (c Client) callJSON(target any, args ...string) error {
	out, err := c.call(args...)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("herdr %s: decode envelope: %w", strings.Join(args, " "), err)
	}
	if err := json.Unmarshal(env.Result, target); err != nil {
		return fmt.Errorf("herdr %s: decode result: %w", strings.Join(args, " "), err)
	}
	return nil
}

// PaneList returns every pane herdr knows about, across all workspaces.
func (c Client) PaneList() ([]Pane, error) {
	var result struct {
		Panes []Pane `json:"panes"`
	}
	// No --json flag: JSON is herdr's only output format and `pane list` rejects
	// the flag with `unknown option: --json` (exit 2).
	if err := c.callJSON(&result, "pane", "list"); err != nil {
		return nil, err
	}
	return result.Panes, nil
}

// FindLabeled returns the first pane carrying label.
func FindLabeled(panes []Pane, label string) (Pane, bool) {
	for _, p := range panes {
		if p.Label != nil && *p.Label == label {
			return p, true
		}
	}
	return Pane{}, false
}

// PaneRename sets a pane's label. The label is how the picker recognizes a pane
// it opened earlier.
func (c Client) PaneRename(paneID, label string) error {
	_, err := c.call("pane", "rename", paneID, label)
	return err
}

// PaneClose closes a plugin-owned pane. Verified signature: `herdr plugin pane
// close <PANE_ID>` — it takes a pane id, not an entrypoint name.
func (c Client) PaneClose(paneID string) error {
	_, err := c.call("plugin", "pane", "close", paneID)
	return err
}

// FocusPane moves the operator's view to p. herdr's `pane focus` only accepts a
// direction, so reaching an arbitrary pane means walking workspace → tab → pane.
// Steps that are already current are skipped: refocusing the current workspace
// is a visible flicker for no gain.
func (c Client) FocusPane(p Pane, currentWorkspace, currentTab string) error {
	if p.WorkspaceID != "" && p.WorkspaceID != currentWorkspace {
		if _, err := c.call("workspace", "focus", p.WorkspaceID); err != nil {
			return err
		}
	}
	if p.TabID != "" && p.TabID != currentTab {
		if _, err := c.call("tab", "focus", p.TabID); err != nil {
			return err
		}
	}
	_, err := c.call("plugin", "pane", "focus", p.PaneID)
	return err
}

// OpenOpts describes a pane to open from a declared plugin entrypoint.
//
// Sizing (--width/--height) is popup-only and has no field here on purpose: this
// API opens no popups, so a guard for it would never be exercised. The floating
// box the picker draws in is a popup, but it comes from a `type = "popup"`
// keybinding, not from this call.
//
// Both `popup` and `overlay` parse as Placement values; `herdr plugin pane
// --help` under-reports the list and the bare usage line is accurate. Do not
// prune the field to match --help.
type OpenOpts struct {
	Plugin     string
	Entrypoint string
	Placement  string // overlay | popup | split | tab | zoomed
	TargetPane string // sent only where PlacementTargetsPane; the others reject it
	Direction  string // right | down; only meaningful for a split
	Env        map[string]string
	Focus      bool
}

// PlacementTargetsPane reports whether placement accepts a --target-pane. Only
// split and zoomed target an existing pane; overlay and popup target the active
// one, and a tab takes a workspace id instead. This is the one definition of
// that set — callers ask here rather than restating the list.
func PlacementTargetsPane(placement string) bool {
	return placement == "split" || placement == "zoomed"
}

// PluginPaneOpen launches one of this plugin's pane entrypoints.
func (c Client) PluginPaneOpen(o OpenOpts) error {
	args := []string{"plugin", "pane", "open", "--plugin", o.Plugin, "--entrypoint", o.Entrypoint}
	if o.Placement != "" {
		args = append(args, "--placement", o.Placement)
	}
	// Enforcing the rule here, not in the callers: they pass the caller's pane
	// id unconditionally and let placement decide whether it reaches the wire.
	// A caller consulting PlacementTargetsPane is deciding whether the id is
	// worth computing, which is a different question.
	if o.TargetPane != "" && PlacementTargetsPane(o.Placement) {
		args = append(args, "--target-pane", o.TargetPane)
	}
	if o.Placement == "split" && o.Direction != "" {
		args = append(args, "--direction", o.Direction)
	}
	keys := make([]string, 0, len(o.Env))
	for k := range o.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--env", k+"="+o.Env[k])
	}
	if o.Focus {
		args = append(args, "--focus")
	}
	_, err := c.call(args...)
	return err
}
