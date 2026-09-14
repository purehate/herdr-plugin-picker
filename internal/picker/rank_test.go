package picker

import (
	"testing"

	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

func aliases(matches []Match) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.Host.Alias)
	}
	return out
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Port is set on every host because sshconfig.Parse always resolves it,
// defaulting to "22". A fixture without it is a Host shape the parser cannot
// produce, and it makes the renderer emit a bare ":" that no real host shows.
var corpus = []sshconfig.Host{
	{Alias: "alpha", HostName: "10.0.0.1", Port: "22"},
	{Alias: "nixos-dev", HostName: "192.0.2.10", Port: "22"},
	{Alias: "devbox", HostName: "10.0.0.7", Port: "22"},
	{Alias: "prod-web", HostName: "dev.example.com", Port: "22"},
	{Alias: "dev", HostName: "10.0.0.9", Port: "22"},
}

func TestRankEmptyQueryPreservesOrder(t *testing.T) {
	got := aliases(Rank(corpus, ""))
	want := []string{"alpha", "nixos-dev", "devbox", "prod-web", "dev"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want config order %v", got, want)
	}
}

func TestRankOrdersExactThenPrefixThenSubstring(t *testing.T) {
	got := aliases(Rank(corpus, "dev"))
	want := []string{"dev", "devbox", "nixos-dev", "prod-web"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v", got, want)
	}
}

func TestRankIsCaseInsensitive(t *testing.T) {
	if got := aliases(Rank(corpus, "NIXOS")); len(got) == 0 || got[0] != "nixos-dev" {
		t.Fatalf("Rank = %v, want nixos-dev first", got)
	}
}

func TestRankMatchesScatteredCharacters(t *testing.T) {
	got := aliases(Rank(corpus, "nxd"))
	if len(got) != 1 || got[0] != "nixos-dev" {
		t.Fatalf("Rank = %v, want only nixos-dev", got)
	}
}

func TestRankDropsNonMatches(t *testing.T) {
	if got := Rank(corpus, "zzzz"); len(got) != 0 {
		t.Fatalf("Rank = %v, want empty", aliases(got))
	}
}

func TestRankDoesNotMutateInput(t *testing.T) {
	Rank(corpus, "dev")
	if corpus[0].Alias != "alpha" {
		t.Fatal("Rank reordered its input slice")
	}
}

func TestRankEmptyQueryHasNoPositions(t *testing.T) {
	// Nothing matched, so there is nothing to highlight. A renderer that sees
	// non-nil positions here would accent the whole unfiltered list.
	for _, m := range Rank(corpus, "") {
		if m.AliasPos != nil || m.HostNamePos != nil {
			t.Fatalf("%q: AliasPos = %v, HostNamePos = %v, want both nil for an empty query",
				m.Host.Alias, m.AliasPos, m.HostNamePos)
		}
	}
}

func TestRankExactPositionsCoverWholeAlias(t *testing.T) {
	got := Rank(corpus, "dev")
	if len(got) == 0 || got[0].Host.Alias != "dev" {
		t.Fatalf("Rank = %v, want dev first", aliases(got))
	}
	if want := []int{0, 1, 2}; !equalInts(got[0].AliasPos, want) {
		t.Errorf("AliasPos = %v, want %v", got[0].AliasPos, want)
	}
	if got[0].HostNamePos != nil {
		t.Errorf("HostNamePos = %v, want nil for an alias match", got[0].HostNamePos)
	}
}

func TestRankPrefixPositionsCoverQueryOnly(t *testing.T) {
	// devbox matches on its first three runes. The trailing "box" was not
	// typed, so it must not be highlighted.
	got := Rank(corpus, "dev")
	if len(got) < 2 || got[1].Host.Alias != "devbox" {
		t.Fatalf("Rank = %v, want devbox second", aliases(got))
	}
	if want := []int{0, 1, 2}; !equalInts(got[1].AliasPos, want) {
		t.Errorf("AliasPos = %v, want %v", got[1].AliasPos, want)
	}
}

