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

const (
	// defaultPreviewLines is how many lines of agent output the agents tab
	// preview shows, and how many it reserves so the list does not jump as a
	// read lands.
	defaultPreviewLines = 8
	// defaultPreviewDebounce is the pause after a cursor move before the read
	// fires, long enough to skip the rows a held ↓ passes through.
	defaultPreviewDebounce = 150 * time.Millisecond
)

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

	// WorkspaceID and CWD are the owning workspace and working directory, for
	// the ^x actions that need them: a new tab in the same workspace, or opening
	// the worktree the agent runs in. Both are empty on tabs and spaces.
	WorkspaceID string
	CWD         string
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

	// AgentRead fetches the tail of an agent pane's terminal output for the
	// agents tab preview. nil disables the preview.
	AgentRead func(paneID string, lines int) (string, error)
	// AgentPrompt submits text to an agent and reports the agent's state after
	// herdr observed it settle. nil disables the ^p prompt affordance.
	AgentPrompt func(paneID, text string) (status string, err error)
	// PreviewLines is how many lines of agent output the preview requests and
	// reserves. Zero means defaultPreviewLines.
	PreviewLines int
	// PreviewDebounce delays the agent read after the cursor moves, so holding
	// ↓ does not fire one herdr subprocess per row. Zero means
	// defaultPreviewDebounce.
	PreviewDebounce time.Duration

	// Actions builds the ^x menu for a row, or nil to disable the menu. The
	// picker owns the menu, input, and confirm UI; the action list and what each
	// action does live with the caller.
	Actions func(section NavSection, item NavItem) []NavAction
	// RunAction executes actionID for a row, with text for the actions that
	// collect it, and returns a short status line for the footer.
	RunAction func(section NavSection, item NavItem, actionID, text string) (string, error)

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
	// Marked, when non-empty, are the ssh hosts the operator marked with space
	// and asked to open together. Item is still the cursor row, so a caller that
	// ignores Marked keeps the single-selection behavior.
	Marked []NavItem
	// Placement and ForceNew apply only to NavSSH: how the chosen hosts open.
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
	probed  map[string]bool
	up      map[string]bool
	latency map[string]time.Duration
	// marked is the ssh tab's multi-select set, keyed by alias. It is a map so
	// writes are visible through every copy of the model that shares it, like
	// probed and up above.
	marked map[string]bool
	chosen *NavSelection
	// refreshErr is the last failed inventory read, kept so the footer can say
	// the list on screen is stale rather than silently pretending it is current.
	refreshErr error

	// agentPreview is the tail of the selected agent's output; agentPreviewFor
	// is the pane id it belongs to, so a read that lands after the cursor moved
	// is recognized as stale. agentPreviewErr is that read's failure.
	agentPreview    string
	agentPreviewFor string
	agentPreviewErr error

	// menuActions is the ^x action list for the row it was opened on, and
	// menuCursor the highlighted action. menuItem and menuSection capture the row
	// so a refresh moving the cursor cannot retarget a chosen action.
	menuActions []NavAction
	menuCursor  int
	menuItem    NavItem
	menuSection NavSection

	// inputOpen is a one-line text entry: an agent prompt when inputAction is
	// empty, otherwise a rename action awaiting its text. The row and section are
	// captured when it opens so a cursor move mid-typing cannot redirect it.
	inputOpen    bool
	inputLabel   string
	inputText    string
	inputTarget  string
	inputAction  string
	inputItem    NavItem
	inputSection NavSection

	// confirmOpen is an action awaiting a y/n, captured with the row it applies
	// to. confirmQuestion is what the footer asks.
	confirmOpen     bool
	confirmAction   NavAction
	confirmItem     NavItem
	confirmSection  NavSection
	confirmQuestion string

	// note is the last prompt or action outcome, scoped to noteFor so it is not
	// shown under a different row. noteErr selects the warning styling and keeps
	// a failure visible even after the cursor moves on.
	note    string
	noteFor string
	noteErr bool
}

