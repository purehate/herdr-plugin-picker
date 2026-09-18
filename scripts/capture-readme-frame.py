"""Capture a picker frame for the README against a synthetic HOME.

Uses tmux as the terminal emulator rather than driving a pty directly. The
direct route works -- a pty with an explicit TIOCSWINSZ is enough to make
Bubble Tea render -- but reading the frame back out of it does not: bubbletea
renders incrementally and positions rows with cursor addressing rather than
literal leading spaces, so stripping escapes from the byte stream flattens the
layout. Expanding cursor-forward fixed some rows and absolute column
addressing broke others, at which point the honest answer is that reassembling
a screen from its diffs needs a terminal emulator. tmux is one, it is already
on this machine, and `capture-pane -p` hands back the settled buffer as text.

Runs on its own tmux socket so it cannot touch the operator's sessions, and
under `env -i` so no HERDR_* variable can reach the child and reconnect it to
the live server. The synthetic HOME is what keeps the operator's real
~/.ssh/config out of a public README -- sshConfigPath() goes through
os.UserHomeDir(), which honours $HOME on unix.

The picker is now the tabbed navigator, so the capture opens on the ssh tab
(Tab presses down the strip) to show the host list, markers, and preview. The
navigator reads its inventory from `herdr` before it draws -- `api snapshot`,
`pane list`, and `machine list` -- so a fake herdr is placed first on PATH: it
answers each with an empty inventory rather than reaching the operator's live
server.

One host points at a socket this script is listening on, so the reachability
probe has a success to report; the rest resolve nowhere and one sits behind a
ProxyJump. That is what makes the captured frame show every marker state.
"""

import shutil
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path

# Matches the picker pane's width/height in herdr-plugin.toml, so the frame
# this captures is the frame herdr actually hands the picker.
ROWS, COLS = 28, 94

# The tab strip, in the order the picker paints it. The capture Tab presses its
# way to the ssh tab, so the count follows this list rather than a magic number
# that silently lands on the wrong tab when a tab is added.
TABS = ["spaces", "agents", "sessions", "panes", "ssh", "machines", "cmd"]
SSH_TAB = TABS.index("ssh")

REPO = Path(__file__).resolve().parent.parent
BIN = REPO / "bin" / "herdr-picker"

# Short and under /tmp on purpose, not mkdtemp's default. The preview truncates
# to the pane width, and it truncates the absolute path the picker actually
# read -- before this script gets to substitute a `~` back in. macOS puts the
# default temp dir under /var/folders/<hash>/.../T/, which is long enough to cut
# the `source` line down to "~/.s" and quietly turn the captured frame into a
# claim about this harness rather than about the picker. 17 characters keeps the
# rendered path within a few of a real "/Users/name" or "/home/name" home.
HOME = Path(tempfile.mkdtemp(dir="/tmp", prefix="hsf-"))
SOCK = "hs-shot"
SESSION = "shot"


def tmux(*args: str, capture: bool = False) -> str:
    """Run one tmux command on this script's private server."""
    result = subprocess.run(
        ["tmux", "-L", SOCK, *args],
        check=True,
        text=True,
        capture_output=capture,
    )
    return result.stdout if capture else ""


def write_herdr_shim() -> Path:
    """Lay down a fake herdr that answers each inventory read with nothing.

    The navigator calls `herdr api snapshot`, `herdr pane list`, and
    `herdr machine list` before it draws. Under `env -i` there is no herdr on
    PATH, so without this the process dies before the first frame. Pointing PATH
    at a real herdr is the wrong fix: it would let this capture reach the
    operator's live server and render their real panes into a public README. The
    shim answers the reads the picker makes and nothing else.
    """
    bin = HOME / "bin"
    bin.mkdir(parents=True, exist_ok=True)
    shim = bin / "herdr"
    shim.write_text(
        "#!/bin/sh\n"
        "case \"$1 $2\" in\n"
        "  'api snapshot') printf '%s' '{\"id\":1,\"result\":{\"snapshot\":{\"workspaces\":[],\"tabs\":[],\"agents\":[]}}}' ;;\n"
        "  'pane list') printf '%s' '{\"id\":1,\"result\":{\"panes\":[]}}' ;;\n"
        "  'machine list') printf '%s' '[]' ;;\n"
        "  *) printf '%s' '{\"id\":1,\"result\":{}}' ;;\n"
        "esac\n",
        encoding="utf-8",
    )
    shim.chmod(0o755)
    return bin


