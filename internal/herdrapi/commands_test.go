package herdrapi

import (
	"strings"
	"testing"
)

func recorder() (Runner, *[][]string) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}
	return run, &calls
}

func argvLines(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

func assertArgv(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	got := argvLines(calls)
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPaneRename(t *testing.T) {
	run, calls := recorder()
	if err := (Client{Run: run}).PaneRename("w5:pB", "ssh:nixos-dev"); err != nil {
		t.Fatalf("PaneRename: %v", err)
	}
	assertArgv(t, *calls, []string{"pane rename w5:pB ssh:nixos-dev"})
}

func TestPaneClose(t *testing.T) {
	run, calls := recorder()
	if err := (Client{Run: run}).PaneClose("w5:pOverlay"); err != nil {
		t.Fatalf("PaneClose: %v", err)
	}
	assertArgv(t, *calls, []string{"plugin pane close w5:pOverlay"})
}

func TestFocusPaneFullSequence(t *testing.T) {
	run, calls := recorder()
	target := Pane{PaneID: "w8:p3", TabID: "w8:t2", WorkspaceID: "w8"}
	if err := (Client{Run: run}).FocusPane(target, "w5", "w5:t1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	assertArgv(t, *calls, []string{
		"workspace focus w8",
		"tab focus w8:t2",
		"plugin pane focus w8:p3",
	})
}

func TestFocusPaneSkipsCurrentSteps(t *testing.T) {
	run, calls := recorder()
	target := Pane{PaneID: "w5:pB", TabID: "w5:t1", WorkspaceID: "w5"}
	if err := (Client{Run: run}).FocusPane(target, "w5", "w5:t1"); err != nil {
		t.Fatalf("FocusPane: %v", err)
	}
	assertArgv(t, *calls, []string{"plugin pane focus w5:pB"})
}

func TestPluginPaneOpenSplit(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "purehate.herdr-picker",
		Entrypoint: "session",
		Placement:  "split",
		TargetPane: "w5:pA",
		Direction:  "right",
		Env:        map[string]string{"HERDR_PICKER_TARGET": "nixos-dev"},
		Focus:      true,
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	assertArgv(t, *calls, []string{
		"plugin pane open --plugin purehate.herdr-picker --entrypoint session " +
			"--placement split --target-pane w5:pA --direction right " +
			"--env HERDR_PICKER_TARGET=nixos-dev --focus",
	})
}

func TestPluginPaneOpenTabOmitsDirection(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "purehate.herdr-picker",
		Entrypoint: "session",
		Placement:  "tab",
		Direction:  "right",
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	// --direction is meaningless outside a split and herdr rejects it there.
	if got := argvLines(*calls)[0]; strings.Contains(got, "--direction") {
		t.Fatalf("argv = %q, want no --direction for a tab placement", got)
	}
}

// --target-pane is only accepted by the placements that target an existing
// pane. herdr rejects it on the others, so the flag has to follow placement,
// not just the presence of a pane id. The split/zoomed cases prove the guard
// did not overreach and break the placements that require the flag.
//
// PlacementTargetsPane is asserted against the same rows as the argv it gates.
// Callers consult the predicate to decide whether producing a pane id is worth
// the work, so the two answers drifting apart would mean a caller skipping work
// for a placement that then demands the flag — checking them together is what
// makes the predicate a description of this argv rather than a second opinion.
func TestPluginPaneOpenTargetPaneFollowsPlacement(t *testing.T) {
	for _, tc := range []struct {
		placement string
		wantFlag  bool
	}{
		{"split", true},
		{"zoomed", true},
		{"tab", false},
		{"overlay", false},
		{"popup", false},
	} {
		t.Run(tc.placement, func(t *testing.T) {
			run, calls := recorder()
			err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
				Plugin:     "purehate.herdr-picker",
				Entrypoint: "session",
				Placement:  tc.placement,
				TargetPane: "w5:pA",
			})
			if err != nil {
				t.Fatalf("PluginPaneOpen: %v", err)
			}
			got := argvLines(*calls)[0]
			if gotFlag := strings.Contains(got, "--target-pane w5:pA"); gotFlag != tc.wantFlag {
				t.Fatalf("argv = %q, --target-pane present = %v, want %v",
					got, gotFlag, tc.wantFlag)
			}
			if got := PlacementTargetsPane(tc.placement); got != tc.wantFlag {
				t.Fatalf("PlacementTargetsPane(%q) = %v, want %v", tc.placement, got, tc.wantFlag)
			}
		})
	}
}

func TestPluginPaneOpenEnvIsSorted(t *testing.T) {
	run, calls := recorder()
	err := (Client{Run: run}).PluginPaneOpen(OpenOpts{
		Plugin:     "p",
		Entrypoint: "session",
		Placement:  "zoomed",
		Env:        map[string]string{"B": "2", "A": "1", "C": "3"},
	})
	if err != nil {
		t.Fatalf("PluginPaneOpen: %v", err)
	}
	got := argvLines(*calls)[0]
	if !strings.Contains(got, "--env A=1 --env B=2 --env C=3") {
		t.Fatalf("argv = %q, want env flags in sorted order", got)
	}
}
