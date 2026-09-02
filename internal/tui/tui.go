// Package tui is the sexy Bubble Tea front-end for `atrium hub`. Three tabbed
// views (chat / perms / agents). The chat view focuses on ONE agent at a time;
// each agent has its own scrollback, unread counter, and waiting-for-input
// indicator. Ctrl-K opens the agent switcher; number keys 1-9 quick-jump.
//
// Falls back to the plain stdin TUI when --simple is passed.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dovholuknf/atrium/internal/hub"
)

// ── styling ─────────────────────────────────────────────────────────────────

var (
	colorAccent = lipgloss.Color("#7AA2F7")
	colorMuted  = lipgloss.Color("#565F89")
	colorOk     = lipgloss.Color("#9ECE6A")
	colorWarn   = lipgloss.Color("#E0AF68")
	colorErr    = lipgloss.Color("#F7768E")
	colorInfo   = lipgloss.Color("#7DCFFF")
	colorAlert  = lipgloss.Color("#FF9E64")

	styleTitle    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleMuted    = lipgloss.NewStyle().Foreground(colorMuted)
	styleOk       = lipgloss.NewStyle().Foreground(colorOk)
	styleWarn     = lipgloss.NewStyle().Foreground(colorWarn)
	styleErr      = lipgloss.NewStyle().Foreground(colorErr)
	styleInfo     = lipgloss.NewStyle().Foreground(colorInfo)
	styleAlert    = lipgloss.NewStyle().Foreground(colorAlert).Bold(true)
	styleAgent    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	stylePromptIn = lipgloss.NewStyle().Foreground(colorOk).Bold(true)

	styleTabActive   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Padding(0, 1)
	styleTabInactive = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	styleStatusBar   = lipgloss.NewStyle().Foreground(colorMuted)

	styleSwitcher       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)
	styleSwitcherSel    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Reverse(true)
	styleSwitcherNorm   = lipgloss.NewStyle()
	styleSwitcherActive = lipgloss.NewStyle().Foreground(colorAlert).Bold(true) // agent currently waiting
)

// ── views / messages ────────────────────────────────────────────────────────

type view int

const (
	viewChat view = iota
	viewPerms
	viewAgents
)

type incomingMsg struct{ Message hub.Message }
type tickMsg time.Time

// ── model ───────────────────────────────────────────────────────────────────

type agentState struct {
	msgs    []hub.Message
	unread  int
	waiting bool
	// activeChoices is the option list parsed from the most recent message's
	// {choices}...{/choices} block. Empty when the agent isn't presenting a
	// choice. Cleared when the human sends any prompt to this agent.
	activeChoices []string
	// displayName overrides the wire name in the TUI. Set via /rename. Wire
	// routing (hub.SendPrompt) still uses the real name; this is UI-only.
	displayName string
}

func (s *agentState) display(realName string) string {
	if s != nil && s.displayName != "" {
		return s.displayName
	}
	return realName
}

type model struct {
	hub *hub.Hub
	ctx context.Context

	width  int
	height int
	view   view

	input    textinput.Model
	viewport viewport.Model

	// per-agent state
	agentNames []string // stable display order = first-submit order
	agents     map[string]*agentState

	activeAgent string
	perms       []hub.PendingPermission

	// switcher overlay
	switcherOpen bool
	switcherIdx  int

	status      string
	blinkTick   int // for the "waiting" flash effect
	knownTimes  map[string]time.Time
	lastPermN   int   // last seen pending-perm count (for new-perm detection)
	lastPermIDs []int // last seen IDs so we can spot truly new ones

	// per-view cursor positions for up/down + enter selection.
	agentsCursor int
	permsCursor  int
}

func New(ctx context.Context, h *hub.Hub) *tea.Program {
	ti := textinput.New()
	ti.Placeholder = "type to send to the active agent. ctrl-k = pick agent. tab = switch view"
	ti.Prompt = stylePromptIn.Render("> ")
	ti.Focus()

	vp := viewport.New(80, 20)

	m := model{
		hub:        h,
		ctx:        ctx,
		view:       viewChat,
		input:      ti,
		viewport:   vp,
		agents:     map[string]*agentState{},
		knownTimes: map[string]time.Time{},
	}

	// Inline mode (no alt-screen): chat content is printed above the frame
	// via tea.Println and becomes normal terminal scrollback. The frame at
	// the bottom only carries the live controls. This sacrifices per-agent
	// isolated views for native scrollback / copy-paste / search.
	p := tea.NewProgram(m, tea.WithContext(ctx))

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-h.Inbox():
				p.Send(incomingMsg{Message: msg})
			}
		}
	}()

	return p
}

