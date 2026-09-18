package herdrapi

import (
	"encoding/json"
	"fmt"
)

// Machine is one saved SSH machine profile. Machines are client-side connection
// profiles, not part of the socket snapshot, and `machine list --json` prints a
// bare array rather than the usual result envelope.
type Machine struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Target   string `json:"target"`
	Session  string `json:"session"`
	Enabled  bool   `json:"enabled"`
	Selected bool   `json:"selected"`
}

// MachineList returns the saved SSH machines. An empty catalog is an empty
// slice and no error.
func (c Client) MachineList() ([]Machine, error) {
	out, err := c.call("machine", "list", "--json")
	if err != nil {
		return nil, err
	}
	var machines []Machine
	if err := json.Unmarshal(out, &machines); err != nil {
		return nil, fmt.Errorf("herdr machine list: decode: %w", err)
	}
	return machines, nil
}
