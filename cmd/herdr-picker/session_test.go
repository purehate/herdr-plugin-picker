package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/pluginconfig"
)

var errRenameTest = errors.New("rename failed")

func TestSessionArgvPutsFlagsBeforeDestination(t *testing.T) {
	// ssh parses `ssh [options] destination [command]`. Flags after the
	// destination become a remote command, so order is a correctness issue.
	got := sessionArgv([]string{"-o", "ConnectTimeout=5"}, "nixos-dev")
	want := "ssh -o ConnectTimeout=5 -- nixos-dev"
	if strings.Join(got, " ") != want {
		t.Fatalf("argv = %v, want %q", got, want)
	}
}

func TestSessionArgvWithoutFlags(t *testing.T) {
	if got := sessionArgv(nil, "web1"); strings.Join(got, " ") != "ssh -- web1" {
		t.Fatalf("argv = %v", got)
	}
}

func TestSessionArgvTreatsADashAliasAsTheDestination(t *testing.T) {
	// An ssh config may name a host with a leading dash. Without the `--`, ssh's
	// option parser claims it and `Host -oProxyCommand=...` runs a command
	// instead of connecting. The whole slice, so a dropped separator is caught
	// rather than absorbed into a substring match.
	got := sessionArgv(nil, "-oProxyCommand=echo")
	want := []string{"ssh", "--", "-oProxyCommand=echo"}
	if !slices.Equal(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestSessionLabel(t *testing.T) {
	if got := sessionLabel("nixos-dev"); got != "ssh:nixos-dev" {
		t.Fatalf("sessionLabel = %q, want ssh:nixos-dev", got)
	}
}

func TestPrepareSessionRenamesOwnPane(t *testing.T) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}}

	argv, err := prepareSession(io.Discard, api, pluginconfig.Defaults(), "nixos-dev", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if strings.Join(argv, " ") != "ssh -- nixos-dev" {
		t.Errorf("argv = %v", argv)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "pane rename w5:pC ssh:nixos-dev" {
		t.Errorf("calls = %v", calls)
	}
}

func TestPrepareSessionRequiresATarget(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) { return nil, nil }}
	if _, err := prepareSession(io.Discard, api, pluginconfig.Defaults(), "", "w5:pC"); err == nil {
		t.Fatal("err = nil, want an error for a missing target")
	}
}

func TestPrepareSessionToleratesRenameFailure(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte("no such pane"), errRenameTest
	}}
	// A failed rename costs pane reuse, not the connection. Connect anyway.
	var screen bytes.Buffer
	argv, err := prepareSession(&screen, api, pluginconfig.Defaults(), "web1", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if strings.Join(argv, " ") != "ssh -- web1" {
		t.Fatalf("argv = %v", argv)
	}
	// The consequence is deferred and invisible: this session connects fine, and
	// the cost lands on some later picker run that cannot find the pane to reuse
	// and opens a duplicate. Nothing at that point can explain why. Without this
	// assertion the test passes on a prepareSession that drops the error, which
	// is precisely the version that makes the later behaviour unexplainable.
	wantDiagnostics(t, screen.String(), "could not label pane", errRenameTest.Error())
}

func TestPrepareSessionSaysNothingWhenTheRenameWorks(t *testing.T) {
	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	var screen bytes.Buffer
	// The pane is about to be handed to ssh, so anything written here stays on
	// the operator's screen underneath their session. The tolerance test above
	// asserts the warning appears; only this one stops it appearing on every
	// successful connect.
	if _, err := prepareSession(&screen, api, pluginconfig.Defaults(), "web1", "w5:pC"); err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	wantQuiet(t, screen.String())
}

