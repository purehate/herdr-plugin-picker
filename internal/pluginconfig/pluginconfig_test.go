package pluginconfig

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := LoadDir(t.TempDir())
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if !reflect.DeepEqual(cfg, Defaults()) {
		t.Fatalf("cfg = %+v, want %+v", cfg, Defaults())
	}
}

func TestLoadEmptyDirYieldsDefaults(t *testing.T) {
	cfg, err := LoadDir("")
	if err != nil || !reflect.DeepEqual(cfg, Defaults()) {
		t.Fatalf("LoadDir(\"\") = (%+v, %v)", cfg, err)
	}
}

func TestLoadPartialOverride(t *testing.T) {
	dir := writeConfig(t, "probe = false\nsplit_direction = \"down\"\nhidden = [\"colima\"]\n")
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false")
	}
	if cfg.SplitDirection != "down" {
		t.Errorf("SplitDirection = %q, want down", cfg.SplitDirection)
	}
	if len(cfg.Hidden) != 1 || cfg.Hidden[0] != "colima" {
		t.Errorf("Hidden = %v", cfg.Hidden)
	}
	// Untouched keys keep their defaults.
	if cfg.ProbeTimeoutMS != 300 || !cfg.ReusePanes || !cfg.ShowPreview {
		t.Errorf("defaults not preserved: %+v", cfg)
	}
}

// TestLoadKeepsValidKeysWhenAnotherIsInvalid pins the rule that one bad key must
// not discard the keys the operator got right. Losing `probe = false` here would
// make the picker send scan traffic the config explicitly asked it not to send.
func TestLoadKeepsValidKeysWhenAnotherIsInvalid(t *testing.T) {
	dir := writeConfig(t, "probe = false\nhidden = [\"colima\"]\nprobe_timeout_ms = 0\nsplit_direction = \"sideways\"\n")
	cfg, err := LoadDir(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want the operator's false to survive an unrelated bad key")
	}
	if !reflect.DeepEqual(cfg.Hidden, []string{"colima"}) {
		t.Errorf("Hidden = %v, want [colima]", cfg.Hidden)
	}
	// Each offender falls back to its own default, independently.
	if cfg.ProbeTimeoutMS != 300 {
		t.Errorf("ProbeTimeoutMS = %d, want the 300 default", cfg.ProbeTimeoutMS)
	}
	if cfg.SplitDirection != "right" {
		t.Errorf("SplitDirection = %q, want the right default", cfg.SplitDirection)
	}
	// Both offenders must be reported, not just the first one found.
	for _, want := range []string{"probe_timeout_ms", "split_direction"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to mention %s", err, want)
		}
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	dir := writeConfig(t, "split_direction = \"sideways\"\n")
	cfg, err := LoadDir(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	// This proves the bad value did not survive; it does not prove an active
	// reset. Defaults() is already "right", so reset-to-default and
	// never-applied are indistinguishable here — the pre-state and the
	// post-reset state are the same string. The reset itself is pinned by
	// TestLoadKeepsValidKeysWhenAnotherIsInvalid, where a valid key above the
	// bad line lands and the bad one does not.
	if cfg.SplitDirection != "right" {
		t.Errorf("SplitDirection = %q, want the bad value rejected", cfg.SplitDirection)
	}
	broken := writeConfig(t, "probe = yes-please\n")
	if _, err := LoadDir(broken); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid for malformed TOML", err)
	}
}

// A config file that exists but cannot be parsed leaves us unable to tell
// whether the operator opted out of probing, so probe fails closed.
//
// The line order is deliberate and must not be "tidied": go-toml applies keys
// as it parses and stops at the syntax error, so `probe_timeout_ms = 250` lands
// and `probe = false` never does. That is what makes this test discriminating —
// it fails if LoadDir returns the partial parse instead of unusable(). Putting the
// opt-out above the error would let the partial parse satisfy it by accident.
func TestLoadMalformedTOMLFailsClosedOnProbe(t *testing.T) {
	cfg, err := LoadDir(writeConfig(t, "probe_timeout_ms = 250\nthis line is not toml\nprobe = false\n"))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false — an unusable config must not send scan traffic")
	}
	// Proves the partial parse was discarded rather than kept: 250 was applied
	// before the syntax error, so surviving here would mean unusable() never ran.
	if cfg.ProbeTimeoutMS != 300 {
		t.Errorf("ProbeTimeoutMS = %d, want the 300 default, not the partially parsed 250", cfg.ProbeTimeoutMS)
	}
	if cfg.SplitDirection != "right" {
		t.Errorf("SplitDirection = %q, want the right default", cfg.SplitDirection)
	}
}