// ── tea.Model ───────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickEvery(400*time.Millisecond))
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		return m, nil

	case tea.KeyMsg:
		if m.switcherOpen {
			return m.handleSwitcherKey(msg)
		}
		return m.handleKey(msg)

	case incomingMsg:
		out := m.handleIncoming(msg.Message)
		// Perm-request announcements are transient UI: they belong only in the
		// floating banner, NOT in scrollback. Same for keepalives (shouldn't
		// arrive here anyway, but be defensive).
		if msg.Message.Kind == "perm-request" || msg.Message.Kind == "keepalive" {
			return m, nil
		}
		if out != "" {
			return m, tea.Println(out)
		}
		return m, nil

	case tickMsg:
		newPerms := m.hub.PendingPermissions()
		// Detect a freshly-arrived perm (an ID we haven't seen before). If
		// found, beep and stash the announcement for the status line.
		if id, ok := firstNewPermID(newPerms, m.lastPermIDs); ok {
			// BEL byte; most terminals (incl. Windows Terminal) beep on this.
			// tea.Print() routes through the program so it doesn't tear up
			// the alt-screen buffer.
			m.status = fmt.Sprintf("⚠ NEW permission #%d -- press y/n", id)
			m.knownTimes = m.hub.KnownAgents()
			m.perms = newPerms
			m.lastPermIDs = permIDs(newPerms)
			m.lastPermN = len(newPerms)
			for name, st := range m.agents {
				st.waiting = m.hub.IsWaiting(name)
			}
			m.blinkTick++
			return m, tea.Batch(tea.Printf("\a"), tickEvery(400*time.Millisecond))
		}
		m.perms = newPerms
		m.lastPermIDs = permIDs(newPerms)
		m.lastPermN = len(newPerms)
		m.knownTimes = m.hub.KnownAgents()
		for name, st := range m.agents {
			st.waiting = m.hub.IsWaiting(name)
		}
		m.blinkTick++
		return m, tickEvery(400 * time.Millisecond)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleIncoming records bookkeeping and returns the formatted message string
// that should be printed above the frame (into the terminal's scrollback). An
// empty return means "no printing needed."
func (m *model) handleIncoming(msg hub.Message) string {
	st, ok := m.agents[msg.Agent]
	if !ok {
		st = &agentState{}
		m.agents[msg.Agent] = st
		m.agentNames = append(m.agentNames, msg.Agent)
		if m.activeAgent == "" {
			m.activeAgent = msg.Agent
		}
	}
	st.msgs = append(st.msgs, msg)
	if msg.Agent != m.activeAgent {
		st.unread++
	}
	if msg.Kind != "keepalive" {
		st.waiting = true
	}
	if _, opts := extractChoices(msg.Content); len(opts) > 0 {
		st.activeChoices = opts
	}
	width := m.width
	if width < 20 {
		width = 80
	}
	display := st.display(msg.Agent)
	return renderMessage(msg, display, width)
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+k":
		m.switcherOpen = true
		m.switcherIdx = m.activeIdx()
		return m, nil
	case "tab":
		m.view = (m.view + 1) % 3
		return m, nil
	case "shift+tab":
		m.view = (m.view + 2) % 3
		return m, nil
	case "pgup":
		m.viewport.HalfViewUp()
		return m, nil
	case "pgdown":
		m.viewport.HalfViewDown()
		return m, nil
	}

	inputEmpty := strings.TrimSpace(m.input.Value()) == ""

	// View-specific arrow-key navigation. The list views (agents, perms) get
	// up/down/enter cursor selection so the user can highlight a row and act
	// on it without memorizing per-view shortcuts.
	switch m.view {
	case viewAgents:
		if inputEmpty {
			switch key {
			case "up", "k":
				if m.agentsCursor > 0 {
					m.agentsCursor--
				}
				return m, nil
			case "down", "j":
				if m.agentsCursor < len(m.agentNames)-1 {
					m.agentsCursor++
				}
				return m, nil
			case "enter":
				if m.agentsCursor >= 0 && m.agentsCursor < len(m.agentNames) {
					m.selectAgent(m.agentNames[m.agentsCursor])
				}
				return m, nil
			case "delete", "x":
				if m.agentsCursor >= 0 && m.agentsCursor < len(m.agentNames) {
					m.forgetAgent(m.agentNames[m.agentsCursor])
				}
				return m, nil
			}
		}
	case viewPerms:
		if inputEmpty {
			switch key {
			case "up", "k":
				if m.permsCursor > 0 {
					m.permsCursor--
				}
				return m, nil
			case "down", "j":
				if m.permsCursor < len(m.perms)-1 {
					m.permsCursor++
				}
				return m, nil
			case "enter", "a":
				return m.decideAt(m.permsCursor, "approve")
			case "d":
				return m.decideAt(m.permsCursor, "block")
			}
		}
	}

	if key == "enter" {
		return m.submitInput()
	}

	// Number keys (1-9) with empty input. Precedence:
	//   1. If the active agent has unresolved {choices}, pick that option.
	//   2. Otherwise quick-switch to the Nth known agent.
	if inputEmpty && len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		if st, ok := m.agents[m.activeAgent]; ok && idx < len(st.activeChoices) {
			choice := st.activeChoices[idx]
			m.hub.SendPrompt(m.activeAgent, choice)
			st.activeChoices = nil
			st.waiting = false
			m.status = fmt.Sprintf("picked: %s", choice)
			return m, nil
		}
		if idx < len(m.agentNames) {
			m.selectAgent(m.agentNames[idx])
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleSwitcherKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+k", "ctrl+c":
		m.switcherOpen = false
	case "up", "k":
		if m.switcherIdx > 0 {
			m.switcherIdx--
		}
	case "down", "j":
		if m.switcherIdx < len(m.agentNames)-1 {
			m.switcherIdx++
		}
	case "enter":
		if m.switcherIdx >= 0 && m.switcherIdx < len(m.agentNames) {
			m.selectAgent(m.agentNames[m.switcherIdx])
		}
		m.switcherOpen = false
	}
	return m, nil
}

