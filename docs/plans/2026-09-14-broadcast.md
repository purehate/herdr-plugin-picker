# Broadcast: one command to many panes

Ported from Da Vinci Console's `prefix+B`, which this plugin's author used in
tmux to kick off the same command across several panes during an engagement.

## Why the socket

`herdr` the CLI exposes roughly thirty commands. The socket API exposes 128
(`herdr api schema --json`). Sending text to an arbitrary pane is only in the
second set:

    pane.send_text  { pane_id, text }   both required

The CLI's nearest relative is `herdr agent send-keys`, which targets agents
only — it cannot reach a plain shell, which is exactly where a broadcast is
aimed. So broadcast needs the socket.

The wire protocol, confirmed against a live server at `$HERDR_SOCKET_PATH`:
newline-delimited JSON, no handshake, request `{id, method, params}`, reply
`{id, result}` or `{id, error: {code, message}}`.

Only `pane.send_text` goes over the socket. Listing stays on the CLI
(`herdr pane list`), which already returns every field the tab needs through
the same `{id, result}` envelope `callJSON` handles. Two transports is one more
than ideal, but the alternative is porting every existing call to the socket
for no gain today.

## The panes tab

There is no pane inventory in `api snapshot` — it carries workspaces, tabs, and
agents. Broadcast needs panes, and a pane list is independently useful: today
you can jump to a workspace, an agent, or a tab, but not to an arbitrary pane.

So this adds a fifth tab, `panes`, listing every pane with its workspace, tab,
agent and status, working directory, and title. Enter focuses the pane.

## Marking and sending

The ssh tab's `space` mark already exists and is the right verb, so it extends
to the panes tab rather than growing a second multi-select idiom:

- `space` marks the pane under the cursor and steps down
- `^a` marks every pane currently listed
- `^b` opens a one-line command input
- Enter asks `send to N panes? y/n` before anything is sent

The confirm is not optional. A broadcast is many writes to many live shells and
nothing undoes it; the existing close action already asks y/n for a single
workspace, and this is strictly more consequential.

Marks are about the pane, not the query, matching the ssh tab: a marked pane
stays marked when a query hides its row. The footer carries the count.

## Out of scope for this pass

Recent-command history (Da Vinci offers it on the broadcast prompt).
`internal/sshusage` already has the frecency store to model it on. Worth doing,
but it is a separate change and broadcast is useful without it.
