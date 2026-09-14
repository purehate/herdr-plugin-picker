package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/picker"
)

// wantDiagnostics asserts that each want appears in got, in the order listed.
//
// Order, not just presence, because presence is the weaker claim and the one
// that keeps passing after the behaviour breaks. Two diagnostics that must be
// read in sequence — the explanation before the accusation, the config warning
// before the failure it caused — are still both "present" when they come out
// backwards, and a reader who sees the accusation first has already drawn the
// wrong conclusion by the time the explanation arrives.
//
// Ordering only means anything within a single stream, so callers pass one
// buffer. Two buffers interleaved by the runtime prove nothing about what the
// operator saw.
func wantDiagnostics(t *testing.T, got string, want ...string) {
	t.Helper()
	rest := got
	for i, w := range want {
		if idx := strings.Index(rest, w); idx >= 0 {
			rest = rest[idx+len(w):]
			continue
		}
		// i == 0 cannot reach here with a match, because rest is still the whole
		// of got on the first pass — so want[i-1] below is always in range.
		if strings.Contains(got, w) {
			t.Fatalf("diagnostic %q is present but precedes %q, which must come first; stream was:\n%s", w, want[i-1], got)
		}
		t.Fatalf("diagnostic %q missing; stream was:\n%s", w, got)
	}
}

// wantQuiet asserts nothing was written at all. Distinct from asserting the
// absence of one string: it also catches a diagnostic that was reworded.
func wantQuiet(t *testing.T, got string) {
	t.Helper()
	if got != "" {
		t.Fatalf("want nothing written, got:\n%s", got)
	}
}

func TestRunRejectsUnknownVerbs(t *testing.T) {
	// {"connect"} is an addition to the plan's list. Without it the connect
	// arm's own arity guard is unpinned, and removing that guard does not
	// degrade to a usage error — runConnect indexes args[0] and panics, so a
	// forgotten alias prints a Go stack trace at the operator instead.
	for _, args := range [][]string{{}, {"wat"}, {"plugin"}, {"plugin", "wat"}, {"connect"}} {
		err := run(args)
		if err == nil {
			t.Fatalf("run(%v) = nil, want a usage error", args)
		}
		if !strings.Contains(err.Error(), "usage") {
			t.Errorf("run(%v) error = %q, want it to mention usage", args, err)
		}
	}
}

// stubNavigator stands in for picker.RunNavigator and captures the NavOptions it
// was handed. Those Options are the only place the footer warnings ever go —
// nothing writes them out — so capturing them is the only way to assert they
// were built.
func stubNavigator(sel picker.NavSelection, ok bool, err error) (navigatorFn, *picker.NavOptions) {
	var got picker.NavOptions
	return func(opts picker.NavOptions) (picker.NavSelection, bool, error) {
		got = opts
		return sel, ok, err
	}, &got
}

// badThemeFile returns a herdr config that theme.LoadFile rejects. Malformed
// rather than missing: a missing file is a documented non-error.
func badThemeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "herdr.toml")
	if err := os.WriteFile(path, []byte("[ui\naccent = "), 0o600); err != nil {
		t.Fatalf("write herdr.toml: %v", err)
	}
	return path
}

// navigatorEnv isolates runNavigatorWith from the ambient environment.
func navigatorEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	t.Setenv("HERDR_CONFIG_PATH", "")
	t.Setenv("HERDR_PANE_ID", "")
	// Cleared because these tests are run from inside herdr as often as not,
	// and an inherited socket path would wire ^b to the operator's live panes.
	t.Setenv("HERDR_SOCKET_PATH", "")
}