func (m *model) activeIdx() int {
	for i, n := range m.agentNames {
		if n == m.activeAgent {
			return i
		}
	}
	return 0
}

func (m *model) selectAgent(name string) {
	m.activeAgent = name
	if st := m.agents[name]; st != nil {
		st.unread = 0
	}
	m.view = viewChat
	m.refreshChatViewport()
}

func (m *model) relayout() {
	// chrome: header(1) + tabs(1) + input(1) + status(1) + 2 newlines
	chrome := 6
	// Reserve banner space when any perm is pending (4 lines + border = ~6).
	if len(m.perms) > 0 {
		chrome += 7
	}
	if h := m.height - chrome; h > 0 {
		m.viewport.Height = h
	}
	m.viewport.Width = m.width
	m.input.Width = m.width - 4
	m.refreshChatViewport()
}

func (m *model) refreshChatViewport() {
	var b strings.Builder
	if m.activeAgent == "" {
		b.WriteString(styleMuted.Render("\n  no agents have greeted yet\n"))
	} else if st, ok := m.agents[m.activeAgent]; ok {
		width := m.viewport.Width
		if width < 20 {
			width = 80
		}
		display := st.display(m.activeAgent)
		for i, msg := range st.msgs {
			b.WriteString(renderMessage(msg, display, width))
			b.WriteString("\n")
			if i < len(st.msgs)-1 {
				b.WriteString(styleMuted.Render(strings.Repeat("─", width)))
				b.WriteString("\n")
			}
		}
	}
	m.viewport.SetContent(b.String())
	m.viewport.GotoBottom()
}

// renderMessage formats one Message for the chat viewport. The body is
// word-wrapped to `width` so a 100-line response doesn't ride off into the
// horizontal void. Headers are colored by kind to make scanning easier.
// `displayName` is what to show for the agent (wire name if no /rename alias).
func renderMessage(m hub.Message, displayName string, width int) string {
	header := styleMuted.Render(fmt.Sprintf("[%s] ", m.At.Format("15:04:05"))) +
		styleAgent.Render(displayName) + styleMuted.Render("/"+m.Kind)
	switch m.Kind {
	case "perm-request":
		header = styleWarn.Render("⚠ ") + header
	case "greeting":
		header = styleInfo.Render("◆ ") + header
	default:
		header = styleMuted.Render("· ") + header
	}
	rawBody := strings.TrimRight(m.Content, "\r\n")
	stripped, opts := extractChoices(rawBody)
	body := applySentinels(stripped)
	wrapped := wrapLines(body, width-2)
	indent := lipgloss.NewStyle().PaddingLeft(2).Render(wrapped)
	out := header + "\n" + indent
	if len(opts) > 0 {
		out += renderChoicesBox(opts)
	}
	return out
}

// wrapLines word-wraps each newline-separated line of s to at most width cols.
// Doesn't try to be smart about ANSI escapes; long runs without spaces just
// break at width. Empty result for empty input.
func wrapLines(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(wrapOne(line, width))
	}
	return out.String()
}

func wrapOne(line string, width int) string {
	if visibleLen(line) <= width {
		return line
	}
	var out strings.Builder
	words := strings.Fields(line)
	col := 0
	first := true
	for _, w := range words {
		wl := visibleLen(w)
		if !first && col+1+wl > width {
			out.WriteByte('\n')
			col = 0
			first = true
		}
		if !first {
			out.WriteByte(' ')
			col++
		}
		// hard-break a single word that's wider than the column
		if wl > width {
			for len(w) > 0 {
				take := width - col
				if take <= 0 {
					out.WriteByte('\n')
					col = 0
					take = width
				}
				if take > len(w) {
					take = len(w)
				}
				out.WriteString(w[:take])
				col += take
				w = w[take:]
				if len(w) > 0 {
					out.WriteByte('\n')
					col = 0
				}
			}
		} else {
			out.WriteString(w)
			col += wl
		}
		first = false
	}
	return out.String()
}

