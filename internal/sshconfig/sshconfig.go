// Package sshconfig parses OpenSSH client configuration into connectable hosts.
package sshconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrNoConfig reports a config file that does not exist. Callers treat this as
// "nothing to pick" rather than a failure.
var ErrNoConfig = errors.New("ssh config not found")

// Host is one connectable target with its keywords already resolved.
type Host struct {
	Alias        string
	HostName     string
	User         string
	Port         string
	IdentityFile string
	ProxyJump    string
	ProxyCommand string
	SourceFile   string
	SourceLine   int

	// Forwards are display-only, like IdentityFile: ssh applies them from the
	// config itself, so the picker never puts them on a command line. They are
	// slices because every occurrence applies — these keywords are additive,
	// unlike the first-wins ones above.
	LocalForward   []string
	RemoteForward  []string
	DynamicForward []string
}

// Warning is a non-fatal parse problem, surfaced in the picker footer.
type Warning struct {
	File string
	Line int
	Msg  string
}

func (w Warning) String() string { return fmt.Sprintf("%s:%d: %s", w.File, w.Line, w.Msg) }

type kv struct{ key, value string }

// block is one `Host <patterns>` stanza and the keywords under it.
type block struct {
	positive []string
	negative []string
	keys     []kv
	file     string
	line     int

	// guard is non-nil when this stanza was declared inside an Include whose
	// enclosing stanza was a real restriction, not the implicit root `Host *`.
	// ssh parses such an include under SSHCONF_NEVERMATCH, so the nested stanza
	// must not activate for an alias the guard rejects — otherwise it lists a
	// phantom host.
	guard *block
}

// stripComment cuts line at the first `#` that starts a comment: one outside
// any double-quoted run and preceded by start-of-line or whitespace. A `#`
// glued to the previous character, or written inside quotes, is literal.
// Verified against OpenSSH_10.3p1 for both exclusions.
func stripComment(line string) string {
	inQuotes := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuotes = !inQuotes
		case '#':
			if !inQuotes && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
				return strings.TrimRight(line[:i], " \t")
			}
		}
	}
	return line
}

// splitLine parses one config line into its key/value pair. badQuotes reports an
// odd number of `"` characters: ssh treats that as a fatal "invalid quotes"
// error and refuses the whole file, but we stay non-fatal and let the caller
// turn it into a Warning rather than render a healthy-looking picker for a
// config ssh would reject.
func splitLine(raw string) (key, value string, ok, badQuotes bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, false
	}
	line = stripComment(line)
	i := strings.IndexAny(line, " \t=")
	if i <= 0 {
		return "", "", false, false
	}
	key = line[:i]
	value = strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
	badQuotes = strings.Count(value, `"`)%2 != 0
	// Strip a single matching pair of surrounding quotes only when the value
	// both starts AND ends with one — an independent trim off each end (the
	// prior behavior) mangles a value that ends but does not start with a
	// quote. We deliberately do not emulate ssh's full strdelim
	// quote-joining beyond this: a value like ~/keys/"id ed" still differs
	// from ssh's ~/keys/id ed. That is an accepted, documented
	// simplification, not a bug.
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = value[1 : len(value)-1]
	}
	if value == "" {
		return "", "", false, badQuotes
	}
	return key, value, true, badQuotes
}

// expandTilde resolves a leading `~` — bare, or `~/...` — against the home
// directory. Any other input, `~user` included, comes back unchanged with a nil
// error: ssh expands `~user` from the passwd file and this package does not.
//
// It reports an error and an empty path when the home directory cannot be
// resolved (os.UserHomeDir fails on an unset or empty $HOME). Returning the
// input there would hand the caller a relative path with a literal `~` directory
// that silently matches nothing; the empty return means a caller ignoring the
// error cannot open a tilde path by accident.
func expandTilde(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve the home directory in %q: %w", p, err)
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/")), nil
}

