# SSH Picker for Herdr — Design

**Date:** 2026-09-09
**Status:** Approved, pending implementation plan
**Plugin id:** `operator.herdr-ssh`

## Goal

A floating fuzzy picker over the hosts in `~/.ssh/config`, opened with one keystroke from
anywhere in a Herdr session. Select a host, get an SSH shell in a new pane, tab, or zoomed
pane without leaving the keyboard. The tmux `sesh` picker as the interaction model, SSH
hosts as the content.

The operator case it serves: a pane is busy on the local NixOS work, something needs
checking on a remote box, and the round trip should be `prefix+i` → three characters →
enter — not a new pane plus a recalled hostname.

## Non-Goals

Deliberately excluded, to keep this to one implementation cycle:

- `known_hosts` as a host source (hashed entries are unusable; plain ones carry no user/port).
- `herdr machine list` / `herdr --remote` remote workspaces. Different connect semantics; a
  second picker type would muddy a single-purpose tool.
- Editing `~/.ssh/config`.
- Session persistence or reconnect-on-drop.
- Live active-pane previews (the sesh plugin has these; not needed here).

## Runtime Context

Verified on the target machine, 2026-09-09:

| Fact               | Value                                                                                       |
| ------------------ | ------------------------------------------------------------------------------------------- |
| Herdr              | 0.9.0, `/opt/homebrew/bin/herdr`                                                            |
| Config             | `~/.config/herdr/config.toml`, prefix `ctrl+a`, theme `terminal`, `[ui].accent = "#14e21a"` |
| SSH config         | `~/.ssh/config`, 20 `Host` entries, one `Include /Users/operator/.config/colima/ssh_config`     |
| Go                 | 1.27.1 darwin/arm64, `GOTOOLCHAIN=auto`                                                     |
| Reference plugin   | `fullerzz.sesh` 0.11.0 (Go, overlay pane, native picker)                                    |
| Prior local plugin | `operator.nvim-open` (manifest-only, pane entrypoint + `--env` handoff)                         |

Plugin environment contract, as exposed by Herdr to plugin processes:

`HERDR_PANE_ID`, `HERDR_TAB_ID`, `HERDR_WORKSPACE_ID`, `HERDR_BIN_PATH`,
`HERDR_CONFIG_PATH`, `HERDR_SOCKET_PATH`, `HERDR_PLUGIN_CONFIG_DIR`,
`HERDR_PLUGIN_STATE_DIR`, `HERDR_PLUGIN_EVENT`, `HERDR_PLUGIN_EVENT_JSON`.

## Constraints That Shaped the Design

Two findings drove the architecture and are worth stating plainly, because the obvious
implementations both fail:

1. **`herdr pane split` and `herdr tab create` accept no command argument.** They spawn the
   default shell. The only supported way to run SSH in a fresh pane is a declared
   `[[panes]]` entrypoint whose command is the plugin's own binary, with the host handed
   over by `herdr plugin pane open --env`. This is the mechanism `operator.nvim-open` already
   uses for `$HERDR_EDIT_FILE`.

   The alternative — split, then `herdr pane send-text "ssh host\n"` — races the new
   shell's prompt and breaks under a slow shell init. Rejected.

2. **The new pane's id is not reliably reported back to the caller.** Renaming for reuse
   detection therefore happens _inside_ the session pane, which knows its own
   `HERDR_PANE_ID` for certain, before it hands the pty to SSH.

## Architecture

Single Go binary, four verbs, six focused packages.

### Manifest (`herdr-plugin.toml`)

```toml
id = "operator.herdr-ssh"
name = "SSH Picker"
version = "0.1.0"
min_herdr_version = "0.9.0"
description = "Fuzzy-pick a host from ~/.ssh/config and SSH into a new pane, tab, or zoomed pane."
platforms = ["macos", "linux"]

[[build]]
command = ["go", "build", "-o", "bin/", "./cmd/herdr-ssh"]

[[actions]]
id = "open-picker"
title = "Open SSH Picker"
contexts = ["workspace", "pane"]
command = ["./bin/herdr-ssh", "plugin", "open-picker"]

[[panes]]
id = "picker"
title = "SSH Hosts"
placement = "overlay"
command = ["./bin/herdr-ssh", "picker"]

[[panes]]
id = "session"
title = "ssh"
placement = "split"
command = ["./bin/herdr-ssh", "session"]
```

`placement` on the `session` entrypoint is a default; `herdr plugin pane open --placement`
overrides it per invocation with `split`, `tab`, or `zoomed`.

### Keybinding

