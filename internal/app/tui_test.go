package app

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wagnersilva/ctx-bisturi/internal/session"
)

func tuiFixture(t *testing.T) *session.Session {
	t.Helper()
	rows := []map[string]any{
		{"type": "user", "uuid": "u1", "parentUuid": nil, "timestamp": "2026-07-09T17:00:00Z",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "topic A"}}}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1",
			"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "did A"}}}},
		{"type": "user", "uuid": "u2", "parentUuid": "a1", "timestamp": "2026-07-09T17:05:00Z",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "topic B"}}}},
		{"type": "assistant", "uuid": "a2", "parentUuid": "u2",
			"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "did B"}}}},
		{"type": "user", "uuid": "u3", "parentUuid": "a2", "timestamp": "2026-07-09T17:10:00Z",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "topic C"}}}},
	}
	var b strings.Builder
	for _, r := range rows {
		j, _ := json.Marshal(r)
		b.Write(j)
		b.WriteByte('\n')
	}
	p := t.TempDir() + "/s.jsonl"
	if err := writeLines(p, splitTrim(b.String())); err != nil {
		t.Fatal(err)
	}
	s, err := session.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func splitTrim(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestTUISelectAndApply(t *testing.T) {
	m := newModel(tuiFixture(t), false)
	// size it
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(model)
	if len(m.sess.Turns) != 3 {
		t.Fatalf("want 3 turns, got %d", len(m.sess.Turns))
	}
	// move to turn 2 and select it
	nm, _ = m.Update(key("down"))
	m = nm.(model)
	nm, _ = m.Update(key(" "))
	m = nm.(model)
	if !m.selected[2] || len(m.selected) != 1 {
		t.Fatalf("expected only turn 2 selected, got %v", m.selected)
	}
	// list view renders without panicking and mentions the status
	if !strings.Contains(m.View(), "cutting 1") {
		t.Errorf("list view missing cut status:\n%s", m.View())
	}
	// open diff, then apply
	nm, _ = m.Update(key("d"))
	m = nm.(model)
	if m.focus != focusDiff {
		t.Fatalf("d should open diff view, focus=%v", m.focus)
	}
	if !strings.Contains(m.View(), "Blocks to remove") {
		t.Errorf("diff view missing content:\n%s", m.View())
	}
	nm, cmd := m.Update(key("enter"))
	m = nm.(model)
	if !m.apply {
		t.Error("enter in diff should set apply")
	}
	if cmd == nil {
		t.Error("apply should return a quit command")
	}
}

func TestTUISelectAll(t *testing.T) {
	m := newModel(tuiFixture(t), false)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(model)
	nm, _ = m.Update(key("a"))
	m = nm.(model)
	if len(m.selected) != 3 {
		t.Errorf("select-all should mark 3, got %d", len(m.selected))
	}
	nm, _ = m.Update(key("n"))
	m = nm.(model)
	if len(m.selected) != 0 {
		t.Errorf("none should clear, got %d", len(m.selected))
	}
}

func TestTUIExpand(t *testing.T) {
	m := newModel(tuiFixture(t), false)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(model)
	nm, _ = m.Update(key("enter"))
	m = nm.(model)
	if m.focus != focusExpand {
		t.Fatalf("enter should expand, focus=%v", m.focus)
	}
	if !strings.Contains(m.View(), "topic A") {
		t.Errorf("expand view should show prompt text:\n%s", m.View())
	}
}
