package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/purehate/herdr-plugin-ssh/internal/pluginconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

// sshConfigPath returns the operator's ssh config path, or "" when there is
// none to name: os.UserHomeDir fails on an unset or empty $HOME, which is how
// the plugin runs under a stripped environment.
//
// Deliberately not a cwd-relative fallback. filepath.Join(".ssh", "config")
// resolves against the process working directory, and ssh never reads
// $CWD/.ssh/config, so the picker would enumerate hosts from a file
// `ssh <alias>` provably ignores and describe every row with a HostName, Port,
// User and proxy route the connection would not use — and in a directory the
// operator did not author, a planted .ssh/config would become the host list.
// That is the extra_config_paths defect (5a31f54) reached by another route; the
// ruling there was that the rows the picker shows must be the set ssh can
// reach.
//
// "" rather than an error because both callers hand the result straight to
// loadHosts, which already owns the operator-facing warning list — an error
// return would only be translated into the same warning one frame earlier.
// loadHosts' empty-path arm is the other half of this contract.
func sshConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// loadHosts parses the operator's ssh config, drops hidden aliases, and returns
// human-readable warnings for the picker footer.
//
// One file in, and its Include chain: sshconfig.Parse walks that itself. There
// is deliberately no way to add a config from outside the chain, because the
// picker's rows have to be the set ssh can reach. Selecting a host execs
// `ssh <alias>` with no -F, so ssh resolves the alias against this file and its
// Includes alone — a row sourced anywhere else would be described here with a
// HostName, Port, User and proxy route that ssh never sees, and the connection
// would go somewhere other than the preview said. On an engagement that is
// traffic from an unauthorized source, straight past the pivot the operator
// picked.
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
// same underlying reason: dialing them would produce a confident answer about an
// address the connection is not going to use.
//
//   - Behind a ProxyJump or ProxyCommand. A direct dial tests the wrong network.
//   - HostName still carrying a `%` token. ssh expands %h, %p, %r and the rest
//     at connect time; this picker does not, so the literal token is what would
//     be dialed. `%` cannot appear in a hostname (RFC 1123), and ssh's own
//     escape for a literal one is `%%`, so a `%` here is always either an
//     unexpanded token or an escape — never something resolvable.
//
// Both would otherwise render a false "down" on a host ssh reaches perfectly
// well, which is the worst direction for this marker to fail in: the operator
// skips a live host. Skipped targets emit no probe.Result, so they render as
// unprobed instead — the honest state, since we did not look.
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
