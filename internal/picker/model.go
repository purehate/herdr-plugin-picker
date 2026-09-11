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
		// copy of the model that shares them. Safe because bubbletea holds one
		// model and discards the predecessor on each Update; do not snapshot a
		// model and expect its probe state to stay frozen.
		m.probed[msg.Alias] = true
		m.up[msg.Alias] = msg.Up
		return m, waitProbe(m.opts.Probes)
	case probeClosedMsg:
		// A no-op, identical to the trailing return: the arm exists so
		// "probes finished" reads as an expected message rather than one the
		// model silently ignores.
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl chords are dispatched before printable text. The two orderings are
	// equivalent on this stack — ultraviolet clears Key.Text for a stronger
	// Mod, and the special codes below never carry Text — so this is not
	// load-bearing today. Keep it anyway: the alternative relies on a library
	// invariant we do not control. It is not evidence that ctrl+t can arrive as
	// Text "t"; it cannot.
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

// deleteWord trims the last word off the query: any trailing separators, then
// the run of non-separators before them. It reuses the scorer's separator set,
// so "word" means the same thing while typing as while matching — ctrl+w on
// "nixos-dev" leaves "nixos-".
//
// Scan runes, not bytes. A byte scan happens to stop in the same place only
// while every separator in isSeparator is ASCII; that equivalence is a property
// of the set, not of this function, and it ends the moment a non-ASCII member
// is added. Do not remove the conversion as a redundant allocation.
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