// visibleLen is a crude printable-width counter that ignores ANSI escapes.
// Real terminal width counting needs a unicode-aware lib (runewidth) but for
// our content this is close enough to keep wraps from being absurd.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r >= 0x40 && r <= 0x7E {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		n++
	}
	return n
}

// ── input handling ──────────────────────────────────────────────────────────

func (m model) submitInput() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	if line == "" {
		return m, nil
	}

	if line == "y" || line == "yes" || line == "n" || line == "no" {
		decision := "approve"
		if line == "n" || line == "no" {
			decision = "block"
		}
		if id, agent, ok := m.hub.DecideOldest(decision, "via "+line); ok {
			m.status = fmt.Sprintf("%s perm #%d (%s)", decision, id, agent)
		} else {
			m.status = "no pending permissions to " + decision
		}
		return m, nil
	}

	if strings.HasPrefix(line, "/") {
		return m.runCommand(line)
	}

	// In the perms view, a typed line is your free-form answer to the
	// highlighted permission: deny it and hand the text back to the agent as
	// the block reason ("no, do X instead"). claude-code surfaces that reason
	// to the model as the reason the tool was refused, so the agent course-
	// corrects. Plain approve is still y / a / enter-on-empty.
	if m.view == viewPerms && len(m.perms) > 0 {
		idx := m.permsCursor
		if idx < 0 || idx >= len(m.perms) {
			idx = 0
		}
		pp := m.perms[idx]
		if id, agent, ok := m.hub.DecideByIDString(strconv.Itoa(pp.ID), "block", line); ok {
			m.status = fmt.Sprintf("denied perm #%d (%s) with guidance", id, agent)
			if m.permsCursor >= len(m.perms)-1 && m.permsCursor > 0 {
				m.permsCursor--
			}
		} else {
			m.status = "no matching pending permission"
		}
		return m, nil
	}

	// Default routing: ACTIVE agent (the one you're viewing in chat). Explicit
	// @<name> targets override. Falls back to most-recent submitter if no
	// active agent yet. Display-name aliases set via /rename are accepted.
	rawAgent, text := parseTarget(line, m.routingDefault())
	if rawAgent == "" {
		m.status = "no agent selected -- ctrl-k to pick one once any have greeted"
		return m, nil
	}
	agent := m.resolveAgentRef(rawAgent)
	if agent == "" {
		m.status = "warn: '" + rawAgent + "' doesn't match any known agent"
		return m, nil
	}
	m.hub.SendPrompt(agent, text)
	if st := m.agents[agent]; st != nil {
		st.waiting = false
		st.activeChoices = nil
	}
	m.status = "-> " + agent
	// Echo the prompt into scrollback so the user sees their own line above
	// the upcoming agent response.
	display := agent
	if st := m.agents[agent]; st != nil && st.displayName != "" {
		display = st.displayName
	}
	echo := styleMuted.Render(fmt.Sprintf("[%s] ", time.Now().Format("15:04:05"))) +
		stylePromptIn.Render("you → "+display) + "\n  " + text
	return m, tea.Println(echo)
}

func (m model) routingDefault() string {
	if m.activeAgent != "" {
		return m.activeAgent
	}
	return m.hub.LastAgent()
}

func (m model) decideOldest(decision string) (tea.Model, tea.Cmd) {
	if id, agent, ok := m.hub.DecideOldest(decision, "via TUI"); ok {
		m.status = fmt.Sprintf("%s perm #%d (%s)", decision, id, agent)
	} else {
		m.status = "no pending permissions to " + decision
	}
	return m, nil
}

// decideAt resolves the permission at index idx in the current pending list.
// Used by the perms-view cursor navigation. Falls back to oldest if idx is
// out of range.
func (m model) decideAt(idx int, decision string) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.perms) {
		return m.decideOldest(decision)
	}
	pp := m.perms[idx]
	if id, agent, ok := m.hub.DecideByIDString(fmt.Sprintf("%d", pp.ID), decision, "via TUI"); ok {
		m.status = fmt.Sprintf("%s perm #%d (%s)", decision, id, agent)
	} else {
		m.status = "no matching pending permission"
	}
	// keep cursor in range after the list shrinks
	if m.permsCursor >= len(m.perms)-1 && m.permsCursor > 0 {
		m.permsCursor--
	}
	return m, nil
}

