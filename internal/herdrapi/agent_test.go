package herdrapi

import (
	"errors"
	"testing"
)

func TestAgentReadArgv(t *testing.T) {
	run, calls := recorder()
	if _, err := (Client{Run: run}).AgentRead("w4:p1", "recent-unwrapped", 8); err != nil {
		t.Fatal(err)
	}
	assertArgv(t, *calls, []string{"agent read w4:p1 --source recent-unwrapped --lines 8"})
}

func TestAgentReadOmitsEmptyOptions(t *testing.T) {
	run, calls := recorder()
	if _, err := (Client{Run: run}).AgentRead("w4:p1", "", 0); err != nil {
		t.Fatal(err)
	}
	assertArgv(t, *calls, []string{"agent read w4:p1"})
}

func TestAgentPromptArgvAndResult(t *testing.T) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`{"id":"cli:agent:prompt","result":{"agent":{"pane_id":"w4:p1","agent_status":"working"},"type":"agent_prompted"}}`), nil
	}
	info, err := (Client{Run: run}).AgentPrompt("w4:p1", "do the thing")
	if err != nil {
		t.Fatal(err)
	}
	assertArgv(t, calls, []string{"agent prompt w4:p1 do the thing --wait"})
	if info.Status != "working" {
		t.Fatalf("status = %q, want working", info.Status)
	}
}

func TestAgentPromptSurfacesRejectionCode(t *testing.T) {
	run := func([]string) ([]byte, error) {
		return []byte(`{"error":{"code":"agent_blocked","message":"agent w4:p1 is blocked"},"id":"cli:agent:prompt"}`), errors.New("exit status 1")
	}
	_, err := (Client{Run: run}).AgentPrompt("w4:p1", "hi")
	var rejected *AgentPromptError
	if !errors.As(err, &rejected) || rejected.Code != "agent_blocked" {
		t.Fatalf("err = %v, want *AgentPromptError{Code: agent_blocked}", err)
	}
	if rejected.Error() != "agent w4:p1 is blocked" {
		t.Fatalf("message = %q", rejected.Error())
	}
}

// A failure that is not herdr's JSON envelope — a missing binary, a transport
// error — must stay a CLIError rather than being misread as a rejection.
func TestAgentPromptKeepsNonEnvelopeFailure(t *testing.T) {
	run := func([]string) ([]byte, error) {
		return []byte("herdr: command not found"), errors.New("exit status 127")
	}
	_, err := (Client{Run: run}).AgentPrompt("w4:p1", "hi")
	var rejected *AgentPromptError
	if errors.As(err, &rejected) {
		t.Fatalf("non-envelope failure parsed as a rejection: %v", err)
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("err = %v, want *CLIError", err)
	}
}
