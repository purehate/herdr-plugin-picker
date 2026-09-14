# SSH Picker for Herdr — Design

> **Superseded (2026-09-14).** The standalone SSH picker this describes was
> folded into the tabbed picker as its `ssh` tab, and the plugin was renamed
> `purehate.herdr-picker`. The manifest, entrypoints, and env vars named below
> are historical; see `README.md` and `internal/picker/navigator.go` for what
> shipped. Kept for the reasoning, not as a description of the current code.

**Date:** 2026-09-09
**Status:** Superseded
**Plugin id:** `purehate.herdr-picker` (was `purehate.herdr-ssh`)

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

Three findings drove the architecture and are worth stating plainly, because the obvious
implementations all fail:

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

3. **`herdr pane focus` cannot focus a pane by id** — it takes `--direction
left|right|up|down` and moves to a _neighbor_. Focusing an arbitrary pane is
   `herdr plugin pane focus <PANE_ID>`, which works for plugin-owned panes (ours are, since
   they were opened via `plugin pane open`). A reused pane may also live in another tab or
   workspace, so reuse is a three-step sequence, each step skipped when already current:

   ```
   herdr workspace focus <workspace_id>
   herdr tab focus <tab_id>
   herdr plugin pane focus <pane_id>
   ```

   `herdr pane list` with no arguments spans every workspace (23 panes across 10 on this
   machine), so reuse lookup is global, not workspace-local.

## Architecture

Single Go binary, four verbs, seven focused packages.

### Manifest (`herdr-plugin.toml`)

```toml
id = "purehate.herdr-ssh"
name = "SSH Picker"
version = "0.1.1"
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
placement = "popup"
width = 94
height = 28
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
command = "purehate.herdr-ssh.open-picker"
```

`prefix+r` was the operator's first instinct but is Herdr's built-in `resize_mode`.
`prefix+i` is free in both the Herdr defaults and the existing local config, and displaces
no built-in. Free single keys remaining afterward: `a`, `u`.

### Packages

Each unit has one purpose, a defined interface, and is testable without a running Herdr.
None should exceed roughly 250 lines.

| Unit                    | Responsibility                               | Interface                                                                        | Depends on                     |
| ----------------------- | -------------------------------------------- | -------------------------------------------------------------------------------- | ------------------------------ |
| `internal/sshconfig`    | Parse SSH config into hosts                  | `Parse(root string) ([]Host, []Warning, error)`                                  | stdlib only                    |
| `internal/herdrapi`     | Every `herdr` CLI call, behind one exec seam | `PaneList()`, `FocusPane(pane)`, `PaneRename(id, label)`, `PluginPaneOpen(opts)` | `$HERDR_BIN_PATH`              |
| `internal/picker`       | Bubble Tea UI: filter, list, preview, keymap | `Run([]Host, Theme, Config) (Selection, error)`                                  | sshconfig, theme, pluginconfig |
| `internal/theme`        | Herdr theme → color tokens                   | `Load(configPath string) Theme`                                                  | `go-toml`                      |
| `internal/pluginconfig` | The plugin's own `config.toml`               | `Defaults() Config`, `Load(dir string) (Config, error)`                          | `go-toml`                      |
| `internal/probe`        | Async TCP reachability                       | `Probe(ctx, []Host, timeout) <-chan Result`                                      | stdlib only                    |
| `cmd/herdr-ssh`         | Wire the verbs                               | `plugin open-picker`, `picker`, `session`, `connect`                             | all of the above               |

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
    ProxyCommand string
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
  `~/.ssh/`. Include cycles are broken by tracking the absolute paths currently open on
  the **descent path** — a stack, unwound as each file closes — plus a depth cap of 16.
  A global visited set would be wrong, not merely different: a fragment included by two
  sibling `Host` blocks must resolve for **both**, and a visited set would skip the
  second, silently dropping every setting in a shared fragment from every stanza after
  the first. That is verified against OpenSSH, which resolves both.
- **`Match` blocks are skipped.** They have no static host to offer, and `Match exec`
  would mean running arbitrary commands to build a picker list.
