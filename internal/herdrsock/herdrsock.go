// Package herdrsock speaks herdr's socket API directly, for the operations the
// herdr CLI does not expose.
//
// The CLI covers navigation well, and the rest of this plugin goes through it
// because a subprocess is trivially fakeable in a test. Sending text to an
// arbitrary pane is the exception: the CLI's `agent send-keys` reaches agent
// panes only, and a broadcast is aimed at ordinary shells.
//
// The protocol is newline-delimited JSON over a unix socket with no handshake:
// write {id, method, params}, read back {id, result} or {id, error}.
package herdrsock

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// defaultTimeout bounds a whole call. The server is local and answers a send in
// microseconds, so a second is already generous; the point is that a wedged
// server cannot wedge the popup, which has no way to cancel a blocked write.
const defaultTimeout = time.Second

// Client dials the herdr API socket. The zero value is unusable: Path must name
// a socket, so a misconfigured plugin fails instead of dialing a default path
// that may belong to something else.
type Client struct {
	Path    string
	Timeout time.Duration
}

// New returns a Client bound to the socket herdr launched the plugin with.
func New() Client {
	return Client{Path: os.Getenv("HERDR_SOCKET_PATH")}
}

// Available reports whether there is a socket path to dial at all, so a caller
// can hide an affordance it cannot deliver rather than offering one that always
// fails.
func (c Client) Available() bool { return c.Path != "" }

// Error is a structured failure from the server, as opposed to a transport
// failure reaching it.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("herdr api: %s: %s", e.Code, e.Message) }

// SendText writes text to a pane exactly as typed. A trailing newline is what
// submits it; SendText does not add one, so a caller can stage a command
// without running it.
func (c Client) SendText(paneID, text string) error {
	return c.call("pane.send_text", map[string]string{"pane_id": paneID, "text": text})
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

// call runs a request whose reply carries nothing worth reading.
func (c Client) call(method string, params any) error {
	_, err := c.callResult(method, params)
	return err
}

// callResult runs one request on its own connection and hands back the raw
// result. One connection per call keeps replies unambiguous without tracking
// outstanding ids, which is the right trade for a handful of calls rather than
// a streaming subscription.
func (c Client) callResult(method string, params any) (json.RawMessage, error) {
	if c.Path == "" {
		return nil, errors.New("herdr api: no socket path (HERDR_SOCKET_PATH unset)")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	conn, err := net.DialTimeout("unix", c.Path, timeout)
	if err != nil {
		return nil, fmt.Errorf("herdr api: dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	// One deadline for the whole exchange, set before the write: a server that
	// accepts and never reads would otherwise block here rather than at the read.
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("herdr api: deadline: %w", err)
	}

	id, err := requestID()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, fmt.Errorf("herdr api: encode %s: %w", method, err)
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return nil, fmt.Errorf("herdr api: write %s: %w", method, err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("herdr api: read %s: %w", method, err)
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("herdr api: decode %s: %w", method, err)
	}
	if resp.ID != id {
		return nil, fmt.Errorf("herdr api: %s: reply id %q does not match request %q", method, resp.ID, id)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

func requestID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("herdr api: request id: %w", err)
	}
	return "picker-" + hex.EncodeToString(b[:]), nil
}