func TestRankSubstringPositionsAreRuneIndicesNotBytes(t *testing.T) {
	// "héllo-dev" is nine runes but ten bytes, because é is two bytes.
	// strings.Index reports the match at byte 7; the renderer walks runes, so
	// the position must be 6.
	hosts := []sshconfig.Host{{Alias: "héllo-dev", HostName: "10.0.0.1"}}
	got := Rank(hosts, "dev")
	if len(got) != 1 {
		t.Fatalf("Rank returned %d matches, want 1", len(got))
	}
	if want := []int{6, 7, 8}; !equalInts(got[0].AliasPos, want) {
		t.Fatalf("AliasPos = %v, want %v (rune indices, not byte offsets)", got[0].AliasPos, want)
	}
}

func TestRankScatteredPositionsRecordEachMatchedRune(t *testing.T) {
	// nixos-dev: n(0) i x(2) o s - d(6) e v
	got := Rank(corpus, "nxd")
	if len(got) != 1 {
		t.Fatalf("Rank returned %d matches, want 1", len(got))
	}
	if want := []int{0, 2, 6}; !equalInts(got[0].AliasPos, want) {
		t.Fatalf("AliasPos = %v, want %v", got[0].AliasPos, want)
	}
}

func TestRankHostNameMatchSetsHostPositionsOnly(t *testing.T) {
	// Only prod-web matches, and it matches on HostName "dev.example.com" —
	// d(0) e v . e(4) x a m p l e(10). The alias contributed nothing, so
	// highlighting it would point the operator at the wrong column.
	got := Rank(corpus, "example")
	if len(got) != 1 || got[0].Host.Alias != "prod-web" {
		t.Fatalf("Rank = %v, want only prod-web", aliases(got))
	}
	if got[0].AliasPos != nil {
		t.Errorf("AliasPos = %v, want nil for a hostname-only match", got[0].AliasPos)
	}
	if want := []int{4, 5, 6, 7, 8, 9, 10}; !equalInts(got[0].HostNamePos, want) {
		t.Errorf("HostNamePos = %v, want %v", got[0].HostNamePos, want)
	}
}

func TestRankPrefersTheAliasWhenBothColumnsMatch(t *testing.T) {
	// rank.go:11-12 states a user-facing promise: "Alias matches always beat
	// hostname matches: the operator typed a name they chose, not an address
	// they were assigned." Every other test in this file matches one column at
	// a time, so the promise is only ever exercised where the alias is the sole
	// candidate — the half that cannot fail. A host matching BOTH columns is
	// the only shape that can tell alias-first from hostname-first apart, and
	// reordering the tier checks in scoreHost survived the whole suite without
	// one.
	//
	// The alias has to match by SUBSTRING, not exact or prefix. scoreHost
	// checks exact and prefix in a switch that returns before the substring and
	// scattered checks are reached, so an alias like "devbox" — which "dev"
	// prefixes — short-circuits ahead of the tier order under test and would
	// pin nothing. That is not hypothetical: it is why the first fixture tried
	// here showed no difference at all. "xdevy" contains "dev" without starting
	// with it, so it reaches the substring check.
	//
	// no-match is declared first so config order opposes the expected result.
	// If the hostname tiers are consulted first, both hosts land in
	// rankHostSubstring, and a tie there resolves to config order — putting
	// no-match ahead. Its alias holds no "d", so it cannot match "dev" under
	// any rule and can only be found through its hostname.
	hosts := []sshconfig.Host{
		{Alias: "no-match", HostName: "dev.internal", Port: "22"},
		{Alias: "xdevy", HostName: "dev.example.com", Port: "22"},
	}
	got := Rank(hosts, "dev")

	if want := []string{"xdevy", "no-match"}; !equal(aliases(got), want) {
		t.Fatalf("Rank = %v, want %v — an alias substring match must outrank a hostname substring match",
			aliases(got), want)
	}
	if want := []int{1, 2, 3}; !equalInts(got[0].AliasPos, want) {
		t.Errorf("AliasPos = %v, want %v — the alias is the column that matched", got[0].AliasPos, want)
	}
	if got[0].HostNamePos != nil {
		t.Errorf("HostNamePos = %v, want nil — highlighting the hostname would point the operator at the column that lost",
			got[0].HostNamePos)
	}
}

