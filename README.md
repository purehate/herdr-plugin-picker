# herdr-ssh

[![ci](https://github.com/purehate/herdr-plugin-ssh/actions/workflows/ci.yml/badge.svg)](https://github.com/purehate/herdr-plugin-ssh/actions/workflows/ci.yml)

A floating fuzzy picker over the hosts in your `~/.ssh/config`. Pick one, get an
SSH session in a new pane, tab, or zoomed pane. Modeled on tmux's `sesh` picker.

```
  ssh ▏
  ──────────────────────────────────────────────────────────────────────────────────────────

  ▸ ● staging      deploy@127.0.0.1:2022
    ○ web1         deploy@web1.example
    ○ db-primary   deploy@192.0.2.10
    ○ bastion      deploy@bastion.example:2222
    ~ behind-jump  via bastion
    ─────
    HostName      127.0.0.1
    Port          2022
    User          deploy
    source        ~/.ssh/conf.d/staging.conf:1

  ↑↓ select   ^o preview   ^u clear
                      ^t tab   ^z zoom   ^n new    ↵ split    esc close
```

That is a real capture, not a mockup — the picker running against a throwaway
config built from names RFC 2606 and RFC 5737 reserve for documentation, so
nothing in it can be a host anyone owns. `staging` comes from an `Include`, and
its `source` line is how you tell an include chain resolved from an alias that
merely exists somewhere. `●` means the port answered, `○` means it did not, and
`~` means the host is proxied and was deliberately left alone.

## Install

Link a local checkout:

```bash
herdr plugin link ~/DEVELOPMENT/herdr-plugin-ssh
```

Or install it from GitHub:

```bash
herdr plugin install purehate/herdr-plugin-ssh
```

Then bind a key in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "purehate.herdr-ssh.open-picker"
description = "SSH picker"
```

That is the whole binding — no paths, nothing machine-specific. The floating
box comes from `placement = "popup"` on the plugin's own picker pane, so the
size lives in `herdr-plugin.toml` next to the code rather than in your config.
A `type = "popup"` keybinding floats too, but it runs a bare command, so it
would need an absolute path to the built binary — a path only the machine that
checked this out can know.

herdr draws the border round the popup itself, in your `[ui].accent` colour;
the picker deliberately draws none of its own so you get one box rather than
two.

If you want a different size, edit `width` and `height` in the manifest's
`[[panes]]` block for `picker` — and note they are **bare integers, not
strings**. herdr accepts either a cell count (`94`) or a percentage string
(`"60%"`), and nothing else: `width = "94"` is a TOML parse error that stops
the whole file loading, not a smaller window. Prefer cells. The picker fills
whatever it is handed, so a percentage of a large terminal gives you a wall
rather than a dialog.

`prefix+i` is a suggestion. Avoid `prefix+r` — that is herdr's built-in resize
mode.

## What it does on your machine

A herdr plugin is ordinary code running as your user, and herdr validates the
manifest but does not sandbox the code. Its own documentation tells you to read
a plugin before installing it. This one reads your SSH config, which is about
the most alarming sentence a plugin can open with, so here is the whole of it.

**Reads, and only reads, `~/.ssh/config` and the files it `Include`s.** Nothing
outside that chain — a config `ssh` cannot see is a config this picker will not
show you. It never writes to them.

**Never opens your keys.** `IdentityFile` shows up in the preview because it is
a line in your config; the file it points at is not read. No key, passphrase or
credential is read, stored or sent anywhere.

**Writes no files.** The action forwards the caller's pane, tab, and workspace
ids directly to the picker process. That is what lets `enter` split the pane you
were working in without storing shared state that another picker could
overwrite.

**Makes one TCP connection per host, if you let it.** That is the `●`/`○`
marker: a connect to `HostName`:`Port`, 300 ms by default, no bytes sent and
none read. It is a port scan of your own inventory and worth deciding about
rather than inheriting — `probe = false` turns it off. Hosts behind a
`ProxyJump` or `ProxyCommand` are skipped either way, since dialing them direct
would test the wrong network and report a confident false "down". An unreadable
config, malformed TOML, or an unknown or misspelled key turns probing **off**
rather than falling back to on. Invalid values of recognized keys are reported
and reset individually, preserving an explicit `probe` setting.

**Does not implement SSH.** Picking a host `exec`s your own `ssh` with the
alias, in a pane herdr opens for it. Your config, your keys, your agent, your
`known_hosts`, your proxy settings. If a host works by hand it works here, and
failures read the same too.

**Talks to herdr only through `$HERDR_BIN_PATH`** — open, close, rename and
focus panes. There is no network client, no telemetry, and no other process it
starts.

Three direct dependencies, all Charm/TOML libraries, listed under
[Development](#development). If you would rather read the code than this
section, `internal/sshconfig` is the parser and `internal/probe` is the only
thing that touches a socket.

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
| ~       | proxied, deliberately not probed              |
| (blank) | not probed yet, or never probed               |

Hosts behind a `ProxyJump` or `ProxyCommand` are not probed — a direct dial
would test the wrong network and report a false "down". `▪` wins over the
reachability markers because it is the one that changes what `enter` does. It
appears only when
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
ssh_args = []                 # non-routing flags passed to ssh before the destination
```

`ssh_args` accepts ordinary client options such as `-v`, `-A`, or
`-o ConnectTimeout=5`. Options that can change the displayed connection — for
example `-F`, `-p`, `-J`, `-l`, `-i`, or `-o HostName=...` — are rejected with
a visible warning, as are options that run a command on your machine
(`-o LocalCommand`, `PermitLocalCommand`, `KnownHostsCommand`). Put those
settings in `~/.ssh/config`; otherwise the preview and probe could describe one
destination while `ssh` connects to another.

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

Both `ProxyJump` and `ProxyCommand` are recognized. Their hosts are shown with
`~` and are never dialed directly by the reachability probe. A value of `none`
correctly disables either proxy mechanism.

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

The frame at the top of this file is regenerated, not edited:

```bash
go build -o bin/ ./cmd/herdr-ssh && python3 scripts/capture-readme-frame.py
```

Its output is meant to replace that block verbatim. It runs the picker under
tmux against a synthetic `$HOME`, so it cannot read your real SSH config, and on
tmux's own socket, so it cannot see your real panes. Regenerate it when you
change the view — a hand-tuned frame stops being a capture.

`herdr-plugin.toml` is pinned by `cmd/herdr-ssh/manifest_test.go`. The pane ids,
entrypoints and plugin id that the Go code hands to `herdr plugin pane open` are
read back out of the argv it sent and checked against the manifest, so editing
one side without the other fails the build here instead of at launch on
someone's machine. Change the manifest and the code in the same commit.

## License

MIT
