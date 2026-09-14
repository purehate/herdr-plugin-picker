package herdrsock

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sockPath returns a path short enough to bind. A unix socket path is capped
// at 104 bytes on macOS, and t.TempDir() spends most of that budget on TMPDIR
// plus the test's own name, so these tests cannot use it.
func sockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

// serve answers one request per connection with reply(request), so a test can
// assert on what the client sent and control what it reads back.
func serve(t *testing.T, reply func(map[string]any) string) string {
	t.Helper()
	path := sockPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
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
				var req map[string]any
				if err := json.Unmarshal([]byte(line), &req); err != nil {
					return
				}
				_, _ = conn.Write([]byte(reply(req) + "\n"))
			}()
		}
	}()
	return path
}

func TestSendTextWritesTheRequestAndReadsTheResult(t *testing.T) {
	var got map[string]any
	path := serve(t, func(req map[string]any) string {
		got = req
		return `{"id":"` + req["id"].(string) + `","result":{}}`
	})

	if err := (Client{Path: path}).SendText("w1:p2", "id\n"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if got["method"] != "pane.send_text" {
		t.Fatalf("method = %v, want pane.send_text", got["method"])
	}
	params, ok := got["params"].(map[string]any)
	if !ok {
		t.Fatalf("params missing from %v", got)
	}
	if params["pane_id"] != "w1:p2" {
		t.Errorf("pane_id = %v, want w1:p2", params["pane_id"])
	}
	if params["text"] != "id\n" {
		t.Errorf("text = %q, want %q", params["text"], "id\n")
	}
	if id, _ := got["id"].(string); id == "" {
		t.Error("request carried no id")
	}
}

// Every request needs its own id: the server echoes it back, and a client that
// reused one could not tell two replies apart on a shared connection.
func TestEachRequestGetsItsOwnID(t *testing.T) {
	ids := make(chan string, 2)
	path := serve(t, func(req map[string]any) string {
		id := req["id"].(string)
		ids <- id
		return `{"id":"` + id + `","result":{}}`
	})

	c := Client{Path: path}
	for range 2 {
		if err := c.SendText("w1:p1", "x"); err != nil {
			t.Fatalf("SendText: %v", err)
		}
	}
	first, second := <-ids, <-ids
	if first == second {
		t.Errorf("both requests used id %q", first)
	}
}

func TestSendTextReportsAServerError(t *testing.T) {
	path := serve(t, func(req map[string]any) string {
		return `{"id":"` + req["id"].(string) + `","error":{"code":"not_found","message":"no such pane"}}`
	})

	err := (Client{Path: path}).SendText("w9:p9", "x")
	if err == nil {
		t.Fatal("SendText succeeded against an error reply")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *Error", err)
	}
	if apiErr.Code != "not_found" {
		t.Errorf("Code = %q, want not_found", apiErr.Code)
	}
	if !strings.Contains(err.Error(), "no such pane") {
		t.Errorf("error %q does not carry the server message", err)
	}
}

// A reply whose id is not the one we sent belongs to another request. Accepting
// it would report someone else's success as ours.
func TestSendTextRejectsAMismatchedID(t *testing.T) {
	path := serve(t, func(map[string]any) string {
		return `{"id":"someone-else","result":{}}`
	})

	err := (Client{Path: path}).SendText("w1:p1", "x")
	if err == nil {
		t.Fatal("SendText accepted a reply for a different request")
	}
	if !strings.Contains(err.Error(), "someone-else") {
		t.Errorf("error %q does not name the mismatched id", err)
	}
}

func TestSendTextFailsWhenTheSocketIsMissing(t *testing.T) {
	err := (Client{Path: filepath.Join(t.TempDir(), "absent")}).SendText("w1:p1", "x")
	if err == nil {
		t.Fatal("SendText succeeded with no socket")
	}
}

// An unconfigured client must fail rather than dial whatever happens to sit at
// a default path.
func TestSendTextFailsWithNoPath(t *testing.T) {
	if err := (Client{}).SendText("w1:p1", "x"); err == nil {
		t.Fatal("SendText succeeded with an empty path")
	}
}

// A server that accepts the connection and never answers must not hang the
// picker: the popup would be wedged with no way to cancel.
func TestSendTextTimesOutOnASilentServer(t *testing.T) {
	path := sockPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Hold the connection open without replying until the test ends.
		t.Cleanup(func() { _ = conn.Close() })
	}()

	done := make(chan error, 1)
	go func() { done <- Client{Path: path, Timeout: 100 * time.Millisecond}.SendText("w1:p1", "x") }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("SendText succeeded against a silent server")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SendText hung past its timeout")
	}
}