func TestPrepareSessionPassesConfiguredSSHArgs(t *testing.T) {
	// prepareSession is the only production caller of sessionArgv, and every
	// other fixture in this file uses Defaults(), whose SSHArgs is nil. Without
	// a populated one, prepareSession could drop the operator's ssh_args
	// entirely and the whole file would stay green.
	cfg := pluginconfig.Defaults()
	cfg.SSHArgs = []string{"-o", "ConnectTimeout=5"}

	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	argv, err := prepareSession(io.Discard, api, cfg, "nixos-dev", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if got, want := strings.Join(argv, " "), "ssh -o ConnectTimeout=5 -- nixos-dev"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestPrepareSessionRenamesNothingItShouldNot(t *testing.T) {
	var calls [][]string
	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":1,"result":{}}`), nil
	}}

	// Outside herdr there is no HERDR_PANE_ID, so there is no pane to label.
	// Renaming anyway would send `pane rename "" ssh:nixos-dev`.
	if _, err := prepareSession(io.Discard, api, pluginconfig.Defaults(), "nixos-dev", ""); err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want none without a pane id", calls)
	}

	// A missing target must be rejected before anything is renamed: a pane left
	// labeled "ssh:" outlives the error and would be reused by the next connect.
	if _, err := prepareSession(io.Discard, api, pluginconfig.Defaults(), "", "w5:pC"); err == nil {
		t.Fatal("err = nil, want an error for a missing target")
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want none for a missing target", calls)
	}
}

// readSpy records whether anything actually read from it. Asserting on the
// printed message alone cannot tell a held pane from one that printed and
// exited, and the exit is what loses the message — a pane entrypoint's output
// reaches the screen and nowhere else, so nothing is left to read afterwards.
// Only the read proves the hold is there.
type readSpy struct {
	r     io.Reader
	reads int
}

func (s *readSpy) Read(p []byte) (int, error) {
	s.reads++
	return s.r.Read(p)
}

// sessionEnv isolates a runSessionWith call from the ambient environment. An
// empty HERDR_PANE_ID matters: it keeps prepareSession from shelling out to
// herdr, which is not present under test.
func sessionEnv(t *testing.T, target, path string) {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	t.Setenv("HERDR_PICKER_TARGET", target)
	t.Setenv("HERDR_PANE_ID", "")
	t.Setenv("PATH", path)
}

func TestRunSessionHoldsThePaneOnEveryFatalExit(t *testing.T) {
	// ssh that LookPath accepts and execve rejects: the file is executable but
	// is neither a Mach-O/ELF image nor a `#!` script, so execve fails ENOEXEC
	// and this process survives to make an assertion.
	badBinDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(badBinDir, "ssh"), []byte("\x00\x01not a binary\x02"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}

	tests := []struct {
		name   string
		target string
		path   string
		want   string // a substring the operator must be shown
	}{
		{
			name:   "target missing",
			target: "",
			path:   badBinDir,
			want:   "HERDR_PICKER_TARGET is not set",
		},
		{
			name:   "ssh not on PATH",
			target: "nixos-dev",
			path:   t.TempDir(), // empty: no ssh anywhere on it
			want:   "ssh not found on PATH",
		},
		{
			name:   "exec fails",
			target: "nixos-dev",
			path:   badBinDir,
			want:   "exec ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sessionEnv(t, tc.target, tc.path)

			var out bytes.Buffer
			in := &readSpy{r: strings.NewReader("\n")}

			err := runSessionWith(&out, in)
			if err == nil {
				t.Fatal("err = nil, want a fatal error")
			}
			if got := out.String(); !strings.Contains(got, tc.want) {
				t.Errorf("output = %q, want it to contain %q", got, tc.want)
			}
			// The error is the operator's only copy, so it must reach the screen
			// rather than only the return value.
			if got := out.String(); !strings.Contains(got, err.Error()) {
				t.Errorf("output = %q, does not contain the returned error %q", got, err)
			}
			if !strings.Contains(out.String(), "press enter to close") {
				t.Errorf("output = %q, want the hold prompt", out.String())
			}
			if in.reads == 0 {
				t.Error("nothing read from stdin: the pane exits without holding, so the message above is lost")
			}
		})
	}
}

