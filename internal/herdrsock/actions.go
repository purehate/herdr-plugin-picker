package herdrsock

import (
	"encoding/json"
	"fmt"
)

// Action is one verb some installed plugin exposes. The fields are the ones a
// palette needs: what to call it, how to invoke it, and where it is legal.
type Action struct {
	PluginID string   `json:"plugin_id"`
	ActionID string   `json:"action_id"`
	Title    string   `json:"title"`
	Contexts []string `json:"contexts"`
}

// ValidIn reports whether the action may be invoked from the given herdr
// context ("pane", "workspace"). An action that declares no contexts is valid
// everywhere, which is how herdr itself reads an absent list.
func (a Action) ValidIn(context string) bool {
	if len(a.Contexts) == 0 {
		return true
	}
	for _, c := range a.Contexts {
		if c == context {
			return true
		}
	}
	return false
}

// Actions lists the actions of every installed plugin. Omitting plugin_id is
// what widens the answer from one plugin to all of them, and that is the whole
// reason this tab can exist: the herdr CLI has no equivalent, so a palette
// built on subprocesses can only ever offer verbs someone hardcoded.
func (c Client) Actions() ([]Action, error) {
	raw, err := c.callResult("plugin.action.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Actions []Action `json:"actions"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("herdr api: decode plugin.action.list: %w", err)
	}
	return result.Actions, nil
}

// InvocationContext tells the invoked plugin where the operator was standing.
// The picker is a popup over someone else's pane, so passing its own ids would
// describe the popup rather than the work, and a plugin like a file explorer
// would open against the wrong directory.
type InvocationContext struct {
	WorkspaceID      string `json:"workspace_id,omitempty"`
	TabID            string `json:"tab_id,omitempty"`
	FocusedPaneID    string `json:"focused_pane_id,omitempty"`
	InvocationSource string `json:"invocation_source,omitempty"`
}

// Invoke runs a socket operation by name and discards its result, for the
// native verbs in the command tab. Params are the caller's to get right: the
// schema is the contract, and wrapping each of five verbs in its own method
// would be five methods to describe what one map already says.
func (c Client) Invoke(method string, params map[string]any) error {
	return c.call(method, params)
}

// InvokeAction runs another plugin's action.
func (c Client) InvokeAction(a Action, ctx InvocationContext) error {
	return c.call("plugin.action.invoke", map[string]any{
		"plugin_id": a.PluginID,
		"action_id": a.ActionID,
		"context":   ctx,
	})
}