Appended to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "operator.herdr-ssh.open-picker"
```

`prefix+r` was the operator's first instinct but is Herdr's built-in `resize_mode`.
`prefix+i` is free in both the Herdr defaults and the existing local config, and displaces
no built-in. Free single keys remaining afterward: `a`, `u`.

### Packages

Each unit has one purpose, a defined interface, and is testable without a running Herdr.
None should exceed roughly 250 lines.

| Unit                 | Responsibility                               | Interface                                                                                            | Depends on        |
| -------------------- | -------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ----------------- |
| `internal/sshconfig` | Parse SSH config into hosts                  | `Parse(root string) ([]Host, []Warning, error)`                                                      | stdlib only       |
| `internal/herdrapi`  | Every `herdr` CLI call, behind one exec seam | `PaneList()`, `PaneFocus(id)`, `PaneRename(id, name)`, `PluginPaneOpen(opts)`, `PluginPaneClose(id)` | `$HERDR_BIN_PATH` |
| `internal/picker`    | Bubble Tea UI: filter, list, preview, keymap | `Run([]Host, Theme, Config) (Selection, error)`                                                      | sshconfig, theme  |
| `internal/theme`     | Herdr theme → color tokens                   | `Load(configPath string) Theme`                                                                      | stdlib only       |
| `internal/probe`     | Async TCP reachability                       | `Probe(ctx, []Host, timeout) <-chan Result`                                                          | stdlib only       |
| `cmd/herdr-ssh`      | Wire the verbs                               | `plugin open-picker`, `picker`, `session`, `connect`                                                 | all of the above  |

`connect` is the non-interactive escape hatch: `herdr-ssh connect <host> --placement tab`
performs a selection's side effects without the UI. It makes the pane-opening path
testable and scriptable independently of Bubble Tea.

### Data Model

```go
type Host struct {
    Alias        string   // the name in `Host <alias>`
    HostName     string   // resolved, defaults to Alias
    User         string
    Port         string   // resolved, defaults to "22"
    IdentityFile string
    ProxyJump    string
    SourceFile   string   // which config file it came from
    SourceLine   int
}

type Selection struct {
    Host      Host
    Placement string // "split" | "tab" | "zoomed"
    ForceNew  bool
}
```

### Host Resolution Rules

`internal/sshconfig` implements real SSH config semantics, not an approximation:

- **First value wins.** For each keyword, the earliest matching declaration is authoritative.
  This is SSH's actual precedence and the most common source of wrong-looking pickers.
- **`Host *` and other patterns are not selectable targets** — they are dropped from the
  list, but their keywords still apply as defaults _onto_ named hosts. A global
  `Host *` / `User root` must reach every entry.
- **`Include` is expanded**, with glob support and relative paths resolved against
  `~/.ssh/`. Include cycles are broken by tracking visited absolute paths.
- **`Match` blocks are skipped.** They have no static host to offer, and `Match exec`
  would mean running arbitrary commands to build a picker list.
- **`Host a b c`** yields three entries sharing one keyword block.
- **`~` is expanded** in `IdentityFile` and include paths.

## Data Flow

```
prefix+i
  └─ Herdr runs action `operator.herdr-ssh.open-picker`
       env: HERDR_PANE_ID (the focused pane), HERDR_WORKSPACE_ID
     1. write caller context → $HERDR_PLUGIN_STATE_DIR/caller.json
     2. herdr plugin pane open --plugin operator.herdr-ssh \
          --entrypoint picker --placement overlay --focus

  └─ overlay pane runs `herdr-ssh picker`
     3. parse SSH config · load theme · herdr pane list → mark `open`
     4. start probes in the background; render immediately
     5. user filters and picks:
          enter → split      ^t → tab
          ^z    → zoomed     ^n → force new
     6a. an `ssh:<host>` pane exists and !ForceNew and reuse_panes
           → herdr pane focus <id>
           → herdr plugin pane close picker
     6b. otherwise
           → herdr plugin pane open --entrypoint session \
               --placement <split|tab|zoomed> \
               --target-pane <caller pane from caller.json> \
               --direction <split_direction> \
               --env HERDR_SSH_TARGET=<alias> --focus
           → herdr plugin pane close picker

  └─ session pane runs `herdr-ssh session`
     7. herdr pane rename $HERDR_PANE_ID "ssh:<alias>"
     8. syscall.Exec(ssh, ssh_args..., alias)   ← SSH owns the pty from here
```

The rename in step 7 is what makes step 6a possible. It is server-side state, so it
survives the `Exec` that replaces the plugin process.

## Picker Behavior

### Row Markers

Two distinct facts get two distinct glyphs. Conflating "a pane is already connected" with
"the host answers on 22" would make the reuse affordance unreadable.

```
▪ nixos-dev    operator@192.0.2.10   ▪ open   pane exists (accent color)
  nixbuild     root@10.0.0.12           ● up     TCP answered (green)
  oldbox       10.0.0.99                ○        no answer
  jumped       via bastion              ~        ProxyJump, not probed