// forgetAgent drops a stale agent from the TUI and the hub's known list. Used
// when an old wire name persists after the agent process died and you don't
// want it cluttering /agents or the switcher.
func (m *model) forgetAgent(name string) {
	delete(m.agents, name)
	out := make([]string, 0, len(m.agentNames))
	for _, n := range m.agentNames {
		if n != name {
			out = append(out, n)
		}
	}
	m.agentNames = out
	m.hub.Forget(name)
	if m.activeAgent == name {
		m.activeAgent = ""
		if len(m.agentNames) > 0 {
			m.activeAgent = m.agentNames[0]
		}
		m.refreshChatViewport()
	}
	if m.agentsCursor >= len(m.agentNames) && m.agentsCursor > 0 {
		m.agentsCursor--
	}
	m.status = "forgot " + name
}

// splitIDReason parses the tail of a /approve or /deny command into an optional
// leading numeric id and an optional free-form reason. "3 do it differently" ->
// ("3", "do it differently"); "do it differently" -> ("", "do it differently");
// "3" -> ("3", ""); "" -> ("", ""). When no leading int is present the id is
// empty, which DecideByIDString treats as "the oldest pending".
func splitIDReason(rest string) (idStr, reason string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", ""
	}
	fields := strings.SplitN(rest, " ", 2)
	if _, err := strconv.Atoi(fields[0]); err == nil {
		idStr = fields[0]
		if len(fields) == 2 {
			reason = strings.TrimSpace(fields[1])
		}
		return idStr, reason
	}
	return "", rest
}

func parseTarget(line, def string) (string, string) {
	if !strings.HasPrefix(line, "@") {
		return def, line
	}
	rest := strings.TrimPrefix(line, "@")
	sp := strings.IndexAny(rest, " \t")
	if sp < 0 {
		return strings.TrimSpace(rest), ""
	}
	return strings.TrimSpace(rest[:sp]), strings.TrimSpace(rest[sp+1:])
}

func (m model) runCommand(line string) (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "/"))
	cmd, rest := body, ""
	if sp := strings.IndexAny(body, " \t"); sp >= 0 {
		cmd, rest = body[:sp], strings.TrimSpace(body[sp+1:])
	}
	switch cmd {
	case "approve":
		idStr, reason := splitIDReason(rest)
		if reason == "" {
			reason = "via /approve"
		}
		if id, agent, ok := m.hub.DecideByIDString(idStr, "approve", reason); ok {
			m.status = fmt.Sprintf("approve perm #%d (%s)", id, agent)
		} else {
			m.status = "no matching pending permission"
		}
	case "deny":
		idStr, reason := splitIDReason(rest)
		if reason == "" {
			reason = "via /deny"
		}
		if id, agent, ok := m.hub.DecideByIDString(idStr, "block", reason); ok {
			m.status = fmt.Sprintf("deny perm #%d (%s)", id, agent)
		} else {
			m.status = "no matching pending permission"
		}
	case "agents":
		m.view = viewAgents
	case "perms":
		m.view = viewPerms
	case "chat":
		m.view = viewChat
	case "pick", "k":
		m.switcherOpen = true
		m.switcherIdx = m.activeIdx()
	case "rename":
		// /rename <wire-or-display> <new-display>
		// Sets a UI-only alias. Wire routing is unchanged. Pass an empty new
		// name to clear the alias.
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) < 1 || parts[0] == "" {
			m.status = "usage: /rename <agent> <new name>"
			return m, nil
		}
		target := m.resolveAgentRef(parts[0])
		if target == "" {
			m.status = "no agent matches '" + parts[0] + "'"
			return m, nil
		}
		st := m.agents[target]
		if st == nil {
			m.status = "no state for '" + target + "'"
			return m, nil
		}
		newName := ""
		if len(parts) == 2 {
			newName = strings.TrimSpace(parts[1])
		}
		st.displayName = newName
		if newName == "" {
			m.status = "cleared alias on " + target
		} else {
			m.status = "renamed " + target + " -> " + newName
		}
		if target == m.activeAgent {
			m.refreshChatViewport()
		}
	case "help", "?":
		m.status = "tab=view | ctrl-k=pick | 1-9=switch | y/n=approve/deny oldest | in perms: type guidance+enter=deny w/ instructions | /approve [id] [why] /deny [id] [why] /rename"
	default:
		m.status = "unknown command '/" + cmd + "'"
	}
	return m, nil
}

// resolveAgentRef takes a name typed by the user and returns the wire name it
// refers to. Match priority: exact wire name, then exact display name, then
// prefix match on either, then nothing.
func (m model) resolveAgentRef(ref string) string {
	if _, ok := m.agents[ref]; ok {
		return ref
	}
	for wire, st := range m.agents {
		if st != nil && st.displayName == ref {
			return wire
		}
	}
	// prefix
	for _, wire := range m.agentNames {
		if strings.HasPrefix(wire, ref) {
			return wire
		}
	}
	for wire, st := range m.agents {
		if st != nil && st.displayName != "" && strings.HasPrefix(st.displayName, ref) {
			return wire
		}
	}
	return ""
}