- **`Host a b c`** yields three entries sharing one keyword block.
- **A `Host` pattern that can never match is inert.** Its keywords are parsed but never
  applied, matching OpenSSH's `SSHCONF_NEVERMATCH` rule. This is part of the contract, not
  a defensive detail: it changes which hosts appear in the picker, so an operator whose
  host is missing needs a documented rule to consult.
- **`~` is expanded** in `IdentityFile` and include paths.

## Data Flow

```
prefix+i
  └─ Herdr runs action `purehate.herdr-ssh.open-picker`
       env: HERDR_PANE_ID (the focused pane), HERDR_WORKSPACE_ID, HERDR_TAB_ID
     1. forward caller context as invocation-scoped HERDR_SSH_CALLER_* env
     2. herdr plugin pane open --plugin purehate.herdr-ssh \
          --entrypoint picker --placement popup --env ... --focus

  └─ popup pane runs `herdr-ssh picker`
     3. parse SSH config · load theme · herdr pane list → mark `open`
     4. start probes in the background; render immediately
     5. user filters and picks:
          enter → split      ^t → tab
          ^z    → zoomed     ^n → force new
          ^j/^k → down/up    ^o → preview
          ^u    → clear      ^w → delete word
     6a. an `ssh:<host>` pane exists and !ForceNew and reuse_panes
           → workspace focus / tab focus / plugin pane focus <id>
           → herdr plugin pane close $HERDR_PANE_ID
     6b. otherwise
           → herdr plugin pane open --entrypoint session \
               --placement <split|tab|zoomed> \
               --target-pane <caller pane from HERDR_SSH_CALLER_PANE_ID> \
               --direction <split_direction> \
               --env HERDR_SSH_TARGET=<alias> --focus
           → herdr plugin pane close $HERDR_PANE_ID

  └─ session pane runs `herdr-ssh session`
     7. herdr pane rename $HERDR_PANE_ID "ssh:<alias>"   (sets PaneInfo.label)
     8. syscall.Exec(ssh, ssh_args..., alias)   ← SSH owns the pty from here
```

The rename in step 7 is what makes step 6a possible. It is server-side state, so it
survives the `Exec` that replaces the plugin process.

## Picker Behavior

### Layout

The picker is a floating box, so it never grows to fill the terminal: the host list is
capped at **12 rows** however tall the pane is. Below that cap the row count is derived
from the pane height reported by the terminal, not fixed — chrome (query line, preview,
separator, key hints, warning line) is measured, not assumed, and the rows get what is
left. When the list is longer than the budget the window **scrolls** to keep the cursor
visible, and the count of hosts outside the window is shown. Typing is still the primary
way to narrow the list, but the cursor is not confined to the first screenful: cursor
movement is clamped to the length of the filtered list, so clipping the view without also
clamping the cursor would let the selection walk off-screen and `enter` connect to a host
the operator cannot see. Before the first size message arrives the cap is the budget.

**The rendered frame must never exceed the reported height.** The picker runs inline
rather than in an alternate screen, so an over-tall frame scrolls the pane instead of
being clipped by it. Chrome is not entitled to the space it wants: the preview is the
only optional element, so in a pane too short for both it yields — and where the preview
cannot fit at all, `^o` does nothing. Losing the panel in a pane that could not display
it is better than a popup whose height depends on which row the cursor is on, since
the preview's height varies with the highlighted host's field count.

Pane **width** is used, and lines wider than it are **truncated**. This is not cosmetic.
The height budget above counts logical lines, but the invariant is about screen rows, so
any line wider than the pane wraps and the frame exceeds its reported height without any
of the budget arithmetic noticing. The key-hints line alone is 77 columns and counts as
one, which made the invariant unachievable in any pane narrower than that — even with the
preview off. Truncation is what makes "never exceed the reported height" a fact rather
than an aspiration.

This section previously recorded the opposite ("pane width is deliberately unused"), on
the reasoning that leaving long rows to the terminal was simpler than eliding them. That
was wrong in a way worth keeping visible: the two clauses could not both hold, and the
one with a stated operator-visible consequence — an over-tall frame scrolls the pane,
because the picker runs inline rather than in an alternate screen — is the one that had
to win.

### Row Markers

