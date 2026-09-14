package picker

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/purehate/herdr-plugin-picker/internal/probe"
	"github.com/purehate/herdr-plugin-picker/internal/sshconfig"
	"github.com/purehate/herdr-plugin-picker/internal/theme"
)

// NavSection is one tab in the unified picker.
type NavSection int

const (
	NavSpaces NavSection = iota
	NavAgents
	NavSessions
	NavSSH
	navSectionCount
)

var navNames = [...]string{"spaces", "agents", "sessions", "ssh"}

const (
	navJumpLabel  = " ↵ jump "
	navCloseLabel = "esc close"
)

// Layout budget. headerRows is the blank padding row, the tabs, the rule, the
// search line, and the blank under it; footerRows is the hints, the action line,
// and the trailing padding row. The ssh tab adds a preview block and a warning
// block on top of these, both budgeted separately so a short pane yields them
// before it yields the list.
const (
	navHeaderRows = 5
	navFooterRows = 3
)

// defaultRefreshInterval is how often an open picker re-reads the server
// inventory. Fast enough that a newly blocked agent appears while the operator
// is still looking at the list, slow enough that the subprocess per tick does
// not dominate the popup's lifetime.
const defaultRefreshInterval = time.Second

// NavItem is display metadata plus the opaque Herdr ID used when selected. For
// the ssh tab, Host carries the parsed config entry the row opens and the
// position slices are the runes the query matched, so the row highlights them
// the way the standalone picker did.
type NavItem struct {
	ID      string
	Label   string
	Detail  string
	Search  string
	Current bool

	Host        sshconfig.Host
	AliasPos    []int
	HostNamePos []int
}

// NavRefresh is the set of server-backed lists the picker rebuilds on each
// live-refresh tick. Hosts are read from disk and cannot change under an open
// popup, so they are not part of a refresh.
type NavRefresh struct {
	Spaces   []NavItem
	Agents   []NavItem
	Sessions []NavItem
}

type NavOptions struct {
	Theme    theme.Theme
	Spaces   []NavItem
	Agents   []NavItem
	Sessions []NavItem

	// Refresh re-reads the server inventory for a live picker. When set, the
	// picker re-runs it every RefreshInterval and swaps in the new lists,
	// holding the cursor on the same item ID and leaving the query alone. nil
	// disables live refresh.
	Refresh func() (NavRefresh, error)
	// RefreshInterval is the tick period; zero means defaultRefreshInterval.
	RefreshInterval time.Duration

	// Hosts is the ssh config inventory, rendered as the ssh tab.
	Hosts []sshconfig.Host
	// Probes streams reachability results for the ssh tab. nil disables them.
	Probes <-chan probe.Result
	// OpenPanes maps ssh alias → pane id for sessions already running, so the
	// ssh tab can mark them and reuse them.
	OpenPanes map[string]string
	// ShowPreview opens the ssh tab with the host preview expanded.
	ShowPreview bool
	// Warnings are the config problems to surface under the ssh tab's list.
	Warnings []string
}

type NavSelection struct {
	Section NavSection
	Item    NavItem
	// Placement and ForceNew apply only to NavSSH: how the chosen host opens.
	Placement string
	ForceNew  bool
}

type navigatorModel struct {
	opts    NavOptions
	section NavSection
	query   string
	items   []NavItem
	cursor  int
	width   int
	height  int
	preview bool
	// probed and up are maps, so probe writes are visible through every copy of
	// the model that shares them. Safe because bubbletea holds one model and
	// discards the predecessor on each Update.
	probed map[string]bool
	up     map[string]bool
	chosen *NavSelection
	// refreshErr is the last failed inventory read, kept so the footer can say
	// the list on screen is stale rather than silently pretending it is current.
	refreshErr error
}

func newNavigatorModel(o NavOptions) navigatorModel {
	m := navigatorModel{
		opts:    o,
		preview: o.ShowPreview,
		probed:  map[string]bool{},
		up:      map[string]bool{},
	}
	return m.refilter()
}

func (m navigatorModel) source() []NavItem {
	switch m.section {
	case NavSpaces:
		return m.opts.Spaces
	case NavAgents:
		return m.opts.Agents
	case NavSessions:
		return m.opts.Sessions
	default:
		return sshNavItems(m.opts.Hosts, "")
	}
}

