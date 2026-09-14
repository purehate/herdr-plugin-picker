package herdrapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// agent.go is the two agent calls that reach past metadata: reading a pane's
// terminal output, and submitting a prompt. Everything else in this package
// only lists or focuses.

// AgentRead returns the tail of an agent pane's terminal output. source selects
// which snapshot herdr renders (visible, recent, recent-unwrapped, detection);
// lines caps how many lines come back. The result is the terminal text as-is —
// callers sanitize it before drawing.
func (c Client) AgentRead(paneID, source string, lines int) (string, error) {
	args := []string{"agent", "read", paneID}
	if source != "" {
		args = append(args, "--source", source)
	}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	out, err := c.call(args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// AgentPrompt submits text to an agent and waits for its next settled state.
// herdr rejects a prompt to an already-blocked agent before sending any input;
// that rejection arrives as *AgentPromptError with Code "agent_blocked", so a
// caller can say what happened instead of dumping a JSON envelope.
func (c Client) AgentPrompt(paneID, text string) (AgentInfo, error) {
	var result struct {
		Agent AgentInfo `json:"agent"`
	}
	if err := c.callJSON(&result, "agent", "prompt", paneID, text, "--wait"); err != nil {
		return AgentInfo{}, agentPromptError(err)
	}
	return result.Agent, nil
}

// AgentPromptError is herdr's rejection of a prompt, with the machine-readable
// code preserved. Callers key off Code rather than matching the human message,
// which is also what keeps a blocked agent distinguishable from a missing one
// without parsing a sentence.
type AgentPromptError struct {
	Code    string
	Message string
}

func (e *AgentPromptError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("herdr rejected the prompt (%s)", e.Code)
}

// agentPromptError recovers the JSON error envelope from a failed call. Anything
// that is not a herdr rejection — a missing binary, a transport failure — is
// returned unchanged.
func agentPromptError(err error) error {
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		return err
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(cliErr.Output), &env) != nil || env.Error.Code == "" {
		return err
	}
	return &AgentPromptError{Code: env.Error.Code, Message: env.Error.Message}
}