Two distinct facts get two distinct glyphs. Conflating "a pane is already connected" with
"the host answers on 22" would make the reuse affordance unreadable.

```
▪ nixos-dev    operator@192.0.2.10      ▪ open   pane exists (accent color)
  nixbuild     root@10.0.0.12           ● up     TCP answered (green)
  oldbox       10.0.0.99                ○        no answer
  jumped       via bastion              ~        proxied, not probed
  fresh        10.0.0.50                         not probed yet (blank)
```

`open` drives reuse. `up` is advisory only — it never changes what a key does.

The blank is a fifth state, not the absence of one: probes stream in, so every row is
blank before its result arrives, and a row that never gets probed stays blank. Only the
annotations to the right of the marker column are commentary; the glyphs themselves are
the contract.

### Reachability Probe

Concurrent TCP dial to each resolved `HostName:Port`, 300ms default timeout, results
streamed into the list as Bubble Tea messages. First paint never waits on the network.

ProxyJump and ProxyCommand hosts are not probed: dialing them directly tests a
route SSH will not use and can report a reachable host as down. `none` disables
either mechanism and therefore does not suppress probing.

Each picker open sends one SYN per host — 20 for the current config. `probe = false`
disables it outright for operators who don't want that traffic.

### Preview Panel

Below the list, resolved fields for the highlighted host, including provenance — which
matters once `Include` is in play:

```
HostName      192.0.2.10
User          operator
Port          22
IdentityFile  ~/.ssh/id_nixos
source        ~/.ssh/config:41
```

The field list is illustrative and open — "resolved fields" means whichever of them the
host actually sets, so `ProxyJump` and `ProxyCommand` belong here too, and a
field the host does not set is omitted rather than rendered empty. The
two-column alignment is not illustrative: labels pad to a common width so the
values form a single scannable edge. That is the whole reason the panel exists.

`^o` toggles it. In a pane too short to fit the list and the panel together the panel
yields — see Layout.

### Query Editing

`^u` clears the query, `^w` deletes the last word — the two fzf editing keys that earn
their place. `^w` treats the scorer's separators as word boundaries, so `^w` on
`nixos-dev` leaves `nixos-`, which is still a useful query.

fzf's `^n` / `^p` (cursor down / up) are **not** adopted: `^n` already means "open a second
pane for this host", and that binding wins because it is a capability with no other key,
while cursor movement already has arrows and `^j` / `^k`. Adding `^p` alone would leave an
asymmetric half-pair, so it is left out too.

### Fuzzy Matching

Ranked in six tiers, best first: exact alias, alias prefix, alias substring, scattered
alias characters, `HostName` substring, scattered `HostName` characters. Alias matches
always outrank `HostName`-only matches, so typing a name you know gets you that host
rather than an IP that happens to contain the digits.

The list above used to name four tiers, collapsing exact-vs-prefix into "exact alias
prefix" and both `HostName` tiers into "matches against `HostName`". Corrected to six
because the two it merged are not cosmetic: an exact alias outranks a host whose alias
merely starts with the query, which is what makes a short alias reachable when it is a
prefix of a longer one. The "other four tiers" sentence below is unchanged — it was
written against the six-tier implementation and is right about it, which is how the
discrepancy surfaced: it never added up against the four-tier list directly above it.

**Matched characters are highlighted in the accent color** — in the alias for an alias
match, in the address for a `HostName`-only match. Ranking without highlighting makes the
order look arbitrary: when `nixos-dev` sits above `prod-web` for the query `nxd`, the
operator can only trust the order if they can see which characters earned it. The
highlight is also what tells them a row surfaced through its address rather than its name.
This is the one visible fzf convention worth adopting wholesale.

Because the accent now means "this character matched", the cursor row is marked by its
pointer and bold text rather than by being fully accented — otherwise the row under the
cursor would be the one row whose highlight is invisible.

