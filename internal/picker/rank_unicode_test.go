package picker

import (
	"strings"
	"testing"
	"unicode"

	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

// assertMatchedRunes checks pos against the ORIGINAL, un-lowercased string —
// the one the renderer walks (view.go highlight) — rather than the lowercased
// copy the positions were computed from. Two things have to hold: every index
// is in range, and the rune it lands on is the query rune it claims to have
// matched.
//
// Both halves are load-bearing. Asserting only len(pos) > 0 would pass for any
// garbage; asserting only that the indices are in range would pass for indices
// that are simply shifted, which is the whole failure mode here. And an
// out-of-range index does not announce itself downstream: highlight builds a
// set of matched indices and walks 0..len(runes), so a stray index just never
// fires — the highlight silently vanishes or lands on the wrong rune instead of
// panicking.
func assertMatchedRunes(t *testing.T, field, original, query string, pos []int) {
	t.Helper()
	rs := []rune(original)
	qs := []rune(strings.ToLower(query))
	if len(pos) != len(qs) {
		t.Fatalf("%s: len(pos) = %d (%v), want %d — one index per rune of query %q",
			field, len(pos), pos, len(qs), query)
	}
	for i, p := range pos {
		if p < 0 || p >= len(rs) {
			t.Fatalf("%s: pos[%d] = %d, out of range for the %d runes of %q",
				field, i, p, len(rs), original)
		}
		if got, want := unicode.ToLower(rs[p]), qs[i]; got != want {
			t.Errorf("%s: pos[%d] = %d points at %q in %q, want the query's rune %q",
				field, i, p, string(rs[p]), original, string(want))
		}
	}
}

// TestRankPositionsSurviveLengthChangingCaseRunes pins the rune-index
// correspondence that Match's "the indices are rune offsets" promise rests on.
// Positions are computed against strings.ToLower(alias) and then used to index
// the original, so an index has to mean the same rune in both strings.
//
// Two runes stress that from opposite sides and both are needed:
//
//   - İ (U+0130) is two bytes and lowercases to one-byte "i", so the lowered
//     copy is SHORTER IN BYTES than the original while holding the same runes.
//     It is the case where re-counting the offset against the wrong one of the
//     two strings gives a wrong answer.
//   - ß (U+00DF) lowercases to itself and stays two bytes, so the lowered copy
//     still has more bytes than runes. It is the case where a byte count used
//     where a rune count was wanted gives a wrong answer — and unlike İ, ß
//     survives lowercasing, so it catches that on the query side too, where a
//     query containing İ has already collapsed to ASCII.
//
// This found no bug: Go's strings.ToLower is Map(unicode.ToLower, ...) and
// unicode.ToLower is rune->rune, so the rune counts always agree. See the
// comment at the ToLower calls in rank.go for what would break that.
func TestRankPositionsSurviveLengthChangingCaseRunes(t *testing.T) {
	cases := []struct {
		name      string
		host      sshconfig.Host
		query     string
		wantAlias []int
		wantHost  []int
	}{{
		// "İ-straße-dev" is 12 runes but 14 bytes, and lowercases to
		// "i-straße-dev" — 12 runes and 13 bytes. Three different numbers, so
		// an index built from the wrong one of them cannot come out right by
		// coincidence.
		name:      "alias substring tier, after both runes",
		host:      sshconfig.Host{Alias: "İ-straße-dev", HostName: "10.0.0.1", Port: "22"},
		query:     "dev",
		wantAlias: []int{9, 10, 11},
	}, {
		// A multi-byte query, which no other fixture in this file has: len("ße")
		// is 3 but its rune count is 2.
		name:      "alias substring tier, multi-byte query",
		host:      sshconfig.Host{Alias: "İ-straße-dev", HostName: "10.0.0.1", Port: "22"},
		query:     "ße",
		wantAlias: []int{6, 7},
	}, {
		// The prefix tier takes its positions from runeRange(0, len(runes(q)))
		// rather than from substringPos, so it needs its own multi-byte query.
		name:      "alias prefix tier, multi-byte query",
		host:      sshconfig.Host{Alias: "straße-dev", HostName: "10.0.0.1", Port: "22"},
		query:     "straß",
		wantAlias: []int{0, 1, 2, 3, 4},
	}, {
		// The exact tier is the third position-producing path. "straße" is six
		// runes and seven bytes, so a byte-length reading yields index 6, one
		// past the end of the alias.
		name:      "alias exact tier, multi-byte query",
		host:      sshconfig.Host{Alias: "straße", HostName: "10.0.0.1", Port: "22"},
		query:     "straße",
		wantAlias: []int{0, 1, 2, 3, 4, 5},
	}, {
		// İ uppercases to itself, so an uppercase query still has to survive
		// the lowering on both sides to land on rune 0.
		name:      "alias prefix tier, uppercase query containing İ",
		host:      sshconfig.Host{Alias: "İstanbul-dev", HostName: "10.0.0.1", Port: "22"},
		query:     "İSTANBUL",
		wantAlias: []int{0, 1, 2, 3, 4, 5, 6, 7},
	}, {
		// The scattered tier walks []rune(lowered) directly instead of
		// converting an offset, so it is the one path where the two runes prove
		// something different: that the walk's indices still address the
		// original.
		name:      "alias scattered tier",
		host:      sshconfig.Host{Alias: "İ-straße-dev", HostName: "10.0.0.1", Port: "22"},
		query:     "ißd",
		wantAlias: []int{0, 6, 9},
	}, {
		// HostName is lowercased by a separate call (rank.go), so it needs its
		// own fixture. "server1" shares no letter with the query, so the match
		// can only come from the hostname half.
		name:     "hostname substring tier, İ before the match",
		host:     sshconfig.Host{Alias: "server1", HostName: "İstanbul-dev.example.com", Port: "22"},
		query:    "dev",
		wantHost: []int{9, 10, 11},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Rank([]sshconfig.Host{c.host}, c.query)
			if len(got) != 1 {
				t.Fatalf("Rank returned %d matches, want 1", len(got))
			}
			if !equalInts(got[0].AliasPos, c.wantAlias) {
				t.Errorf("AliasPos = %v, want %v", got[0].AliasPos, c.wantAlias)
			}
			if !equalInts(got[0].HostNamePos, c.wantHost) {
				t.Errorf("HostNamePos = %v, want %v", got[0].HostNamePos, c.wantHost)
			}
			if c.wantAlias != nil {
				assertMatchedRunes(t, "AliasPos", c.host.Alias, c.query, got[0].AliasPos)
			}
			if c.wantHost != nil {
				assertMatchedRunes(t, "HostNamePos", c.host.HostName, c.query, got[0].HostNamePos)
			}
		})
	}
}

// TestRankDoesNotFoldSharpS pins the behaviour that makes the correspondence
// above safe, at the level an operator can see: simple case mapping leaves ß
// alone, so typing the ASCII "strasse" does not find a "straße" host.
//
// That is a real limitation, and it is the pressure a future editor will feel
// to reach for full case folding. This test is here so that reach is a
// deliberate, visible change: folding ß to "ss" makes the lowered copy longer
// in RUNES than the original, and every index after it stops addressing the
// rune the renderer will draw.
func TestRankDoesNotFoldSharpS(t *testing.T) {
	hosts := []sshconfig.Host{{Alias: "straße-dev", HostName: "10.0.0.1", Port: "22"}}
	if got := Rank(hosts, "strasse"); len(got) != 0 {
		t.Fatalf("Rank = %v, want no match — strings.ToLower is simple case mapping, so ß does not fold to ss", aliases(got))
	}
	// The same query with the ß typed does match, so the miss above is the
	// folding, not a broken fixture.
	if got := Rank(hosts, "straße"); len(got) != 1 {
		t.Fatalf("Rank = %v, want straße-dev to match its own spelling", aliases(got))
	}
}
