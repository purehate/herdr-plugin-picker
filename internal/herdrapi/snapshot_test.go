package herdrapi

import (
	"reflect"
	"testing"
)

func TestSnapshotDecodesNavigationMetadata(t *testing.T) {
	var calls [][]string
	c := Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"result":{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"project","tab_count":2,"pane_count":3}],"tabs":[{"tab_id":"w1:t1","workspace_id":"w1","label":"build"}],"agents":[{"pane_id":"w1:p1","agent":"codex","agent_status":"working","terminal_title_stripped":"review"}]}}}`), nil
	}}
	s, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"api", "snapshot"}}) {
		t.Fatalf("calls = %v", calls)
	}
	if len(s.Workspaces) != 1 || s.Workspaces[0].ID != "w1" || s.Workspaces[0].TabCount != 2 {
		t.Fatalf("workspaces = %+v", s.Workspaces)
	}
	if len(s.Tabs) != 1 || s.Tabs[0].ID != "w1:t1" {
		t.Fatalf("tabs = %+v", s.Tabs)
	}
	if len(s.Agents) != 1 || s.Agents[0].PaneID != "w1:p1" || s.Agents[0].Status != "working" {
		t.Fatalf("agents = %+v", s.Agents)
	}
}

func TestNavigatorFocusCommands(t *testing.T) {
	var calls [][]string
	c := Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return nil, nil
	}}
	if err := c.FocusWorkspace("w1"); err != nil {
		t.Fatal(err)
	}
	if err := c.FocusAgent("w1:p2"); err != nil {
		t.Fatal(err)
	}
	if err := c.FocusTab("w1:t3"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"workspace", "focus", "w1"}, {"agent", "focus", "w1:p2"}, {"tab", "focus", "w1:t3"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
