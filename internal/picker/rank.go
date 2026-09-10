// Package picker renders the SSH host overlay.
package picker

import (
	"sort"
	"strings"

	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
)

// Match quality, best first. Alias matches always beat hostname matches: the
// operator typed a name they chose, not an address they were assigned.
const (
	rankExact = iota
	rankAliasPrefix
	rankAliasSubstring
	rankAliasScattered
	rankHostSubstring
	rankHostScattered
	rankNone
)

// Scoring bonuses for the scattered tiers, in the shape fzf uses
// (src/algo/algo.go). They break ties *within* a tier and never move a host
// across tiers, so the ranking order the spec fixes is untouched.
const (
	bonusBoundary    = 8 // rune 0, or the rune after a separator
	bonusConsecutive = 4 // rune immediately after the previous match
	bonusFirstRune   = 2 // multiplier: fzf doubles the first matched rune
)

// Match is one ranked host plus the rune positions the query matched, so the
// picker can highlight exactly the characters that earned the row its place.
//
// For a non-empty query exactly one of AliasPos / HostNamePos is non-nil:
// scoreHost stops at the first tier that matches, and every alias tier is
// checked before every hostname tier. Both are nil for an empty query, where
// nothing matched and there is nothing to highlight.
//
// The indices are rune offsets, not byte offsets. The renderer walks runes.
type Match struct {
	Host        sshconfig.Host
	AliasPos    []int
	HostNamePos []int
}

// runeRange returns n consecutive indices starting at start.
func runeRange(start, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = start + i
	}
	return out
}

// substringPos returns the rune positions of the first occurrence of q in s, or
// nil. Both must already be lowercased.
func substringPos(s, q string) []int {
	b := strings.Index(s, q)
	if b < 0 {
		return nil
	}
	// strings.Index counts bytes; re-count the prefix in runes so the caller
	// gets an index it can use against []rune(s).
	return runeRange(len([]rune(s[:b])), len([]rune(q)))
}

// scatteredPos greedily matches every rune of q against s left to right and
// returns the positions it landed on, or nil if q does not fit.
func scatteredPos(s, q string) []int {
	rs := []rune(s)
	out := make([]int, 0, len(q))
	at := 0
	for _, want := range q {
		for at < len(rs) && rs[at] != want {
			at++
		}
		if at == len(rs) {
			return nil
		}
		out = append(out, at)
		at++
	}
	return out
}

// isSeparator reports whether r ends a word. SSH aliases and hostnames are
// segmented by punctuation, not whitespace, so "-", "_", and "." carry the
// boundaries an operator actually types around.
func isSeparator(r rune) bool {
	switch r {
	case '-', '_', '.', '/', ':', '@', ' ':
		return true
	}
	return false
}

// positionScore rates a scattered match; higher is better. Matches sitting at
// word starts beat matches buried mid-word, so typing "nd" prefers "nixos-dev"
// (n at the start, d after the hyphen) over a host where both land mid-word.
func positionScore(s string, pos []int) int {
	rs := []rune(s)
	total := 0
	for i, p := range pos {
		b := 0
		if p == 0 || isSeparator(rs[p-1]) {
			b = bonusBoundary
		}
		if i > 0 && pos[i-1] == p-1 {
			b += bonusConsecutive
		}
		if i == 0 {
			b *= bonusFirstRune
		}
		total += b
	}
	return total
}

// scoreHost classifies h against q and returns the tier plus the rune positions
// that matched. It stops at the first matching tier, so the positions always
// describe the reading that earned the tier — a contiguous run for the
// substring tiers, the greedy walk for the scattered ones.
func scoreHost(h sshconfig.Host, q string) (tier int, aliasPos, hostPos []int) {
	// These positions are computed against the lowercased copy and then used to
	// index the ORIGINAL — Match promises rune offsets, and the renderer walks
	// the original's runes. That only holds because strings.ToLower is simple
	// case mapping: it is Map(unicode.ToLower, ...), and unicode.ToLower is
	// rune->rune, so the copy has exactly as many runes as the original and
	// index i means the same rune in both. Bytes are not preserved (İ is two
	// bytes and lowercases to one-byte "i") — only runes are, which is why the
	// re-count in substringPos is a rune count and has to stay one.
	//
	// Full case folding would end that. Fold ß to "ss" and the copy grows a
	// rune, so every index past it addresses the wrong rune of the original —
	// and the ones past the end simply stop highlighting, because highlight
	// membership-tests indices rather than bounds-checking them. So do not swap
	// these for cases.Fold or ToLowerSpecial to make "strasse" match "straße"
	// without also re-deriving the positions against the original. See
	// TestRankPositionsSurviveLengthChangingCaseRunes.
	alias := strings.ToLower(h.Alias)
	host := strings.ToLower(h.HostName)

	switch {
	case alias == q:
		return rankExact, runeRange(0, len([]rune(q))), nil
	case strings.HasPrefix(alias, q):
		return rankAliasPrefix, runeRange(0, len([]rune(q))), nil
	}
	if p := substringPos(alias, q); p != nil {
		return rankAliasSubstring, p, nil
	}
	if p := scatteredPos(alias, q); p != nil {
		return rankAliasScattered, p, nil
	}
	if p := substringPos(host, q); p != nil {
		return rankHostSubstring, nil, p
	}
	if p := scatteredPos(host, q); p != nil {
		return rankHostScattered, nil, p
	}
	return rankNone, nil, nil
}

// Rank returns the hosts matching query, best first, each paired with the rune
// positions that matched. An empty query returns every host in config order.
// Ties keep config order, so the list never reshuffles for reasons the operator
// cannot see.
func Rank(hosts []sshconfig.Host, query string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]Match, 0, len(hosts))
		for _, h := range hosts {
			out = append(out, Match{Host: h})
		}
		return out
	}

	type scored struct {
		match Match
		tier  int
		bonus int
		index int
	}
	matches := make([]scored, 0, len(hosts))
	for i, h := range hosts {
		tier, aliasPos, hostPos := scoreHost(h, q)
		if tier == rankNone {
			continue
		}
		// The bonus only discriminates inside the scattered tiers, which would
		// otherwise be entirely undifferentiated. Leaving it at zero everywhere
		// else keeps config order as the spec defines it.
		bonus := 0
		switch tier {
		case rankAliasScattered:
			bonus = positionScore(strings.ToLower(h.Alias), aliasPos)
		case rankHostScattered:
			bonus = positionScore(strings.ToLower(h.HostName), hostPos)
		}
		matches = append(matches, scored{
			match: Match{Host: h, AliasPos: aliasPos, HostNamePos: hostPos},
			tier:  tier,
			bonus: bonus,
			index: i,
		})
	}
	sort.SliceStable(matches, func(a, b int) bool {
		if matches[a].tier != matches[b].tier {
			return matches[a].tier < matches[b].tier
		}
		if matches[a].bonus != matches[b].bonus {
			return matches[a].bonus > matches[b].bonus
		}
		return matches[a].index < matches[b].index
	})

	out := make([]Match, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.match)
	}
	return out
}
