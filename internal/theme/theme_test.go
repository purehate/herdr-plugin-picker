package theme

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// mustLoad loads a config expected to parse cleanly, asserting the nil error.
func mustLoad(t *testing.T, path string) Theme {
	t.Helper()
	got, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(%s): %v", path, err)
	}
	return got
}

// An absent theme config is normal, not an error.
func TestLoadMissingFileYieldsDefault(t *testing.T) {
	if got := mustLoad(t, filepath.Join(t.TempDir(), "absent.toml")); got != Default() {
		t.Fatalf("LoadFile = %+v, want %+v", got, Default())
	}
}

// TestLoadTerminalThemeUsesANSI pins the spec: the `terminal` theme maps to
// ANSI 16, so the picker inherits the terminal's own palette instead of
// imposing a dark-background hex guess.
func TestLoadTerminalThemeUsesANSI(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"terminal\"\n")
	got := mustLoad(t, path)
	want := Theme{Accent: "4", Text: "", Muted: "8", Up: "2"}
	if got != want {
		t.Fatalf("LoadFile = %+v, want %+v", got, want)
	}
	if strings.HasPrefix(got.Accent, "#") {
		t.Errorf("Accent = %q, want an ANSI index rather than a hex color", got.Accent)
	}
}

// TestLoadAccentOverride is this machine's real configuration: the terminal
// theme with a bright-green accent override.
func TestLoadAccentOverride(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"terminal\"\n\n[ui]\naccent = \"#14e21a\"\n")
	got := mustLoad(t, path)
	if got.Accent != "#14e21a" {
		t.Errorf("Accent = %q, want #14e21a", got.Accent)
	}
	if got.Text != "" {
		t.Errorf("Text = %q, want an empty string so the terminal foreground is inherited", got.Text)
	}
}

func TestLoadUnknownThemeYieldsDefault(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"no-such-theme\"\n")
	if got := mustLoad(t, path); got != Default() {
		t.Fatalf("LoadFile = %+v, want %+v", got, Default())
	}
}

func TestLoadThemeNameAccent(t *testing.T) {
	path := writeConfig(t, "[theme]\nname = \"tokyonight\"\n")
	if got := mustLoad(t, path).Accent; got != "#7aa2f7" {
		t.Fatalf("Accent = %q, want the tokyonight accent", got)
	}
}

// TestLoadIgnoresMalformedTOML pins that a parse failure discards everything
// go-toml applied before it. The valid `name = "terminal"` above the syntax
// error is deliberate: go-toml applies keys as it goes, so without the error
// guard LoadFile would return terminalTheme() instead of Default(). A fixture that
// breaks on line 1 cannot tell those two apart — do not "tidy" this one.
func TestLoadIgnoresMalformedTOML(t *testing.T) {
	broken := writeConfig(t, "[theme]\nname = \"terminal\"\n[ui\naccent =\n")
	got, err := LoadFile(broken)
	if got != Default() {
		t.Errorf("LoadFile on malformed TOML = %+v, want %+v", got, Default())
	}
	// Always usable, but the operator still gets told — silently rendering a
	// near-white default on a light terminal with no hint is the failure mode.
	if err == nil {
		t.Error("err = nil, want the parse failure reported")
	}
	var decodeErr *toml.DecodeError
	if !errors.As(err, &decodeErr) {
		t.Errorf("err = %v, want it to unwrap to *toml.DecodeError", err)
	}
}

// An unreadable theme config is reported too, and still renders.
func TestLoadUnreadableFileReportsAndStillRenders(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	path := writeConfig(t, "[theme]\nname = \"tokyonight\"\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	got, err := LoadFile(path)
	if got != Default() {
		t.Errorf("LoadFile = %+v, want %+v", got, Default())
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("err = %v, want it to unwrap to *fs.PathError", err)
	}
}

// TestLoadAccentValidation pins both halves of the hex matcher: the 6-digit
// length and the ^…$ anchoring. An accent that fails validation leaves the
// theme's own accent in place.
func TestLoadAccentValidation(t *testing.T) {
	rejected := []struct{ name, accent string }{
		{"no leading hash", "not-a-color"},
		{"too short", "#abc"},
		{"right length but not hex", "#gggggg"},
		{"valid hex with a prefix", "xx#14e21a"},
		{"valid hex with a suffix", "#14e21axx"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, "[ui]\naccent = \""+tc.accent+"\"\n")
			if got := mustLoad(t, path).Accent; got != Default().Accent {
				t.Errorf("Accent = %q for %q, want the default %q", got, tc.accent, Default().Accent)
			}
		})
	}
	t.Run("uppercase hex is accepted", func(t *testing.T) {
		path := writeConfig(t, "[ui]\naccent = \"#14E21A\"\n")
		if got := mustLoad(t, path).Accent; got != "#14E21A" {
			t.Errorf("Accent = %q, want #14E21A", got)
		}
	})
}