func TestRankBoundaryBonusOutranksConfigOrder(t *testing.T) {
	// Both hosts land in the scattered-alias tier for "nx": neither contains
	// the literal "nx". nixos-dev matches its n at rune 0 — a word start, and
	// the first matched rune, so 8 doubled to 16. banana-xray matches its n at
	// rune 2, mid-word and worth nothing, then its x after the hyphen for 8.
	// 16 beats 8, so the bonus has to pull nixos-dev ahead of the host declared
	// above it; config order alone would keep banana-xray first.
	hosts := []sshconfig.Host{
		{Alias: "banana-xray", HostName: "10.0.0.1"},
		{Alias: "nixos-dev", HostName: "10.0.0.2"},
	}
	got := aliases(Rank(hosts, "nx"))
	want := []string{"nixos-dev", "banana-xray"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v — the word-boundary bonus should outrank config order inside the scattered tier", got, want)
	}
}

func TestRankBoundaryBonusDoesNotReorderSubstringTier(t *testing.T) {
	// Both are alias-substring matches for "dev". The spec fixes ties at config
	// order, so the bonus must not fire outside the scattered tiers. The scores
	// are deliberately lopsided: mid-dev's match starts after a hyphen and
	// would score 24, xdevy's starts mid-word and would score 8. A bonus
	// leaking into this tier flips the order; config order keeps it.
	hosts := []sshconfig.Host{
		{Alias: "xdevy", HostName: "10.0.0.1"},
		{Alias: "mid-dev", HostName: "10.0.0.2"},
	}
	got := aliases(Rank(hosts, "dev"))
	want := []string{"xdevy", "mid-dev"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v — config order must survive inside the substring tier", got, want)
	}
}

func TestRankConsecutiveBonusOutranksConfigOrder(t *testing.T) {
	// bonusConsecutive had no observer at all: deleting the term outright left
	// the whole suite green. TestRankBoundaryBonusOutranksConfigOrder is the
	// only test that reaches positionScore, and its query "nx" against
	// nixos-dev/banana-xray produces no adjacent pair in either host, so the
	// consecutive arm never executed anywhere under test.
	//
	// Both hosts here land in the alias-scattered tier for "abcd": neither
	// contains the literal "abcd", so the substring check misses and the greedy
	// walk runs. No matched rune touches a word boundary — every one sits
	// mid-word with a non-separator before it — so bonusBoundary contributes
	// nothing to either side and the consecutive term is the only thing that
	// can separate them.
	//
	//	xabczd:  x a(1) b(2) c(3) z d(5)   -> pairs at 2 and 3, so 4+4 =  8
	//	xazbzcd: x a(1) z b(3) z c(5) d(6) -> one pair at 6,     so     4
	//
	// 8 beats 4, so the bonus has to pull xabczd ahead of the host declared
	// above it; zeroing the term flattens both to 0 and config order stands.
	//
	// This is deliberately also the fixture for the off-by-one. Comparing
	// pos[i-1] to p-2 rather than p-1 scores xabczd at 4 and xazbzcd at 8,
	// which inverts the expected order instead of flattening it — so one
	// fixture covers both mutations, and neither can be repaired by the other.
	hosts := []sshconfig.Host{
		{Alias: "xazbzcd", HostName: "10.0.0.1", Port: "22"},
		{Alias: "xabczd", HostName: "10.0.0.2", Port: "22"},
	}
	got := aliases(Rank(hosts, "abcd"))
	want := []string{"xabczd", "xazbzcd"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v — the consecutive-run bonus should outrank config order inside the scattered tier",
			got, want)
	}
}