// go-toml applies keys as it parses and stops at the syntax error, so an opt-out
// below the bad line never lands. Failing closed must not depend on line order.
func TestLoadMalformedTOMLProbeIsOrderIndependent(t *testing.T) {
	cfg, err := LoadDir(writeConfig(t, "this is not toml\nprobe = false\n"))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false regardless of where the syntax error sits")
	}
}

// Unknown keys are configuration errors, not comments. In particular, silently
// accepting `proeb = false` applies the default `probe = true` and sends the
// exact network traffic the operator tried to disable.
func TestLoadUnknownKeyFailsClosedOnProbe(t *testing.T) {
	cfg, err := LoadDir(writeConfig(t,
		"probe_timeout_ms = 250\nproeb = false\nssh_args = [\"-v\"]\n"))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	var strictErr *toml.StrictMissingError
	if !errors.As(err, &strictErr) {
		t.Fatalf("err = %v, want it to retain *toml.StrictMissingError", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false — a misspelled opt-out must fail closed")
	}
	if cfg.ProbeTimeoutMS != 300 || cfg.SSHArgs != nil {
		t.Errorf("cfg = %+v, want partial strict decode discarded", cfg)
	}
}

func TestLoadUnreadableFileFailsClosedOnProbe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("probe = false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore before TempDir teardown, which cannot remove an unreadable file.
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	cfg, err := LoadDir(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if cfg.Probe {
		t.Error("Probe = true, want false — an unreadable config must not send scan traffic")
	}
}

// TestLoadProbeOpenClosedAsymmetry pins a deliberate inconsistency: no config
// location supplied leaves probing ON, while a config that exists but cannot be
// read turns it OFF. The difference is evidence of operator intent — a location
// we were never given says nothing about what they want, whereas an unreadable
// file is something they wrote and we failed to parse. Do not "make these
// consistent" without re-deciding the traffic question.
func TestLoadProbeOpenClosedAsymmetry(t *testing.T) {
	t.Run("no config location supplied keeps probing on", func(t *testing.T) {
		cfg, err := LoadDir("")
		if err != nil {
			t.Fatalf("LoadDir: %v", err)
		}
		if !cfg.Probe {
			t.Error("Probe = false, want true — defaults apply when no location is given")
		}
	})

	t.Run("unreadable config turns probing off", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses file permissions")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte("probe = true\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

		cfg, err := LoadDir(dir)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		if cfg.Probe {
			t.Error("Probe = true, want false — an unreadable config must not send scan traffic")
		}
	})
}

// ErrInvalid is the caller-facing contract, but the underlying error has to
// survive alongside it: a *toml.DecodeError carries the line number the
// operator needs to hand-edit the file, and a *fs.PathError is what separates
// "fix the permissions" from "fix the syntax".
func TestLoadWrapsTheUnderlyingError(t *testing.T) {
	t.Run("malformed TOML keeps *toml.DecodeError", func(t *testing.T) {
		_, err := LoadDir(writeConfig(t, "probe = yes-please\n"))
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		var decodeErr *toml.DecodeError
		if !errors.As(err, &decodeErr) {
			t.Fatalf("err = %v, want it to unwrap to *toml.DecodeError", err)
		}
		if row, col := decodeErr.Position(); row < 1 || col < 1 {
			t.Errorf("Position() = (%d, %d), want a real 1-based location", row, col)
		}
	})

	t.Run("unreadable file keeps *fs.PathError", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses file permissions")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte("probe = false\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

		_, err := LoadDir(dir)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		var pathErr *fs.PathError
		if !errors.As(err, &pathErr) {
			t.Fatalf("err = %v, want it to unwrap to *fs.PathError", err)
		}
	})
}

