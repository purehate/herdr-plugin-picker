# The command tab: one popup that does anything

The five tabs this plugin ships are all nouns — a space, an agent, a tab, a
pane, a host. Every one of them answers "take me there." None of them answers
"do this." That is the gap between a picker and the thing this is meant to be:
one floating popup an operator drives everything from, without leaving the
keyboard and without memorising a keybinding per verb.

## Why the socket makes this possible here and nowhere else

Broadcast already established that the CLI exposes roughly thirty commands
against the socket's 128, and that the picker can speak the socket. That
groundwork pays off twice, because two of those 128 are:

    plugin.action.list    { plugin_id? }   omit plugin_id for every plugin
    plugin.action.invoke  { action_id, plugin_id?, context? }

`plugin.action.list` with no `plugin_id` returns the actions of every installed
plugin, each with a `title`, an `action_id`, and the `contexts` it is valid in.
On the author's machine that is 42 actions across 13 plugins — including the
actions of other pickers. `plugin.action.invoke` then runs any of them.

There is no CLI equivalent. Every other command-palette plugin in the
marketplace shells out to `herdr`, which caps them at the verbs the CLI
happens to expose and blinds them to what else is installed. Reading the
action list off the socket is the whole differentiator, and it is three
requests of work because the socket client already exists.

The practical consequence is that the tab is not a fixed menu. It is a
projection of whatever the operator has installed, and it grows when they
install something new without this plugin shipping a release.

## What goes in the list

Two sources, merged into one flat ranked list:

- **Discovered plugin actions**, from `plugin.action.list`. Labelled with
  their `title`, since plugin authors already wrote a human-readable one.
- **Native verbs**, a hand-picked set of socket operations that are useful
  to invoke by name — `pane.split`, `pane.zoom`, `tab.create`,
  `workspace.rename`, `worktree.create`, `layout.apply`, and so on.

Hand-picked rather than generated from the schema, because the 128 include
events, getters, and plumbing (`pane.report_metadata`, `client_shell.surface.set`)
that would bury the dozen verbs anyone actually wants. A generated list is a
worse list.

## Contexts decide what is shown, not what errors

Actions carry `contexts`, and there are three that matter, not two: `pane`,
`workspace`, and `global`. Absent means "anywhere". Counted across the 42
actions installed on the author's machine:

    ["pane","workspace"]   11
    ["global"]             11
    ["workspace"]          10
    (absent)                4
    ["pane"]                3
    ["global","workspace"]  3

The tab filters to what the picker can satisfy rather than listing everything
and letting the invoke fail, because a row that is offered and then refused
teaches the operator to distrust the list. But "what the picker can satisfy" is
all three: it runs as a popup pane, inside a workspace, and `global` asks for no
id at all.

This was worth counting rather than assuming. Filtering to `pane` and
`workspace` alone — which is what "the context the picker was opened from"
sounds like it means — silently dropped eleven of the forty-one actions, and
they were the best ones: every pane-navigation and resize verb `herdr-splits`
exposes declares `global`.

## Ranking

The list starts static: native verbs first, then plugin actions by plugin id
and action id. That is predictable, which is the property that matters before
muscle memory exists, and the fuzzy query does the real work of getting to a
row.

Frecency then floats what the operator actually runs, reusing `internal/sshusage`
rather than growing a second ranker — same counts, same decay, a separate file
so a host and a command id cannot inherit each other's rank. The important half
is what it does _not_ do: a command with no history keeps its static position,
so the tab an operator is still learning does not reshuffle underneath them.
Only rows they have already chosen move, and a command herdr refused is not
recorded, so a failed invocation does not climb.

Worth noting as its own differentiator: of the 1114 plugins in the marketplace,
zero mention frecency.

## Destructive verbs

`pane.close`, `workspace.close` and `server.stop` are in the native set and all
three are irreversible from a popup that is about to disappear. They reuse the
broadcast confirmation — the same y/n line, for the same reason. `server.stop`
is excluded entirely: nothing about "kill the server I am running inside" is a
picker's job.
