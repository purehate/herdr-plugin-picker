package herdrapi

import (
	"reflect"
	"testing"
)

// machine list --json is a bare array, not the {"result": ...} envelope every
// other call returns, so it cannot go through callJSON.
func TestMachineListParsesBareArray(t *testing.T) {
	var calls [][]string
	run := func(args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(`[{"id":"m1","label":"Build","target":"workbox","session":"agents","enabled":true,"selected":false}]`), nil
	}
	machines, err := (Client{Run: run}).MachineList()
	if err != nil {
		t.Fatal(err)
	}
	assertArgv(t, calls, []string{"machine list --json"})
	want := []Machine{{ID: "m1", Label: "Build", Target: "workbox", Session: "agents", Enabled: true}}
	if !reflect.DeepEqual(machines, want) {
		t.Fatalf("machines = %+v, want %+v", machines, want)
	}
}

func TestMachineListEmptyAndMalformed(t *testing.T) {
	empty := func([]string) ([]byte, error) { return []byte(`[]`), nil }
	if got, err := (Client{Run: empty}).MachineList(); err != nil || len(got) != 0 {
		t.Fatalf("empty catalog = (%v, %v)", got, err)
	}
	// The shared fixture answers every call with a result envelope. Decoding it
	// as a machine array must fail rather than silently yielding an empty tab.
	envelope := func([]string) ([]byte, error) { return []byte(`{"result":{"panes":[]}}`), nil }
	if _, err := (Client{Run: envelope}).MachineList(); err == nil {
		t.Fatal("a result envelope decoded as a machine array")
	}
}