// TestRunNavigatorBuildsTheFooterWarningsInAFixedOrder breaks the plugin config,
// the theme file, and the ssh config at once, deliberately.
//
// Do not split this into one test per source "for clarity". `append(A, B...)`
// and `append(B, A...)` are the same expression whenever either side is empty,
// so a test that breaks only some of the three cannot distinguish the two
// append orders at all — the reversal it exists to catch becomes unkillable by
// any assertion the split tests could make. All three broken simultaneously is
// what makes the ordering observable.
func TestRunNavigatorBuildsTheFooterWarningsInAFixedOrder(t *testing.T) {
	navigatorEnv(t, brokenIncludeHome(t))
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", badPluginConfigDir(t))
	t.Setenv("HERDR_CONFIG_PATH", badThemeFile(t))

	// The operator cancels, so nothing past the picker runs and this test is
	// only about what was handed to it.
	pick, opts := stubNavigator(picker.NavSelection{}, false, nil)
	api, _ := fakeAPI(openPanesJSON)
	var screen bytes.Buffer
	if err := runNavigatorWith(&screen, strings.NewReader(""), pick, api); err != nil {
		t.Fatalf("runNavigatorWith: %v", err)
	}

	// One plugin-config warning, one theme warning, one per broken include. A
	// count, because the ordered check below is satisfied by a slice that also
	// contains duplicates, and `append(loadWarnings, warnings...)` is one stray
	// line away from producing them.
	if len(opts.Warnings) != 4 {
		t.Fatalf("warnings = %#v, want 4", opts.Warnings)
	}
	// The order the comment at the build site promises. Both load errors are
	// appended to one slice before the parse warnings, and the alternative it
	// warns against — prepending each in turn — silently reverses the pair,
	// which no presence check can see. Asserting by position is the only thing
	// that holds that comment to its claim.
	//
	// Naming both includes rather than matching "include unreadable" once also
	// pins the tail as two entries in file order, so a merge that keeps only
	// the head of either side fails here.
	//
	// Both head anchors are the loaders' own words — pluginconfig's ErrInvalid
	// and theme.LoadFile's prefix — not labels the build site adds. That makes
	// them stable identifiers to order by, and it means this test says nothing
	// about whether a label is present. TestALoadWarningNamesItsSourceOnce is
	// what holds that.
	wantDiagnostics(t, strings.Join(opts.Warnings, "\n"),
		"plugin config:", "theme config", firstInclude, secondInclude)

	// And none of them written out. This is the footer policy the build site
	// argues for: the picker has an overlay to render into, so these belong in
	// it, and a write here would print above the overlay instead of in it.
	wantQuiet(t, screen.String())
}

// rejectedConfigWarning runs the picker against a config dir and returns the
// footer warning about the rejected keys. Found by its remedy clause rather
// than by position: loadHosts contributes warnings of its own to the same
// slice, and how many is not this test's business.
func rejectedConfigWarning(t *testing.T, configDir string) string {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", configDir)

	// The operator cancels, so nothing past the picker runs.
	pick, opts := stubNavigator(picker.NavSelection{}, false, nil)
	api, _ := fakeAPI(openPanesJSON)
	var screen bytes.Buffer
	if err := runNavigatorWith(&screen, strings.NewReader(""), pick, api); err != nil {
		t.Fatalf("runNavigatorWith: %v", err)
	}
	for _, w := range opts.Warnings {
		if strings.Contains(w, "ignoring the rejected keys") {
			return w
		}
	}
	t.Fatalf("no warning about the rejected keys in %#v", opts.Warnings)
	return ""
}

// TestARejectedConfigReadsTheSameOnBothPaths pins one sentence for one failure.
// The navigator built "plugin config: %v — ignoring the rejected keys" and connect
// built "herdr-picker: %v — ignoring the rejected keys", so a single bad
// config.toml had two spellings depending on which verb the operator happened to
// type, and neither reader could tell they were reading the same thing.
//
// The prefix is the whole difference and it belongs to the stream, not the
// message: connect writes to a stderr it shares with ssh and the operator's
// shell, so it has to say who is speaking, while inside the picker's own footer
// there is nobody else to confuse it with. So the body must match exactly and
// the picker's copy must carry no program prefix at all.
//
// Asserted across the two paths rather than against a literal on one of them: a
// literal is satisfied by whichever half is edited next, which is exactly how
// the two drifted apart.
func TestARejectedConfigReadsTheSameOnBothPaths(t *testing.T) {
	navigatorEnv(t, t.TempDir())
	dir := badPluginConfigDir(t)
	footer := rejectedConfigWarning(t, dir)

	// The same config through the verb with no picker to render into.
	var stderr bytes.Buffer
	if err := runConnectWith(&stderr, []string{"definitely-not-a-host"}); err == nil {
		t.Fatal("runConnectWith err = nil, want a not-found error")
	}

	if want := "herdr-picker: " + footer + "\n"; !strings.Contains(stderr.String(), want) {
		t.Errorf("the two verbs word one failure differently.\npicker footer: %q\nconnect stderr:\n%s",
			footer, stderr.String())
	}
	if strings.Contains(footer, "herdr-picker:") {
		t.Errorf("the footer names the program: %q — inside the picker's own footer nobody else is speaking", footer)
	}
}

