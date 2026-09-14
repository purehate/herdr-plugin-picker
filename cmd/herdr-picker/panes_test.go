package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/herdrsock"
)

func strptr(s string) *string { return &s }

// The picker draws inside a popup pane of its own. Listing it would offer the
// operator a jump to the popup they are already in, and — worse — a broadcast
// target that types the command into this TUI instead of a shell.
func TestNavPaneItemsDropsThePickersOwnPane(t *testing.T) {
	items := navPaneItems([]herdrapi.Pane{
		{PaneID: "w1:p1", WorkspaceID: "w1", Title: "zsh"},
		{PaneID: "w1:pSELF", WorkspaceID: "w1", Title: "herdr-picker"},
	}, map[string]string{"w1": "project"}, "w1:pSELF")

	if len(items) != 1 || items[0].ID != "w1:p1" {
		t.Fatalf("items = %+v, want only w1:p1", items)
	}
}

// The row carries the pane's place in herdr, because reaching a pane means
// walking workspace → tab → pane and the picker has left by the time that runs.
func TestNavPaneItemsCarryTheRouteToThePane(t *testing.T) {
	items := navPaneItems([]herdrapi.Pane{
		{PaneID: "w1:p1", TabID: "w1:t2", WorkspaceID: "w1", Status: "working", Focused: true},
	}, map[string]string{"w1": "project"}, "")

	got := items[0]
	if got.WorkspaceID != "w1" || got.TabID != "w1:t2" {
		t.Errorf("route = %q/%q, want w1/w1:t2", got.WorkspaceID, got.TabID)
	}
	if got.Status != "working" || !got.Current {
		t.Errorf("Status = %q Current = %v", got.Status, got.Current)
	}
}

func TestPaneLabelPicksTheMostSpecificName(t *testing.T) {
	for _, tc := range []struct {
		name string
		pane herdrapi.Pane
		want string
	}{
		{"agent and title", herdrapi.Pane{Agent: "codex", Title: "review"}, "codex  review"},
		{"agent alone", herdrapi.Pane{Agent: "codex"}, "codex"},
		{"ssh session label", herdrapi.Pane{Label: strptr("ssh:web1"), Title: "web1"}, "ssh:web1"},
		{"title", herdrapi.Pane{Title: "vim main.go"}, "vim main.go"},
		{"directory", herdrapi.Pane{CWD: "/home/a/src/api"}, "api"},
		{"nothing but an id", herdrapi.Pane{PaneID: "w1:p1"}, "w1:p1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := paneLabel(tc.pane); got != tc.want {
				t.Errorf("paneLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

// Titles come from whatever is running in the pane, so they reach the row the
// same way workspace labels do: with the control bytes stripped.
func TestPaneLabelStripsTerminalControls(t *testing.T) {
	got := paneLabel(herdrapi.Pane{Title: "vim \x1b[31mmain.go"})
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("unsafe control in %q", got)
	}
}

func TestShortenHomeWritesATilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct{ in, want string }{
		{home, "~"},
		{filepath.Join(home, "src", "api"), filepath.Join("~", "src", "api")},
		{"/etc", "/etc"},
		{"", ""},
		// A sibling directory that merely starts with the same bytes is not
		// inside $HOME, so it keeps its full path.
		{home + "-backup", home + "-backup"},
	} {
		if got := shortenHome(tc.in); got != tc.want {
			t.Errorf("shortenHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// sendRecorder is a socket server that records every pane.send_text it is sent
// and answers each with ok, or with an error for the panes in fail.
func sendRecorder(t *testing.T, fail map[string]bool) (herdrsock.Client, *[]bcast) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hp")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	got := &[]bcast{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				line, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil {
					return
				}
				var req struct {
					ID     string `json:"id"`
					Params struct {
						PaneID string `json:"pane_id"`
						Text   string `json:"text"`
					} `json:"params"`
				}
				if err := json.Unmarshal([]byte(line), &req); err != nil {
					return
				}
				mu.Lock()
				*got = append(*got, bcast{pane: req.Params.PaneID, text: req.Params.Text})
				mu.Unlock()
				reply := `{"id":"` + req.ID + `","result":{}}`
				if fail[req.Params.PaneID] {
					reply = `{"id":"` + req.ID + `","error":{"code":"not_found","message":"no such pane"}}`
				}
				_, _ = conn.Write([]byte(reply + "\n"))
			}()
		}
	}()
	return herdrsock.Client{Path: path}, got
}

type bcast struct{ pane, text string }

// The newline is the whole point: without it the command is staged in each
// shell's input line and nothing runs.
func TestBroadcastSendsTheCommandWithItsNewline(t *testing.T) {
	sock, got := sendRecorder(t, nil)

	status, err := broadcastText(sock, []string{"w1:p1", "w1:p2"}, "id")
	if err != nil {
		t.Fatalf("broadcastText: %v", err)
	}
	if status != "sent to 2 panes" {
		t.Errorf("status = %q", status)
	}
	if len(*got) != 2 {
		t.Fatalf("sent %d times, want 2: %+v", len(*got), *got)
	}
	for _, g := range *got {
		if g.text != "id\n" {
			t.Errorf("pane %s got %q, want %q", g.pane, g.text, "id\n")
		}
	}
}

// A pane that closed between the list and the send must not silently swallow
// the whole broadcast, nor stop the panes that are still there.
func TestBroadcastReportsTheRefusalsAndKeepsGoing(t *testing.T) {
	sock, got := sendRecorder(t, map[string]bool{"w1:p2": true})

	_, err := broadcastText(sock, []string{"w1:p1", "w1:p2", "w1:p3"}, "id")
	if err == nil {
		t.Fatal("err = nil, want the refusal reported")
	}
	if !strings.Contains(err.Error(), "1 of 3 panes") {
		t.Errorf("err = %q, does not carry the counts", err)
	}
	if !strings.Contains(err.Error(), "no such pane") {
		t.Errorf("err = %q, does not carry the reason", err)
	}
	if len(*got) != 3 {
		t.Fatalf("sent %d times, want 3 — a refusal stopped the rest", len(*got))
	}
}
