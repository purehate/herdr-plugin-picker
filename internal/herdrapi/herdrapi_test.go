package herdrapi

import (
	"errors"
	"strings"
	"testing"
)

// fakeRunner records argv and replays canned output.
func fakeRunner(out string, err error) (Runner, *[][]string) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), err
	}
	return run, &calls
}

const panesJSON = `{"id":1,"result":{"panes":[
  {"pane_id":"w5:pA","tab_id":"w5:t1","workspace_id":"w5","label":null},
  {"pane_id":"w5:pB","tab_id":"w5:t7","workspace_id":"w5","label":"ssh:nixos-dev"}
]}}`

func TestPaneList(t *testing.T) {
	run, calls := fakeRunner(panesJSON, nil)
	c := Client{Run: run}

	panes, err := c.PaneList()
	if err != nil {
		t.Fatalf("PaneList: %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(panes))
	}
	if panes[0].Label != nil {
		t.Errorf("panes[0].Label = %v, want nil", panes[0].Label)
	}
	if panes[1].Label == nil || *panes[1].Label != "ssh:nixos-dev" {
		t.Errorf("panes[1].Label = %v", panes[1].Label)
	}
	if panes[1].PaneID != "w5:pB" || panes[1].TabID != "w5:t7" || panes[1].WorkspaceID != "w5" {
		t.Errorf("panes[1] = %+v", panes[1])
	}

	want := []string{"pane", "list"}
	if len(*calls) != 1 || strings.Join((*calls)[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", *calls, want)
	}
}

func TestPaneListPropagatesCLIError(t *testing.T) {
	boom := errors.New("exit status 1")
	run, _ := fakeRunner("socket not found", boom)
	c := Client{Run: run}

	_, err := c.PaneList()
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the runner error", err)
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("err = %v, want a *CLIError", err)
	}
	if !strings.Contains(cliErr.Error(), "socket not found") {
		t.Errorf("CLIError message lost the output: %q", cliErr.Error())
	}
}

func TestPaneListRejectsBadJSON(t *testing.T) {
	run, _ := fakeRunner("not json at all", nil)
	c := Client{Run: run}
	if _, err := c.PaneList(); err == nil {
		t.Fatal("err = nil, want a decode error")
	}
}

func TestFindLabeled(t *testing.T) {
	label := "ssh:nixos-dev"
	panes := []Pane{{PaneID: "w5:pA"}, {PaneID: "w5:pB", Label: &label}}

	got, ok := FindLabeled(panes, "ssh:nixos-dev")
	if !ok || got.PaneID != "w5:pB" {
		t.Fatalf("FindLabeled = (%+v, %v)", got, ok)
	}
	if _, ok := FindLabeled(panes, "ssh:absent"); ok {
		t.Error("FindLabeled matched a label that is not present")
	}
}