// TestARejectedConfigCanCarryMoreThanOneLine is why picker.oneLine exists.
// pluginconfig checks every key and joins the failures with errors.Join, whose
// Error() separates them with a newline, so two bad keys make one warning
// string that draws two rows in a footer that reserved one.
//
// It asserts the input rather than the rendering — the flattening itself is
// pinned in the picker package, where the row budget lives. What this holds is
// that the shape is reachable from a config an operator can write, so the
// picker's handling of it is not defending against a hypothetical.
func TestARejectedConfigCanCarryMoreThanOneLine(t *testing.T) {
	navigatorEnv(t, t.TempDir())
	// probe = false keeps the run off the network; both other keys are rejected,
	// which is what makes the join have something to join.
	footer := rejectedConfigWarning(t, pluginConfigDir(t,
		"probe = false\nsplit_direction = \"sideways\"\nprobe_timeout_ms = -1\n"))

	if !strings.Contains(footer, "\n") {
		t.Errorf("warning %q is a single line; pluginconfig no longer joins its rejections "+
			"and the picker's flattening has nothing left to defend", footer)
	}
	for _, key := range []string{"split_direction", "probe_timeout_ms"} {
		if !strings.Contains(footer, key) {
			t.Errorf("warning %q does not name the rejected key %q", footer, key)
		}
	}
}

// warningWith returns the one footer warning containing substr. Looked up by the
// remedy clause, which the build site owns, so the lookup cannot depend on the
// prefix its caller is about to assert on.
func warningWith(t *testing.T, warnings []string, substr string) string {
	t.Helper()
	var found []string
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			found = append(found, w)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one warning containing %q, got %#v out of %#v", substr, found, warnings)
	}
	return found[0]
}