// A contiguous name hit beats a scattered hit; metadata remains searchable.
// Equal scores retain the API's ordering, keeping the list steady as you type.
func rankNav(items []NavItem, query string) []NavItem {
	if query == "" {
		return append([]NavItem(nil), items...)
	}
	q := strings.ToLower(query)
	type hit struct {
		item  NavItem
		score int
	}
	hits := make([]hit, 0, len(items))
	for _, item := range items {
		label := strings.ToLower(item.Label)
		all := strings.ToLower(item.Label + " " + item.Detail + " " + item.Search)
		score := 0
		switch {
		case strings.HasPrefix(label, q):
			score = 0
		case strings.Contains(label, q):
			score = 1
		case strings.Contains(all, q):
			score = 2
		case scatteredPos(all, q) != nil:
			score = 3
		default:
			continue
		}
		hits = append(hits, hit{item: item, score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	out := make([]NavItem, len(hits))
	for i, h := range hits {
		out[i] = h.item
	}
	return out
}

// refilter ranks the active section against the query. The ssh tab ranks through
// Rank so the matched runes come back with the row; the other sections only need
// an order.
func (m navigatorModel) refilter() navigatorModel {
	if m.section == NavSSH {
		m.items = sshNavItems(m.opts.Hosts, m.query)
	} else {
		m.items = rankNav(m.source(), m.query)
	}
	m.cursor = 0
	if m.query == "" {
		for i, item := range m.items {
			if item.Current {
				m.cursor = i
				break
			}
		}
	}
	return m
}

func (m navigatorModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.opts.Probes != nil {
		cmds = append(cmds, waitProbe(m.opts.Probes))
	}
	if m.opts.Refresh != nil {
		cmds = append(cmds, navTick(m.interval()))
	}
	return tea.Batch(cmds...)
}

// applyRefresh swaps in a freshly read inventory. The cursor is restored by
// item ID rather than index: a refresh reorders rows (blocked agents float to
// the top), so an index would slide the selection onto a neighbour. The query
// is untouched, so the list stays filtered as it was.
func (m navigatorModel) applyRefresh(r NavRefresh) navigatorModel {
	m.refreshErr = nil
	if !navRefreshChanged(m.opts, r) {
		return m
	}
	prevID := ""
	if m.cursor >= 0 && m.cursor < len(m.items) {
		prevID = m.items[m.cursor].ID
	}
	m.opts.Spaces, m.opts.Agents, m.opts.Sessions = r.Spaces, r.Agents, r.Sessions
	return m.refilterKeeping(prevID)
}

// refilterKeeping re-ranks the active section and puts the cursor back on id,
// falling back to refilter's default when that item is gone.
func (m navigatorModel) refilterKeeping(id string) navigatorModel {
	m = m.refilter()
	if id == "" {
		return m
	}
	for i, item := range m.items {
		if item.ID == id {
			m.cursor = i
			return m
		}
	}
	return m
}

func navRefreshChanged(o NavOptions, r NavRefresh) bool {
	return !navItemsEqual(o.Spaces, r.Spaces) ||
		!navItemsEqual(o.Agents, r.Agents) ||
		!navItemsEqual(o.Sessions, r.Sessions)
}

// navItemsEqual compares the fields the three server-backed lists populate.
// Host and the match positions are not compared: they belong to the ssh tab,
// whose inventory comes from disk and is not part of a refresh.
func navItemsEqual(a, b []NavItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Label != b[i].Label ||
			a[i].Detail != b[i].Detail || a[i].Search != b[i].Search ||
			a[i].Current != b[i].Current {
			return false
		}
	}
	return true
}

// probeMsg and probeClosedMsg carry one probe result and the channel's close
// into the bubbletea loop. waitProbe reads one result and re-arms itself,
// turning the probe channel into a stream of messages.
type probeMsg probe.Result

type probeClosedMsg struct{}

func waitProbe(ch <-chan probe.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return probeClosedMsg{}
		}
		return probeMsg(r)
	}
}

// navTickMsg fires the next inventory re-read. The read itself runs as a
// separate command so a slow herdr call cannot stall the tick loop.
type navTickMsg struct{}

// navRefreshMsg carries a freshly read inventory, and navRefreshErrMsg the
// failure to read one. A failed read keeps the last good list on screen and
// flags it, rather than emptying a popup the operator is navigating.
type navRefreshMsg NavRefresh
type navRefreshErrMsg struct{ err error }

// navTick waits d and then asks for one refresh. Ticks are re-armed when a
// refresh completes, not when it starts, so a herdr call slower than the
// interval cannot queue ticks behind it.
func navTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return navTickMsg{} })
}

func (m navigatorModel) refreshCmd() tea.Cmd {
	refresh := m.opts.Refresh
	return func() tea.Msg {
		r, err := refresh()
		if err != nil {
			return navRefreshErrMsg{err: err}
		}
		return navRefreshMsg(r)
	}
}

func (m navigatorModel) interval() time.Duration {
	if m.opts.RefreshInterval > 0 {
		return m.opts.RefreshInterval
	}
	return defaultRefreshInterval
}

