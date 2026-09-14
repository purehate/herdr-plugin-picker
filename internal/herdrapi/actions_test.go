package herdrapi

import "testing"

func TestWorkspaceActionsArgv(t *testing.T) {
	run, calls := recorder()
	api := Client{Run: run}
	if err := api.RenameWorkspace("w1", "new name"); err != nil {
		t.Fatal(err)
	}
	if err := api.CloseWorkspace("w1"); err != nil {
		t.Fatal(err)
	}
	if err := api.CreateWorkspace("/tmp/x", "label", true); err != nil {
		t.Fatal(err)
	}
	if err := api.CreateWorkspace("", "", false); err != nil {
		t.Fatal(err)
	}
	assertArgv(t, *calls, []string{
		"workspace rename w1 new name",
		"workspace close w1",
		"workspace create --cwd /tmp/x --label label --focus",
		"workspace create",
	})
}

func TestTabActionsArgv(t *testing.T) {
	run, calls := recorder()
	api := Client{Run: run}
	if err := api.RenameTab("w1:t1", "build"); err != nil {
		t.Fatal(err)
	}
	if err := api.CloseTab("w1:t1"); err != nil {
		t.Fatal(err)
	}
	if err := api.CreateTab("w1", "/tmp/x", "label", true); err != nil {
		t.Fatal(err)
	}
	if err := api.CreateTab("", "", "", false); err != nil {
		t.Fatal(err)
	}
	assertArgv(t, *calls, []string{
		"tab rename w1:t1 build",
		"tab close w1:t1",
		"tab create --workspace w1 --cwd /tmp/x --label label --focus",
		"tab create",
	})
}

func TestPaneAndAgentActionsArgv(t *testing.T) {
	run, calls := recorder()
	api := Client{Run: run}
	if err := api.ClosePane("w1:p1"); err != nil {
		t.Fatal(err)
	}
	if err := api.RenameAgent("w1:p1", "reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := api.WorktreeOpen("/tmp/x", true); err != nil {
		t.Fatal(err)
	}
	if err := api.WorktreeOpen("", false); err != nil {
		t.Fatal(err)
	}
	assertArgv(t, *calls, []string{
		"pane close w1:p1",
		"agent rename w1:p1 reviewer",
		"worktree open --cwd /tmp/x --focus",
		"worktree open",
	})
}
