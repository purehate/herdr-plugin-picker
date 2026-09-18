package picker

import (
	"strings"
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

func TestSlashTogglesRegexMode(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m = navKey(m, '/', "/", 0)
	if !m.regex || m.queryPrompt() != ".* " {
		t.Fatalf("/ = regex %v prompt %q, want regex mode", m.regex, m.queryPrompt())
	}
	m = navKey(m, '/', "/", 0)
	if m.regex || m.queryPrompt() != "/ " {
		t.Fatalf("second / = regex %v prompt %q, want fuzzy mode", m.regex, m.queryPrompt())
	}
}

// Regex mode is a filter, not a ranker: the pattern alone narrows, and the same
// string as a fuzzy query would treat the metacharacters literally.
func TestRegexFiltersInsteadOfRanking(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.regex = true
	m.query = "^n"
	m = m.refilter()
	if len(m.items) != 1 || m.items[0].ID != "w2" {
		t.Fatalf("^n matched %v, want only w2", m.items)
	}

	fuzzy := newNavigatorModel(navFixture())
	fuzzy.query = "^n"
	fuzzy = fuzzy.refilter()
	if len(fuzzy.items) != 0 {
		t.Fatalf("fuzzy ^n matched %v, want nothing", fuzzy.items)
	}
}

func TestRegexIsCaseInsensitive(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.regex = true
	m.query = "PROJECT"
	m = m.refilter()
	if len(m.items) != 1 || m.items[0].ID != "w1" {
		t.Fatalf("PROJECT matched %v, want w1", m.items)
	}
}

// A bad pattern empties the list, so the message has to say why rather than
// reading as a plain "no match".
func TestBadRegexIsReportedNotSilent(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.regex = true
	m.query = "["
	m = m.refilter()
	if m.regexErr == nil {
		t.Fatal("an unclosed class did not error")
	}
	if len(m.items) != 0 {
		t.Fatalf("bad regex kept %v", m.items)
	}
	if got := m.emptyMessage(); !strings.Contains(got, "bad regex") {
		t.Fatalf("empty message = %q, want the bad-regex report", got)
	}
}

// A live refresh must not silently drop the mode back to fuzzy.
func TestRegexSurvivesLiveRefresh(t *testing.T) {
	m := newNavigatorModel(navFixture())
	m.regex = true
	m.query = "^n"
	m = m.refilter()
	m = m.applyRefresh(NavRefresh{Spaces: []NavItem{
		{ID: "w2", Label: "nixos-dev"},
		{ID: "w3", Label: "other"},
	}})
	if !m.regex || len(m.items) != 1 || m.items[0].ID != "w2" {
		t.Fatalf("after refresh = regex %v items %v", m.regex, m.items)
	}
}

func TestSSHRegexMatchesAliasAndHostname(t *testing.T) {
	o := navFixture()
	o.Hosts = []sshconfig.Host{
		{Alias: "web1", HostName: "10.0.0.1"},
		{Alias: "db1", HostName: "db.internal"},
	}
	m := newNavigatorModel(o).setSection(NavSSH)
	m.regex = true
	m.query = "internal$"
	m = m.refilter()
	if len(m.items) != 1 || m.items[0].ID != "db1" {
		t.Fatalf("internal$ matched %v, want db1", m.items)
	}
}
