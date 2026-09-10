// Package pluginconfig loads the plugin's own config.toml from
// $HERDR_PLUGIN_CONFIG_DIR.
package pluginconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// ErrInvalid reports a config file that exists but cannot be used.
var ErrInvalid = errors.New("invalid plugin config")

const (
	defaultProbeTimeoutMS = 300
	// defaultSplitDirection and splitDirectionDown are the only two accepted
	// values for split_direction.
	defaultSplitDirection = "right"
	splitDirectionDown    = "down"
)

// Config is the operator-facing plugin configuration. Every key is optional.
type Config struct {
	Probe          bool     `toml:"probe"`
	ProbeTimeoutMS int      `toml:"probe_timeout_ms"`
	SplitDirection string   `toml:"split_direction"`
	ShowPreview    bool     `toml:"show_preview"`
	ReusePanes     bool     `toml:"reuse_panes"`
	Hidden         []string `toml:"hidden"`
	SSHArgs        []string `toml:"ssh_args"`
}

// Defaults returns the configuration used when no file is present.
func Defaults() Config {
	return Config{
		Probe:          true,
		ProbeTimeoutMS: defaultProbeTimeoutMS,
		SplitDirection: defaultSplitDirection,
		ShowPreview:    true,
		ReusePanes:     true,
	}
}

// unusable is the safe fallback for a config file that exists but cannot be
// trusted. Probe fails closed: probing sends one SYN per host in the operator's
// ssh config, and an unusable config means we cannot tell whether they opted
// out. Losing the up/down indicators is cheap; putting packets on a network the
// operator meant to leave alone is not.
func unusable() Config {
	cfg := Defaults()
	cfg.Probe = false
	return cfg
}

// LoadDir reads dir/config.toml over the defaults. A missing file is not an error.
//
// LoadDir ALWAYS returns a usable Config. When the file parses, every key the
// operator got right is kept and each invalid key falls back to its own default.
// When the file exists but cannot be read or parsed at all, nothing is kept and
// Probe is forced off — see unusable. A non-nil error means at least one
// key was rejected. Callers must report that error and then use the returned
// Config — NOT discard it for Defaults(). Discarding it would undo an operator's
// valid `probe = false` because of an unrelated typo elsewhere in the same file,
// sending scan traffic the config asked it not to.
func LoadDir(dir string) (Config, error) {
	cfg := Defaults()
	// Deliberately fails OPEN (Probe stays true) where an unreadable file fails
	// closed. The difference is evidence of operator intent. An empty dir means
	// no config location was supplied at all — the env var is unset, so we are
	// running outside herdr — and a location we were never given tells us
	// nothing about what the operator wants; the documented defaults are the
	// honest answer. A file that exists but will not parse is the opposite:
	// the operator wrote something we failed to read, and it may well have been
	// `probe = false`. Only that second case earns the traffic opt-out.
	if dir == "" {
		return cfg, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return unusable(), fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		// go-toml applies keys as it parses and stops at the syntax error, so
		// cfg now holds whatever happened to sit above it. That truncation
		// point is arbitrary — `probe = false` above the bad line lands, the
		// identical line below it does not — so keeping the partial result
		// would make behavior depend on line order. Discard it.
		return unusable(), fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	// Check every key and reset each offender individually. Returning on the
	// first bad key would hand back a Config still carrying the second one's
	// invalid value. errors.Join of an empty slice is nil, so the happy path
	// stays error-free, and errors.Is still matches ErrInvalid through the join.
	var errs []error
	if cfg.SplitDirection != defaultSplitDirection && cfg.SplitDirection != splitDirectionDown {
		errs = append(errs, fmt.Errorf("%w: split_direction must be %q or %q, got %q", ErrInvalid, defaultSplitDirection, splitDirectionDown, cfg.SplitDirection))
		cfg.SplitDirection = defaultSplitDirection
	}
	if cfg.ProbeTimeoutMS <= 0 {
		errs = append(errs, fmt.Errorf("%w: probe_timeout_ms must be positive, got %d", ErrInvalid, cfg.ProbeTimeoutMS))
		cfg.ProbeTimeoutMS = defaultProbeTimeoutMS
	}
	return cfg, errors.Join(errs...)
}
