// Package theme reads Herdr's active theme so the picker matches the rest of
// the workspace.
package theme

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

// Theme holds the only tokens the picker draws with. There is no Down token:
// a host that is off renders with Muted, which is the right visual for it —
// it should not shout.
type Theme struct {
	Accent string
	Text   string
	Muted  string
	Up     string
}

const (
	defaultAccent = "#89b4fa"
	defaultText   = "#edf1f3"
	defaultMuted  = "#7b8496"
	defaultUp     = "#2ecc71"
)

// A bare decimal index is a complete lipgloss color, not an unfinished hex
// value — do not "fix" these into "#..." literals. Verified against the
// installed lipgloss v2.0.6: Color("4") emits \x1b[34m, Color("2") emits
// \x1b[32m, and Color("8") emits \x1b[90m, even with the color profile forced
// to TrueColor. That is the point — the index selects a slot in the terminal's
// own palette, so it tracks whatever colors the operator configured instead of
// pinning an absolute RGB value the way a hex accent does.
const (
	ansiBlue        = "4"
	ansiGreen       = "2"
	ansiBrightBlack = "8"
)

// accentByTheme covers the themes Herdr ships. An unknown name keeps the
// default, which is always readable. `terminal` is absent on purpose: it
// resolves to ANSI 16 via terminalTheme rather than to a hex accent.
var accentByTheme = map[string]string{
	"catppuccin-mocha": "#89b4fa",
	"catppuccin-latte": "#1e66f5",
	"tokyonight":       "#7aa2f7",
	"dracula":          "#bd93f9",
	"nord":             "#88c0d0",
	"gruvbox":          "#d79921",
	"solarized":        "#268bd2",
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type herdrConfig struct {
	Theme struct {
		Name string `toml:"name"`
	} `toml:"theme"`
	UI struct {
		Accent string `toml:"accent"`
	} `toml:"ui"`
}

// Default is the theme used when Herdr's config is absent or unreadable.
func Default() Theme {
	return Theme{Accent: defaultAccent, Text: defaultText, Muted: defaultMuted, Up: defaultUp}
}

// terminalTheme is herdr's `terminal` theme: ANSI 16 indices rather than hex, so
// the picker inherits whatever palette the terminal is configured with instead of
// imposing a dark-background guess. An empty Text emits no escape at all, which
// leaves the terminal's own foreground in place on light and dark alike.
func terminalTheme() Theme {
	return Theme{Accent: ansiBlue, Text: "", Muted: ansiBrightBlack, Up: ansiGreen}
}

// LoadFile reads herdr's own config from configPath, a FILE path (contrast
// pluginconfig.LoadDir, which takes a directory).
//
// The returned Theme is ALWAYS usable — Default() on every failure, never a
// zero value — so a caller may ignore the error and still render. A picker that
// refuses to open because of a color lookup is worse than a picker with the
// wrong accent. The error exists only so a caller CAN surface the problem:
// silently falling back leaves the operator staring at a near-white default on
// a light terminal with no hint that their config failed to parse. An absent
// config is not an error; having no theme config is normal.
func LoadFile(configPath string) (Theme, error) {
	t := Default()
	if configPath == "" {
		return t, nil
	}
	raw, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, fmt.Errorf("theme config: %w", err)
	}
	var cfg herdrConfig
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return t, fmt.Errorf("theme config %s: %w", configPath, err)
	}
	// Pick the base token set first so an explicit [ui].accent still wins.
	if cfg.Theme.Name == "terminal" {
		t = terminalTheme()
	} else if accent, ok := accentByTheme[cfg.Theme.Name]; ok {
		t.Accent = accent
	}
	if hexColor.MatchString(cfg.UI.Accent) {
		t.Accent = cfg.UI.Accent
	}
	return t, nil
}