```

`open` drives reuse. `up` is advisory only — it never changes what a key does.

### Reachability Probe

Concurrent TCP dial to each resolved `HostName:Port`, 300ms default timeout, results
streamed into the list as Bubble Tea messages. First paint never waits on the network.

ProxyJump hosts are not probed: reaching them means dialing through the bastion, which is
slow and authenticates a jump the operator did not ask for.

Each picker open sends one SYN per host — 20 for the current config. `probe = false`
disables it outright for operators who don't want that traffic.

### Preview Panel

Bottom third, resolved fields for the highlighted host, including provenance — which
matters once `Include` is in play:

```
HostName      192.0.2.10
User          operator
Port          22
IdentityFile  ~/.ssh/id_nixos
source        ~/.ssh/config:41
```

`^o` toggles it.

### Fuzzy Matching

Ranked: exact alias prefix, then alias substring, then scattered alias characters, then
matches against `HostName`. Alias matches always outrank `HostName`-only matches, so typing
a name you know gets you that host rather than an IP that happens to contain the digits.

### Theme

`internal/theme` reads `[theme].name` and `[ui].accent` from `$HERDR_CONFIG_PATH` once at
startup — the same approach the sesh plugin takes. The `terminal` theme maps to ANSI 16
with the accent override applied, so on this machine the picker comes up bright green
(`#14e21a`) and matches the sidebar, tmux, and Ghostty. No `auto_switch` watching.

## Plugin Configuration

`$HERDR_PLUGIN_CONFIG_DIR/config.toml`. Every key optional; defaults shown.

```toml
probe = true
probe_timeout_ms = 300
split_direction = "right"     # or "down"
show_preview = true
reuse_panes = true
hidden = []                   # globs matched against alias, e.g. ["colima", "*-old"]
extra_config_paths = []       # SSH configs outside the Include chain
ssh_args = []                 # flags passed to ssh, before the destination
```

## Error Handling

Each failure mode gets a specific error type and a visible outcome. Nothing is swallowed,
and no failure leaves a pane that disappears before the operator can read why.

| Failure                        | Behavior                                                                                                                                                           |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| No `~/.ssh/config`             | Overlay renders `no ~/.ssh/config — nothing to pick`; esc closes; exit 0                                                                                           |
| `Include` target unreadable    | Skip that file, continue parsing, footer notes `1 include unreadable`                                                                                              |
| Malformed config line          | Skip the line, never abort the parse; collect as a `Warning`                                                                                                       |
| `herdr` CLI call fails         | Footer shows the error, overlay **stays open**, full detail to the plugin log (`herdr plugin log`)                                                                 |
| `ssh` not on PATH              | Session pane prints the resolved command and the error, then waits for a keypress instead of exec'ing — otherwise the pane vanishes before the message is readable |
| `caller.json` missing or stale | Omit `--target-pane`; Herdr falls back to the focused pane                                                                                                         |
| Probe timeout                  | Row shows `○`; never blocks selection                                                                                                                              |

## Testing

- **`internal/sshconfig`** — golden tests over `testdata/`: `Include` chains, cycles,
  `Host a b c` multi-alias, wildcard exclusion with defaults still applied, `Match`
  skipping, first-value-wins precedence, `~` expansion.
- **`internal/herdrapi`** — a fake `herdr` binary on `PATH` that logs its argv to a file;
  assertions on exact command lines. This mirrors the `HERDR_FAKE_LOG` pattern the sesh
  plugin uses and is worth adopting directly.
- **`internal/picker`** — fuzzy ranking order, keymap → `Selection` mapping, empty-state
  rendering.
- **`internal/probe`** — real listener on `127.0.0.1:0` for the up path, a closed port for
  down, an unroutable address for the timeout path.
- **Manual smoke** after `herdr plugin link`: split, tab, zoomed, reuse, and force-new,
  each exercised once in a live session. Tests passing is not the finish line; the picker
  has to behave in the running cockpit.
- **CI** — GitHub Actions: build, test, `golangci-lint`, matching the ecosystem norm.

## Distribution

Local development through `herdr plugin link ~/DEVELOPMENT/herdr-plugin-ssh`. Publishing is
a GitHub topic, not a submission: add `herdr-plugin` to the public repo with a
`herdr-plugin.toml` at its root and the herdr.dev index picks it up on its next refresh.

A search of the 1045-plugin index for "ssh config" returns no equivalent plugin, so this
fills a real gap rather than duplicating one.