func newNavigatorModel(o NavOptions) navigatorModel {
	m := navigatorModel{
		opts:    o,
		preview: o.ShowPreview,
		probed:  map[string]bool{},
		up:      map[string]bool{},
		latency: map[string]time.Duration{},
		marked:  map[string]bool{},
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

// navAgentReadMsg carries one agent output read, tagged with the pane it was
// requested for so a late result can be recognized as stale. navPromptMsg and
// navPromptErrMsg carry a submitted prompt's outcome.
type navAgentReadMsg struct {
	target string
	text   string
	err    error
}
type navPromptMsg struct {
	target string
	status string
}
type navPromptErrMsg struct {
	target string
	err    error
}

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
	next, cmd := m.update(msg)
	m = next.(navigatorModel)
	// One place watches the selected agent change and starts a preview read,
	// rather than every cursor-moving branch remembering to.
	if preview := m.startAgentPreview(); preview != nil {
		cmd = tea.Batch(cmd, preview)
	}
	return m, cmd
}

// startAgentPreview mutates Update's local model and returns the debounced read
// to run when the selected agent differs from the one already shown. It records
// the target immediately, so the tick is not re-armed on every message while the
// cursor sits still, and a result for a target the cursor has left is discarded
// on arrival.
func (m *navigatorModel) startAgentPreview() tea.Cmd {
	if m.section != NavAgents || m.opts.AgentRead == nil {
		return nil
	}
	target := m.selectedItemID()
	if target == "" || target == m.agentPreviewFor {
		return nil
	}
	m.agentPreviewFor = target
	m.agentPreview = ""
	m.agentPreviewErr = nil
	read := m.opts.AgentRead
	lines := m.agentPreviewRequestLines()
	delay := m.opts.PreviewDebounce
	if delay <= 0 {
		delay = defaultPreviewDebounce
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		text, err := read(target, lines)
		return navAgentReadMsg{target: target, text: text, err: err}
	})
}

