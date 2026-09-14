package herdrsock

import (
	"testing"
)

// Omitting plugin_id is what widens plugin.action.list from one plugin to every
// installed one, so a request that carries it would quietly return a fraction
// of the palette.
func TestActionsAsksForEveryPlugin(t *testing.T) {
	var got map[string]any
	path := serve(t, func(req map[string]any) string {
		got = req
		// One line: the protocol is newline-delimited, so a pretty-printed
		// reply would be read as a truncated first line.
		return `{"id":"` + req["id"].(string) + `","result":{"type":"plugin_action_list","actions":[` +
			`{"plugin_id":"a.b","action_id":"open","title":"Open it","contexts":["pane"],"command":["x"]},` +
			`{"plugin_id":"c.d","action_id":"go","title":"Go"}]}}`
	})

	acts, err := (Client{Path: path}).Actions()
	if err != nil {
		t.Fatalf("Actions: %v", err)
	}
	if got["method"] != "plugin.action.list" {
		t.Fatalf("method = %v", got["method"])
	}
	if params, _ := got["params"].(map[string]any); len(params) != 0 {
		t.Errorf("params = %v, want empty so every plugin is listed", params)
	}
	if len(acts) != 2 {
		t.Fatalf("got %d actions, want 2", len(acts))
	}
	if acts[0].PluginID != "a.b" || acts[0].ActionID != "open" || acts[0].Title != "Open it" {
		t.Errorf("first action = %+v", acts[0])
	}
}

func TestActionsReportsAServerError(t *testing.T) {
	path := serve(t, func(req map[string]any) string {
		return `{"id":"` + req["id"].(string) + `","error":{"code":"denied","message":"nope"}}`
	})
	if _, err := (Client{Path: path}).Actions(); err == nil {
		t.Fatal("err = nil, want the refusal reported")
	}
}

// An action with no contexts is legal anywhere; herdr reads an absent list that
// way, and a palette that hid them would drop a quarter of what is installed.
func TestValidInTreatsNoContextsAsAnywhere(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contexts []string
		in       string
		want     bool
	}{
		{"contextless is valid anywhere", nil, "pane", true},
		{"contextless in a workspace", []string{}, "workspace", true},
		{"listed context matches", []string{"pane"}, "pane", true},
		{"unlisted context does not", []string{"pane"}, "workspace", false},
		{"one of several", []string{"workspace", "pane"}, "pane", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Action{Contexts: tc.contexts}
			if got := a.ValidIn(tc.in); got != tc.want {
				t.Errorf("ValidIn(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// The context describes where the operator was standing, not where the popup
// is, so a file explorer opens against their work rather than against the
// picker's own pane.
func TestInvokeActionCarriesTheCallersContext(t *testing.T) {
	var got map[string]any
	path := serve(t, func(req map[string]any) string {
		got = req
		return `{"id":"` + req["id"].(string) + `","result":{}}`
	})

	act := Action{PluginID: "a.b", ActionID: "open"}
	err := (Client{Path: path}).InvokeAction(act, InvocationContext{
		WorkspaceID:      "w1",
		TabID:            "w1:t2",
		FocusedPaneID:    "w1:p3",
		InvocationSource: "picker",
	})
	if err != nil {
		t.Fatalf("InvokeAction: %v", err)
	}
	if got["method"] != "plugin.action.invoke" {
		t.Fatalf("method = %v", got["method"])
	}
	params, _ := got["params"].(map[string]any)
	if params["plugin_id"] != "a.b" || params["action_id"] != "open" {
		t.Errorf("params = %v", params)
	}
	ctx, ok := params["context"].(map[string]any)
	if !ok {
		t.Fatalf("context missing from %v", params)
	}
	for k, want := range map[string]string{
		"workspace_id":      "w1",
		"tab_id":            "w1:t2",
		"focused_pane_id":   "w1:p3",
		"invocation_source": "picker",
	} {
		if ctx[k] != want {
			t.Errorf("context[%q] = %v, want %q", k, ctx[k], want)
		}
	}
}

// An empty context must not ship keys with empty values: herdr validates ids,
// and "" is not a pane.
func TestInvokeActionOmitsAnEmptyContext(t *testing.T) {
	var got map[string]any
	path := serve(t, func(req map[string]any) string {
		got = req
		return `{"id":"` + req["id"].(string) + `","result":{}}`
	})

	if err := (Client{Path: path}).InvokeAction(Action{ActionID: "go"}, InvocationContext{}); err != nil {
		t.Fatalf("InvokeAction: %v", err)
	}
	params, _ := got["params"].(map[string]any)
	ctx, _ := params["context"].(map[string]any)
	if len(ctx) != 0 {
		t.Errorf("context = %v, want no empty ids", ctx)
	}
}
