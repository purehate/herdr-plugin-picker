package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

func actionIDs(actions []picker.NavAction) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, a.ID)
	}
	return out
}

func TestNavActionsPerSection(t *testing.T) {
	spaces := navActions(picker.NavSpaces, picker.NavItem{ID: "w1"})
	if got, want := actionIDs(spaces), []string{"rename", "new-tab", "new-workspace", "close", "copy-id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("spaces actions = %v, want %v", got, want)
	}
	for _, a := range spaces {
		if a.ID == "copy-id" && a.Copy != "w1" {
			t.Fatalf("copy-id payload = %q, want w1", a.Copy)
		}
		if a.ID == "close" && a.Confirm == "" {
			t.Fatal("close has no confirmation")
		}
	}

	agents := navActions(picker.NavAgents, picker.NavItem{ID: "w1:p1", CWD: "/tmp/x"})
	if got, want := actionIDs(agents), []string{"rename", "new-tab", "close", "copy-id", "copy-cwd", "worktree"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("agent actions = %v, want %v", got, want)
	}

	// Without a cwd, the cwd-backed actions are not offered: a copy of "" or a
	// worktree open at "" would both be nonsense.
	noCWD := navActions(picker.NavAgents, picker.NavItem{ID: "w1:p1"})
	if got := actionIDs(noCWD); strings.Contains(strings.Join(got, ","), "worktree") {
		t.Fatalf("agent actions without a cwd = %v, want no worktree", got)
	}

	sessions := navActions(picker.NavSessions, picker.NavItem{ID: "w1:t1"})
	if got, want := actionIDs(sessions), []string{"rename", "new-tab", "close", "copy-id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sessions actions = %v, want %v", got, want)
	}

	if got := navActions(picker.NavSSH, picker.NavItem{ID: "web1"}); got != nil {
		t.Fatalf("ssh actions = %v, want none", got)
	}
}

func TestRunNavActionArgvs(t *testing.T) {
	cases := []struct {
		name    string
		section picker.NavSection
		item    picker.NavItem
		action  string
		text    string
		want    string
	}{
		{"rename workspace", picker.NavSpaces, picker.NavItem{ID: "w1"}, "rename", "new name", "workspace rename w1 new name"},
		{"new tab here", picker.NavSpaces, picker.NavItem{ID: "w1"}, "new-tab", "", "tab create --workspace w1 --focus"},
		{"new workspace", picker.NavSpaces, picker.NavItem{ID: "w1"}, "new-workspace", "", "workspace create --focus"},
		{"close workspace", picker.NavSpaces, picker.NavItem{ID: "w1"}, "close", "", "workspace close w1"},
		{"rename agent", picker.NavAgents, picker.NavItem{ID: "w1:p1"}, "rename", "reviewer", "agent rename w1:p1 reviewer"},
		{"agent new tab", picker.NavAgents, picker.NavItem{ID: "w1:p1", WorkspaceID: "w1", CWD: "/tmp/x"}, "new-tab", "", "tab create --workspace w1 --cwd /tmp/x --focus"},
		{"close pane", picker.NavAgents, picker.NavItem{ID: "w1:p1"}, "close", "", "pane close w1:p1"},
		{"worktree", picker.NavAgents, picker.NavItem{ID: "w1:p1", CWD: "/tmp/x"}, "worktree", "", "worktree open --cwd /tmp/x --focus"},
		{"rename tab", picker.NavSessions, picker.NavItem{ID: "w1:t1"}, "rename", "build", "tab rename w1:t1 build"},
		{"close tab", picker.NavSessions, picker.NavItem{ID: "w1:t1"}, "close", "", "tab close w1:t1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
				calls = append(calls, args)
				return nil, nil
			}}
			status, err := runNavAction(api, tc.section, tc.item, tc.action, tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if status == "" {
				t.Fatal("no status line returned")
			}
			if len(calls) != 1 {
				t.Fatalf("calls = %v, want one", calls)
			}
			if got := strings.Join(calls[0], " "); got != tc.want {
				t.Fatalf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunNavActionUnknownErrors(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) { return nil, nil }}
	if _, err := runNavAction(api, picker.NavSpaces, picker.NavItem{ID: "w1"}, "bogus", ""); err == nil {
		t.Fatal("unknown action did not error")
	}
	if _, err := runNavAction(api, picker.NavSSH, picker.NavItem{ID: "web1"}, "rename", ""); err == nil {
		t.Fatal("ssh section did not error")
	}
}
