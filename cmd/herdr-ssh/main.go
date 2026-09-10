// Command herdr-ssh is the SSH picker plugin for herdr.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/purehate/herdr-plugin-ssh/internal/herdrapi"
	"github.com/purehate/herdr-plugin-ssh/internal/picker"
	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

const usage = `usage: herdr-ssh <picker|session|connect <alias> [--placement split|tab|zoomed]|plugin open-picker>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		reportFatal(os.Stderr, err)
		os.Exit(1)
	}
}

// reportFatal prints err on the way out — unless fatalInPane already put it on
// the operator's screen and held the pane until they read it. Printing it again
// would show the same line twice: once above the "press enter to close" prompt,
// and once more after the keypress, when the pane is already going away.
//
// Only errors carrying errReported are suppressed. Everything else has been
// reported nowhere yet — the usage errors, runConnect's flag errors,
// openPicker's writeCaller failure — and this is the only place they would ever
// be printed, so they must still come out here.
func reportFatal(out io.Writer, err error) {
	if errors.Is(err, errReported) {
		return
	}
	_, _ = fmt.Fprintf(out, "herdr-ssh: %v\n", err)
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "picker":
		return runPicker()
	case "session":
		return runSession()
	case "connect":
		if len(args) < 2 {
			return errors.New(usage)
		}
		return runConnect(args[1:])
	case "plugin":
		if len(args) < 2 || args[1] != "open-picker" {
			return errors.New(usage)
		}
		return openPicker(herdrapi.New())
	default:
		return errors.New(usage)
	}
}

// openPicker runs in the caller's pane: it records where the operator was, then
// opens the overlay.
func openPicker(api herdrapi.Client) error {
	if err := writeCaller(os.Getenv("HERDR_PLUGIN_STATE_DIR"), currentCaller()); err != nil {
		return err
	}
	return api.PluginPaneOpen(herdrapi.OpenOpts{
		Plugin:     pluginID,
		Entrypoint: "picker",
		Placement:  "overlay",
		Focus:      true,
	})
}

// pickerFn is picker.Run's signature, injected so everything around the picker
// can be tested without a terminal to draw into.
//
// A parameter rather than a package-level variable, following internal/probe's
// dialFn for the same reason argued there: as a variable, every test wanting a
// fake would assign it and restore it with a defer, making it shared mutable
// state that races both the other tests and runPicker's own read of it. Passing
// it in makes that race unrepresentable rather than merely discouraged.
type pickerFn func(picker.Options) (picker.Selection, bool, error)

// runPicker draws the overlay and acts on the operator's choice.
func runPicker() error {
	return runPickerWith(os.Stdout, os.Stdin, picker.Run, herdrapi.New())
}

// runPickerWith is runPicker with the operator's terminal, the picker, and the
// herdr client injected. Without the picker seam nothing below picker.Run is
// reachable from a test: every path past it needs a selection, and producing
// one needs a tty.
func runPickerWith(out io.Writer, in io.Reader, pick pickerFn, api herdrapi.Client) error {
	// Keep the returned Config; only the rejected keys were reset. See runSession.
	cfg, cfgErr := pluginconfig.LoadDir(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	// theme.LoadFile always returns a usable Theme, so th is safe to render with
	// even when themeErr is non-nil. The error is reported, not acted on.
	th, themeErr := theme.LoadFile(os.Getenv("HERDR_CONFIG_PATH"))

	hosts, warnings := loadHosts(sshConfigPath(), cfg)
	// Surface load errors in the footer rather than writing them out. This path
	// has an overlay to render into, and the footer is where the operator is
	// already looking; a plain write would survive (no alt-screen switch) but
	// prints above the overlay instead of in it. runSession and runConnect have
	// no picker, so theirs go straight to their diagnostic stream. Built in a
	// fixed order rather than prepended twice, which would silently reverse them.
	var loadWarnings []string
	if cfgErr != nil {
		// One sentence for one failure, whichever verb produced it: this is the
		// body runConnectWith prints too, and the "herdr-ssh: " it prefixes there
		// is the stream's, not the message's. That prefix says who is speaking on
		// a stderr shared with ssh and the operator's own shell; inside the
		// picker's footer there is nobody else to confuse it with, so repeating it
		// here would be noise in the line the operator has to read. Adding a
		// "plugin config: " label of its own was the same mistake twice over —
		// pluginconfig's error already opens with "invalid plugin config".
		loadWarnings = append(loadWarnings, fmt.Sprintf("%v — ignoring the rejected keys", cfgErr))
	}
	if themeErr != nil {
		// The same reasoning as above, applied to the other loader: theme.LoadFile
		// already opens with "theme config", so a "theme: " of its own said the
		// word the operator had just read. Only the remedy clause is ours.
		loadWarnings = append(loadWarnings, fmt.Sprintf("%v — using the default palette", themeErr))
	}
	warnings = append(loadWarnings, warnings...)

	// Only ask herdr for the session panes when reuse is on. OpenPanes exists to
	// paint the ▪ marker, and the marker's whole claim is that enter focuses the
	// session that is already there instead of opening a second one — a promise
	// only performSelection's `cfg.ReusePanes && !sel.ForceNew` branch can keep.
	// With reuse off that branch never runs, so the marker would sit on hosts
	// where enter opens another pane, and it would do so while suppressing the
	// reachability glyph it deliberately outranks. The tiebreak is justified by
	// ▪ being the marker that changes what enter does; where it no longer
	// changes that, it is a confident claim hiding an accurate one.
	//
	// Gated at the population rather than after it, because the call is the
	// cost: openSessions spawns `herdr pane list` and decodes its reply on the
	// picker's startup path, ahead of the first frame, and with reuse off
	// nothing downstream can read the result. Left nil, which is already the
	// no-sessions case every reader handles.
	//
	// ReusePanes alone, without ForceNew: that key is pressed inside the picker,
	// so there is no selection to consult yet at this point.
	var openPanes map[string]string
	if cfg.ReusePanes {
		openPanes = openSessions(out, api)
	}

	opts := picker.Options{
		Hosts:       hosts,
		Theme:       th,
		ShowPreview: cfg.ShowPreview,
		OpenPanes:   openPanes,
		Warnings:    warnings,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.Probe && len(hosts) > 0 {
		opts.Probes = probe.Run(ctx, targetsFor(hosts), time.Duration(cfg.ProbeTimeoutMS)*time.Millisecond)
	}

	sel, ok, err := pick(opts)
	if err != nil {
		// Same reasoning as the selection failure below: this is a pane
		// entrypoint, so returning bare drops the message into a pane that is
		// closing and it is never seen. Deliberately no closeOverlay — the fence
		// does not close here either, and whether a pane whose picker never
		// rendered needs an explicit close is a question for the operator's
		// first run, not something to guess at.
		return fatalInPane(out, in, err)
	}

	self := os.Getenv("HERDR_PANE_ID")
	if !ok {
		closeOverlay(out, api, self)
		return nil
	}

	if err := performSelection(out, api, cfg, sel, readCaller(os.Getenv("HERDR_PLUGIN_STATE_DIR"))); err != nil {
		// Hold the overlay open with the error on screen, and only then close.
		// Closing first would take the only explanation with it.
		//
		// The footer argument above does not reach this error: picker.Run has
		// already returned, so there is no overlay left to render into. It is a
		// fatal pane exit exactly like runSession's, so it goes through the same
		// helper and onto the same stream rather than re-inlining it on stderr.
		// The leading newline is the one picker-specific part — bubbletea leaves
		// the cursor on the last frame's line, so without it the error is glued
		// to the picker's final row. runSession's pane is fresh and needs no
		// separator, which is why this stays at the call site.
		_, _ = fmt.Fprintln(out)
		err = fatalInPane(out, in, err)
		closeOverlay(out, api, self)
		return err
	}
	closeOverlay(out, api, self)
	return nil
}

// closeOverlay dismisses the picker pane. Best effort: if the pane is already
// gone, saying so is noise.
func closeOverlay(out io.Writer, api herdrapi.Client, paneID string) {
	if paneID == "" {
		return
	}
	if err := api.PaneClose(paneID); err != nil {
		_, _ = fmt.Fprintf(out, "herdr-ssh: could not close the picker pane: %v\n", err)
	}
}

// runConnect opens a session for an alias without the picker, so the plugin is
// scriptable and bindable to a single key for a favorite host.
func runConnect(args []string) error { return runConnectWith(os.Stderr, args) }

// runConnectWith is runConnect with the diagnostic stream injected, so a test
// can read what the operator would have been shown.
//
// os.Stderr, unlike the pane verbs' os.Stdout: connect is a scriptable CLI verb
// rather than a pane entrypoint, so it has two real streams and stdout belongs
// to whatever the operator pipes it into.
func runConnectWith(out io.Writer, args []string) error {
	alias := args[0]
	placement := "split"
	for i := 1; i < len(args); i++ {
		if args[i] != "--placement" {
			return fmt.Errorf("%s\nunknown flag %q", usage, args[i])
		}
		if i+1 >= len(args) {
			return errors.New("--placement needs a value: split, tab, or zoomed")
		}
		i++
		switch args[i] {
		case "split", "tab", "zoomed":
			placement = args[i]
		default:
			return fmt.Errorf("placement %q must be split, tab, or zoomed", args[i])
		}
	}

	// Keep the returned Config; only the rejected keys were reset. See runSession.
	cfg, err := pluginconfig.LoadDir(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		_, _ = fmt.Fprintf(out, "herdr-ssh: %v — ignoring the rejected keys\n", err)
	}
	hosts, warnings := loadHosts(sshConfigPath(), cfg)
	// These are the only record that part of the config contributed nothing:
	// sshconfig skips past a failed Include, so every Host block inside one is
	// simply absent from hosts. Discarding them turns the lookup below into
	// "host %q not found in ssh config" for an alias the operator can see in
	// their own file, with nothing on screen to explain it.
	//
	// Before the loop, not after it, and not only on the not-found path. A
	// broken include can coexist with an alias that still resolves, and the
	// operator needs to know the rest of their config went missing either way.
	// runPicker puts these in the footer instead; there is no picker here, so
	// this is the stderr half of the same policy.
	for _, w := range warnings {
		_, _ = fmt.Fprintf(out, "herdr-ssh: %s\n", w)
	}
	for _, h := range hosts {
		if h.Alias == alias {
			sel := picker.Selection{Host: h, Placement: placement}
			return performSelection(out, herdrapi.New(), cfg, sel, currentCaller())
		}
	}
	return fmt.Errorf("host %q not found in ssh config", alias)
}