// TestALoadWarningNamesItsSourceOnce holds the decision that neither load
// warning carries a label of its own. Both loaders already name themselves —
// pluginconfig's errors open with "invalid plugin config", theme.LoadFile's with
// "theme config" — so a "plugin config: " or "theme: " added at the build site
// printed the same words twice in the one line the operator has to read.
//
// Asserted as a prefix rather than as a count of the loader's phrase, because a
// count cannot see the defect it would exist to catch. The label was "theme: ",
// which does not repeat the phrase "theme config", so "theme: theme config /p:
// ..." counts one occurrence and passes — as does every Contains assertion in
// this file, including the ordered one above. Requiring the warning to *begin*
// with the loader's own text is the form that fails for any prefix at all,
// including one nobody has thought of yet.
func TestALoadWarningNamesItsSourceOnce(t *testing.T) {
	navigatorEnv(t, t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", badPluginConfigDir(t))
	t.Setenv("HERDR_CONFIG_PATH", badThemeFile(t))

	// The operator cancels, so nothing past the picker runs.
	pick, opts := stubNavigator(picker.NavSelection{}, false, nil)
	api, _ := fakeAPI(openPanesJSON)
	var screen bytes.Buffer
	if err := runNavigatorWith(&screen, strings.NewReader(""), pick, api); err != nil {
		t.Fatalf("runNavigatorWith: %v", err)
	}

	for _, tc := range []struct{ remedy, opens string }{
		{"ignoring the rejected keys", "invalid plugin config"},
		{"using the default palette", "theme config"},
	} {
		w := warningWith(t, opts.Warnings, tc.remedy)
		if !strings.HasPrefix(w, tc.opens) {
			t.Errorf("warning %q does not open with %q — the build site is labelling a message that already names its own source", w, tc.opens)
		}
	}
}

// TestRunNavigatorMarksPanesOnlyWhenReuseIsOn ties the ▪ marker's input to the
// one config key that can make the marker true. OpenPanes has exactly two
// effects — it paints ▪, and ▪ outranks the ●/○ reachability glyphs — and with
// reuse_panes = false both are wrong: enter opens a second pane to a host the
// marker says already has one, and the glyph it hid was the accurate one.
//
// The call log is asserted alongside it, and is identical in both rows on
// purpose. The panes tab needs the inventory unconditionally, so reuse_panes no
// longer decides whether `pane list` runs — only what is done with the result.
// Pinning the log to one list proves the two consumers share that read: a
// second one would be a subprocess and a JSON decode on the path that runs
// before the first frame renders, for an answer already in hand.
func TestRunNavigatorMarksPanesOnlyWhenReuseIsOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		// probe = false keeps the run hermetic. Probing puts a SYN on the wire
		// per host, and this test is about a herdr round-trip, not a network.
		body string
		// The whole herdr call log, not just whether "pane list" is in it. The
		// snapshot and the pane list are the only calls this path can make,
		// because the operator cancels and HERDR_PANE_ID is empty.
		wantCalls string
		wantPanes int
	}{
		{
			name:      "reuse on marks",
			body:      "probe = false\nreuse_panes = true\n",
			wantCalls: "api snapshot | pane list",
			wantPanes: 1,
		},
		{
			name:      "reuse off does not mark",
			body:      "probe = false\nreuse_panes = false\n",
			wantCalls: "api snapshot | pane list",
			wantPanes: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			navigatorEnv(t, t.TempDir())
			t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir(t, tc.body))

			// Cancelling, so nothing past the picker runs. performSelection
			// reads ReusePanes as well and lists panes of its own, and in a flat
			// call log its list is indistinguishable from the picker's.
			pick, opts := stubNavigator(picker.NavSelection{}, false, nil)
			api, calls := fakeAPI(openPanesJSON)
			var screen bytes.Buffer
			if err := runNavigatorWith(&screen, strings.NewReader(""), pick, api); err != nil {
				t.Fatalf("runNavigatorWith: %v", err)
			}

			if got := strings.Join(joined(*calls), " | "); got != tc.wantCalls {
				t.Fatalf("herdr calls = %q, want %q", got, tc.wantCalls)
			}
			// The operator-visible half. An unpopulated map is what the picker
			// already reads as "no session pane exists", so no marker is drawn
			// and the reachability glyph underneath it survives.
			if got := len(opts.OpenPanes); got != tc.wantPanes {
				t.Fatalf("OpenPanes = %v, want %d entries", opts.OpenPanes, tc.wantPanes)
			}
			// A successful list must stay silent. The pane read prints "could
			// not list panes" when it fails, and that line above a working
			// picker would be the first thing the operator sees.
			wantQuiet(t, screen.String())
		})
	}
}

func TestRunConnectReportsARejectedConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", badPluginConfigDir(t))

	var stderr bytes.Buffer
	err := runConnectWith(&stderr, []string{"definitely-not-a-host"})
	if err == nil {
		t.Fatal("err = nil, want a not-found error")
	}
	// The rejected keys are reset to their defaults and the run continues, so
	// this line is the only record that the operator's config was not the one
	// that took effect. Reporting it is what stops "not found" from being their
	// only clue, and nothing else records it.
	wantDiagnostics(t, stderr.String(), "split_direction", "ignoring the rejected keys")
}

func TestRunConnectRejectsAnUnknownAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	err := runConnect([]string{"definitely-not-a-host"})
	if err == nil {
		t.Fatal("err = nil, want a not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %q", err)
	}
}

func TestRunConnectValidatesPlacement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")

	err := runConnect([]string{"host", "--placement", "sideways"})
	if err == nil || !strings.Contains(err.Error(), "must be split, tab, or zoomed") {
		t.Fatalf("err = %v, want a placement validation error", err)
	}
	if err := runConnect([]string{"host", "--placement"}); err == nil {
		t.Fatal("err = nil, want an error for a value-less --placement")
	}
	if err := runConnect([]string{"host", "--nope"}); err == nil {
		t.Fatal("err = nil, want an error for an unknown flag")
	}
}