// truncate shortens s for warning messages so one very long malformed line
// cannot blow up the picker footer.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Back off to a rune boundary. Cutting at a fixed byte count can sever a
	// multibyte character and leave invalid UTF-8 in the footer.
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// matchPattern reports whether alias matches an ssh_config(5) PATTERNS pattern.
// ssh recognizes only two wildcards — '*' and '?' — and every other byte,
// including '[', ']', and '\', is literal. This differs from filepath.Match,
// whose "[...]" classes would treat an alias like "web[12]" as glob syntax.
func matchPattern(pattern, alias string) bool {
	// Greedy backtracking match, byte-wise: track the most recent '*' so a
	// failed literal/'?' match can retry by consuming one more alias byte
	// under that star instead of failing outright.
	var pi, si int
	starAt, starSi := -1, 0
	for si < len(alias) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == alias[si]):
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			starAt, starSi = pi, si
			pi++
		case starAt != -1:
			pi = starAt + 1
			starSi++
			si = starSi
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// isPattern reports whether a positive Host entry is a wildcard rather than a
// selectable alias. Only `*` and `?` qualify. `!` is deliberately absent: it
// negates a pattern-list entry only as a leading character, which newHostBlock
// has already stripped into block.negative, so testing for it here would only
// misfire on a literal alias containing a mid-string `!`.
func isPattern(s string) bool { return strings.ContainsAny(s, "*?") }

// matches reports whether this stanza applies to alias. A negated pattern wins
// over any positive match, mirroring ssh_config semantics. A guard that
// rejects alias wins over everything: the stanza was parsed under
// SSHCONF_NEVERMATCH and never truly activates.
func (b block) matches(alias string) bool {
	if b.guard != nil && !b.guard.matches(alias) {
		return false
	}
	for _, n := range b.negative {
		if matchPattern(n, alias) {
			return false
		}
	}
	for _, p := range b.positive {
		if matchPattern(p, alias) {
			return true
		}
	}
	return false
}

// declares reports whether alias is named literally, which is what makes it a
// selectable target and fixes its source location.
func (b block) declares(alias string) bool {
	for _, p := range b.positive {
		if p == alias {
			return true
		}
	}
	return false
}

// Parse reads root and returns its connectable hosts in declaration order.
// Non-fatal problems come back as warnings; only an unreadable root is an error.
func Parse(root string) ([]Host, []Warning, error) {
	// ssh_config(5) fixes the include base at ~/.ssh for a user config. When the
	// home directory cannot be resolved there is no base, and "" says so:
	// parseIncludes warns and skips a relative Include rather than globbing the
	// process working directory. Not an error return, because root may still be
	// readable and its own hosts worth having — and root can produce warnings of
	// its own, which an error would be mutually exclusive with.
	//
	// Assigned explicitly rather than leaning on expandTilde's empty return, so
	// the "no base" signal does not depend on what the error path happens to
	// return.
	base, err := expandTilde("~/.ssh")
	if err != nil {
		base = ""
	}
	return parse(root, base)
}

// maxIncludeDepth bounds Include recursion. ssh_config(5) has no visited set
// of its own — verified against OpenSSH_10.3p1, a chain of Include directives
// resolves through depth 16 and fails at depth 17 with "Too many recursive
// configuration includes". We use the same cap to stop a true cycle (a file
// that is its own ancestor on the current include path) as well as a
// pathologically wide/deep include graph — either way the result is a
// Warning and a halt, never a hang and never a returned error.
const maxIncludeDepth = 16

// parse takes the include base explicitly so tests can root a config tree in a
// temp dir. ssh_config(5) resolves a relative Include against ~/.ssh for a user
// config at every nesting depth — the base never follows the including file.
func parse(root, includeBase string) ([]Host, []Warning, error) {
	ancestors := map[string]bool{}
	blocks, warns, err := parseFile(root, includeBase, block{positive: []string{"*"}}, ancestors, 0, nil)
	if err != nil {
		return nil, warns, err
	}
	return resolve(blocks), warns, nil
}

