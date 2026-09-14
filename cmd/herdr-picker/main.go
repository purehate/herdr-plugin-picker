// Command herdr-picker is the tabbed picker plugin for herdr.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
	"github.com/purehate/herdr-plugin-picker/internal/pluginconfig"
)

const usage = `usage: herdr-picker <navigator|session|connect <alias> [--placement split|tab|zoomed]|plugin open-navigator>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		reportFatal(os.Stderr, err)
		os.Exit(1)
	}
}

// reportFatal prints err on the way out, unless fatalInPane already put it on
// the operator's screen and held the pane until they read it — printing again
// would show the same line twice. Only errors carrying errReported are
// suppressed; usage errors, runConnect's flag errors, and a failed pane open are
// reported nowhere else and must still come out here.
func reportFatal(out io.Writer, err error) {
	if errors.Is(err, errReported) {
		return
	}
	_, _ = fmt.Fprintf(out, "herdr-picker: %v\n", err)
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "navigator":
		return runNavigator()
	case "session":
		return runSession()
	case "connect":
		if len(args) < 2 {
			return errors.New(usage)
		}
		return runConnect(args[1:])
	case "plugin":
		if len(args) != 2 || args[1] != "open-navigator" {
			return errors.New(usage)
		}
		return openNavigator(herdrapi.New())
	default:
		return errors.New(usage)
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
	// Resolved for the same reason runNavigatorWith resolves it, and for one
	// more: connect is a scriptable verb, so it also runs from a plain shell
	// with none of herdr's variables set. See env.go.
	cfg, err := pluginconfig.LoadDir(resolvePluginConfigDir())
	if err != nil {
		_, _ = fmt.Fprintf(out, "herdr-picker: %v — ignoring the rejected keys\n", err)
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
	// The navigator puts these in the ssh tab's footer instead; there is no
	// picker here, so this is the stderr half of the same policy.
	for _, w := range warnings {
		_, _ = fmt.Fprintf(out, "herdr-picker: %s\n", w)
	}
	for _, h := range hosts {
		if h.Alias == alias {
			sel := picker.Selection{Host: h, Placement: placement}
			return performSelection(out, herdrapi.New(), cfg, sel, currentCaller())
		}
	}
	return fmt.Errorf("host %q not found in ssh config", alias)
}
