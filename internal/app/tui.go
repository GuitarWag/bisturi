package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/GuitarWag/bisturi/internal/session"
)

// runTUI shows the selector and, if the user applies, performs the cut after
// the alt-screen has been torn down (so normal stdout is clean).
func runTUI(sess *session.Session) int {
	m := newModel(sess)
	p := tea.NewProgram(m, tea.WithAltScreen())
	fm, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return 1
	}
	final := fm.(model)
	if !final.apply || len(final.selected) == 0 {
		fmt.Println("nothing cut.")
		return 0
	}
	// The diff view is the review step, so applying commits to the real file
	// (a .bak-* backup and a restorable surgery are still written).
	return applyCut(sess, final.selected, true, false)
}

type focus int

const (
	focusList focus = iota
	focusExpand
	focusDiff
)

type model struct {
	sess     *session.Session
	cursor   int
	selected map[int]bool
	focus    focus
	vp       viewport.Model
	w, h     int
	ready    bool
	apply    bool
}

func newModel(sess *session.Session) model {
	return model{sess: sess, selected: map[int]bool{}}
}

func (m model) Init() tea.Cmd { return nil }

// --- styles ---

var (
	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	stStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	stCut    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	stCursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("237"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	stKey    = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	stRole   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	stFooter = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	stWarn   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	stHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("24")).Padding(0, 1)
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New(msg.Width, m.bodyHeight())
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = m.bodyHeight()
		}
		if m.focus == focusExpand {
			m.vp.SetContent(m.expandContent())
		} else if m.focus == focusDiff {
			m.vp.SetContent(m.diffContent())
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.focus == focusExpand || m.focus == focusDiff {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusExpand:
		switch msg.String() {
		case "q", "esc", "enter", "backspace", "left":
			m.focus = focusList
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	case focusDiff:
		switch msg.String() {
		case "y", "enter":
			m.apply = true
			return m, tea.Quit
		case "q", "esc", "backspace", "left", "d":
			m.focus = focusList
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	// focusList
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.sess.Turns)-1 {
			m.cursor++
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.sess.Turns) - 1
	case " ", "x":
		n := m.sess.Turns[m.cursor].Number
		if m.selected[n] {
			delete(m.selected, n)
		} else {
			m.selected[n] = true
		}
	case "a":
		for _, t := range m.sess.Turns {
			m.selected[t.Number] = true
		}
	case "n", "A":
		m.selected = map[int]bool{}
	case "enter", "right", "l":
		m.focus = focusExpand
		m.vp.SetContent(m.expandContent())
		m.vp.GotoTop()
	case "d":
		if len(m.selected) > 0 {
			m.focus = focusDiff
			m.vp.SetContent(m.diffContent())
			m.vp.GotoTop()
		}
	case "y":
		if len(m.selected) > 0 {
			m.apply = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) bodyHeight() int {
	// header (1) + status (1) + blank (1) + footer (1) reserved
	h := m.h - 4
	if h < 3 {
		h = 3
	}
	return h
}

func (m model) View() string {
	if !m.ready {
		return "loading…"
	}
	switch m.focus {
	case focusExpand:
		return m.chrome(m.vp.View(), "expanded — "+m.sess.Turns[m.cursor].Title(40),
			"↑/↓ scroll · q/enter back")
	case focusDiff:
		return m.chrome(m.vp.View(), "diff preview",
			stCut.Render("y")+" apply  · "+stKey.Render("q")+" back · ↑/↓ scroll")
	default:
		return m.listView()
	}
}

func (m model) chrome(body, title, help string) string {
	header := stHeader.Render("ctx-bisturi") + " " + stTitle.Render(title)
	footer := stFooter.Render(help)
	return header + "\n" + body + "\n" + footer
}

func (m model) listView() string {
	total := m.sess.TotalTokens()
	kept := 0
	for _, t := range m.sess.Turns {
		if !m.selected[t.Number] {
			kept += t.TokenEstimate()
		}
	}
	header := stHeader.Render("ctx-bisturi") + " " +
		stDim.Render(fmt.Sprintf("%s · %d blocks · ~%d tok",
			short(sessionID(m.sess.Path)), len(m.sess.Turns), total))

	status := stStatus.Render(fmt.Sprintf(" cutting %d → keeps ~%d/%d tok (~%d removed)",
		len(m.selected), kept, total, total-kept))
	if isLiveWarn(m.sess.Path) {
		status += "  " + stWarn.Render("[live session — applies on next resume]")
	}

	var b strings.Builder
	b.WriteString(header + "\n" + status + "\n\n")

	rows := m.bodyHeight()
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := start + rows
	if end > len(m.sess.Turns) {
		end = len(m.sess.Turns)
	}
	for i := start; i < end; i++ {
		b.WriteString(m.rowLine(i) + "\n")
	}

	footer := stFooter.Render("↑/↓ move · ") + stKey.Render("space") +
		stFooter.Render(" select · ") + stKey.Render("a") + stFooter.Render(" all · ") +
		stKey.Render("n") + stFooter.Render(" none · ") + stKey.Render("enter") +
		stFooter.Render(" expand · ") + stKey.Render("d") + stFooter.Render(" diff · ") +
		stKey.Render("y") + stFooter.Render(" apply · ") + stKey.Render("q") + stFooter.Render(" quit")
	return b.String() + "\n" + footer
}

func (m model) rowLine(i int) string {
	t := m.sess.Turns[i]
	mark := "[ ]"
	if m.selected[t.Number] {
		mark = "[x]"
	}
	ts := tsShort(t.Timestamp())
	if len(ts) >= 16 {
		ts = ts[11:16]
	}
	detail := t.Detail()
	titleW := m.w - 30
	if titleW < 10 {
		titleW = 10
	}
	line := fmt.Sprintf(" %s %2d. %-5s ~%6dt  %s", mark, t.Number, ts, t.TokenEstimate(), t.Title(titleW))
	if detail != "" {
		line += stDim.Render("  (" + detail + ")")
	}
	switch {
	case i == m.cursor:
		return stCursor.Render(fmt.Sprintf("%-*s", m.w, trimANSI(line)))
	case m.selected[t.Number]:
		return stCut.Render(line)
	default:
		return line
	}
}

func (m model) expandContent() string {
	t := m.sess.Turns[m.cursor]
	var b strings.Builder
	b.WriteString(stDim.Render(fmt.Sprintf("block %d · %s · ~%d tok · %d entries\n\n",
		t.Number, t.Timestamp(), t.TokenEstimate(), len(t.Entries))))
	for _, line := range t.PreviewLines() {
		if strings.HasPrefix(line, "▸ ") {
			b.WriteString(stRole.Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

func (m model) diffContent() string {
	total := m.sess.TotalTokens()
	var removed int
	var b strings.Builder
	b.WriteString(stTitle.Render("Blocks to remove:") + "\n\n")
	for _, t := range m.sess.Turns {
		if !m.selected[t.Number] {
			continue
		}
		removed += t.TokenEstimate()
		b.WriteString(stCut.Render(fmt.Sprintf("  − %2d. ~%6dt  %s", t.Number, t.TokenEstimate(), t.Title(m.w-20))) + "\n")
	}
	b.WriteString("\n" + stTitle.Render("Kept, in order:") + "\n\n")
	for _, t := range m.sess.Turns {
		if m.selected[t.Number] {
			continue
		}
		b.WriteString(stDim.Render(fmt.Sprintf("  · %2d. %s", t.Number, t.Title(m.w-12))) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("context: ~%d tok  →  ~%d tok  (%s)\n",
		total, total-removed, stCut.Render(fmt.Sprintf("−%d", removed))))
	b.WriteString(stDim.Render("apply writes straight to the session file — a .bak-* backup is kept") + "\n")
	if isLiveWarn(m.sess.Path) {
		b.WriteString(stWarn.Render("this is the live session — the cut takes effect on the next resume") + "\n")
	}
	b.WriteString("\nundo any time with " + stKey.Render("bisturi --restore <id>") + " (even after the session grows)\n")
	return b.String()
}

// trimANSI is a cheap width helper for the reverse-video cursor row: we render
// the plain text padded to width, letting lipgloss apply the style uniformly.
func trimANSI(s string) string { return s }