// parseFile parses one config file. enclosing supplies the patterns governing
// keywords before this file's first Host stanza: an implicit `Host *` for the
// root config, but for an included file the stanza the Include sat inside, since
// ssh processes an Include with the caller's active block still in effect.
//
// ancestors tracks files open on this descent path, not every file ever parsed —
// marked here and unmarked via defer, so a fragment shared by two sibling
// Includes resolves for both. depth is the Include hop count, used with
// ancestors to cap recursion rather than deduplicate by path.
func parseFile(path, includeBase string, enclosing block, ancestors map[string]bool, depth int, warns []Warning) ([]block, []Warning, error) {
	expanded, err := expandTilde(path)
	if err != nil {
		// Only reachable for a root config written as `~/...` while the home
		// directory is unresolvable. parse's caller turns this into a footer
		// warning that already names the path, and parseIncludes downgrades it
		// to an "include unreadable" warning like any other parseFile error.
		return nil, warns, err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return nil, warns, err
	}
	ancestors[abs] = true
	defer delete(ancestors, abs)

	// This error return (ErrNoConfig or otherwise) only ever reaches a caller
	// that inspects it when path is the root config: parse's only caller of
	// parseFile that does not go through parseIncludes. Every other caller is
	// parseIncludes, which downgrades any error from this file — ErrNoConfig
	// included — into a Warning instead of propagating it. That split is
	// deliberate: an unreadable root has nothing to show at all, but an
	// unreadable Include target must not cost the operator the rest of their
	// hosts.
	raw, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, warns, fmt.Errorf("%w: %s", ErrNoConfig, abs)
		}
		return nil, warns, err
	}

	var blocks []block
	// Inherit the enclosing stanza's patterns, not its keywords. This seed is
	// just as much "content parsed inside the include" as a nested Host
	// stanza is, so it inherits the same guard: if enclosing was itself a
	// NEVERMATCH restriction, keywords sitting before any Host line in this
	// file are equally inert.
	var seedGuard *block
	if depth > 0 {
		seedGuard = &enclosing
	}
	cur := block{positive: enclosing.positive, negative: enclosing.negative, file: abs, guard: seedGuard}

	for i, line := range strings.Split(string(raw), "\n") {
		lineNo := i + 1
		key, value, ok, badQuotes := splitLine(line)
		if !ok {
			if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				warns = append(warns, Warning{File: abs, Line: lineNo, Msg: "malformed line: " + truncate(trimmed, 60)})
			}
			continue
		}
		if badQuotes {
			// ssh treats this as a fatal "invalid quotes" error and refuses
			// the whole file. We stay non-fatal, but the operator must know:
			// our best-effort value here would otherwise render a
			// healthy-looking picker for a config ssh itself would reject.
			warns = append(warns, Warning{File: abs, Line: lineNo, Msg: "unbalanced quotes: " + truncate(strings.TrimSpace(line), 60)})
		}
		switch strings.ToLower(key) {
		case "host":
			blocks = append(blocks, cur)
			var guard *block
			if depth > 0 {
				// This file was reached via Include; enclosing is the stanza
				// that was active at the Include line. If it's a real
				// restriction, a nested Host here only ever activates for an
				// alias enclosing itself would also match.
				guard = &enclosing
			}
			cur = newHostBlock(value, abs, lineNo, guard)
		case "match":
			// A Match stanza has no static host to offer, and `Match exec` would
			// mean running commands to build a picker list. Skip its keywords by
			// starting a stanza that matches nothing.
			blocks = append(blocks, cur)
			cur = block{file: abs, line: lineNo}
		case "include":
			blocks = append(blocks, cur)
			var included []block
			included, warns = parseIncludes(value, abs, includeBase, cur, lineNo, ancestors, depth, warns)
			blocks = append(blocks, included...)
			// keys = nil, not cur.keys[:0]: cur was just copied into blocks
			// on the line above, and a struct copy shares the keys slice's
			// backing array. Reslicing to [:0] would keep that same backing
			// array (only len changes), so a keyword set later in this
			// stanza could reuse the array and silently overwrite index 0 —
			// corrupting a keyword recorded before this Include in the copy
			// already sitting in blocks. nil forces the next append here to
			// allocate a fresh array instead of aliasing.
			resumed := cur
			resumed.keys = nil
			cur = resumed
		default:
			cur.keys = append(cur.keys, kv{strings.ToLower(key), value})
		}
	}
	return append(blocks, cur), warns, nil
}