// ── rendering ───────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	// Inline mode: the frame is a small bottom panel. Chat messages live in
	// the terminal's native scrollback (printed via tea.Println in Update).
	// Perm-requests and other transient UI live ONLY in this floating frame.
	var b strings.Builder

	// Loud perm banner at the TOP of the frame (so it's adjacent to the
	// scrollback boundary -- catches the eye).
	if banner := m.renderPermBanner(); banner != "" {
		b.WriteString(banner)
		b.WriteString("\n")
	}

	// Perms / agents views render a small list inside the frame. Chat view
	// renders nothing in the middle -- the conversation IS the scrollback.
	switch m.view {
	case viewPerms:
		b.WriteString(m.renderPerms())
		b.WriteString("\n")
	case viewAgents:
		b.WriteString(m.renderAgents())
		b.WriteString("\n")
	}

	// Fixed bottom panel: header (active agent), tabs, input, status.
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(m.renderStatus())

	if m.switcherOpen {
		return overlay(b.String(), m.renderSwitcher(), m.width, m.height)
	}
	return b.String()
}

// renderPermBanner draws a multi-line, flashing, bordered box across the top
// of every view when one or more permission requests are pending. The first
// pending perm is shown in full; others are summarized. Designed to be
// genuinely unmissable.
func (m model) renderPermBanner() string {
	if len(m.perms) == 0 {
		return ""
	}
	bg := colorAlert
	if m.blinkTick%2 == 0 {
		bg = colorErr
	}
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1A1B26")).
		Background(bg).
		Bold(true).
		Padding(0, 2).
		Border(lipgloss.ThickBorder()).
		BorderForeground(bg).
		Width(m.width - 2)

	first := m.perms[0]
	totalLine := fmt.Sprintf("⚠  PERMISSION PENDING  ─  %d total", len(m.perms))
	if len(m.perms) > 1 {
		totalLine += fmt.Sprintf("   (showing oldest; %d more queued -- see perms tab)", len(m.perms)-1)
	}
	detail := fmt.Sprintf("#%d  %s  wants  %s", first.ID, first.Agent, first.Tool)
	cmd := truncate(first.Command, m.width-10)
	hint := "press [y] approve  ·  [n] deny  ·  [tab] → perms tab"
	body := strings.Join([]string{totalLine, detail, "  " + cmd, hint}, "\n")
	return bannerStyle.Render(body)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// firstNewPermID compares the current pending list against the last-seen
// IDs and returns the ID of the first NEW perm if any. Used to fire the
// boop + status nudge exactly once per arrival, not on every tick.
func firstNewPermID(curr []hub.PendingPermission, seen []int) (int, bool) {
	known := map[int]bool{}
	for _, id := range seen {
		known[id] = true
	}
	for _, p := range curr {
		if !known[p.ID] {
			return p.ID, true
		}
	}
	return 0, false
}

func permIDs(pp []hub.PendingPermission) []int {
	out := make([]int, len(pp))
	for i, p := range pp {
		out[i] = p.ID
	}
	return out
}

// renderHeader is the top line: app name + identity of the active agent.
// Two-line header (this + renderTabs) makes the agent feel like the PARENT of
// the chat/perms views, not a sibling tab.
func (m model) renderHeader() string {
	title := styleTitle.Render(" atrium ")
	if m.activeAgent == "" {
		return title + styleMuted.Render("│ ") + styleMuted.Render("no agent selected -- waiting for first greeting")
	}
	st := m.agents[m.activeAgent]
	display := st.display(m.activeAgent)
	identStyle := styleAgent
	if st != nil && st.waiting && m.blinkTick%2 == 0 {
		identStyle = styleAlert
	}
	ident := identStyle.Render(display)
	if display != m.activeAgent {
		ident += styleMuted.Render(" (" + m.activeAgent + ")")
	}
	suffix := ""
	if st != nil && st.waiting {
		suffix += styleAlert.Render("  ← waiting")
	}
	if other := m.totalUnread() - m.activeUnread(); other > 0 {
		suffix += styleWarn.Render(fmt.Sprintf("  (+%d unread elsewhere)", other))
	}
	if len(m.agentNames) > 1 {
		suffix += styleMuted.Render(fmt.Sprintf("  ·  %d agents total (ctrl-k to switch)", len(m.agentNames)))
	}
	return title + styleMuted.Render("│ ") + ident + suffix
}

func (m model) renderTabs() string {
	tabs := []string{"chat", "perms", "all agents"}
	var out []string
	for i, t := range tabs {
		label := t
		switch i {
		case int(viewPerms):
			if len(m.perms) > 0 {
				// flash count cell in red on top of yellow base
				countStyle := styleWarn
				if m.blinkTick%2 == 0 {
					countStyle = styleErr
				}
				label = t + countStyle.Render(fmt.Sprintf(" (%d!)", len(m.perms)))
			}
		case int(viewAgents):
			if w := m.waitingCount(); w > 0 {
				label = fmt.Sprintf("%s [%d waiting]", t, w)
			}
		}
		if int(m.view) == i {
			out = append(out, styleTabActive.Render(label))
		} else {
			out = append(out, styleTabInactive.Render(label))
		}
	}
	return styleMuted.Render("   ") + strings.Join(out, styleMuted.Render("│"))
}

func (m model) renderPerms() string {
	if len(m.perms) == 0 {
		return styleMuted.Render("\n  no pending permissions\n")
	}
	var b strings.Builder
	b.WriteString("\n")
	for i, p := range m.perms {
		cursor := "  "
		if i == m.permsCursor {
			cursor = styleOk.Render("> ")
		}
		idStyle := styleWarn
		if i == m.permsCursor {
			idStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		b.WriteString(cursor + idStyle.Render(fmt.Sprintf("#%d  ", p.ID)))
		b.WriteString(styleAgent.Render(p.Agent))
		b.WriteString(styleMuted.Render(fmt.Sprintf("  %s  ", p.At.Format("15:04:05"))))
		b.WriteString(styleInfo.Render("(" + p.Tool + ")"))
		b.WriteString("\n      ")
		b.WriteString(p.Command)
		b.WriteString("\n\n")
	}
	b.WriteString(styleMuted.Render("  ↑/↓ move · enter/a approve · d deny · type a message + enter = deny WITH that guidance · y/n = oldest\n"))
	return b.String()
}

func (m model) renderAgents() string {
	if len(m.agentNames) == 0 {
		return styleMuted.Render("\n  no agents have submitted yet\n")
	}
	var b strings.Builder
	b.WriteString("\n")
	for i, name := range m.agentNames {
		st := m.agents[name]
		idx := fmt.Sprintf("%d. ", i+1)
		// Two distinct markers: cursor highlight (>) and active-agent marker (*)
		marker := "  "
		if i == m.agentsCursor {
			marker = styleOk.Render("> ")
		}
		activeMark := "  "
		if name == m.activeAgent {
			activeMark = styleAccent("●")
		}
		b.WriteString(marker + activeMark + " " + styleMuted.Render(idx))
		nameStyle := styleAgent
		if st != nil && st.waiting && m.blinkTick%2 == 0 {
			nameStyle = styleAlert
		}
		if i == m.agentsCursor {
			nameStyle = nameStyle.Underline(true)
		}
		shown := name
		if st != nil && st.displayName != "" {
			shown = st.displayName
		}
		b.WriteString(nameStyle.Render(shown))
		if shown != name {
			b.WriteString(styleMuted.Render("  (" + name + ")"))
		}
		if st != nil && st.unread > 0 {
			b.WriteString(styleWarn.Render(fmt.Sprintf("  (%d unread)", st.unread)))
		}
		if st != nil && st.waiting {
			b.WriteString(styleAlert.Render("  ← waiting"))
		}
		if t, ok := m.knownTimes[name]; ok {
			b.WriteString(styleMuted.Render(fmt.Sprintf("   last %s", t.Format("15:04:05"))))
		}
		b.WriteString("\n")
	}
	b.WriteString(styleMuted.Render("\n  ↑/↓ to move, enter to view chat, 'x' or 'delete' to forget. ● = active. ctrl-k for picker.\n"))
	return b.String()
}

// styleAccent renders s in the accent color. Helper for inline usage.
func styleAccent(s string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Render(s)
}

func (m model) renderStatus() string {
	hint := "tab=view  ctrl-k=pick  enter=send  y/n=perm  ctrl-c=quit"
	if m.view == viewPerms {
		hint = "a=approve  d=deny  type+enter=deny w/ guidance  " + hint
	}
	if m.status != "" {
		return styleStatusBar.Render(" "+m.status+"  │  ") + styleMuted.Render(hint)
	}
	return styleMuted.Render(" " + hint)
}

func (m model) renderSwitcher() string {
	if len(m.agentNames) == 0 {
		return styleSwitcher.Render(" no agents to pick yet ")
	}
	var lines []string
	lines = append(lines, styleTitle.Render(" pick agent  ")+styleMuted.Render("(↑/↓, enter, esc)"))
	for i, n := range sortedSwitcherNames(m.agentNames) {
		st := m.agents[n]
		shown := n
		if st != nil && st.displayName != "" {
			shown = fmt.Sprintf("%s (%s)", st.displayName, n)
		}
		row := fmt.Sprintf(" %d. %s", i+1, shown)
		if st != nil && st.waiting {
			row += "  ← waiting"
		}
		if st != nil && st.unread > 0 {
			row += fmt.Sprintf("  (%d unread)", st.unread)
		}
		if i == m.switcherIdx {
			lines = append(lines, styleSwitcherSel.Render(row))
		} else if st != nil && st.waiting {
			lines = append(lines, styleSwitcherActive.Render(row))
		} else {
			lines = append(lines, styleSwitcherNorm.Render(row))
		}
	}
	return styleSwitcher.Render(strings.Join(lines, "\n"))
}

func sortedSwitcherNames(names []string) []string {
	// keep insertion order; copy to avoid aliasing
	out := make([]string, len(names))
	copy(out, names)
	return out
}

// overlay centers a small box on top of the main view. Crude but readable.
func overlay(base, box string, w, h int) string {
	if w == 0 || h == 0 {
		return base + "\n" + box
	}
	// Render the base as-is, then append the box on top with newlines. A real
	// compositor would slice base into lines; this is the cheap version that
	// works fine for an MVP.
	return base + "\n\n" + lipgloss.PlaceHorizontal(w, lipgloss.Center, box)
}

// ── counters ────────────────────────────────────────────────────────────────

func (m model) totalUnread() int {
	n := 0
	for _, st := range m.agents {
		n += st.unread
	}
	return n
}

func (m model) activeUnread() int {
	if st, ok := m.agents[m.activeAgent]; ok {
		return st.unread
	}
	return 0
}

func (m model) waitingCount() int {
	n := 0
	for _, st := range m.agents {
		if st.waiting {
			n++
		}
	}
	return n
}

// ── {choices}...{/choices} parsing ──────────────────────────────────────────

// extractChoices pulls out the first {choices}...{/choices} block from s and
// returns (stripped-content, options). Options are lines between the markers,
// trimmed; blank lines and lines with only "-" / "*" bullets are filtered.
// If no block is present, returns (s, nil).
func extractChoices(s string) (string, []string) {
	const open, close = "{choices}", "{/choices}"
	openIdx := strings.Index(s, open)
	if openIdx < 0 {
		return s, nil
	}
	rest := s[openIdx+len(open):]
	closeIdx := strings.Index(rest, close)
	if closeIdx < 0 {
		// unterminated block: leave content alone
		return s, nil
	}
	block := rest[:closeIdx]
	tail := rest[closeIdx+len(close):]
	stripped := s[:openIdx] + tail
	// Tidy stripped: collapse extra blank lines created by the removal.
	stripped = strings.TrimRight(stripped, "\n")

	var opts []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		// allow optional bullet/number prefixes from the model
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")
		if line == "" {
			continue
		}
		opts = append(opts, line)
	}
	return stripped, opts
}

// renderChoicesBox returns a styled block listing the options with numbers,
// suitable for appending to a message's body in the chat view.
func renderChoicesBox(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleInfo.Render("  ┌─ choices ─────────"))
	b.WriteString("\n")
	for i, o := range opts {
		num := fmt.Sprintf("%d", i+1)
		b.WriteString(styleInfo.Render("  │ "))
		b.WriteString(styleAgent.Render(num + ") "))
		b.WriteString(o)
		b.WriteString("\n")
	}
	b.WriteString(styleInfo.Render("  └─ press 1-"))
	b.WriteString(styleAgent.Render(fmt.Sprintf("%d", len(opts))))
	b.WriteString(styleInfo.Render(" to pick (or type your own reply)"))
	return b.String()
}