func TestRankBoundaryBonusOutweighsConsecutiveRuns(t *testing.T) {
	// TestRankBoundaryBonusOutranksConfigOrder pins that bonusBoundary exists
	// and fires, which is enough to catch zeroing it. It is not enough to catch
	// halving it, and the magnitude is the behavior: the word-boundary tiebreak
	// was approved by name, and what was approved is that a match at a word
	// start beats a match that merely runs on — not that some non-zero number
	// is added.
	//
	//	axbxcxd: a(0) x b(2) x c(4) x d(6) -> boundary at the first matched
	//	                                      rune, so 8 doubled = 16
	//	xabczd:  x a(1) b(2) c(3) z d(5)   -> no boundary, two pairs =  8
	//
	// At 8 the boundary match wins 16 to 8. At 4 it scores 8 and merely ties,
	// and a tie resolves to config order — which is why xabczd is declared
	// first. So this fails for any bonusBoundary of 4 or less. It does not
	// constrain the constant from above, and deliberately not: 6 would still
	// order these correctly, and no behavior anyone approved distinguishes 6
	// from 8.
	hosts := []sshconfig.Host{
		{Alias: "xabczd", HostName: "10.0.0.1", Port: "22"},
		{Alias: "axbxcxd", HostName: "10.0.0.2", Port: "22"},
	}
	got := aliases(Rank(hosts, "abcd"))
	want := []string{"axbxcxd", "xabczd"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v — a word-boundary match must outweigh two consecutive runs", got, want)
	}
}

func TestRankTreatsADotAsAWordBoundary(t *testing.T) {
	// isSeparator's set is shared with deleteWord, and "-" is the only member
	// any test exercises — through the Ctrl-W tests in model_test.go, never
	// through positionScore. So the set's role in *scoring* had no observer,
	// and "." is the member worth pinning: hostnames are dotted, so a query
	// matching just after a dot is the common case this bonus exists to reward.
	// The other five members are left unpinned on purpose; they would buy
	// coverage of the set's identity rather than of any behavior.
	//
	// Both hosts must reach the host-scattered tier. The aliases hold no "d",
	// so they cannot match "dev" under any rule, and neither hostname contains
	// a contiguous "dev". Keeping them out of the substring tier is essential
	// rather than incidental: the bonus is never applied there, so a fixture
	// whose hostnames contain "dev" contiguously compares tier order and says
	// nothing about separators. The first fixture tried here made exactly that
	// mistake and showed no difference.
	//
	//	x.dxexv:  d(2) after ".", and it is the first matched rune -> 8*2 = 16
	//	xxdx-exv: e(5) after "-", not the first matched rune       ->       8
	//
	// Drop "." from the set and the first scores 0, so it falls behind the host
	// declared above it instead of climbing past it.
	hosts := []sshconfig.Host{
		{Alias: "no-dot", HostName: "xxdx-exv", Port: "22"},
		{Alias: "with-dot", HostName: "x.dxexv", Port: "22"},
	}
	got := aliases(Rank(hosts, "dev"))
	want := []string{"with-dot", "no-dot"}
	if !equal(got, want) {
		t.Fatalf("Rank = %v, want %v — a match just after a dot must earn the word-boundary bonus", got, want)
	}
}

func TestRankPositionsComeFromTheTierThatMatched(t *testing.T) {
	// "deav_dev" contains "dev" as a contiguous run at runes 5,6,7 — the
	// substring tier. But a greedy left-to-right scattered walk for the same
	// query lands on runes 0,1,3 instead (d@0, e@1, then the first available
	// v@3, skipping the 'a'). scoreHost must stop at whichever tier actually
	// matched — the substring check runs before the scattered one — and
	// return THAT tier's positions, never a separately recomputed scattered
	// reading. Every host in the shared corpus happens to produce identical
	// positions under both readings, so only a fixture shaped like this one
	// can tell substring-first apart from scattered-first.
	hosts := []sshconfig.Host{{Alias: "deav_dev", HostName: "10.0.0.1", Port: "22"}}
	got := Rank(hosts, "dev")
	if len(got) != 1 {
		t.Fatalf("Rank returned %d matches, want 1", len(got))
	}
	if want := []int{5, 6, 7}; !equalInts(got[0].AliasPos, want) {
		t.Fatalf("AliasPos = %v, want %v (the contiguous substring run, not the greedy scattered walk)", got[0].AliasPos, want)
	}
}