func newHostBlock(value, file string, line int, guard *block) block {
	b := block{file: file, line: line, guard: guard}
	for _, field := range strings.Fields(value) {
		if strings.HasPrefix(field, "!") {
			b.negative = append(b.negative, strings.TrimPrefix(field, "!"))
			continue
		}
		b.positive = append(b.positive, field)
	}
	return b
}

func resolve(blocks []block) []Host {
	var order []string
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, p := range b.positive {
			if isPattern(p) || seen[p] {
				continue
			}
			if b.guard != nil && !b.guard.matches(p) {
				// Declared under SSHCONF_NEVERMATCH: not a real config entry,
				// so it must not appear in the picker at all.
				continue
			}
			seen[p] = true
			order = append(order, p)
		}
	}

	hosts := make([]Host, 0, len(order))
	for _, alias := range order {
		hosts = append(hosts, resolveHost(alias, blocks))
	}
	return hosts
}

func resolveHost(alias string, blocks []block) Host {
	h := Host{Alias: alias}
	values := map[string]string{}

	// One ordered walk: ssh uses the first obtained value for each keyword and
	// does not rank literal stanzas above patterns. Ordering is the operator's
	// job, which is why `Host *` belongs at the end of a config.
	for _, b := range blocks {
		if !b.matches(alias) {
			continue
		}
		if h.SourceFile == "" && b.declares(alias) {
			h.SourceFile, h.SourceLine = b.file, b.line
		}
		for _, pair := range b.keys {
			// Forwards are additive, not first-wins: ssh applies every
			// LocalForward/RemoteForward/DynamicForward it sees, so collecting
			// them through the first-wins map below would drop all but the first.
			switch pair.key {
			case "localforward":
				h.LocalForward = append(h.LocalForward, pair.value)
			case "remoteforward":
				h.RemoteForward = append(h.RemoteForward, pair.value)
			case "dynamicforward":
				h.DynamicForward = append(h.DynamicForward, pair.value)
			}
			if _, exists := values[pair.key]; !exists {
				values[pair.key] = pair.value
			}
		}
	}

	h.HostName = firstNonEmpty(values["hostname"], alias)
	h.User = values["user"]
	h.Port = firstNonEmpty(values["port"], "22")
	// IdentityFile is display-only: the preview panel renders it and nothing in
	// this program ever opens it, because `ssh <alias>` resolves the keyword
	// itself. So an unresolvable home directory keeps the operator's own
	// spelling rather than blanking the field or inventing a path — showing
	// `~/.ssh/id_x` is the honest answer to "what does your config say", and
	// there is no glob or open here to fail closed about. resolveHost has no
	// warning channel by design; the callers that build paths from expandTilde
	// (parseFile, parseIncludes) are the ones that warn.
	if expanded, err := expandTilde(values["identityfile"]); err == nil {
		h.IdentityFile = expanded
	} else {
		h.IdentityFile = values["identityfile"]
	}
	// `none` explicitly disables either proxy mechanism. Keeping it as a
	// non-empty value would make the picker skip a perfectly direct host and
	// render "via none". OpenSSH treats keyword values case-insensitively here.
	if v := strings.TrimSpace(values["proxyjump"]); !strings.EqualFold(v, "none") {
		h.ProxyJump = v
	}
	if v := strings.TrimSpace(values["proxycommand"]); !strings.EqualFold(v, "none") {
		h.ProxyCommand = v
	}
	return h
}