// badPluginConfigDir returns a config dir whose config.toml is rejected. Load
// still yields a usable Config from it, so this exercises the survivable arm:
// the run continues, and the only trace is what gets written out.
//
// probe = false is load-bearing, not decoration. It parses cleanly, so it adds
// no second error, but Probe defaults to true and runPicker probes whenever it
// has hosts — which would put a SYN per fixture host onto the network of
// whoever runs the tests.
func badPluginConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := "probe = false\nsplit_direction = \"sideways\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	return dir
}

func TestRunSessionReportsARejectedConfigBeforeItBlocks(t *testing.T) {
	sessionEnv(t, "", t.TempDir())
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", badPluginConfigDir(t))

	var screen bytes.Buffer
	in := &readSpy{r: strings.NewReader("\n")}
	if err := runSessionWith(&screen, in); err == nil {
		t.Fatal("runSessionWith err = nil, want a fatal error")
	}

	// Onto the same stream the hold writes to, and before it. A pane owns one
	// screen: a warning written to os.Stderr instead is invisible to this test
	// but not to the operator, and a warning written after the read has already
	// been dismissed by the keypress that releases the pane. Order here is not
	// cosmetic — it is the difference between a message the operator reads and
	// one that scrolls past as the pane closes.
	wantDiagnostics(t, screen.String(),
		"split_direction",
		"ignoring the rejected keys",
		"HERDR_PICKER_TARGET is not set",
		"press enter to close",
	)
}

func TestRunSessionHoldsTheRealTerminal(t *testing.T) {
	// runSession is what Task 19's main calls; runSessionWith is only the seam.
	// Nothing above this reaches runSession, so if the delegation were dropped
	// every fatal path would go unheld in production while the table stayed
	// green. Swapping os.Stdout and os.Stdin is the only way to observe that.
	//
	// That swap constrains exactly two things, and nothing wider. This test must
	// stay serial: no t.Parallel() in it, and no t.Parallel() subtest under it.
	// And no test that does call t.Parallel() may read or swap os.Stdout or
	// os.Stdin. Every other test in this package is free to be parallelised —
	// testing runs a parallel test only alongside other parallel tests, so a
	// serial test never overlaps one, and a parent's deferred restore runs
	// before its parallel subtests are released.
	sessionEnv(t, "", t.TempDir())

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	// The trailing sentinel is how the read is observed. A readSpy cannot be
	// used here because os.Stdin is an *os.File, so instead: the hold consumes
	// exactly the newline and stops, leaving the sentinel behind. If os.Stdin
	// were not wired through, nothing is consumed and the newline is still there.
	if _, err := inW.WriteString("\nsentinel"); err != nil {
		t.Fatalf("prime stdin: %v", err)
	}
	_ = inW.Close()

	stdout, stdin := os.Stdout, os.Stdin
	defer func() { os.Stdout, os.Stdin = stdout, stdin }()
	os.Stdout, os.Stdin = outW, inR

	runErr := runSession()

	// Close the write end before reading, or ReadAll never sees EOF. The message
	// is a few dozen bytes, well inside the pipe buffer, so the write above
	// cannot have blocked waiting for this.
	_ = outW.Close()
	got, err := io.ReadAll(outR)
	_ = outR.Close()
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	unread, err := io.ReadAll(inR)
	_ = inR.Close()
	if err != nil {
		t.Fatalf("read stdin remainder: %v", err)
	}

	if runErr == nil {
		t.Fatal("runSession err = nil, want a fatal error")
	}
	if string(unread) != "sentinel" {
		t.Errorf("stdin remainder = %q, want %q: runSession did not hold on the real os.Stdin", unread, "sentinel")
	}
	if !strings.Contains(string(got), "press enter to close") {
		t.Errorf("runSession stdout = %q, want the hold prompt on os.Stdout", got)
	}
	if !strings.Contains(string(got), runErr.Error()) {
		t.Errorf("runSession stdout = %q, does not contain the returned error %q", got, runErr)
	}
}

// pluginConfigDir writes a config.toml with the given body and returns its dir.
func pluginConfigDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	return dir
}