func TestAFatalPaneExitIsPrintedExactlyOnce(t *testing.T) {
	// runSessionWith prints the message and holds the pane; main then hands what
	// it returned to reportFatal. Both write into one buffer here because on the
	// operator's terminal they are one screen — a second print lands underneath
	// the "press enter to close" prompt they have already answered, as the pane
	// is going away.
	//
	// strings.Count, not strings.Contains: "the message is present" is true in
	// both the fixed and the broken world, so it would assert nothing.
	sessionEnv(t, "", t.TempDir())

	var screen bytes.Buffer
	err := runSessionWith(&screen, strings.NewReader("\n"))
	if err == nil {
		t.Fatal("runSessionWith err = nil, want a fatal error")
	}
	reportFatal(&screen, err)

	if got := strings.Count(screen.String(), "herdr-picker:"); got != 1 {
		t.Errorf("the operator's screen was:\n%s\n\"herdr-picker:\" appears %d times, want 1", screen.String(), got)
	}
}

func TestReportFatalPrintsOnlyWhatIsNotOnScreenYet(t *testing.T) {
	// The other half of the guard above. Suppressing everything would trade a
	// duplicated message for a swallowed one, and these errors reach the
	// operator here or nowhere: nothing else prints them, and a pane
	// entrypoint's stderr is not in `herdr plugin log` to fall back on.
	usageErr := run(nil)
	if usageErr == nil {
		t.Fatal("run(nil) = nil, want a usage error to test with")
	}

	tests := []struct {
		name string
		err  error
		want int // times "herdr-picker:" must appear
	}{
		{"a usage error, printed nowhere else", usageErr, 1},
		{"any other unreported error", errors.New("boom"), 1},
		{"one fatalInPane already held on screen", reported{errors.New("boom")}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			reportFatal(&out, tc.err)
			if got := strings.Count(out.String(), "herdr-picker:"); got != tc.want {
				t.Errorf("reportFatal wrote %q; \"herdr-picker:\" appears %d times, want %d", out.String(), got, tc.want)
			}
		})
	}
}

// mainProbeEnv carries the verb that re-enters this test binary as the process
// under test. main calls os.Exit, so a subprocess is the only place it can be
// observed at all. mainProbeTest is the test holding the re-entry branch, so
// every probe re-runs that one test whichever test spawned it.
const (
	mainProbeEnv  = "HERDR_PICKER_TEST_MAIN_VERB"
	mainProbeTest = "TestMainDispatchesThroughRunAndSetsTheExitCode"
)

// runMain re-execs this test binary as the process under test. The two streams
// are kept apart rather than combined, so a caller can assert which stream a
// message went to as well as what order things appeared on it.
func runMain(t *testing.T, verb string, env ...string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+mainProbeTest+"$")
	// exec dedups by key keeping the last, so these override anything the
	// developer's own shell already had set.
	cmd.Env = append(os.Environ(), mainProbeEnv+"="+verb)
	cmd.Env = append(cmd.Env, env...)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()

	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("subprocess did not run: %v\nstdout:\n%s\nstderr:\n%s", err, outBuf.String(), errBuf.String())
	}
	return code, outBuf.String(), errBuf.String()
}

// fakeHerdr writes a herdr stand-in that answers every call successfully, so a
// verb can reach the end of run without a live herdr on the box.
func fakeHerdr(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "herdr")
	script := "#!/bin/sh\nprintf '%s' '{\"id\":1,\"result\":{}}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake herdr: %v", err)
	}
	return bin
}

