package picker

import "github.com/purehate/herdr-plugin-picker/internal/sshconfig"

// Selection is the host a caller chose and how it wants it opened. It is the
// non-interactive counterpart to NavSelection: connect builds one directly, and
// the navigator builds one when the operator picks a row on the ssh tab.
type Selection struct {
	Host      sshconfig.Host
	Placement string // split | tab | zoomed
	ForceNew  bool
}
