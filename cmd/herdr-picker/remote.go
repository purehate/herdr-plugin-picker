package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// remote.go is the machines tab's pane entrypoint. It attaches a full herdr
// client to a saved machine's own server — `herdr --remote <target>` — which is
// a different thing from the ssh tab's shell on the same host.

// runRemote replaces this process with a remote herdr client.
func runRemote() error { return runRemoteWith(os.Stdout, os.Stdin) }

// runRemoteWith is runRemote with the operator's terminal injected, so the fatal
// path can be driven by a test. It mirrors runSessionWith: same env contract,
// same exec-and-hold-on-failure shape.
func runRemoteWith(out io.Writer, in io.Reader) error {
	target := os.Getenv("HERDR_PICKER_TARGET")
	if target == "" {
		return fatalInPane(out, in, errors.New("HERDR_PICKER_TARGET is not set"))
	}
	argv := remoteArgv(target, os.Getenv("HERDR_PICKER_SESSION"))
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return fatalInPane(out, in, fmt.Errorf("cannot run %v: herdr not found on PATH: %w", argv, err))
	}
	// Exec, so herdr owns the pty: no wrapper process, and detaching closes the
	// pane. It returns only on failure — on success this process no longer
	// exists, which makes the hold below unreachable rather than skipped.
	execErr := syscall.Exec(resolved, argv, os.Environ())
	return fatalInPane(out, in, fmt.Errorf("exec %s: %w", resolved, execErr))
}

// remoteArgv builds `herdr --remote <target> [--session <name>]`. --session is
// only passed when the profile names one; an unset session is the remote's
// default and does not need saying.
func remoteArgv(target, session string) []string {
	argv := []string{"herdr", "--remote", target}
	if session != "" {
		argv = append(argv, "--session", session)
	}
	return argv
}