func (m navigatorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case probeMsg:
		m.probed[msg.Alias] = true
		m.up[msg.Alias] = msg.Up
		return m, waitProbe(m.opts.Probes)
	case probeClosedMsg:
		return m, nil
	case navTickMsg:
		if m.opts.Refresh == nil {
			return m, nil
		}
		return m, m.refreshCmd()
	case navRefreshMsg:
		return m.applyRefresh(NavRefresh(msg)), navTick(m.interval())
	case navRefreshErrMsg:
		m.refreshErr = msg.err
		return m, navTick(m.interval())
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			break
		}
		x, y := msg.X, msg.Y
		w, h := m.size()
		if x < 0 || x >= w {
			break
		}
		if y == 1 {
			left := lipgloss.Width(frameIndent)
			for i, name := range navNames {
				right := left + len(name) + 2
				if x >= left && x < right {
					if m.section != NavSection(i) {
						m.section = NavSection(i)
						m.query = ""
						return m.refilter(), nil
					}
					return m, nil
				}
				left = right
			}
		}
		start, rows := m.listWindow()
		if y >= navHeaderRows && y < navHeaderRows+rows && start+y-navHeaderRows < len(m.items) {
			m.cursor = start + y - navHeaderRows
			return m.choose()
		}
		if y == h-2 {
			left := lipgloss.Width(frameIndent)
			if x >= left && x < left+lipgloss.Width(navJumpLabel) {
				return m.choose()
			}
			left += lipgloss.Width(navJumpLabel) + 3
			if x >= left && x < left+lipgloss.Width(navCloseLabel) {
				return m, tea.Quit
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.move(-1), nil
		case tea.MouseWheelDown:
			return m.move(1), nil
		}
	case tea.KeyPressMsg:
		if msg.Mod&tea.ModCtrl != 0 {
			return m.handleCtrl(msg)
		}
		switch msg.Code {
		case tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			return m.choose()
		case tea.KeyDown:
			return m.move(1), nil
		case tea.KeyUp:
			return m.move(-1), nil
		case tea.KeyRight, tea.KeyTab:
			delta := 1
			if msg.Mod&tea.ModShift != 0 {
				delta = -1
			}
			return m.switchSection(delta), nil
		case tea.KeyLeft:
			return m.switchSection(-1), nil
		case tea.KeyBackspace:
			if m.query != "" {
				r := []rune(m.query)
				m.query = string(r[:len(r)-1])
				return m.refilter(), nil
			}
		default:
			if msg.Text != "" {
				m.query += msg.Text
				return m.refilter(), nil
			}
		}
	}
	return m, nil
}

// handleCtrl handles every ctrl-chord key. The placement chords only mean
// anything on the ssh tab, where a host is what opens; on the other tabs they
// are ignored rather than switching the section out from under the operator.
func (m navigatorModel) handleCtrl(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Code {
	case 'c':
		return m, tea.Quit
	case 'u':
		m.query = ""
		return m.refilter(), nil
	case 'j':
		return m.move(1), nil
	case 'k':
		return m.move(-1), nil
	case 'o':
		if m.section == NavSSH {
			m.preview = !m.preview
		}
		return m, nil
	case 't':
		return m.chooseSSH("tab", false)
	case 'z':
		return m.chooseSSH("zoomed", false)
	case 'n':
		return m.chooseSSH("split", true)
	}
	return m, nil
}

func (m navigatorModel) choose() (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	sel := NavSelection{Section: m.section, Item: m.items[m.cursor]}
	if m.section == NavSSH {
		sel.Placement = "split"
	}
	m.chosen = &sel
	return m, tea.Quit
}

// chooseSSH picks the cursor host with an explicit placement. It is a no-op off
// the ssh tab, so a stray ^t on the spaces list cannot open a host that is not
// on screen.
func (m navigatorModel) chooseSSH(placement string, forceNew bool) (tea.Model, tea.Cmd) {
	if m.section != NavSSH || m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	sel := NavSelection{Section: NavSSH, Item: m.items[m.cursor], Placement: placement, ForceNew: forceNew}
	m.chosen = &sel
	return m, tea.Quit
}

func (m navigatorModel) size() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 90
	}
	if h <= 0 {
		h = 26
	}
	return w, h
}

func (m navigatorModel) listWindow() (start, rows int) {
	_, h := m.size()
	rows = max(1, h-navHeaderRows-navFooterRows-m.sshPreviewLines()-m.warningLines())
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	return start, rows
}

func (m navigatorModel) move(delta int) navigatorModel {
	if next := m.cursor + delta; next >= 0 && next < len(m.items) {
		m.cursor = next
	}
	return m
}

func (m navigatorModel) switchSection(delta int) navigatorModel {
	m.section = (m.section + NavSection(delta) + navSectionCount) % navSectionCount
	m.query = ""
	return m.refilter()
}