// update is the real message handler. Update wraps it so the preview watch runs
// once, after every message, instead of in each branch.
func (m navigatorModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case probeMsg:
		m.probed[msg.Alias] = true
		m.up[msg.Alias] = msg.Up
		m.latency[msg.Alias] = msg.Latency
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
	case navAgentReadMsg:
		if msg.target != m.agentPreviewFor {
			return m, nil // the cursor moved on; this read is stale
		}
		m.agentPreview = msg.text
		m.agentPreviewErr = msg.err
		return m, nil
	case navPromptMsg:
		m.noteFor = msg.target
		m.note = "sent · " + msg.status
		m.noteErr = false
		m.agentPreviewFor = "" // force a re-read now the agent has new output
		return m, nil
	case navPromptErrMsg:
		m.noteFor = msg.target
		m.note = oneLine(msg.err.Error())
		m.noteErr = true
		return m, nil
	case navActionMsg:
		m.noteFor = msg.forID
		m.note = msg.status
		m.noteErr = false
		return m, nil
	case navActionErrMsg:
		m.noteFor = msg.forID
		m.note = oneLine(msg.err.Error())
		m.noteErr = true
		return m, nil
	case tea.MouseClickMsg:
		if m.inputOpen || m.confirmOpen || m.menuActions != nil || msg.Button != tea.MouseLeft {
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
		switch {
		case m.confirmOpen:
			return m.confirmKey(msg)
		case m.inputOpen:
			return m.inputKey(msg)
		case m.menuActions != nil:
			return m.menuKey(msg)
		}
		if msg.Mod&tea.ModCtrl != 0 {
			return m.handleCtrl(msg)
		}
		// Space marks on the ssh tab. It is not a useful query character there —
		// ssh aliases are whitespace-separated, so a space in the query can never
		// match — which is what makes it safe to take for marking.
		if m.section == NavSSH && msg.Code == tea.KeySpace {
			return m.toggleMark(), nil
		}
		switch msg.Code {
		case tea.KeyEsc:
			// Esc clears marks before it closes, so a mistaken space does not cost
			// the operator the popup.
			if m.section == NavSSH && len(m.marked) > 0 {
				m.marked = map[string]bool{}
				return m, nil
			}
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
	case 'p':
		return m.openPromptInput()
	case 'x':
		return m.openMenu()
	}
	return m, nil
}

func (m navigatorModel) promptCmd(target, text string) tea.Cmd {
	prompt := m.opts.AgentPrompt
	return func() tea.Msg {
		status, err := prompt(target, text)
		if err != nil {
			return navPromptErrMsg{target: target, err: err}
		}
		return navPromptMsg{target: target, status: status}
	}
}

func (m navigatorModel) choose() (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	sel := NavSelection{Section: m.section, Item: m.items[m.cursor]}
	if m.section == NavSSH {
		sel.Placement = "split"
		sel.Marked = m.markedHosts()
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
	sel.Marked = m.markedHosts()
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
	rows = max(1, h-navHeaderRows-navFooterRows-m.previewBlockLines()-m.warningLines())
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	return start, rows
}

// previewBlockLines is the rows the active section's preview occupies. Only the
// ssh and agents tabs have one.
func (m navigatorModel) previewBlockLines() int {
	switch m.section {
	case NavSSH:
		return m.sshPreviewLines()
	case NavAgents:
		return m.agentPreviewLines()
	default:
		return 0
	}
}

func (m navigatorModel) renderPreviewBlock(s styles) []string {
	switch m.section {
	case NavSSH:
		return m.renderSSHPreview(s)
	case NavAgents:
		return m.renderAgentPreview(s)
	default:
		return nil
	}
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
	case m.menuActions != nil:
		lines = append(lines, m.renderMenu(s, selected, rows, w)...)
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
	if preview := m.renderPreviewBlock(s); len(preview) > 0 {
		lines = append(lines, preview...)
	}
	// Pad after the preview so it hugs the last row and the footer stays
	// anchored to the bottom of the popup, the way every other tab's footer is.
	for len(lines) < navHeaderRows+rows+m.previewBlockLines() {
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
	switch {
	case m.confirmOpen:
		return frameIndent + s.muted.Render("y confirm   n cancel")
	case m.menuActions != nil:
		return frameIndent + s.muted.Render("↑↓ choose   ↵ run   esc cancel")
	case m.inputOpen:
		// The action line carries the prompt, so the hints stay to the keys.
		return frameIndent + s.muted.Render("↵ send   esc cancel")
	}
	hints := "↑↓ select   ←→/tab section   ^u clear"
	if m.section == NavSSH {
		hints = "↑↓ select   ←→/tab section   ^o preview   space mark   ^u clear"
	}
	if m.section == NavSSH && len(m.marked) > 0 {
		hints = fmt.Sprintf("%d marked   space toggle   ↵ open all   esc clear", len(m.marked))
	}
	if m.note != "" && (m.noteErr || m.noteFor == m.selectedItemID()) {
		mark := "✓ "
		if m.noteErr {
			mark = "⚠ "
		}
		hints += "   " + mark + m.note
	}
	if m.refreshErr != nil {
		hints += "   ⚠ refresh failed"
	}
	return frameIndent + s.muted.Render(hints)
}

func (m navigatorModel) footerActions(s styles, selected lipgloss.Style) string {
	if m.inputOpen {
		return frameIndent + s.accent.Render(m.inputLabel+" ") + s.text.Render(m.inputText+"▏")
	}
	if m.confirmOpen {
		return frameIndent + s.muted.Render(m.confirmQuestion)
	}
	if m.menuActions != nil {
		return frameIndent + selected.Render(" ↵ run ") + s.muted.Render("   esc cancel")
	}
	if m.section == NavSSH {
		action := " ↵ split "
		closeLabel := "esc close"
		if len(m.marked) > 0 {
			action = fmt.Sprintf(" ↵ open %d ", len(m.marked))
			closeLabel = "esc clear"
		}
		return frameIndent + s.muted.Render("^t tab   ^z zoom   ^n new") +
			"   " + selected.Render(action) + s.muted.Render("   "+closeLabel)
	}
	actions := ""
	if m.opts.Actions != nil {
		actions = s.muted.Render("^x actions") + "   "
	}
	if m.section == NavAgents && m.opts.AgentPrompt != nil {
		return frameIndent + actions + s.muted.Render("^p prompt") + "   " +
			selected.Render(navJumpLabel) + s.muted.Render("   "+navCloseLabel)
	}
	return frameIndent + actions + selected.Render(navJumpLabel) + s.muted.Render("   "+navCloseLabel)
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
