package picker

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
)

// view.go holds the pieces the ssh tab shares with what used to be a standalone
// picker: the row markers, the preview block, the rune highlighter, and the
// warning flattener. The frame and the model live in navigator.go/navssh.go now.

// Markers, per the spec. "a pane is already connected" and "the host answers on
// 22" are two different facts and get two different glyphs; conflating them
// would make the reuse affordance unreadable.
const (
	openMarker  = "▪" // a session pane exists (accent)
	upMarker    = "●" // TCP answered (green)
	downMarker  = "○" // no answer
	skipMarker  = "~" // proxied, deliberately not probed
	blankMarker = " " // not probed yet
	// maxWarnings is the ceiling on warning lines under the ssh tab. The count
	// is unbounded — loadHosts emits one per unreadable include — and unlike the
	// preview the warning block never yields, so every line here is a host row
	// the operator does not get and the list is what the picker is for.
	//
	// Three covers the shape a real failure takes: the two sources that can
	// warn at most once each (a rejected plugin config, a rejected theme) plus
	// one parse warning. Past that the overflow notice carries the rest, so the
	// block is never more than four lines however broken the config is.
	maxWarnings = 3
)

// previewLabelWidth is the column the preview's values start at: the longest
// label, "IdentityFile" at 12, plus two spaces.
//
// Fixed rather than computed from the fields the current host happens to set.
// A per-host width would be tighter but the value edge would shift every time
// the operator moved the cursor onto a host with a different field set, and
// scanning down the list is the whole reason the panel exists — an edge that
// moves while you scan is worse than one that sits further right.
const previewLabelWidth = 14

// previewFields is the preview's content, one "Label value" line per populated
// field, with the labels padded so the values form a single edge. The alignment
// is a requirement, not presentation: scanning down the list is why the panel
// exists, and an edge that moves is worse than one further right.
//
// sshconfig.Parse always resolves Port and sets SourceFile, so a parsed host
// has at least three fields here, never two; a hand-built test Host can have
// two and understate the chrome.
func previewFields(h sshconfig.Host) []string {
	type field struct{ label, value string }
	fields := []field{{"HostName", h.HostName}, {"Port", h.Port}}
	if h.User != "" {
		fields = append(fields, field{"User", h.User})
	}
	if h.IdentityFile != "" {
		fields = append(fields, field{"IdentityFile", h.IdentityFile})
	}
	if h.ProxyJump != "" {
		fields = append(fields, field{"ProxyJump", h.ProxyJump})
	}
	if h.ProxyCommand != "" {
		fields = append(fields, field{"ProxyCommand", h.ProxyCommand})
	}
	if h.SourceFile != "" {
		// Provenance matters as soon as Include is in play: "which file did this
		// host actually come from" is otherwise unanswerable from the picker.
		fields = append(fields, field{"source", fmt.Sprintf("%s:%d", h.SourceFile, h.SourceLine)})
	}
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		lines = append(lines, fmt.Sprintf("%-*s%s", previewLabelWidth, f.label, f.value))
	}
	return lines
}

// previewSeparator is the divider above the preview's fields, indented to the
// same column as the fields under it rather than spanning the box.
const previewSeparator = frameIndent + "  ─────"

// maxAliasColumn caps how far the detail column can be pushed right. One
// unusually long alias should not cost every other row the width of it; past
// this the long row goes ragged on its own and the rest stay aligned.
const maxAliasColumn = 28

// highlight renders s with the runes at pos in the hit style and everything else
// in base. Runs of same-styled runes are batched into one Render call, so the
// output carries one escape pair per run rather than one per rune.
//
// pos holds rune indices, so s is converted once and indexed as runes. Using
// byte offsets here would slice multi-byte runes in half.
func highlight(s string, pos []int, base, hit lipgloss.Style) string {
	if len(pos) == 0 {
		return base.Render(s)
	}
	matched := make(map[int]bool, len(pos))
	for _, p := range pos {
		matched[p] = true
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); {
		j := i
		for j < len(rs) && matched[j] == matched[i] {
			j++
		}
		style := base
		if matched[i] {
			style = hit
		}
		b.WriteString(style.Render(string(rs[i:j])))
		i = j
	}
	return b.String()
}

// oneLine folds s onto a single line, so one warning costs the one row
// warningLines reserved for it. Warnings are opaque strings built outside this
// package, and pluginconfig's errors.Join separates multiple rejected keys with
// a newline.
//
// FieldsFunc rather than a replace, so "\r\n" collapses to one space and a lone
// carriage return cannot overwrite the start of its own line.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r'
	}), " ")
}