// A non-positive probe_timeout_ms can only come from the operator explicitly
// typing one, so it is an error rather than a silent clamp: nothing is swallowed.
func TestLoadRejectsNonPositiveProbeTimeout(t *testing.T) {
	for _, body := range []string{"probe_timeout_ms = 0\n", "probe_timeout_ms = -1\n"} {
		cfg, err := LoadDir(writeConfig(t, body))
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("LoadDir(%q) err = %v, want ErrInvalid", body, err)
		}
		if !reflect.DeepEqual(cfg, Defaults()) {
			t.Errorf("LoadDir(%q) cfg = %+v, want %+v", body, cfg, Defaults())
		}
	}
}

// An absent probe_timeout_ms keeps the 300 default. This is the case the error
// branch above must not catch.
func TestLoadAbsentProbeTimeoutKeepsDefault(t *testing.T) {
	cfg, err := LoadDir(writeConfig(t, "probe = false\n"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if cfg.ProbeTimeoutMS != 300 {
		t.Errorf("ProbeTimeoutMS = %d, want the 300 default", cfg.ProbeTimeoutMS)
	}
}

func TestValidateSSHArgsAllowsOptionsThatDoNotChangeThePreview(t *testing.T) {
	cases := [][]string{
		nil,
		{"-v"},
		{"-vvv", "-A", "-C"},
		{"-o", "ConnectTimeout=5", "-oServerAliveInterval=30"},
		{"-L", "8080:127.0.0.1:80", "-R2222:127.0.0.1:22"},
		{"--"},
	}
	for _, args := range cases {
		if err := validateSSHArgs(args); err != nil {
			t.Errorf("validateSSHArgs(%q) = %v, want nil", args, err)
		}
	}
}

func TestValidateSSHArgsRejectsOptionsThatCanChangeThePreview(t *testing.T) {
	cases := [][]string{
		{"-F", "/tmp/other-config"},
		{"-F/tmp/other-config"},
		{"-p2222"},
		{"-J", "bastion"},
		{"-o", "HostName=elsewhere.invalid"},
		{"-oProxyCommand=nc somewhere 22"},
		{"-o", "CanonicalizeHostname=yes"},
		{"other-host"},
		{"--", "other-host"},
	}
	for _, args := range cases {
		if err := validateSSHArgs(args); err == nil {
			t.Errorf("validateSSHArgs(%q) = nil, want rejection", args)
		}
	}
}

func TestValidateSSHArgsRejectsOptionsThatRunLocalCommands(t *testing.T) {
	// These do not change which host is reached, so the preview stays honest —
	// but they execute a command on this machine, which an option list
	// advertised as ordinary client flags should not quietly permit.
	cases := [][]string{
		{"-o", "LocalCommand=rm -rf /"},
		{"-oLocalCommand=rm -rf /"},
		{"-o", "PermitLocalCommand=yes"},
		{"-oKnownHostsCommand=/tmp/x"},
	}
	for _, args := range cases {
		if err := validateSSHArgs(args); err == nil {
			t.Errorf("validateSSHArgs(%q) = nil, want rejection", args)
		}
	}
}

func TestLoadRejectsOnlyUnsafeSSHArgsKey(t *testing.T) {
	cfg, err := LoadDir(writeConfig(t,
		"probe = false\nhidden = [\"old-*\"]\nssh_args = [\"-F\", \"/tmp/other-config\"]\n"))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "ssh_args") {
		t.Fatalf("err = %v, want ErrInvalid naming ssh_args", err)
	}
	if cfg.Probe {
		t.Error("valid probe = false was discarded with the rejected ssh_args")
	}
	if !reflect.DeepEqual(cfg.Hidden, []string{"old-*"}) {
		t.Errorf("Hidden = %v, want valid hidden key preserved", cfg.Hidden)
	}
	if cfg.SSHArgs != nil {
		t.Errorf("SSHArgs = %v, want unsafe arguments reset", cfg.SSHArgs)
	}
}
