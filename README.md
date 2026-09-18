# herdr-picker

[![ci](https://github.com/purehate/herdr-plugin-picker/actions/workflows/ci.yml/badge.svg)](https://github.com/purehate/herdr-plugin-picker/actions/workflows/ci.yml)

One floating popup you drive from the keyboard, over everything Herdr is
running. Jump to any space, agent, open tab, or pane; **broadcast one command
to every pane you mark**; and SSH out of your real `~/.ssh/config` — `Include`s
expanded, `ProxyJump` followed, **each host probed so you can see what is
actually up before you connect**. Modeled on tmux's `sesh` picker, `setw
synchronize-panes`, and Herdr's own settings dialog.

It stays a popup on purpose. It floats over your layout, does its one job, and
gets out of the way — it never takes a pane hostage to show you a list.

```
   spaces  agents  sessions  panes  ssh  machines  cmd
  ──────────────────────────────────────────────────────────────────────────────────────────
  / ▏

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
  ↑↓ select   ←→/tab section   ^o preview   ^u clear
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
herdr plugin link ~/DEVELOPMENT/herdr-plugin-picker
```

Or install it from GitHub:

```bash
herdr plugin install purehate/herdr-plugin-picker
```

Then bind a key in `~/.config/herdr/config.toml`. The picker takes `prefix+g`,
so move Herdr's built-in goto aside first if you still want its full
workspace/tab/pane tree:

```toml
[keys]
goto = "prefix+shift+j"

[[keys.command]]
key = "prefix+g"
type = "plugin_action"
command = "purehate.herdr-picker.open-navigator"
description = "spaces, agents, sessions, panes, ssh, machines, cmd"
```

That is the whole binding — no paths, nothing machine-specific. The floating
box comes from `placement = "popup"` on the plugin's own pane, so the size lives
in `herdr-plugin.toml` next to the code rather than in your config. A
`type = "popup"` keybinding floats too, but it runs a bare command, so it would
need an absolute path to the built binary — a path only the machine that checked
this out can know.

herdr draws the border round the popup itself, in your `[ui].accent` colour;
the picker deliberately draws none of its own so you get one box rather than
two.

## The tabs

The picker opens on **spaces**. `←`/`→` or Tab/Shift-Tab change tabs, and
switching tabs clears the query.

- **spaces** lists workspaces, with their tab and pane counts.
- **agents** lists recognized agent panes, with their workspace and status. The
  selected agent's recent output is previewed below the list, and `^p` opens a
  one-line prompt to send it a message.
- **sessions** lists the open tabs in the current Herdr server. Here
  "sessions" means tabs, not separate named Herdr servers.
- **panes** lists every pane across every workspace, with what is running in it
  and where. `space` marks panes and `^b` sends one command to all of them.
- **ssh** lists the hosts in `~/.ssh/config` and opens one.
- **machines** lists the SSH machines saved with `herdr machine add` and
  attaches to one's remote herdr server.
- **cmd** lists every verb you can invoke by name: a handful of built-in
  Herdr operations, plus every action each of your _other_ installed plugins
  exposes. Enter runs the selected one.

On the first four, Enter jumps to the selected workspace, agent, tab, or pane. The
inventory re-reads `herdr api snapshot` about once a second while the popup is
open, so newly created spaces, agents, and tabs appear without reopening it;
the cursor stays on the row it was on and the query is kept. Blocked agents
sort to the top so the ones waiting on you come first. If a read fails, the
footer says `⚠ refresh failed` and the last good list stays on screen.

On **agents**, the selected agent's recent terminal output is previewed below
the list, and `^p` opens a one-line prompt that Enter sends and Esc cancels.
The preview reads `herdr agent read`; a prompt goes through `herdr agent
prompt`, which herdr refuses for an already-blocked agent rather than sending
input. Neither reads the agent's session file.

It reads only the metadata in `herdr api snapshot`, `herdr pane list`, and
`herdr machine list`, and invokes Herdr's own workspace, agent, tab, or pane
focus command; it does not create panes on the first four tabs.

On **ssh**, Enter opens a session in a split, `^t` in a new tab, `^z` in a
zoomed pane, and `^n` forces a new pane even when a session for that host is
already open. Type to fuzzy-filter on alias, then hostname; `^o` toggles the
host preview. See [Markers](#markers) and [Keys](#keys).

On **machines**, Enter attaches a full herdr client to the saved machine's own
server — `herdr --remote <target>`, plus `--session <name>` when the profile
names one — in a new split. That is a different thing from the ssh tab's shell
on the same host: the remote machine keeps its own workspaces, tabs, agents,
and running processes, and this client shows them. `^x` copies the profile id
or the ssh target. The profile catalog belongs to `herdr machine`, so the
picker only reads it; use `herdr machine add|rename|remove|enable|disable` to
change it. A disabled profile still lists, with `○` instead of `●`.

On **cmd**, Enter invokes the selected verb and closes the popup. The list is
two things merged: a few built-in Herdr operations (split, zoom, new tab, new
workspace), and every action your other installed plugins expose — their own
titles, read live from the running server. Install another plugin and its
actions appear here next time you open the picker, with nothing to configure.
The verbs you run float to the top the same way hosts do on the ssh tab, while
the ones you have never run stay exactly where they started, so the list you
are still learning does not reshuffle underneath you.

This is the one thing here that a CLI-based plugin cannot do. `herdr` the
command exposes roughly thirty operations; the socket API exposes 128, and two
of them — `plugin.action.list` and `plugin.action.invoke` — are how one plugin
enumerates and runs another's actions. On the author's machine that turns into
46 rows: 5 built-in verbs and 41 actions across 13 other plugins.

Verbs act where _you_ were, not where the popup is: a split splits the pane you
opened the picker from. This plugin's own actions are left out of its own list.
If the socket is unavailable the tab still shows the built-in verbs, and if a
plugin action cannot be listed the footer says so.

With a mouse, click tabs to switch, click a row to act, or use the wheel to
scroll. The footer's primary action and Close are clickable too.

If you want a different size, edit `width` and `height` in the manifest's
`[[panes]]` block for `navigator` — and note they are **bare integers, not
strings**. herdr accepts either a cell count (`94`) or a percentage string
(`"60%"`), and nothing else: `width = "94"` is a TOML parse error that stops
the whole file loading, not a smaller window. Prefer cells. The picker fills
whatever it is handed, so a percentage of a large terminal gives you a wall
rather than a dialog.

## Keys

On every tab:

| Key                    | Action                                              |
| ---------------------- | --------------------------------------------------- |
| type                   | fuzzy filter on the current list                    |
| `backspace`            | delete the last character                           |
| `^w`                   | delete the last word of the query                   |
| `^u`                   | clear the query                                     |
| `↑` / `↓`, `^k` / `^j` | move the cursor                                     |
| `g` `g` / `G`          | jump to the top / bottom of the list                |
| `home` / `end`         | the same jumps without the `g` chord                |
| `/`                    | toggle regex matching for the query                 |
| `←` / `→`, Tab         | change tab                                          |
| `enter`                | jump to the row, ssh in a split, or run the command |
| `^x`                   | row actions (not on the ssh tab)                    |
| `space`                | mark the row (panes and ssh tabs)                   |
| `esc`, `^c`            | close                                               |

Additional keys on the **ssh** tab:

| Key     | Action                                |
| ------- | ------------------------------------- |
| `^t`    | ssh in a new tab                      |
| `^z`    | ssh in a zoomed pane                  |
| `^n`    | force a new pane even if one exists   |
| `^o`    | toggle the host preview               |
| `space` | mark the host; Enter opens all marked |

`space` marks a host and steps down, so several can be marked in a row. The
footer shows the count, Enter opens them all with the chosen placement, and Esc
clears the marks before it closes the picker. A mark is about the host, not the
current filter, so a marked host opens even when a query hides its row. Space
is only a mark key on the ssh tab; elsewhere it is an ordinary query character.

Additional keys on the **panes** tab:

| Key     | Action                                 |
| ------- | -------------------------------------- |
| `space` | mark the pane                          |
| `^a`    | mark every listed pane; again to clear |
| `^b`    | send one command to every marked pane  |

`^b` opens a one-line input. Enter asks `y`/`n` naming the command and the pane
count, and only `y` sends it — it is many writes to live shells and nothing
undoes them. With nothing marked, `^b` targets the row under the cursor. `^a` is
bounded by the query, so filtering is how you narrow a broadcast. The picker's
own popup pane is never listed, so a broadcast cannot type into the picker.

This needs `$HERDR_SOCKET_PATH`, which herdr sets for its plugins. The command
goes over herdr's socket API as `pane.send_text`, because that is the only
operation that writes to a plain shell — the CLI's `agent send-keys` reaches
agents only. Without the socket, `^b` does nothing rather than failing once per
pane after you have confirmed.

Additional keys on the **agents** tab:

| Key  | Action                                  |
| ---- | --------------------------------------- |
| `^p` | type a prompt; Enter sends, Esc cancels |

A prompt to an agent that is already blocked is rejected by herdr before any
input is sent, and the picker shows why rather than swallowing it.

If you came from `fzf`, note that `^n` is a placement key here, not
cursor-down — `^j` / `^k` move the cursor.

`g` is a chord, not a jump: the first `g` arms it and the query line shows the
pending `g`, the second `g` jumps to the top. Any other key in between commits
the `g` to the query, so a search can still start with the letter, and Esc
cancels the half-typed chord.

`/` switches the query between fuzzy matching and a regular expression. The
prompt changes from `/` to `.*` so the mode is visible, and the query is kept,
so a fuzzy search can be refined into a pattern without retyping it. The
pattern is case-insensitive; it filters the rows rather than ranking them, so
the list keeps its source order. A pattern that does not compile empties the
list and the message says `bad regex:` and why.

## Actions

`^x` opens a menu of actions for the row under the cursor on the spaces,
agents, and sessions tabs. The machines tab gets a shorter, read-only menu
(copy only), and the ssh tab has none: a host is not a herdr object. `↑`/`↓`
choose, Enter runs, Esc cancels.

| Action            | On        | What it does                                    |
| ----------------- | --------- | ----------------------------------------------- |
| rename            | all three | renames the workspace, agent, or tab            |
| new tab here      | all three | opens a tab in the same workspace               |
| new workspace     | spaces    | opens a workspace                               |
| close             | all three | closes the workspace, pane, or tab, after a y/n |
| copy id           | all three | copies the herdr id                             |
| copy cwd          | agents    | copies the agent's working directory            |
| open git worktree | agents    | opens the worktree the agent runs in            |
| copy id           | machines  | copies the profile id                           |
| copy ssh target   | machines  | copies the profile's ssh target                 |

Rename asks for the new name in a one-line input and close asks `y`/`n` first;
nothing else prompts. Copy uses OSC 52, so it works over ssh and needs no
`pbcopy` or `xclip` — a terminal that does not speak OSC 52 will simply not
receive it. The result appears in the footer, and the live refresh picks up a
rename or a close on its next tick.

## Markers

The **panes** tab's first column, per pane:

| Marker  | Meaning                 |
| ------- | ----------------------- |
| ▪       | the pane you are in     |
| ◉       | an agent waiting on you |
| ●       | an agent working        |
| ○       | an agent idle or done   |
| (blank) | a plain shell           |

A marked pane is shown with `▣` to the left of the row.

The **ssh** tab's first column, per host:

| Marker  | Meaning                                 |
| ------- | --------------------------------------- |
| ▪       | a session for this host is already open |
| ●       | port reachable                          |
| ○       | port not reachable                      |
| ~       | proxied, deliberately not probed        |
| (blank) | not probed yet, or never probed         |

Hosts behind a `ProxyJump` or `ProxyCommand` are not probed — a direct dial
would test the wrong network and report a false "down". `▪` wins over the
reachability markers because it is the one that changes what `enter` does. It
appears only when `reuse_panes` is on: with reuse off, `enter` opens a new pane
whether or not one is already there, so the marker would be claiming something
`enter` does not do.

A blank marker means one of two things and does not distinguish them — the probe
has not answered yet, or this host is never probed at all (a `HostName` carrying
a `%` token, below). The first resolves on its own; the second stays blank.

The `●` marker carries the dial's round-trip time (`● 12ms`), so a host that
answers slowly is visible before you connect. It is measured on the same dial
that produced the marker, and shown only for a host that answered.

## Configuration

Optional, at `~/.config/herdr/plugins/config/purehate.herdr-picker/config.toml`
(the directory herdr passes as `$HERDR_PLUGIN_CONFIG_DIR`). Ask herdr rather
than assuming the path:

```bash
herdr plugin config-dir purehate.herdr-picker
```

```toml
probe = true                  # TCP-check hosts
probe_timeout_ms = 300
split_direction = "right"     # or "down"
show_preview = true
reuse_panes = true            # focus an existing ssh:<host> pane instead of opening another
hidden = []                   # globs matched against the alias
ssh_args = []                 # non-routing flags passed to ssh before the destination
pinned = []                   # aliases to keep at the top of the ssh tab
mosh = false                  # open sessions with mosh instead of ssh
```

`ssh_args` accepts ordinary client options such as `-v`, `-A`, or
`-o ConnectTimeout=5`. Options that can change the displayed connection — for
example `-F`, `-p`, `-J`, `-l`, `-i`, or `-o HostName=...` — are rejected with
a visible warning, as are options that run a command on your machine
(`-o LocalCommand`, `PermitLocalCommand`, `KnownHostsCommand`). Put those
settings in `~/.ssh/config`; otherwise the preview and probe could describe one
destination while `ssh` connects to another.

`mosh = true` opens sessions with `mosh` instead of `ssh`, which keeps them
alive across roaming and sleep. mosh must be installed locally and on the host.
The configured `ssh_args` are handed to mosh's own ssh via `--ssh`, so `-v` or
an identity option still reaches the connection mosh makes.

The **ssh** tab orders hosts by `pinned`, then by how often and how recently
you have opened them, then by config order. Typing a query still decides — the
ordering only breaks ties — so a host you use daily does not outrank an exact
name match. The usage is kept in `$HERDR_PLUGIN_STATE_DIR/ssh-usage.json`,
falling back to
`~/.local/state/herdr/plugins/purehate.herdr-picker/ssh-usage.json` when that
variable is not set (which it is not in popup mode). It holds nothing but
aliases and counters, and is written through a temp file and a rename, so a
crash cannot truncate it; delete it and you lose only the ordering.

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

`LocalForward`, `RemoteForward`, and `DynamicForward` are parsed and shown in
the host preview. Every occurrence is kept, in config order, because these
keywords are additive where the rest are first-wins. Like `IdentityFile` they
are display-only: ssh applies them from the config, so the picker never puts
them on a command line.

## Direct connect

Skip the picker entirely from a shell in any pane:

```bash
herdr-picker connect nixos-dev
herdr-picker connect nixos-dev --placement tab
```

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

**Writes two files.** `ssh-usage.json` in the plugin's state directory records
how often each alias is opened, so the ssh tab can put the ones you use first;
`command-usage.json` does the same for the cmd tab, keyed by command id. They
hold names and counters only — no arguments, no hostnames beyond the alias you
already wrote in your config — and each is written through a temp file and a
rename so a crash cannot truncate it. Everything else is stateless: the action
forwards the caller's pane, tab, and workspace ids directly to the picker
process, which is what lets `enter` split the pane you were working in without
storing shared state that another picker could overwrite.

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

**Talks to herdr, and to nothing else.** Almost all of it goes through
`$HERDR_BIN_PATH` — snapshot, pane list, machine list, focus, rename, close,
and create workspaces, tabs, and panes; worktree open; read or prompt agents;
and attach a remote client. There is no network client, no telemetry, and no
other process it starts. While the picker is open it re-runs `herdr api
snapshot` and `herdr pane list` about once a second, so those subprocesses
start repeatedly for as long as the popup is on screen; closing the popup stops
it. The agents tab preview runs `herdr agent read` for the selected agent, and
`^p` submits text with `herdr agent prompt` — so pressing Enter in the prompt
sends that text to the agent you selected. `^x` can rename or close what is on
screen and open tabs, workspaces, and worktrees, all through herdr. On the
machines tab, Enter opens a pane running `herdr --remote <target>`, which
execs your `ssh` to reach the machine's own herdr server — the picker itself
never opens that connection.

**Two things do not go through the CLI: `^b` and the cmd tab.** Both use
herdr's own unix socket at `$HERDR_SOCKET_PATH` — herdr's socket, set by herdr
for its own plugins. The picker dials it, writes one JSON line, reads one line
back, and closes.

Broadcast calls `pane.send_text`, because no CLI command writes to a plain
shell — `agent send-keys` reaches agents only. It is the only place the picker
writes into a pane you did not ask it to open, and nothing is sent without the
`y`/`n` you answer first.

The cmd tab calls `plugin.action.list` when it opens, which returns the actions
your other installed plugins declare in their manifests, and
`plugin.action.invoke` when you pick one. Invoking runs _that_ plugin's command,
as your user, exactly as pressing its own keybinding would — the picker is
choosing it, not sandboxing it, so the cmd tab is worth trusting only as far as
you trust the plugins you installed. Titles from other manifests are stripped
of terminal control sequences before being drawn. Nothing on that tab runs
until you press Enter on it.

Three direct dependencies, all Charm/TOML libraries, listed under
[Development](#development). If you would rather read the code than this
section, `internal/sshconfig` is the parser, and `internal/probe` and
`internal/herdrsock` are the only things that touch a socket.

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
go build -o bin/ ./cmd/herdr-picker && python3 scripts/capture-readme-frame.py
```

Its output is meant to replace that block verbatim. It runs the picker under
tmux against a synthetic `$HOME`, so it cannot read your real SSH config, and on
tmux's own socket, so it cannot see your real panes. It drives the picker onto
the `ssh` tab and answers the one `herdr api snapshot` call with an empty
inventory through a shim on `PATH`, so the capture never reaches your live
server. Regenerate it when you change the view — a hand-tuned frame stops being
a capture.

`herdr-plugin.toml` is pinned by `cmd/herdr-picker/manifest_test.go`. The pane ids,
entrypoints and plugin id that the Go code hands to `herdr plugin pane open` are
read back out of the argv it sent and checked against the manifest, so editing
one side without the other fails the build here instead of at launch on
someone's machine. Change the manifest and the code in the same commit.

## License

MIT