// parseIncludes expands one Include directive. Relative patterns resolve
// against includeBase, fixed at ~/.ssh for the primary config at every depth per
// ssh_config(5) — never the including file's directory, never the process cwd.
// enclosing carries the caller's active stanza into the included file, since ssh
// processes an Include with that stanza still in effect.
//
// An absent target (zero-match glob or missing literal path) is silent, verified
// against OpenSSH_10.3p1; warning would false-positive on an optional,
// tool-managed include. A malformed pattern or a present-but-unreadable target
// is a warning, so one bad include does not cost the operator the rest.
//
// A match already open on this descent path (a true cycle) or past
// maxIncludeDepth is also a warning, and parsing does not descend: ssh_config(5)
// has no visited set, so re-including a live ancestor is bounded by the same cap
// rather than silently deduplicated.
func parseIncludes(value, parent, includeBase string, enclosing block, line int, ancestors map[string]bool, depth int, warns []Warning) ([]block, []Warning) {
	var out []block
	for _, rawPattern := range strings.Fields(value) {
		pattern, err := expandTilde(rawPattern)
		if err != nil {
			// A `~` this package cannot expand. Warned rather than globbed:
			// the unexpanded form is a relative path with a directory named
			// `~` in it, so Glob would return zero matches and take the silent
			// branch below, costing the operator every host in the included
			// file with nothing on screen to say so.
			warns = append(warns, Warning{File: parent, Line: line, Msg: "include unresolvable: " + err.Error()})
			continue
		}
		if !filepath.IsAbs(pattern) {
			if includeBase == "" {
				// Parse leaves the base empty when it could not resolve ~/.ssh,
				// which ssh_config(5) fixes as the base for every relative
				// Include in a user config. Joining onto "" would leave the
				// pattern relative to the process working directory — a
				// directory ssh does not read config from, and one whose
				// contents the operator may not have authored.
				warns = append(warns, Warning{File: parent, Line: line, Msg: "include unresolvable: " + rawPattern + ": cannot resolve the home directory for the ~/.ssh include base"})
				continue
			}
			pattern = filepath.Join(includeBase, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			warns = append(warns, Warning{File: parent, Line: line, Msg: "include unreadable: " + rawPattern})
			continue
		}
		if len(matches) == 0 {
			// Absent: silent. See the doc comment above.
			continue
		}
		for _, match := range matches {
			// A Glob result is never tilde-prefixed, so expandTilde is a
			// pass-through here; its error is folded into the same warning as
			// filepath.Abs's rather than given a branch of its own.
			abs, err := expandTilde(match)
			if err == nil {
				abs, err = filepath.Abs(abs)
			}
			if err != nil {
				warns = append(warns, Warning{File: parent, Line: line, Msg: "include unreadable: " + match})
				continue
			}
			if ancestors[abs] {
				warns = append(warns, Warning{File: parent, Line: line, Msg: "include cycle: " + match})
				continue
			}
			if depth >= maxIncludeDepth {
				warns = append(warns, Warning{File: parent, Line: line, Msg: "too many recursive includes: " + match})
				continue
			}
			blocks, updated, err := parseFile(match, includeBase, enclosing, ancestors, depth+1, warns)
			warns = updated
			if err != nil {
				// Downgrades every parseFile error uniformly, including a
				// wrapped ErrNoConfig — see the comment at parseFile's
				// os.ReadFile error return for why that seam only matters
				// for the root config.
				warns = append(warns, Warning{File: parent, Line: line, Msg: "include unreadable: " + match})
				continue
			}
			out = append(out, blocks...)
		}
	}
	return out, warns
}

// Exclude filters out hosts whose alias matches any pattern (ssh_config(5)
// PATTERNS, via matchPattern — not filepath.Match, so "[" is literal here,
// unlike a real filesystem glob), preserving order. It never mutates hosts,
// but when there is nothing to exclude it returns hosts itself rather than a
// copy.
func Exclude(hosts []Host, patterns []string) []Host {
	if len(patterns) == 0 {
		return hosts
	}
	kept := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		hidden := false
		for _, p := range patterns {
			if matchPattern(p, h.Alias) {
				hidden = true
				break
			}
		}
		if !hidden {
			kept = append(kept, h)
		}
	}
	return kept
}
