# herdr-ssh

A floating fuzzy picker over the hosts in your `~/.ssh/config`. Pick one, get an
SSH session in a new pane, tab, or zoomed pane. Modeled on tmux's `sesh` picker.

## Install

Link a local checkout:

```bash
herdr plugin link ~/DEVELOPMENT/herdr-plugin-ssh
```

Once this repo is published, the shorter route works too:

```bash
herdr plugin install purehate/herdr-plugin-ssh
```

Then bind a key in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "purehate.herdr-ssh.open-picker"
```

`prefix+i` is a suggestion. Avoid `prefix+r` — that is herdr's built-in resize mode.

## Keys

| Key                    | Action                               |
| ---------------------- | ------------------------------------ |
| type                   | fuzzy filter on alias, then hostname |
| `backspace`            | delete the last character            |
| `^w`                   | delete the last word of the query    |
| `^u`                   | clear the query                      |
| `↑` / `↓`, `^k` / `^j` | move the cursor                      |
| `enter`                | ssh in a split                       |
| `^t`                   | ssh in a new tab                     |
| `^z`                   | ssh in a zoomed pane                 |
| `^n`                   | force a new pane even if one exists  |
| `^o`                   | toggle the host preview              |
| `esc`, `^c`            | close                                |

If you came from `fzf`, note that `^n` is a placement key here, not cursor-down —
`^j` / `^k` move the cursor.

## Markers

| Marker  | Meaning                                       |
| ------- | --------------------------------------------- |
| ▪       | a session for this host is already open       |
| ●       | port reachable                                |
| ○       | port not reachable                            |
| ~       | behind a `ProxyJump`, deliberately not probed |
| (blank) | not probed yet, or never probed               |

Hosts behind a `ProxyJump` are not probed — a direct dial would test the wrong
network and report a false "down". `▪` wins over the reachability markers,
because it is the one that changes what `enter` does. It appears only when
`reuse_panes` is on: with reuse off, `enter` opens a new pane whether or not one
is already there, so the marker would be claiming something `enter` does not do.

A blank marker means one of two things and does not distinguish them — the probe
has not answered yet, or this host is never probed at all (a `HostName` carrying
a `%` token, below). The first resolves on its own; the second stays blank.

## Configuration

Optional, at `~/.config/herdr/plugins/config/purehate.herdr-ssh/config.toml`
(the directory herdr passes as `$HERDR_PLUGIN_CONFIG_DIR`). Ask herdr rather
than assuming the path:

```bash
herdr plugin config-dir purehate.herdr-ssh
```

```toml
probe = true                  # TCP-check hosts
probe_timeout_ms = 300
split_direction = "right"     # or "down"
show_preview = true
reuse_panes = true            # focus an existing ssh:<host> pane instead of opening another
hidden = []                   # globs matched against the alias
ssh_args = []                 # flags passed to ssh, before the destination
```

## What it understands

Reads `~/.ssh/config` the way ssh does: first value wins, `Host *` and glob
patterns supply defaults without being selectable, `!negated` patterns are
honored, and a `Host` pattern that can never match is parsed but never applied
(ssh's `SSHCONF_NEVERMATCH` rule) — so if a host is missing from the picker,
check whether its pattern is satisfiable. `Match` blocks are skipped —
evaluating `Match exec` would mean running commands to build a list.

`Include` is expanded with globbing. Cycles are broken by tracking the files
currently open on the descent path, plus a depth cap of 16 — not by a global
visited set, which would break the common case of one fragment included by
several `Host` blocks. Only `~/.ssh/config` and what it includes is read; a
config ssh cannot see is a config this picker will not show you.

Not implemented: `%`-token expansion. A `HostName %h.example.com` shows the
token literally and is never probed — the literal string is not a dialable
address, and `%` cannot appear in a real hostname. `ssh` still expands it on
connect, so the connection is correct; the display is literal, and the
reachability marker abstains rather than reporting a result it cannot get.

## Direct connect

Skip the picker entirely from a shell in any pane:

```bash
herdr-ssh connect nixos-dev
herdr-ssh connect nixos-dev --placement tab
```

## Development

The module is tidy, and CI enforces it: a step runs `go mod tidy` and fails on
any resulting diff. So run it freely — if it changes something, that is the
finding, not the noise. `bubbletea/v2`, `lipgloss/v2` and `go-toml/v2` are the
only direct dependencies; everything else in `go.mod` is transitive.

`go.sum` names 21 modules against those three imports — the transitive closure
plus the hashes that verify it. None of them are stowaways: `go mod why -m
<module>` reports a real path to every one, and `go list -m all` resolves 24
modules for the build. If you find yourself wondering how something got in
there, run that command rather than assuming.

`herdr-plugin.toml` is pinned by `cmd/herdr-ssh/manifest_test.go`. The pane ids,
entrypoints and plugin id that the Go code hands to `herdr plugin pane open` are
read back out of the argv it sent and checked against the manifest, so editing
one side without the other fails the build here instead of at launch on
someone's machine. Change the manifest and the code in the same commit.

## License

MIT