func TestRankExactlyOnePositionFieldIsSet(t *testing.T) {
	// rank.go documents that a Match carries positions from whichever field
	// produced it — AliasPos for an alias-tier match, HostNamePos for a
	// hostname-tier match — and never both. Other tests spot-check one field
	// per tier; this walks every tier instead and asserts the other field is
	// nil every time, not just in the cases that happened to get checked.
	//
	// "dev" against the shared corpus alone reaches four tiers at once: dev
	// is exact, devbox is alias-prefix, nixos-dev is alias-substring, and
	// prod-web matches only on HostName. "nxd" adds the alias-scattered tier
	// (nixos-dev again). Nothing in the corpus reaches host-scattered, so
	// hostScattered is a local fixture built just for that last tier: its
	// alias "qqq" cannot match "abc" by any rule, but its HostName
	// "ax-by-cz" matches "abc" only as a scattered subsequence (a@0, b@3,
	// c@6) — there is no contiguous "abc" in it.
	hostScattered := []sshconfig.Host{{Alias: "qqq", HostName: "ax-by-cz", Port: "22"}}

	cases := []struct {
		name  string
		hosts []sshconfig.Host
		query string
	}{
		{"alias tiers + host-substring", corpus, "dev"},
		{"alias-scattered", corpus, "nxd"},
		{"host-scattered", hostScattered, "abc"},
	}

	for _, c := range cases {
		matches := Rank(c.hosts, c.query)
		if len(matches) == 0 {
			t.Fatalf("%s: Rank returned no matches, want at least one", c.name)
		}
		for _, m := range matches {
			aliasSet, hostSet := m.AliasPos != nil, m.HostNamePos != nil
			if aliasSet == hostSet {
				t.Errorf("%s: %q: AliasPos = %v, HostNamePos = %v, want exactly one non-nil",
					c.name, m.Host.Alias, m.AliasPos, m.HostNamePos)
			}
		}
	}
}

func TestRankMatchesRegardlessOfHaystackCase(t *testing.T) {
	// scoreHost lowercases both haystacks before matching (rank.go:125-126):
	//	alias := strings.ToLower(h.Alias)
	//	host := strings.ToLower(h.HostName)
	// Every Alias/HostName fixture elsewhere in this file is already
	// all-lowercase, so that lowercasing is a no-op under test and a mutant
	// that deletes it survives the whole suite undetected. These fixtures
	// are mixed case on purpose so each one actually exercises a ToLower
	// call: a mixed-case alias found by a lowercase query (three tiers,
	// since substringPos, scatteredPos, and the prefix check are separate
	// code paths), a mixed-case hostname found the same way with an alias
	// that cannot match by any rule (so the match can only come from the
	// host half of the lowercasing), and — the other direction — a
	// lowercase alias found by an uppercase query.
	cases := []struct {
		name         string
		hosts        []sshconfig.Host
		query        string
		wantAlias    string
		wantAliasSet bool
		wantHostSet  bool
	}{
		{
			name:         "alias prefix tier, mixed-case alias",
			hosts:        []sshconfig.Host{{Alias: "NixOS-Dev", HostName: "10.0.0.1", Port: "22"}},
			query:        "nixos",
			wantAlias:    "NixOS-Dev",
			wantAliasSet: true,
		},
		{
			name:         "alias substring tier (not a prefix), mixed-case alias",
			hosts:        []sshconfig.Host{{Alias: "Prod-GitHub-Box", HostName: "10.0.0.2", Port: "22"}},
			query:        "github",
			wantAlias:    "Prod-GitHub-Box",
			wantAliasSet: true,
		},
		{
			name:         "alias scattered tier, mixed-case alias",
			hosts:        []sshconfig.Host{{Alias: "GitHub-Work", HostName: "10.0.0.3", Port: "22"}},
			query:        "gw",
			wantAlias:    "GitHub-Work",
			wantAliasSet: true,
		},
		{
			// "server1" has none of the letters n/i/x/o/s, so it cannot match
			// "nixos" under any tier — the only way this host is found at all
			// is the mixed-case HostName being lowercased.
			name:        "hostname substring tier, mixed-case hostname, alias cannot match",
			hosts:       []sshconfig.Host{{Alias: "server1", HostName: "NixOS-Dev.example.com", Port: "22"}},
			query:       "nixos",
			wantAlias:   "server1",
			wantHostSet: true,
		},
		{
			name:         "uppercase query, already-lowercase alias",
			hosts:        []sshconfig.Host{{Alias: "nixos-dev", HostName: "10.0.0.4", Port: "22"}},
			query:        "NIXOS",
			wantAlias:    "nixos-dev",
			wantAliasSet: true,
		},
	}

	for _, c := range cases {
		got := Rank(c.hosts, c.query)
		if len(got) != 1 || got[0].Host.Alias != c.wantAlias {
			t.Fatalf("%s: Rank = %v, want [%s]", c.name, aliases(got), c.wantAlias)
		}
		if aliasSet := got[0].AliasPos != nil; aliasSet != c.wantAliasSet {
			t.Errorf("%s: AliasPos set = %v, want %v", c.name, aliasSet, c.wantAliasSet)
		}
		if hostSet := got[0].HostNamePos != nil; hostSet != c.wantHostSet {
			t.Errorf("%s: HostNamePos set = %v, want %v", c.name, hostSet, c.wantHostSet)
		}
	}
}

