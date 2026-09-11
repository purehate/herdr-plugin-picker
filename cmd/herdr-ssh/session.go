package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
)

const labelPrefix = "ssh:"

func sessionLabel(alias string) string { return labelPrefix + alias }

// sessionArgv builds ssh's argv. Configured flags go before the destination;
// anything after it would be sent to the remote shell as a command.
//
// The `--` keeps an alias from being read as a flag. An ssh config may name a
// host with a leading `-`, and without the separator ssh's option parser claims
// it: `Host -oProxyCommand=...` would hand ssh a proxy command instead of a
// destination. With it, the alias is always the destination, which is what this
// function intends.
func sessionArgv(sshArgs []string, alias string) []string {
	argv := make([]string, 0, len(sshArgs)+3)
	argv = append(argv, "ssh")
	argv = append(argv, sshArgs...)
	return append(argv, "--", alias)
}

// prepareSession labels this pane so the picker can find it again, then returns
// the argv to exec. A rename failure is logged to out and ignored: losing pane
// reuse is much cheaper than losing the connection the operator asked for.
func prepareSession(out io.Writer, api herdrapi.Client, cfg pluginconfig.Config, alias, paneID string) ([]string, error) {
	if alias == "" {
		return nil, errors.New("HERDR_SSH_TARGET is not set")
	}
	if paneID != "" {
		if err := api.PaneRename(paneID, sessionLabel(alias)); err != nil {
			_, _ = fmt.Fprintf(out, "herdr-ssh: could not label pane: %v\n", err)
		}
	}
	return sessionArgv(cfg.SSHArgs, alias), nil
}

// errReported marks an error whose message fatalInPane has already put on the
// operator's screen and held the pane until they read it. main tests for it, so
// that a fatal pane exit is not printed a second time after the keypress.
var errReported = errors.New("already reported")

// reported pairs an error with errReported without disturbing it: Error() is
// unchanged and errors.Is still finds the original, so a caller matching on the
// underlying error is unaffected by the fact that it was printed.
type reported struct{ err error }

func (r reported) Error() string   { return r.err.Error() }
func (r reported) Unwrap() []error { return []error{r.err, errReported} }

// fatalInPane reports why the pane is about to close, then holds it open until
// the operator acknowledges. Every fatal exit from a pane verb goes through
// here; returning bare from one loses the diagnostic outright, because a pane
// process owns a pty and its output reaches the terminal rather than a pipe
// herdr can read — `herdr plugin log` captures actions and event hooks, never
// pane entrypoints. The screen is the only channel. The returned error carries
// errReported so main does not print it again.
func fatalInPane(out io.Writer, in io.Reader, err error) error {
	_, _ = fmt.Fprintf(out, "herdr-ssh: %v\n\npress enter to close\n", err)
	_, _ = fmt.Fscanln(in)
	return reported{err}
}

// runSession replaces this process with ssh.
func runSession() error { return runSessionWith(os.Stdout, os.Stdin) }

// runSessionWith is runSession with the operator's terminal injected, so that
// the fatal paths — which all block for a keypress — can be driven by a test.
func runSessionWith(out io.Writer, in io.Reader) error {
	// Load always returns a usable Config, with only the rejected keys reset, so
	// report the error and keep the Config. Replacing it with Defaults() here
	// would undo a valid `probe = false` because of an unrelated typo.
	cfg, err := pluginconfig.LoadDir(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		// Deliberately not a fatalInPane: this one is survivable, and the exec
		// below leaves the operator a live pane to read it in. Onto out, not
		// os.Stderr: a pane has exactly one output channel, and both land on the
		// same screen anyway — but a test can tell them apart, and the split was
		// an accident rather than a decision.
		_, _ = fmt.Fprintf(out, "herdr-ssh: %v — ignoring the rejected keys\n", err)
	}

	argv, err := prepareSession(out, herdrapi.New(), cfg, os.Getenv("HERDR_SSH_TARGET"), os.Getenv("HERDR_PANE_ID"))
	if err != nil {
		// A broken env contract: plausible as an install or packaging fault, and
		// the operator sees only a pane that vanished unless we hold it.
		return fatalInPane(out, in, err)
	}

	bin, err := exec.LookPath("ssh")
	if err != nil {
		return fatalInPane(out, in, fmt.Errorf("cannot run %v: ssh not found on PATH: %w", argv, err))
	}

	// Exec, so ssh owns the pty: no wrapper process, and ^d closes the pane. It
	// returns only on failure — on success this process no longer exists, which
	// makes the hold below unreachable rather than skipped.
	execErr := syscall.Exec(bin, argv, os.Environ())
	return fatalInPane(out, in, fmt.Errorf("exec %s: %w", bin, execErr))
}
