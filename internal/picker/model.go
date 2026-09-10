package picker

import (
	tea "charm.land/bubbletea/v2"
	"github.com/purehate/herdr-plugin-ssh/internal/probe"
	"github.com/purehate/herdr-plugin-ssh/internal/sshconfig"
	"github.com/purehate/herdr-plugin-ssh/internal/theme"
)

// Selection is what the operator picked and how they want it opened.
type Selection struct {
	Host      sshconfig.Host
	Placement string // split | tab | zoomed
	ForceNew  bool
}

// Options configures one picker run.
type Options struct {
	Hosts       []sshconfig.Host
	Theme       theme.Theme
	ShowPreview bool
	// OpenPanes maps alias → pane id for sessions already running, so the
	// picker can mark them and reuse them.
	OpenPanes map[string]string
	Probes    <-chan probe.Result
	Warnings  []string
}

type probeMsg probe.Result

type probeClosedMsg struct{}

type model struct {
	opts  Options
	query string
	// view holds Matches, not Hosts, because Task 14 highlights the runes that
	// matched and only Rank knows which those were.
	view     []Match
	cursor   int
	preview  bool
	up       map[string]bool
	probed   map[string]bool
	width    int
	height   int
	chosen   *Selection
	quitting bool
}

func newModel(o Options) model {
	return model{
		opts:    o,
		view:    Rank(o.Hosts, ""),
		preview: o.ShowPreview,
		up:      map[string]bool{},
		probed:  map[string]bool{},
	}
}

// waitProbe reads one probe result and re-arms itself, turning the probe
// channel into a stream of messages.
func waitProbe(ch <-chan probe.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return probeClosedMsg{}
		}
		return probeMsg(r)
	}
}

func (m model) Init() tea.Cmd {
	if m.opts.Probes == nil {
		return nil
	}
	return waitProbe(m.opts.Probes)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case probeMsg:
		// probed and up are maps, so these writes are visible through every
		// copy of the model that shares them. Deliberate: bubbletea holds
		// exactly one model and discards the predecessor on each Update, so
		// there is no observer of the older copies. Do not snapshot a model
		// and expect its probe state to stay frozen.
		m.probed[msg.Alias] = true
		m.up[msg.Alias] = msg.Up
		return m, waitProbe(m.opts.Probes)
	case probeClosedMsg:
		// Explicitly a no-op: the trailing return below would handle this
		// identically. The arm exists so that "probes finished" reads as a
		// message the model expects rather than one it silently ignores, and
		// so there is somewhere obvious to hang behavior if it ever needs
		// any. Deleting it changes nothing today — no test can catch that,
		// which is why this comment is here instead of a test.
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl chords are dispatched before printable text. On this stack the two
	// orderings are in fact equivalent: ultraviolet clears Key.Text whenever
	// Mod is stronger than ModShift, and the special codes in the second
	// switch below (esc, enter, up, down, backspace) are non-printable and
	// never carry Text either. So this order is not currently load-bearing —
	// but keep it. It costs nothing, and the alternative relies on a library
	// invariant we do not control. What it is not is evidence that ctrl+t can
	// arrive as Text "t": it cannot, and a guard written on that assumption
	// would be guarding nothing.
	if k.Mod&tea.ModCtrl != 0 {
		return m.handleCtrl(k)
	}

	switch k.Code {
	case tea.KeyEsc:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEnter:
		return m.choose("split", false)
	case tea.KeyDown:
		return m.moveCursor(1), nil
	case tea.KeyUp:
		return m.moveCursor(-1), nil
	case tea.KeyBackspace:
		if m.query != "" {
			r := []rune(m.query)
			m.query = string(r[:len(r)-1])
			m = m.refilter()
		}
		return m, nil
	}

	if k.Text != "" {
		m.query += k.Text
		m = m.refilter()
	}
	return m, nil
}

// handleCtrl handles every ctrl-chord key. Split out of handleKey to keep each
// function under the project's line limit; behavior is unchanged.
func (m model) handleCtrl(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Code {
	case 'c':
		m.quitting = true
		return m, tea.Quit
	case 't':
		return m.choose("tab", false)
	case 'z':
		return m.choose("zoomed", false)
	case 'n':
		return m.choose("split", true)
	case 'o':
		m.preview = !m.preview
		return m, nil
	case 'j':
		return m.moveCursor(1), nil
	case 'k':
		return m.moveCursor(-1), nil
	case 'u':
		m.query = ""
		return m.refilter(), nil
	case 'w':
		m.query = deleteWord(m.query)
		return m.refilter(), nil
	}
	return m, nil
}

func (m model) refilter() model {
	m.view = Rank(m.opts.Hosts, m.query)
	m.cursor = 0
	return m
}

// deleteWord trims the last word off the query: first any trailing separators,
// then the run of non-separators before them. It reuses the scorer's separator
// set so "word" means the same thing while typing as it does while matching —
// ctrl+w on "nixos-dev" leaves "nixos-", which is still a useful query.
//
// Scan runes, not bytes. A byte scan would work today, but only because every
// separator in isSeparator is ASCII and no byte of a multi-byte UTF-8 rune can
// equal an ASCII byte — so it happens to stop exactly where a rune scan does.
// That equivalence is a property of the separator set, not of this function,
// and it ends the moment isSeparator gains a non-ASCII member, at which point
// a byte scan starts cutting runes in half. The conversion is what makes this
// correct independently of that set; do not remove it as a redundant
// allocation.
func deleteWord(q string) string {
	r := []rune(q)
	i := len(r)
	for i > 0 && isSeparator(r[i-1]) {
		i--
	}
	for i > 0 && !isSeparator(r[i-1]) {
		i--
	}
	return string(r[:i])
}

func (m model) moveCursor(delta int) model {
	next := m.cursor + delta
	if next < 0 || next >= len(m.view) {
		return m
	}
	m.cursor = next
	return m
}

func (m model) choose(placement string, forceNew bool) (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.view) {
		return m, nil
	}
	m.chosen = &Selection{Host: m.view[m.cursor].Host, Placement: placement, ForceNew: forceNew}
	m.quitting = true
	return m, tea.Quit
}
