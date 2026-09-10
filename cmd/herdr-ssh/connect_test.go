package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

const openPanesJSON = `{"id":1,"result":{"panes":[
  {"pane_id":"w5:pA","tab_id":"w5:t1","workspace_id":"w5","label":null},
  {"pane_id":"w8:pQ","tab_id":"w8:t3","workspace_id":"w8","label":"ssh:nixos-dev"}
]}}`

func fakeAPI(out string) (herdrapi.Client, *[][]string) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), nil
	}}
	return api, &calls
}

func joined(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// openArgv returns the one `plugin pane open` call. Indexing calls[0] instead
// asserts against whichever call happens to come first, so a test that gained a
// preceding `pane list` would keep passing while checking a string that cannot
// contain what it is looking for.
func openArgv(t *testing.T, calls [][]string) string {
	t.Helper()
	var found []string
	for _, c := range joined(calls) {
		if strings.HasPrefix(c, "plugin pane open ") {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("calls = %v, want exactly one plugin pane open", joined(calls))
	}
	return found[0]
}

var devHost = sshconfig.Host{Alias: "nixos-dev", HostName: "192.0.2.10", Port: "22"}

func TestPerformSelectionOpensASplit(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	if err := performSelection(io.Discard, api, cfg, sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}

	got := joined(*calls)
	// Two calls, not one. Reuse is off, so there is no session scan — but the
	// caller's pane id is still on its way to herdr as a placement target, and
	// the pane list is the only thing that can say whether it still exists.
	if len(got) != 2 || got[0] != "pane list" {
		t.Fatalf("calls = %v, want a pane list then an open", got)
	}
	want := "plugin pane open --plugin purehate.herdr-ssh --entrypoint session " +
		"--placement split --target-pane w5:pA --direction right " +
		"--env HERDR_SSH_TARGET=nixos-dev --focus"
	// w5:pA is in the list, so a live id survives the check with reuse off.
	if argv := openArgv(t, *calls); argv != want {
		t.Fatalf("argv =\n  %q\nwant\n  %q", argv, want)
	}
}

func TestPerformSelectionReusesAnExistingPane(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(io.Discard, api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}

	want := []string{
		"pane list",
		"workspace focus w8",
		"tab focus w8:t3",
		"plugin pane focus w8:pQ",
	}
	got := joined(*calls)
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPerformSelectionForceNewSkipsReuse(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split", ForceNew: true}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(io.Discard, api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	for _, c := range joined(*calls) {
		if strings.Contains(c, "pane focus") {
			t.Fatalf("calls = %v, want no reuse when ForceNew is set", joined(*calls))
		}
	}
}

func TestPerformSelectionTabPlacementOmitsDirection(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "tab"}

	if err := performSelection(io.Discard, api, cfg, sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	got := openArgv(t, *calls)
	if strings.Contains(got, "--direction") {
		t.Fatalf("argv = %q, want no --direction for a tab", got)
	}
	// A tab takes a workspace id, not a target pane: herdr rejects the open with
	// "tab plugin panes support workspace_id but not target_pane_id or
	// direction". performSelection passes the caller's pane id unconditionally,
	// so this asserts herdrapi filtered it back out.
	if strings.Contains(got, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane for a tab", got)
	}
	if !strings.Contains(got, "--placement tab") {
		t.Fatalf("argv = %q, want --placement tab", got)
	}
}

func TestPerformSelectionHonorsSplitDirection(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	cfg.SplitDirection = "down"
	sel := picker.Selection{Host: devHost, Placement: "split"}

	if err := performSelection(io.Discard, api, cfg, sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	if got := openArgv(t, *calls); !strings.Contains(got, "--direction down") {
		t.Fatalf("argv = %q, want --direction down", got)
	}
}

func TestPerformSelectionWithoutACallerPaneStillOpens(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "split"}

	if err := performSelection(io.Discard, api, cfg, sel, caller{}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	// Nothing recorded means nothing to confirm, so the check must not spend a
	// pane list discovering that "" is still "".
	if got := joined(*calls); len(got) != 1 {
		t.Fatalf("calls = %v, want just the open", got)
	}
	if got := openArgv(t, *calls); strings.Contains(got, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane when the caller is unknown", got)
	}
}

func TestPerformSelectionToleratesAFailedPaneList(t *testing.T) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "pane" && args[1] == "list" {
			return []byte("socket gone"), errRenameTest
		}
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	sel := picker.Selection{Host: devHost, Placement: "split"}
	// Reuse is a convenience. If the lookup fails, open a fresh pane.
	var screen bytes.Buffer
	if err := performSelection(&screen, api, pluginconfig.Defaults(), sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	// Tolerating the failure is not the same as hiding it. Everything below
	// this asserts that the operator silently got a worse placement than they
	// asked for; without this line the whole test passes on a listPanes that
	// swallows the error, and the operator is left with a pane in the wrong
	// place and nothing on screen connecting the two.
	//
	// The underlying error text, not just the prefix: "could not list panes"
	// alone tells them nothing they could act on, and it is the wrapped cause
	// that distinguishes a dead socket from a herdr that is not running.
	wantDiagnostics(t, screen.String(), "could not list panes", errRenameTest.Error())
	got := joined(calls)
	if len(got) != 2 {
		t.Fatalf("calls = %v, want a failed list then an open", got)
	}
	// The same list that would have found a session is the only thing that can
	// confirm the caller's pane, so a failed list leaves it unverifiable. Send it
	// anyway and herdr rejects the open if the pane is gone, costing the session;
	// omit it and the worst case is a default placement.
	if argv := openArgv(t, calls); strings.Contains(argv, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane when the list failed", argv)
	}
}

// strangerPanesJSON holds neither an ssh: label nor the caller pane the test
// below records, so the reuse scan misses and the caller pane is stale.
const strangerPanesJSON = `{"id":1,"result":{"panes":[
  {"pane_id":"w9:pZ","tab_id":"w9:t1","workspace_id":"w9","label":null}
]}}`

func TestPerformSelectionDropsAStaleCallerPane(t *testing.T) {
	api, calls := fakeAPI(strangerPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split"}
	// caller.json outlives the pane it names: it survives herdr restarts and the
	// closure of the pane the operator opened the picker from.
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(io.Discard, api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	got := joined(*calls)
	// One list, not two: the reuse scan and the staleness check read the same
	// response.
	if len(got) != 2 || got[0] != "pane list" {
		t.Fatalf("calls = %v, want one pane list then an open", got)
	}
	if argv := openArgv(t, *calls); strings.Contains(argv, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane for a pane that is gone", argv)
	}
}

// The two cells below are the ones the reuse-on stale test and the reuse-off
// live test do not reach between them: a stale id on a path that never runs the
// reuse scan. Their union looks like full coverage of performSelection and
// satisfies every line of livePaneID, but the defect lives on the diagonal.

func TestPerformSelectionDropsAStaleCallerPaneWithReuseOff(t *testing.T) {
	api, calls := fakeAPI(strangerPanesJSON)
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(io.Discard, api, cfg, sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	// Turning reuse off says nothing about whether the recorded pane still
	// exists, so the check still has to run and still has to fetch a list.
	if got := joined(*calls); len(got) != 2 || got[0] != "pane list" {
		t.Fatalf("calls = %v, want a pane list then an open", got)
	}
	if argv := openArgv(t, *calls); strings.Contains(argv, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane with reuse off", argv)
	}
}

func TestPerformSelectionDropsAStaleCallerPaneOnForceNew(t *testing.T) {
	api, calls := fakeAPI(strangerPanesJSON)
	sel := picker.Selection{Host: devHost, Placement: "split", ForceNew: true}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(io.Discard, api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	// ForceNew deliberately skips the reuse scan, which is exactly why it must
	// not skip the check: the operator asked for a pane, and a dead target
	// means they get none.
	if got := joined(*calls); len(got) != 2 || got[0] != "pane list" {
		t.Fatalf("calls = %v, want a pane list then an open", got)
	}
	if argv := openArgv(t, *calls); strings.Contains(argv, "--target-pane") {
		t.Fatalf("argv = %q, want no --target-pane on force-new", argv)
	}
}

func TestPerformSelectionKeepsALiveCallerPane(t *testing.T) {
	api, calls := fakeAPI(openPanesJSON)
	// web1 has no session, so this takes the open path with the pane list in
	// hand — the one path where the staleness check runs.
	sel := picker.Selection{Host: sshconfig.Host{Alias: "web1"}, Placement: "split"}
	ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

	if err := performSelection(io.Discard, api, pluginconfig.Defaults(), sel, ctx); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	got := joined(*calls)
	if len(got) != 2 {
		t.Fatalf("calls = %v, want one pane list then an open", got)
	}
	// w5:pA is in the list, so the check must leave it alone. Without this,
	// blanking every caller pane would satisfy the staleness test and quietly
	// throw away the placement the operator asked for on every connect.
	if argv := openArgv(t, *calls); !strings.Contains(argv, "--target-pane w5:pA") {
		t.Fatalf("argv = %q, want --target-pane w5:pA", argv)
	}
}

// The caller's pane id only reaches herdr on the placements that take a
// --target-pane, so resolving it anywhere else spends a `pane list` on an
// answer PluginPaneOpen discards. The split and zoomed rows are what stop the
// gate over-reaching: an assertion that a call is absent passes just as well
// against a harness that records nothing at all, so the placements that must
// still make the call sit in the same table as the ones that must not.
func TestPerformSelectionResolvesTheCallerPaneOnlyWherePlacementUsesIt(t *testing.T) {
	for _, tc := range []struct {
		placement string
		wantList  bool
	}{
		{"split", true},
		{"zoomed", true},
		{"tab", false},
		{"overlay", false},
	} {
		t.Run(tc.placement, func(t *testing.T) {
			api, calls := fakeAPI(openPanesJSON)
			cfg := pluginconfig.Defaults()
			// Reuse off: the scan fetches a list for its own reasons, which
			// would answer this question before the check under test is asked.
			cfg.ReusePanes = false
			sel := picker.Selection{Host: devHost, Placement: tc.placement}
			ctx := caller{PaneID: "w5:pA", TabID: "w5:t1", WorkspaceID: "w5"}

			if err := performSelection(io.Discard, api, cfg, sel, ctx); err != nil {
				t.Fatalf("performSelection: %v", err)
			}
			got := joined(*calls)
			gotList := len(got) > 0 && got[0] == "pane list"
			if gotList != tc.wantList {
				t.Fatalf("calls = %v, pane list present = %v, want %v", got, gotList, tc.wantList)
			}
		})
	}
}

func TestPerformSelectionStaysQuietWhenAPlacementIgnoresTheCallerPane(t *testing.T) {
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		if args[0] == "pane" && args[1] == "list" {
			return []byte("socket gone"), errRenameTest
		}
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	cfg := pluginconfig.Defaults()
	cfg.ReusePanes = false
	sel := picker.Selection{Host: devHost, Placement: "tab"}

	var screen bytes.Buffer
	if err := performSelection(&screen, api, cfg, sel, caller{PaneID: "w5:pA"}); err != nil {
		t.Fatalf("performSelection: %v", err)
	}
	// The cost that reaches the operator. listPanes reports a failed lookup to
	// out, so a lookup made for a placement that discards the answer turns a
	// broken `pane list` into a warning about a pane this open was never going
	// to reference — and nothing on screen marks it as irrelevant. The paired
	// positive is TestPerformSelectionToleratesAFailedPaneList, which asserts
	// the same failure is reported on a split, where the id does matter.
	wantQuiet(t, screen.String())
}

func TestOpenSessionsMapsLabeledPanes(t *testing.T) {
	api, _ := fakeAPI(openPanesJSON)
	var screen bytes.Buffer
	got := openSessions(&screen, api)
	if len(got) != 1 || got["nixos-dev"] != "w8:pQ" {
		t.Fatalf("openSessions = %v, want nixos-dev → w8:pQ", got)
	}
	// A successful list must say nothing. The failure test below asserts the
	// message is printed; only this one stops it being printed unconditionally,
	// which would put "could not list panes" above every picker that worked.
	wantQuiet(t, screen.String())
}

func TestOpenSessionsReportsAFailedList(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte("socket gone"), errRenameTest
	}}
	var screen bytes.Buffer
	// An empty map is indistinguishable from "no sessions are open", so the
	// picker draws a plausible screen with every open session missing its
	// marker. The map cannot carry that difference; the message is the only
	// thing that can.
	if got := openSessions(&screen, api); len(got) != 0 {
		t.Fatalf("openSessions = %v, want an empty map when the list fails", got)
	}
	wantDiagnostics(t, screen.String(), "could not list panes", errRenameTest.Error())
}

func TestTrimLabel(t *testing.T) {
	if alias, ok := trimLabel("ssh:web1"); !ok || alias != "web1" {
		t.Errorf("trimLabel(ssh:web1) = (%q, %v)", alias, ok)
	}
	for _, label := range []string{"ssh:", "build", "", "sshweb1"} {
		if _, ok := trimLabel(label); ok {
			t.Errorf("trimLabel(%q) matched, want no match", label)
		}
	}
}