func TestMainDispatchesThroughRunAndSetsTheExitCode(t *testing.T) {
	// Nothing else in this package reaches main: run, reportFatal, and every
	// verb are tested directly through the seam. So main could stop calling run,
	// stop calling reportFatal, or ignore run's error entirely, and the rest of
	// the file would stay green. This is the pin on that hop.
	if verb := os.Getenv(mainProbeEnv); verb != "" {
		// Flags are parsed before any test body runs, so os.Args is free to
		// rewrite here. Doing so makes the verb explicit rather than whatever
		// -test flags this binary happened to be handed, and it is still main
		// that has to read os.Args[1:] and hand it to run.
		os.Args = append([]string{"herdr-picker"}, strings.Fields(verb)...)
		main()
		// Reached only when run returned nil. A failing verb that gets here has
		// exited 0, which the parent's exit-code assertion catches.
		return
	}

	// Both cases are needed to pin one conditional: with only the failure, main
	// could exit 1 unconditionally; with only the success, it could exit 0
	// unconditionally. Neither alone says the exit code follows run's error.
	tests := []struct {
		name     string
		verb     string
		env      []string
		wantCode int
		wantOut  string
		wantMsgs int // times "herdr-picker:" must appear
	}{
		{
			name:     "a verb that fails is reported once and exits 1",
			verb:     "wat",
			wantCode: 1,
			wantOut:  "herdr-picker: usage:",
			wantMsgs: 1,
		},
		{
			// The one above is an error main is the first and only reporter of.
			// This one has already been printed and held by fatalInPane, so it
			// is the case errReported exists for, run end to end on a real
			// process: whichever way the suppression breaks — the marker not
			// set, not unwrapped, not checked, or main printing on its own
			// instead of calling reportFatal — the count moves off 1 here.
			// Stdin is /dev/null, so the hold reads EOF and returns at once.
			name: "a verb whose error was already held on screen is not printed again",
			verb: "session",
			env: []string{
				"HERDR_PICKER_TARGET=",
				"HERDR_PANE_ID=",
				"HERDR_PLUGIN_CONFIG_DIR=",
			},
			wantCode: 1,
			wantOut:  "press enter to close",
			wantMsgs: 1,
		},
		{
			name: "a verb that succeeds says nothing and exits 0",
			verb: "plugin open-navigator",
			env: []string{
				"HERDR_BIN_PATH=" + fakeHerdr(t),
				"HERDR_PANE_ID=w5:pA",
				"HERDR_TAB_ID=w5:t1",
				"HERDR_WORKSPACE_ID=w5",
			},
			wantCode: 0,
			wantMsgs: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runMain(t, tc.verb, tc.env...)
			// Both streams together, because the operator reads one pane:
			// fatalInPane writes to stdout and reportFatal to stderr, and a
			// duplicate across the two is still a duplicate on screen.
			out := stdout + stderr

			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d; output:\n%s", code, tc.wantCode, out)
			}
			if tc.wantOut != "" && !strings.Contains(out, tc.wantOut) {
				t.Errorf("output = %q, want it to contain %q", out, tc.wantOut)
			}
			// Same assertion as the pane exit, on a real process this time.
			if got := strings.Count(out, "herdr-picker:"); got != tc.wantMsgs {
				t.Errorf("output was:\n%s\n\"herdr-picker:\" appears %d times, want %d", out, got, tc.wantMsgs)
			}
		})
	}
}

// hiddenAlias and alsoHiddenAlias are defined only inside the unreadable
// includes, so they are reachable only if those includes parse. visibleAlias
// sits in the primary config and is always reachable.
const (
	hiddenAlias     = "client-jump"
	alsoHiddenAlias = "client-db"
	visibleAlias    = "direct-host"

	firstInclude  = "work.conf"
	secondInclude = "personal.conf"
)

// brokenIncludeHome writes an ssh config whose two Includes cannot be read. The
// parser skips a failed include entirely rather than failing, so every Host
// block inside it is absent from the result and the warning is the only record
// that anything went missing. Returns the HOME to run the child against.
//
// Exactly two warnings, cited at lines 1 and 2. Callers that count them —
// TestRunNavigatorBuildsTheFooterWarningsInAFixedOrder — depend on that number.
func brokenIncludeHome(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads through mode 0000, so the include would parse and there would be no warning")
	}

	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	// Two, not one. A loop asserted against a single-element fixture is
	// asserting that iteration happened at least once, not that it completed:
	// `print the first warning and break` is indistinguishable from `print
	// every warning` when there is only ever one. The second include costs
	// four lines and makes that difference observable.
	includes := []struct{ name, alias string }{
		{firstInclude, hiddenAlias},
		{secondInclude, alsoHiddenAlias},
	}
	for _, inc := range includes {
		path := filepath.Join(dir, inc.name)
		if err := os.WriteFile(path, []byte("Host "+inc.alias+"\n\tHostName 10.0.0.2\n"), 0o600); err != nil {
			t.Fatalf("write include %s: %v", inc.name, err)
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("chmod include %s: %v", inc.name, err)
		}
	}
	// Line 1 and line 2 of the primary config, which is what the warnings cite
	// and what wantIncludeWarning's callers pin.
	primary := "Include " + firstInclude + "\nInclude " + secondInclude +
		"\n\nHost " + visibleAlias + "\n\tHostName 10.0.0.1\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(primary), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home
}

