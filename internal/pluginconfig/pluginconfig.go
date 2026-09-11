// Package pluginconfig loads the plugin's own config.toml from
// $HERDR_PLUGIN_CONFIG_DIR.
package pluginconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// LoadDir ALWAYS returns a usable Config. When the file decodes with only known
// keys, every key the operator got right is kept and each invalid value falls
// back to its own default. When the file cannot be read, is malformed, or names
// an unknown key, nothing is kept and Probe is forced off — see unusable. A
// non-nil error means at least one key was rejected. Callers must report that
// error and then use the returned Config — NOT discard it for Defaults(). Doing
// that would undo a valid `probe = false` because of an unrelated bad value,
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
	decoder := toml.NewDecoder(bytes.NewReader(raw)).DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		// go-toml applies keys as it parses and stops at the syntax error, so
		// cfg now holds whatever happened to sit above it. Strict decoding also
		// rejects unknown keys: without it, a misspelled `proeb = false` is
		// silently ignored and probing stays on. In either case we cannot trust
		// the partial result, so discard it and fail probing closed.
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
	if err := validateSSHArgs(cfg.SSHArgs); err != nil {
		errs = append(errs, fmt.Errorf("%w: %w", ErrInvalid, err))
		cfg.SSHArgs = nil
	}
	return cfg, errors.Join(errs...)
}

// validateSSHArgs accepts SSH client options while rejecting anything that
// can make ssh resolve a different connection than the picker displayed and
// probed, or run a command on this machine. Values such as ConnectTimeout
// remain available through -o; routing, identity and address-selection
// settings belong in ~/.ssh/config so there is one source of truth for both
// paths.
func validateSSHArgs(args []string) error {
	const noValue = "AaCfGgKkMNnqsTtVvXxYy"
	const withValue = "cDEeILmOQRSWw"
	const changesPreview = "46BbFiJlPp"

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i != len(args)-1 {
				return fmt.Errorf("ssh_args may contain options only; %q follows --", args[i+1])
			}
			continue
		}
		if len(arg) < 2 || arg[0] != '-' {
			return fmt.Errorf("ssh_args may contain options only, got positional argument %q", arg)
		}

		for pos := 1; pos < len(arg); pos++ {
			option := arg[pos]
			switch {
			case strings.ContainsRune(changesPreview, rune(option)):
				return fmt.Errorf("ssh_args option -%c can change the displayed connection; put it in ~/.ssh/config", option)
			case strings.ContainsRune(noValue, rune(option)):
				continue
			case option == 'o' || strings.ContainsRune(withValue, rune(option)):
				value := arg[pos+1:]
				if value == "" {
					i++
					if i >= len(args) {
						return fmt.Errorf("ssh_args option -%c needs a value", option)
					}
					value = args[i]
				}
				if option == 'o' {
					if err := validateSSHOption(value); err != nil {
						return err
					}
				}
				// An option with a value consumes the rest of this argv element,
				// so none of its bytes are more short options to inspect.
				pos = len(arg)
			default:
				return fmt.Errorf("ssh_args contains unknown option -%c", option)
			}
		}
	}
	return nil
}

func validateSSHOption(value string) error {
	key := value
	if i := strings.IndexAny(key, "= \t"); i >= 0 {
		key = key[:i]
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || strings.HasPrefix(key, "-") {
		return fmt.Errorf("ssh_args -o needs a keyword=value option, got %q", value)
	}

	changesPreview := map[string]bool{
		"addressfamily":               true,
		"bindaddress":                 true,
		"bindinterface":               true,
		"canonicaldomains":            true,
		"canonicalizefallbacklocal":   true,
		"canonicalizehostname":        true,
		"canonicalizemaxdots":         true,
		"canonicalizepermittedcnames": true,
		"hostname":                    true,
		"identityfile":                true,
		"include":                     true,
		"port":                        true,
		"proxycommand":                true,
		"proxyjump":                   true,
		"tag":                         true,
		"user":                        true,
	}
	if changesPreview[key] {
		return fmt.Errorf("ssh_args -o %s can change the displayed connection; put it in ~/.ssh/config", key)
	}
	// Local command execution is not a preview mismatch, but it is the same
	// kind of surprise in a list advertised as ordinary client flags, and it is
	// the one that can do damage rather than merely mislead. ProxyCommand is
	// already rejected above as a routing option; these are its siblings.
	runsLocally := map[string]bool{
		"knownhostscommand":  true,
		"localcommand":       true,
		"permitlocalcommand": true,
	}
	if runsLocally[key] {
		return fmt.Errorf("ssh_args -o %s runs a local command; put it in ~/.ssh/config if you need it", key)
	}
	return nil
}
