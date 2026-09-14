package sshusage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

var testNow = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func hosts(aliases ...string) []sshconfig.Host {
	out := make([]sshconfig.Host, len(aliases))
	for i, a := range aliases {
		out[i] = sshconfig.Host{Alias: a, HostName: a + ".example", Port: "22"}
	}
	return out
}

func aliases(hs []sshconfig.Host) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.Alias)
	}
	return out
}

func TestScoreRewardsRecency(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    Usage
		want int
	}{
		{"never used", Usage{}, 0},
		{"count only", Usage{Count: 5, LastUsed: testNow.Add(-90 * 24 * time.Hour).Unix()}, 5},
		{"today", Usage{Count: 1, LastUsed: testNow.Add(-time.Hour).Unix()}, 4},
		{"this week", Usage{Count: 1, LastUsed: testNow.Add(-3 * 24 * time.Hour).Unix()}, 3},
		{"this month", Usage{Count: 1, LastUsed: testNow.Add(-10 * 24 * time.Hour).Unix()}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.u.Score(testNow); got != tc.want {
				t.Fatalf("Score = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestOrderPinsThenFrecencyThenConfig(t *testing.T) {
	usage := map[string]Usage{
		"b": {Count: 5, LastUsed: testNow.Unix()}, // 8
		"c": {Count: 2, LastUsed: testNow.Unix()}, // 5
	}
	got := aliases(Order(hosts("a", "b", "c", "d"), usage, []string{"d"}, testNow))
	want := []string{"d", "b", "c", "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Order = %v, want %v", got, want)
	}
}

func TestOrderKeepsPinListOrderAndIgnoresDuplicates(t *testing.T) {
	got := aliases(Order(hosts("a", "b", "c"), nil, []string{"c", "a", "c"}, testNow))
	want := []string{"c", "a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Order = %v, want %v", got, want)
	}
}

func TestOrderDoesNotMutateInput(t *testing.T) {
	in := hosts("a", "b")
	_ = Order(in, map[string]Usage{"b": {Count: 9}}, nil, testNow)
	if got := aliases(in); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("input mutated: %v", got)
	}
}

func TestRecordRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ssh-usage.json")
	later := testNow.Add(time.Hour)
	for _, rec := range []struct {
		alias string
		at    time.Time
	}{{"web1", testNow}, {"web1", later}, {"db", testNow}} {
		if err := Record(path, rec.alias, rec.at); err != nil {
			t.Fatalf("Record(%s): %v", rec.alias, err)
		}
	}
	usage := Load(path)
	if usage["web1"].Count != 2 || usage["web1"].LastUsed != later.Unix() {
		t.Fatalf("web1 = %+v", usage["web1"])
	}
	if usage["db"].Count != 1 {
		t.Fatalf("db = %+v", usage["db"])
	}
	// The temp file must be gone after the rename, not left beside the real one.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "ssh-usage.json" {
			t.Fatalf("leftover file %q after a write", e.Name())
		}
	}
}

func TestLoadMissingCorruptAndEmptyPath(t *testing.T) {
	if got := Load(filepath.Join(t.TempDir(), "absent.json")); len(got) != 0 {
		t.Fatalf("missing file = %v", got)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(bad); len(got) != 0 {
		t.Fatalf("corrupt file = %v", got)
	}
	if got := Load(""); len(got) != 0 {
		t.Fatalf("empty path = %v", got)
	}
}

func TestRecordWithoutPathOrAliasIsNoop(t *testing.T) {
	if err := Record("", "web1", testNow); err != nil {
		t.Fatal(err)
	}
	if err := Record(filepath.Join(t.TempDir(), "x.json"), "", testNow); err != nil {
		t.Fatal(err)
	}
}