func TestRankPositionsAreCorrectForMixedCaseHaystacks(t *testing.T) {
	// Positions are computed against the lowercased haystack (rank.go:125-126)
	// but the renderer highlights the original, un-lowercased Alias/HostName
	// (view.go). That's only safe because strings.ToLower is rune-count- and
	// index-preserving simple case mapping for these fixtures — it never
	// merges or splits runes — so an index into the lowered string is always
	// the same index into the original. This test pins the indices exactly,
	// so it would fail if that assumption ever broke (e.g. a switch to full
	// Unicode case folding, where "ß" folds to "ss" and shifts everything
	// after it).
	t.Run("prefix tier", func(t *testing.T) {
		hosts := []sshconfig.Host{{Alias: "NixOS-Dev", HostName: "10.0.0.1", Port: "22"}}
		got := Rank(hosts, "nixos")
		if len(got) != 1 {
			t.Fatalf("Rank returned %d matches, want 1", len(got))
		}
		if want := []int{0, 1, 2, 3, 4}; !equalInts(got[0].AliasPos, want) {
			t.Errorf("AliasPos = %v, want %v", got[0].AliasPos, want)
		}
		if got[0].HostNamePos != nil {
			t.Errorf("HostNamePos = %v, want nil for an alias match", got[0].HostNamePos)
		}
	})

	t.Run("scattered tier", func(t *testing.T) {
		// github-work: g(0) i t h u b - w(7) o r k.
		hosts := []sshconfig.Host{{Alias: "GitHub-Work", HostName: "10.0.0.2", Port: "22"}}
		got := Rank(hosts, "gw")
		if len(got) != 1 {
			t.Fatalf("Rank returned %d matches, want 1", len(got))
		}
		if want := []int{0, 7}; !equalInts(got[0].AliasPos, want) {
			t.Errorf("AliasPos = %v, want %v", got[0].AliasPos, want)
		}
		if got[0].HostNamePos != nil {
			t.Errorf("HostNamePos = %v, want nil for an alias match", got[0].HostNamePos)
		}
	})

	t.Run("hostname tier", func(t *testing.T) {
		hosts := []sshconfig.Host{{Alias: "server1", HostName: "NixOS-Dev.example.com", Port: "22"}}
		got := Rank(hosts, "nixos")
		if len(got) != 1 {
			t.Fatalf("Rank returned %d matches, want 1", len(got))
		}
		if want := []int{0, 1, 2, 3, 4}; !equalInts(got[0].HostNamePos, want) {
			t.Errorf("HostNamePos = %v, want %v", got[0].HostNamePos, want)
		}
		if got[0].AliasPos != nil {
			t.Errorf("AliasPos = %v, want nil for a hostname match", got[0].AliasPos)
		}
	})
}
