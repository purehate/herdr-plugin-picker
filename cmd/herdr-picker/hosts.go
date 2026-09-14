package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/purehate/herdr-plugin-picker/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-picker/internal/probe"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
	"github.com/purehate/herdr-plugin-picker/internal/sshusage"
)

// sshConfigPath returns the operator's ssh config path, or "" when there is none
// to name: os.UserHomeDir fails on an unset or empty $HOME, which is how the
// plugin runs under a stripped environment.
//
// Deliberately not a cwd-relative fallback: ssh never reads $CWD/.ssh/config, so
// the picker would enumerate hosts from a file `ssh <alias>` ignores — and in a
// directory the operator did not author, a planted .ssh/config would become the
// host list. The rows the picker shows must be the set ssh can reach.
//
// "" rather than an error because both callers hand the result to loadHosts,
// which owns the operator-facing warning list.
func sshConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// sshUsagePath is where the ssh tab's frecency is stored, or "" when there is no
// state directory to write it to.
func sshUsagePath() string {
	dir := resolvePluginStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "ssh-usage.json")
}

// recordHostUse bumps alias's frecency for the next time the picker opens. A
// failure is reported and otherwise ignored: losing the ordering is much
// cheaper than losing the connection the operator asked for.
func recordHostUse(out io.Writer, alias string) {
	if err := sshusage.Record(sshUsagePath(), alias, time.Now()); err != nil {
		_, _ = fmt.Fprintf(out, "herdr-picker: could not save host usage: %v\n", err)
	}
}

// loadHosts parses the operator's ssh config, drops hidden aliases, and returns
// human-readable warnings for the picker footer.
//
// One file in, and its Include chain: sshconfig.Parse walks that itself. There
// is no way to add a config from outside the chain, because the picker's rows
// have to be the set ssh can reach — selecting a host execs `ssh <alias>` with
// no -F, so ssh resolves against this file and its Includes alone. A row sourced
// elsewhere would be described with a route ssh never sees, and the connection
// would go somewhere other than the preview said.
func loadHosts(primary string, cfg pluginconfig.Config) ([]sshconfig.Host, []string) {
	var hosts []sshconfig.Host
	var warnings []string

	if primary == "" {
		// sshConfigPath could not resolve the home directory, so there is no
		// config to read. Falling through would be worse than useless:
		// filepath.Abs("") is the working directory, so sshconfig.Parse would
		// report "read <cwd>: is a directory" — a file the operator never
		// configured, and no mention of the thing that actually failed.
		//
		// The picker renders "no ~/.ssh/config — nothing to pick" for an empty
		// host list; this is the footer line that says why. runConnect has no
		// picker and prints the same warning to its diagnostic stream.
		return nil, []string{"cannot resolve the home directory ($HOME is unset or empty) — no ssh config to read"}
	}

	found, warns, err := sshconfig.Parse(primary)
	switch {
	case errors.Is(err, sshconfig.ErrNoConfig):
		// No ssh config is a normal state, not a problem to report.
	case err != nil:
		// No path prefix: err already names the file. ErrNoConfig is wrapped as
		// "%w: %s" with the absolute path, and the only other reachable error
		// here is os.ReadFile's *fs.PathError, which embeds it too — so
		// prefixing printed the file twice ("<p>: read <p>: is a directory").
		// The tests assert the path appears exactly once, which is also what
		// catches the reverse: this is correct only while sshconfig keeps
		// embedding the path, and a bare error would otherwise leave the
		// operator unable to tell which file failed.
		warnings = append(warnings, err.Error())
	default:
		hosts = found
	}
	for _, w := range warns {
		warnings = append(warnings, w.String())
	}

	return sshconfig.Exclude(hosts, cfg.Hidden), warnings
}

// targetsFor builds probe targets. Two kinds of host are marked Skip, for the
// same reason: dialing them would produce a confident answer about an address
// the connection is not going to use.
//
//   - Behind a ProxyJump or ProxyCommand: a direct dial tests the wrong network.
//   - HostName still carrying a `%` token: ssh expands it at connect time, this
//     picker does not, and `%` cannot appear in a real hostname (RFC 1123), so
//     the literal token is never dialable.
//
// Both would otherwise render a false "down" on a host ssh reaches, which is the
// worst direction for this marker to fail in. Skipped targets emit no Result, so
// they render as unprobed — the honest state, since we did not look.
func targetsFor(hosts []sshconfig.Host) []probe.Target {
	out := make([]probe.Target, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, probe.Target{
			Alias: h.Alias,
			Addr:  net.JoinHostPort(h.HostName, h.Port),
			Skip:  h.ProxyJump != "" || h.ProxyCommand != "" || strings.Contains(h.HostName, "%"),
		})
	}
	return out
}