// wantIncludeWarning asserts the warning for one include, whole and prefixed.
//
// Matching the payload alone ("include unreadable") leaves two things unpinned.
// The `herdr-picker: ` prefix is how an operator tells plugin output from ssh's
// own on a shared stderr, and dropping it changes nothing a payload match can
// see. And naming the specific include is what distinguishes a loop that
// printed every warning from one that printed the first and stopped.
func wantIncludeWarning(t *testing.T, stderr, home string, line int, include string) {
	t.Helper()
	want := fmt.Sprintf("herdr-picker: %s:%d: include unreadable: %s",
		filepath.Join(home, ".ssh", "config"), line, filepath.Join(home, ".ssh", include))
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q,\nwant it to contain %q", stderr, want)
	}
}

func TestConnectExplainsAnAliasThatWentMissingWithIt(t *testing.T) {
	// Not "a warning is printed somewhere" — that passes even when the print
	// lands where the operator never reaches it. The alias asked for here is
	// defined only inside the include that failed to parse, so the discarded
	// warning is causally the reason the lookup fails. Without it the operator
	// is told a host does not exist while looking straight at it in their own
	// file.
	home := brokenIncludeHome(t)
	code, stdout, stderr := runMain(t, "connect "+hiddenAlias,
		"HOME="+home,
		"HERDR_PLUGIN_CONFIG_DIR=",
		"HERDR_BIN_PATH="+fakeHerdr(t),
	)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// Both, not just the first: the loop has to finish, not merely start.
	wantIncludeWarning(t, stderr, home, 1, firstInclude)
	wantIncludeWarning(t, stderr, home, 2, secondInclude)

	warning := strings.Index(stderr, "include unreadable")
	notFound := strings.Index(stderr, "not found in ssh config")
	if warning < 0 {
		t.Errorf("stderr = %q, want the include warning that explains the miss", stderr)
	}
	if notFound < 0 {
		t.Errorf("stderr = %q, want the not-found error", stderr)
	}
	// Ordering is part of the behaviour, not presentation. Printed after the
	// lookup, the explanation arrives below the accusation, or on the success
	// path not at all. Both indexes are already known present above.
	if warning >= 0 && notFound >= 0 && warning > notFound {
		t.Errorf("the explanation prints after the error it explains:\n%s", stderr)
	}
	if strings.Contains(stdout, "include unreadable") {
		t.Errorf("warning went to stdout, not stderr: %q", stdout)
	}
}

func TestConnectWarnsEvenWhenTheAliasStillResolves(t *testing.T) {
	// The case that pins placement rather than mere presence. A broken include
	// can coexist with an alias that resolves; if the warnings print after the
	// lookup loop, this path returns first and the operator never learns that
	// the rest of their config is missing. Only this test notices that.
	home := brokenIncludeHome(t)
	code, stdout, stderr := runMain(t, "connect "+visibleAlias,
		"HOME="+home,
		"HERDR_PLUGIN_CONFIG_DIR=",
		"HERDR_BIN_PATH="+fakeHerdr(t),
		"HERDR_PANE_ID=w5:pA",
		"HERDR_TAB_ID=w5:t1",
		"HERDR_WORKSPACE_ID=w5",
	)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 — %s resolves: stdout:\n%s\nstderr:\n%s", code, visibleAlias, stdout, stderr)
	}
	// Both warnings, on the path that succeeds. The success path is where a
	// short-circuiting loop hides best: the operator gets their session, so
	// nothing looks wrong, and the half of their config that vanished is
	// reported half as loudly.
	wantIncludeWarning(t, stderr, home, 1, firstInclude)
	wantIncludeWarning(t, stderr, home, 2, secondInclude)
}