// ── sentinel translation ────────────────────────────────────────────────────

var sentinelMap = map[string]string{
	"{reset}": "\x1b[0m", "{bold}": "\x1b[1m", "{dim}": "\x1b[2m", "{underline}": "\x1b[4m",
	"{black}": "\x1b[30m", "{red}": "\x1b[31m", "{green}": "\x1b[32m", "{yellow}": "\x1b[33m",
	"{blue}": "\x1b[34m", "{magenta}": "\x1b[35m", "{cyan}": "\x1b[36m", "{white}": "\x1b[37m",
	"{gray}":    "\x1b[90m",
	"{bgblack}": "\x1b[40m", "{bgred}": "\x1b[41m", "{bggreen}": "\x1b[42m", "{bgyellow}": "\x1b[43m",
	"{bgblue}": "\x1b[44m", "{bgmagenta}": "\x1b[45m", "{bgcyan}": "\x1b[46m", "{bgwhite}": "\x1b[47m",
}

func applySentinels(s string) string {
	if !strings.Contains(s, "{") {
		return s
	}
	for token, ansi := range sentinelMap {
		s = strings.ReplaceAll(s, token, ansi)
	}
	if !strings.HasSuffix(s, "\x1b[0m") {
		s += "\x1b[0m"
	}
	return s
}