// fakeSSHDir writes an `ssh` that execve accepts, and returns the dir to put on
// PATH.
//
// The opposite of the ENOEXEC stand-in above, and deliberately so. That one is
// a file execve rejects, which is what makes every fatal branch reachable — and
// it is also why nothing could observe the successful branch: on success this
// process is replaced, so an in-process test has no one left to make an
// assertion. The harness that opened the failure paths foreclosed the only path
// where argv and the environment are visible at all.
//
// This script is the other side of that hop. It lets the exec succeed and
// reports what it was actually handed, which is the only place that can be
// seen. printf is a shell builtin, so it still works under the empty
// environment a dropped os.Environ() would produce — a fixture needing PATH to
// find its own tools would fail for the wrong reason there.
func fakeSSHDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '" + sshRanSentinel + "\\n'\n" +
		"printf 'ARGV0=%s\\n' \"$0\"\n" +
		"printf 'ARGV=%s\\n' \"$*\"\n" +
		"printf 'AUTH_SOCK=%s\\n' \"$SSH_AUTH_SOCK\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	return dir
}

const (
	// sshRanSentinel proves the exec happened. Without it a dropped argv and a
	// failed exec are the same observation: both leave the wrong host absent
	// from stdout, so an assertion phrased as "the wrong host does not appear"
	// passes when nothing ran at all.
	sshRanSentinel = "FAKE_SSH_RAN"
	// authSockSentinel is compared by value, not presence. syscall.Exec with a
	// nil environment still runs the script, so an assertion that the variable
	// is merely set-or-unset turns on whether the developer running the tests
	// happens to have an agent.
	authSockSentinel = "/tmp/herdr-picker-test-agent.sock"
)

func TestSessionExecsSSHWithTheArgvAndEnvironmentItBuilt(t *testing.T) {
	// The one hop in this file that a fake can only cross by letting it work.
	// syscall.Exec returns only on failure, so every other test here observes
	// the arguments by never delivering them; this one delivers them and reads
	// them back from the far side.
	cfg := pluginConfigDir(t, "probe = false\nssh_args = [\"-o\", \"ConnectTimeout=5\"]\n")
	sshDir := fakeSSHDir(t)
	code, stdout, stderr := runMain(t, "session",
		"PATH="+sshDir,
		"HERDR_PICKER_TARGET=nixos-dev",
		"HERDR_PANE_ID=",
		"HERDR_PLUGIN_CONFIG_DIR="+cfg,
		"SSH_AUTH_SOCK="+authSockSentinel,
	)

	// First, and fatal: everything below is meaningless if ssh never ran.
	if !strings.Contains(stdout, sshRanSentinel) {
		t.Fatalf("ssh never executed — exec failed instead.\ncode = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	// The whole argv as one line, not a substring of it. Truncating argv drops
	// the flags and the destination together, so matching on the destination
	// alone leaves the half that silently discards the operator's ssh_args
	// alive, and matching on a flag alone leaves the half that connects
	// somewhere they did not ask for.
	if want := "ARGV=-o ConnectTimeout=5 -- nixos-dev\n"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q,\nwant the exact argv line %q", stdout, want)
	}
	// $0 here is the path handed to execve, not argv[0]: for a `#!` script the
	// kernel re-invokes the interpreter as `/bin/sh <execve-path> <argv[1:]>`
	// and drops argv[0] entirely. That makes it a direct reading of the
	// LookPath result, which is the thing worth pinning — passing argv[0]
	// instead would send execve a bare "ssh", and execve does not search PATH,
	// so it would fail outright on a machine with no ./ssh in the cwd and
	// succeed on one that has it.
	if want := "ARGV0=" + filepath.Join(sshDir, "ssh") + "\n"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q,\nwant %q — exec did not get the resolved path", stdout, want)
	}
	// ssh inherits this process's environment or it cannot reach the agent, and
	// the operator blames their agent rather than this plugin.
	if want := "AUTH_SOCK=" + authSockSentinel + "\n"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q,\nwant %q — the environment did not survive the exec", stdout, want)
	}
}