**Ties inside the two scattered tiers break on word boundaries.** A scattered match that
lands at the start of a word (position 0, or after `-`, `_`, `.`, `/`, `:`, `@`, or a
space) is worth more than one buried mid-word, and consecutive matched characters are
worth more than spread-out ones — fzf's scoring shape, at fzf's weights. This tier had no
discrimination at all before: every scattered match tied and fell back to config order.
The other four tiers keep config order on a tie, so the ranking above stands unchanged.

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
ssh_args = []                 # non-routing flags passed to ssh before the destination
```

**There is deliberately no key for adding a config from outside the `Include` chain.** An
earlier draft of this spec offered `extra_config_paths = []`, described as "SSH configs
outside the Include chain", and it was implemented and then removed at `5a31f54`. The
definition and the mechanism contradicted each other: selecting a host execs `ssh <alias>`
with no `-F`, so ssh resolves that alias against `~/.ssh/config` and its `Include` chain
alone — by construction, not the extra files. Rows sourced from one were therefore
displayed with a `HostName`, `Port`, `User` and proxy route that ssh never saw, and the
connection went somewhere other than the preview said.

The `ProxyJump` case is why this is a correctness rule and not a preference: such a host is
marked `~` and skipped by the probe, so nothing looked wrong, and the operator selected a
host they believed was reached through a bastion while ssh connected directly. On an
engagement that is traffic from an unauthorized source, off the authorized pivot.

`ssh_args = ["-F", other]` is rejected: `-F` replaces `~/.ssh/config` rather than
merging with it, so the preview and connection would disagree. The same rule
rejects other routing and identity overrides. `Include <abs-path>` is the
supported mechanism, and it is the one ssh itself resolves.

## Error Handling

Each failure mode gets a specific error type and a visible outcome. Nothing is swallowed,
and no failure leaves a pane that disappears before the operator can read why.

| Failure                            | Behavior                                                                                                                                        |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| No `~/.ssh/config`                 | Popup renders `no ~/.ssh/config — nothing to pick`; esc closes; exit 0                                                                           |
| `Include` target unreadable        | Skip that file, continue parsing, and show a footer warning                                                                                      |
| Malformed SSH config line          | Skip the line, continue parsing, and show a footer warning                                                                                       |
| Malformed or unknown plugin config | Show a warning, use safe defaults, and disable probing                                                                                           |
| Invalid recognized plugin value    | Show a warning, reset that value, and preserve the other valid values                                                                            |
| `herdr` CLI call fails             | Show the error and hold the popup open; pane entrypoints are not captured by `herdr plugin log` — see Logging Reach below                        |
| `ssh` not on PATH                  | Print the resolved command and error, then wait for enter so the pane does not vanish before the message is read                                 |
| Forwarded caller missing           | Use `HERDR_ACTIVE_*` for a direct popup; otherwise omit `--target-pane` and let Herdr use its current pane                                        |
| Forwarded caller pane became stale | Check it against the pane list and omit `--target-pane` if it is no longer live                                                                  |
| Unsafe `ssh_args`                  | Warn, discard `ssh_args`, and keep other valid plugin settings                                                                                   |
| Probe timeout                      | Row shows `○`; never blocks selection                                                                                                            |

### Logging Reach

`herdr plugin log` captures **action and event-hook invocations**, not pane
entrypoints. Verified against Herdr 0.9.0 rather than assumed: `herdr plugin log
list` returns entries carrying `command`, `stdout`, `stderr`, `exit_code`,
`status` and `event`, and every entry is an action or a hook. No pane entrypoint
appears — including from `fullerzz.sesh`, the reference overlay-pane plugin this
design takes its pane model from. A pane process owns a pty, so its stderr goes
to the terminal rather than to a pipe Herdr can read.

That splits this plugin's three verbs:

- **`open-picker` is an action.** Its stderr and exit code are captured
  automatically. Nothing needs building for it, and the error table's promise of
  durable detail holds.
- **`picker` and `session` are pane entrypoints.** Every diagnostic they write
  reaches the operator's screen and nowhere else. The promise does not hold for
  them and cannot be made to without a channel Herdr does not offer.

Writing to the pty is the best available channel for the pane verbs, so no code
change follows from this — the operator can still read the message. What follows
is that the pane verbs must not rely on the log as a fallback for anything the
operator needs to see: a diagnostic written to a pane that is about to close is
lost, which is why the `ssh` not on PATH row waits for a keypress rather than
trusting that the detail survives elsewhere.

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