def write_config(port: int) -> None:
    """Lay down a synthetic ssh config using only reserved names.

    RFC 2606 reserves .example for documentation and RFC 5737 reserves
    192.0.2.0/24, so nothing here can collide with a real host. The Include
    comes first so the host it contributes lands under the cursor -- that is
    the row whose preview carries the `source` line, which is the part that
    shows the include chain resolved rather than the alias merely existing.
    """
    ssh = HOME / ".ssh"
    (ssh / "conf.d").mkdir(parents=True, exist_ok=True)

    # reuse_panes off. With it on -- the default -- the picker asks herdr for
    # its open panes so it can paint the "session already open" marker, and
    # under `env -i` there is no herdr on PATH, so the frame comes out with a
    # "could not list panes" line above it. Putting herdr back on PATH is the
    # wrong fix: it would let this capture reach the operator's live server and
    # render their real panes into a public README. Turning the lookup off
    # removes the call instead of satisfying it.
    #
    # This lands on resolvePluginConfigDir's fallback, which is
    # ~/.config/herdr/plugins/config/<id>/ -- reachable here precisely because
    # HOME is synthetic, and reached without setting any HERDR_* variable.
    cfg = HOME / ".config" / "herdr" / "plugins" / "config" / "purehate.herdr-picker"
    cfg.mkdir(parents=True, exist_ok=True)
    with open(cfg / "config.toml", "w", encoding="utf-8") as f:
        f.write("reuse_panes = false\n")

    with open(ssh / "conf.d" / "staging.conf", "w", encoding="utf-8") as f:
        f.write(f"Host staging\n  HostName 127.0.0.1\n  Port {port}\n  User deploy\n")

    with open(ssh / "config", "w", encoding="utf-8") as f:
        f.write(
            "Include conf.d/*.conf\n"
            "\n"
            "Host *\n"
            "  User deploy\n"
            "\n"
            "Host web1\n"
            "  HostName web1.example\n"
            "\n"
            "Host db-primary\n"
            "  HostName 192.0.2.10\n"
            "  User postgres\n"
            "\n"
            "Host bastion\n"
            "  HostName bastion.example\n"
            "  Port 2222\n"
            "\n"
            "Host behind-jump\n"
            "  HostName 192.0.2.50\n"
            "  ProxyJump bastion\n"
            # Enough rows below the jump host to fill the 28-row pane, so the
            # captured frame has no run of blank list rows between the preview
            # and the footer. All reserved names, like the rest.
            "\n"
            "Host cache1\n"
            "  HostName 192.0.2.21\n"
            "\n"
            "Host worker1\n"
            "  HostName 192.0.2.31\n"
            "\n"
            "Host worker2\n"
            "  HostName 192.0.2.32\n"
            "\n"
            "Host metrics\n"
            "  HostName 192.0.2.40\n"
            "\n"
            "Host logs\n"
            "  HostName 192.0.2.41\n"
            "\n"
            "Host build\n"
            "  HostName 192.0.2.60\n"
            "\n"
            "Host registry\n"
            "  HostName 192.0.2.61\n"
            "\n"
            "Host vpn-gw\n"
            "  HostName vpn.example\n"
            "\n"
            "Host mail\n"
            "  HostName mail.example\n"
        )


def capture(port: int) -> str:
    write_config(port)
    bindir = write_herdr_shim()
    tmux(
        "new-session",
        "-d",
        "-s",
        SESSION,
        "-x",
        str(COLS),
        "-y",
        str(ROWS),
        # env -i rather than a filtered copy: an unset HERDR_* variable that
        # gets added to herdr later would otherwise silently reconnect this.
        # PATH starts with the shim dir so the snapshot call lands there.
        f"env -i HOME={HOME} TERM=xterm-256color PATH={bindir}:/usr/bin:/bin {BIN} navigator",
    )
    try:
        # Let the first frame paint, then move from spaces to the ssh tab.
        time.sleep(0.8)
        for _ in range(SSH_TAB):
            tmux("send-keys", "-t", SESSION, "Tab")
            time.sleep(0.2)
        # Long enough for the probes to answer: the markers arrive from the
        # network after the first paint, and a frame captured before they land
        # shows a blank marker column, which is a different claim. Fifteen hosts
        # probe at once, but the fixed-port one waits on the accept loop below.
        time.sleep(4.0)
        return tmux("capture-pane", "-p", "-t", SESSION, capture=True)
    finally:
        tmux("send-keys", "-t", SESSION, "Escape")
        time.sleep(0.3)
        subprocess.run(
            ["tmux", "-L", SOCK, "kill-server"],
            check=False,
            capture_output=True,
        )


def bind() -> socket.socket:
    """Listen on a fixed port if it is free, an ephemeral one if it is not.

    Fixed for legibility: 2022 reads as a forwarded local port, where a
    five-digit ephemeral one reads as noise in a frame meant to be looked at.
    It is not worth failing the capture over, hence the fallback.
    """
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        srv.bind(("127.0.0.1", 2022))
    except OSError:
        srv.bind(("127.0.0.1", 0))
    srv.listen(8)
    return srv


def main() -> None:
    try:
        with bind() as srv:
            frame = capture(srv.getsockname()[1])
    finally:
        shutil.rmtree(HOME, ignore_errors=True)
    # The synthetic HOME stands in for the reader's own, so show it as theirs.
    # The picker prints the absolute path it actually read, which is correct
    # and which would otherwise put this harness's scratch directory in a
    # public README.
    frame = frame.replace(str(HOME), "~")
    # Trailing blank rows are the unused bottom of a 28-row pane. The leading
    # one is real -- the picker paints an empty first row for breathing room
    # under herdr's border -- but inside a README fence it reads as a stray
    # newline after the ``` rather than as part of the frame. Dropping both ends
    # is what makes this output paste into README.md unedited, which is the
    # point: the frame there is only honest for as long as nobody hand-tunes it.
    sys.stdout.write(
        "\n".join(line.rstrip() for line in frame.split("\n")).strip("\n") + "\n"
    )


if __name__ == "__main__":
    main()