func TestSessionKeepsTheValidKeysOfARejectedConfig(t *testing.T) {
	// session.go's comment says replacing the Config with Defaults() here
	// "would undo a valid `probe = false` because of an unrelated typo", and
	// LoadDir's own doc says callers must report the error and then use the
	// returned Config. Both state the hazard; neither was observable, because
	// runSession reads only cfg.SSHArgs and the one test reaching this branch
	// died on an empty target first.
	//
	// So: a file that parses, carries a good ssh_args and one bad key, and a
	// target that lets the run get far enough to use it. The warning proves the
	// rejecting arm was taken; the argv proves the survivable arm survived.
	cfg := pluginConfigDir(t, "probe = false\nssh_args = [\"-o\", \"ConnectTimeout=5\"]\nsplit_direction = \"sideways\"\n")
	code, stdout, stderr := runMain(t, "session",
		"PATH="+fakeSSHDir(t),
		"HERDR_PICKER_TARGET=nixos-dev",
		"HERDR_PANE_ID=",
		"HERDR_PLUGIN_CONFIG_DIR="+cfg,
		"SSH_AUTH_SOCK="+authSockSentinel,
	)

	if !strings.Contains(stdout, sshRanSentinel) {
		t.Fatalf("ssh never executed — exec failed instead.\ncode = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	wantDiagnostics(t, stdout, "split_direction", "ignoring the rejected keys")
	if want := "ARGV=-o ConnectTimeout=5 -- nixos-dev\n"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q,\nwant %q — the rejected key took the valid ssh_args with it", stdout, want)
	}
}

func TestFatalInPaneLeavesTheCauseMatchable(t *testing.T) {
	// reported's comment claims three things: Error() is unchanged, errors.Is
	// finds errReported, and errors.Is still finds the original. Only the
	// middle one was asserted — and it is the one that would have failed
	// loudly, since main suppressing nothing is visible immediately. A caller
	// matching on the underlying error is the quiet half.
	cause := errors.New("the underlying failure")
	err := fatalInPane(io.Discard, strings.NewReader("\n"), cause)

	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false: wrapping the error to mark it printed also made it unmatchable")
	}
	if !errors.Is(err, errReported) {
		t.Errorf("errors.Is(err, errReported) = false: main would print it a second time")
	}
	if err.Error() != cause.Error() {
		t.Errorf("Error() = %q, want it unchanged at %q", err.Error(), cause.Error())
	}
}

func TestSessionCommandUsesSSHByDefault(t *testing.T) {
	if got := sessionCommand(pluginconfig.Defaults(), "web1"); strings.Join(got, " ") != "ssh -- web1" {
		t.Fatalf("argv = %v, want ssh", got)
	}
}

func TestSessionCommandUsesMoshWhenConfigured(t *testing.T) {
	cfg := pluginconfig.Defaults()
	cfg.Mosh = true
	if got := sessionCommand(cfg, "web1"); strings.Join(got, " ") != "mosh -- web1" {
		t.Fatalf("argv = %v, want mosh", got)
	}
}

// mosh takes the ssh command as one --ssh string, so the configured flags are
// joined into it rather than appended as separate arguments.
func TestMoshArgvHandsSSHFlagsToMosh(t *testing.T) {
	cfg := pluginconfig.Defaults()
	cfg.Mosh = true
	cfg.SSHArgs = []string{"-o", "ConnectTimeout=5"}
	got := sessionCommand(cfg, "web1")
	want := "mosh --ssh=ssh -o ConnectTimeout=5 -- web1"
	if strings.Join(got, " ") != want {
		t.Fatalf("argv = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestPrepareSessionUsesMoshWhenConfigured(t *testing.T) {
	cfg := pluginconfig.Defaults()
	cfg.Mosh = true
	api := herdrapi.Client{Run: func([]string) ([]byte, error) {
		return []byte(`{"id":1,"result":{}}`), nil
	}}
	argv, err := prepareSession(io.Discard, api, cfg, "nixos-dev", "w5:pC")
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if got, want := strings.Join(argv, " "), "mosh -- nixos-dev"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}
