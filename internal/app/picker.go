package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/GuitarWag/bisturi/internal/session"
)

// runPicker shows a searchable list of sessions and returns the chosen file
// path. An empty path (nil error) means the user cancelled.
func runPicker(metas []session.Meta, title string) (string, error) {
	p := tea.NewProgram(newPicker(metas, title), tea.WithAltScreen())
	fm, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("picker: %w", err)
	}
	pk := fm.(picker)
	if pk.chosen < 0 {
		fmt.Println("no session selected.")
		return "", nil
	}
	return pk.filtered()[pk.chosen].Path, nil
}

type picker struct {
	title  string
	all    []session.Meta
	query  string
	cursor int
	chosen int
	w, h   int
	ready  bool
}

func newPicker(metas []session.Meta, title string) picker {
	return picker{title: title, all: metas, chosen: -1}
}

func (p picker) Init() tea.Cmd { return nil }

// filtered returns the sessions matching the current query (by name/title/id).
func (p picker) filtered() []session.Meta {
	if strings.TrimSpace(p.query) == "" {
		return p.all
	}
	return session.Match(p.all, p.query)
}

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.w, p.h, p.ready = msg.Width, msg.Height, true
		return p, nil
	case tea.KeyMsg:
		list := p.filtered()
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return p, tea.Quit
		case tea.KeyEnter:
			if len(list) > 0 {
				p.chosen = clamp(p.cursor, 0, len(list)-1)
			}
			return p, tea.Quit
		case tea.KeyUp:
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case tea.KeyDown:
			if p.cursor < len(list)-1 {
				p.cursor++
			}
			return p, nil
		case tea.KeyBackspace:
			if p.query != "" {
				p.query = p.query[:len(p.query)-1]
				p.cursor = 0
			}
			return p, nil
		case tea.KeyRunes, tea.KeySpace:
			p.query += string(msg.Runes)
			p.cursor = 0
			return p, nil
		}
	}
	return p, nil
}

func (p picker) View() string {
	if !p.ready {
		return "loading…"
	}
	list := p.filtered()
	var b strings.Builder
	b.WriteString(stHeader.Render("ctx-bisturi") + " " + stTitle.Render(p.title) + "\n")
	b.WriteString(stDim.Render("type to filter by name / title / id · ↑/↓ move · enter open · esc cancel") + "\n")
	q := p.query
	if q == "" {
		q = stDim.Render("(all)")
	}
	b.WriteString(" search: " + stKey.Render(q) + "\n\n")

	rows := p.h - 6
	if rows < 3 {
		rows = 3
	}
	start := 0
	if p.cursor >= rows {
		start = p.cursor - rows + 1
	}
	end := start + rows
	if end > len(list) {
		end = len(list)
	}
	if len(list) == 0 {
		b.WriteString(stDim.Render("  no matches"))
	}
	for i := start; i < end; i++ {
		m := list[i]
		when := time.Unix(m.ModTime, 0).Format("01-02 15:04")
		name := m.Name
		if name == "" {
			name = stDim.Render("(" + m.Display() + ")")
		} else {
			name = stTitle.Render(name)
		}
		line := fmt.Sprintf(" %-11s  %-8s  %s", when, short(m.ID), name)
		if m.Title != "" {
			line += stDim.Render("  — " + truncate(m.Title, 40))
		}
		if i == p.cursor {
			b.WriteString(stCursor.Render(fmt.Sprintf("›%-*s", max(0, p.w-1), stripForWidth(line))))
		} else {
			b.WriteString(" " + line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// stripForWidth is a placeholder so the reverse-video cursor row pads uniformly;
// lipgloss applies the style over the whole string.
func stripForWidth(s string) string { return s }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
