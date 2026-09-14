package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/purehate/herdr-plugin-picker/internal/herdrapi"
	"github.com/purehate/herdr-plugin-picker/internal/picker"
	"github.com/purehate/herdr-plugin-picker/internal/sshusage"
)

func TestResolvePluginStateDir(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "/tmp/state")
	if got := resolvePluginStateDir(); got != "/tmp/state" {
		t.Fatalf("env override = %q", got)
	}

	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	want := filepath.Join("/tmp/xdg", "herdr", "plugins", pluginID)
	if got := resolvePluginStateDir(); got != want {
		t.Fatalf("xdg fallback = %q, want %q", got, want)
	}

	// With XDG_STATE_HOME unset, the fallback is ~/.local/state.
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	want = filepath.Join(home, ".local", "state", "herdr", "plugins", pluginID)
	if got := resolvePluginStateDir(); got != want {
		t.Fatalf("home fallback = %q, want %q", got, want)
	}
}

func TestRecordHostUseWritesState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	var out strings.Builder
	recordHostUse(&out, "web1")
	if out.String() != "" {
		t.Fatalf("unexpected diagnostic: %s", out.String())
	}
	usage := sshusage.Load(filepath.Join(dir, "ssh-usage.json"))
	if usage["web1"].Count != 1 || usage["web1"].LastUsed == 0 {
		t.Fatalf("usage = %+v", usage)
	}
}

// TestRunNavigatorOrdersHosts pins the ordering end to end: a pinned host first,
// then the one with the most use, then the rest in config order.
func TestRunNavigatorOrdersHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_CONFIG_PATH", t.TempDir()+"/absent.toml")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", pluginConfigDir(t, "reuse_panes = false\nprobe = false\npinned = [\"c\"]\n"))

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "Host a\n  HostName 10.0.0.1\nHost b\n  HostName 10.0.0.2\nHost c\n  HostName 10.0.0.3\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	if err := sshusage.Record(filepath.Join(stateDir, "ssh-usage.json"), "b", time.Now()); err != nil {
		t.Fatal(err)
	}

	api := herdrapi.Client{Run: func(args []string) ([]byte, error) {
		if reflect.DeepEqual(args, []string{"api", "snapshot"}) {
			return []byte(`{"result":{"snapshot":{}}}`), nil
		}
		return nil, nil
	}}
	pick := func(o picker.NavOptions) (picker.NavSelection, bool, error) {
		var got []string
		for _, h := range o.Hosts {
			got = append(got, h.Alias)
		}
		if want := []string{"c", "b", "a"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("host order = %v, want %v", got, want)
		}
		return picker.NavSelection{}, false, nil
	}
	if err := runNavigatorWith(io.Discard, strings.NewReader(""), pick, api); err != nil {
		t.Fatal(err)
	}
}
