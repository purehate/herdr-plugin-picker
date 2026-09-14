// Package sshusage records how often something is reached for, so the picker
// can rank what the operator actually uses above what they merely have.
//
// It is named for its first caller, the ssh tab. Everything but Order is keyed
// by a plain string and is reused by the cmd tab against its own file; Order is
// the only part that knows about hosts.
package sshusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

// Usage is one host's history. LastUsed is unix seconds so the file is readable
// and stable across time zones.
type Usage struct {
	Count    int   `json:"count"`
	LastUsed int64 `json:"last_used"`
}

// Load reads the usage file. A missing, unreadable, or undecodable file yields
// an empty map and no error: this is derived state, and refusing to open the
// picker because the ordering file is corrupt would be a worse trade than
// losing the ordering.
func Load(path string) map[string]Usage {
	usage := map[string]Usage{}
	if path == "" {
		return usage
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return usage
	}
	_ = json.Unmarshal(data, &usage)
	if usage == nil {
		usage = map[string]Usage{}
	}
	return usage
}

// Record bumps alias's count and stamps it now, then writes the file. It is
// called on the way to opening a host, so a write failure must not be fatal;
// callers report it and carry on.
func Record(path, alias string, now time.Time) error {
	if path == "" || alias == "" {
		return nil
	}
	usage := Load(path)
	u := usage[alias]
	u.Count++
	u.LastUsed = now.Unix()
	usage[alias] = u
	return save(path, usage)
}

// save writes the whole map through a temp file and a rename, so a crash
// mid-write leaves the previous file intact rather than a truncated one. The
// temp file is created in the destination directory because rename is only
// atomic within a filesystem.
func save(path string, usage map[string]Usage) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(usage, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".usage-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once the rename below succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // the write error is the one worth reporting
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Score is a host's frecency: how often it was opened, plus a small bonus for
// how recently. Frequency alone ranks a host used ten times last year above one
// used twice this week; recency alone forgets a daily driver after a quiet
// fortnight. The bonus is deliberately coarse — it orders rows, and a formula
// nobody can predict is worse than one that is roughly right.
func (u Usage) Score(now time.Time) int {
	score := u.Count
	switch age := now.Sub(time.Unix(u.LastUsed, 0)); {
	case age < 24*time.Hour:
		score += 3
	case age < 7*24*time.Hour:
		score += 2
	case age < 30*24*time.Hour:
		score += 1
	}
	return score
}

// Order returns hosts pinned-first, then by frecency, then in the order given.
// Pins win outright: they are an explicit statement, so a pinned host the
// operator rarely uses still sits above one they use daily but never pinned.
// Pins keep their configured order among themselves. The input is not modified,
// and hosts absent from usage keep their input order behind the scored ones.
func Order(hosts []sshconfig.Host, usage map[string]Usage, pinned []string, now time.Time) []sshconfig.Host {
	pinRank := make(map[string]int, len(pinned))
	for i, alias := range pinned {
		if _, seen := pinRank[alias]; !seen {
			pinRank[alias] = i
		}
	}
	ordered := append([]sshconfig.Host(nil), hosts...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		pa, aPinned := pinRank[a.Alias]
		pb, bPinned := pinRank[b.Alias]
		if aPinned != bPinned {
			return aPinned
		}
		if aPinned && pa != pb {
			return pa < pb
		}
		return usage[a.Alias].Score(now) > usage[b.Alias].Score(now)
	})
	return ordered
}