func (m navigatorModel) View() tea.View {
	s := newStyles(m.opts.Theme) // share the SSH picker's text and muted palette
	selected := navigatorSelectedStyle(m.opts.Theme, s.chip)
	w, _ := m.size()
	start, rows := m.listWindow()

	tabs := frameIndent
	for i, name := range navNames {
		label := " " + name + " "
		if NavSection(i) == m.section {
			tabs += selected.Render(label)
		} else {
			tabs += s.text.Render(label)
		}
	}
	lines := []string{
		"",
		tabs,
		frameIndent + rule(w-2*len(frameIndent), s.muted),
		frameIndent + s.muted.Render("/ ") + s.text.Render(m.query+"▏"),
		"",
	}
	switch {
	case len(m.items) == 0:
		lines = append(lines, frameIndent+s.muted.Render(m.emptyMessage()))
	case m.section == NavSSH:
		lines = append(lines, m.renderSSHRows(s, selected, start, rows, w)...)
	default:
		for i := start; i < len(m.items) && i < start+rows; i++ {
			item := m.items[i]
			prefix := frameIndent + "  "
			if i == m.cursor {
				prefix = frameIndent + "▸ "
			}
			if item.Current {
				prefix += "◆ "
			}
			plain := prefix + item.Label
			if item.Detail != "" {
				plain += "  " + item.Detail
			}
			plain = lipgloss.NewStyle().MaxWidth(w).Render(plain)
			if i == m.cursor {
				plain += strings.Repeat(" ", max(0, w-lipgloss.Width(plain)))
				lines = append(lines, selected.Render(plain))
			} else {
				lines = append(lines, s.text.Render(plain))
			}
		}
	}
	if m.section == NavSSH {
		lines = append(lines, m.renderSSHPreview(s)...)
	}
	// Pad after the preview so it hugs the last host row and the footer stays
	// anchored to the bottom of the popup, the way every other tab's footer is.
	for len(lines) < navHeaderRows+rows+m.sshPreviewLines() {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.footerHints(s),
		m.footerActions(s, selected),
		"",
	)
	lines = append(lines, m.renderWarnings(s)...)
	clamp := lipgloss.NewStyle().MaxWidth(w)
	for i, line := range lines {
		lines[i] = clamp.Render(line)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m navigatorModel) emptyMessage() string {
	if m.section == NavSSH {
		if len(m.opts.Hosts) == 0 {
			return "no ~/.ssh/config — nothing to pick"
		}
		return "no ssh match"
	}
	if len(m.source()) == 0 {
		return "no " + navNames[m.section] + " open"
	}
	return "no " + navNames[m.section] + " match"
}

func (m navigatorModel) footerHints(s styles) string {
	hints := "↑↓ select   ←→/tab section   ^u clear"
	if m.section == NavSSH {
		hints = "↑↓ select   ←→/tab section   ^o preview   ^u clear"
	}
	if m.refreshErr != nil {
		hints += "   ⚠ refresh failed"
	}
	return frameIndent + s.muted.Render(hints)
}

func (m navigatorModel) footerActions(s styles, selected lipgloss.Style) string {
	if m.section == NavSSH {
		return frameIndent + s.muted.Render("^t tab   ^z zoom   ^n new") +
			"   " + selected.Render(" ↵ split ") + s.muted.Render("   esc close")
	}
	return frameIndent + selected.Render(navJumpLabel) + s.muted.Render("   "+navCloseLabel)
}

// Herdr's own Settings dialog paints the accent as a background. The SSH
// picker uses reverse video, which some terminals resolve to white instead.
// Use an explicit background for RGB accents and keep the SSH fallback for
// terminal-palette color names that do not carry a portable RGB luminance.
func navigatorSelectedStyle(t theme.Theme, fallback lipgloss.Style) lipgloss.Style {
	var r, g, b int
	if _, err := fmt.Sscanf(t.Accent, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return fallback
	}
	text := "#000000"
	if 299*r+587*g+114*b < 128000 {
		text = "#ffffff"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(text)).Background(lipgloss.Color(t.Accent))
}

// RunNavigator shows the Settings-style, tabbed fuzzy picker.
func RunNavigator(o NavOptions) (NavSelection, bool, error) {
	// Herdr already paints RGB popup chrome. Its child process can inherit
	// NO_COLOR=1 from a shell, which would strip only the picker's RGB bands
	// and leave the green Herdr border. Keep this one surface consistent.
	final, err := tea.NewProgram(newNavigatorModel(o), tea.WithColorProfile(colorprofile.TrueColor)).Run()
	if err != nil {
		return NavSelection{}, false, err
	}
	m, ok := final.(navigatorModel)
	if !ok || m.chosen == nil {
		return NavSelection{}, false, nil
	}
	return *m.chosen, true, nil
}
