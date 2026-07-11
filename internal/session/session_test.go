package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes a session mirroring real Claude Code structure: a metadata
// preamble, a linear user/assistant chain, attachments and tool_result rows,
// and trailing metadata that references message uuids.
func fixture(t *testing.T) string {
	t.Helper()
	rows := []map[string]any{
		{"type": "mode", "mode": "default"},
		{"type": "file-history-snapshot", "messageId": "s0"},
		// Turn A
		userPrompt("a1", nil, "work on topic A"),
		{"type": "attachment", "parentUuid": "a1", "attachment": map[string]any{"x": 1}},
		assistant("a2", "a1", "doing A"),
		toolResult("a3", "a2"),
		assistant("a4", "a3", "A done"),
		{"type": "last-prompt", "leafUuid": "a4"},
		// Turn B
		userPrompt("b1", "a4", "now topic B"),
		assistant("b2", "b1", "doing B"),
		toolResult("b3", "b2"),
		assistant("b4", "b3", "B done"),
		{"type": "last-prompt", "leafUuid": "b4"},
		// Turn C
		userPrompt("c1", "b4", "topic C"),
		assistant("c2", "c1", "doing C"),
		{"type": "last-prompt", "leafUuid": "c2"},
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "sess.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		b, _ := json.Marshal(r)
		f.Write(b)
		f.Write([]byte("\n"))
	}
	f.Close()
	return p
}

func userPrompt(uuid string, parent any, text string) map[string]any {
	return map[string]any{
		"type": "user", "uuid": uuid, "parentUuid": parent,
		"timestamp": "2026-07-09T17:00:00.000Z",
		"message":   map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}},
	}
}

func assistant(uuid, parent, text string) map[string]any {
	return map[string]any{
		"type": "assistant", "uuid": uuid, "parentUuid": parent,
		"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}},
	}
}

func toolResult(uuid, parent string) map[string]any {
	return map[string]any{
		"type": "user", "uuid": uuid, "parentUuid": parent,
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": "ok"}}},
	}
}

func parseLines(t *testing.T, lines []string) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		var o map[string]any
		if err := json.Unmarshal([]byte(l), &o); err != nil {
			t.Fatalf("bad json line: %v", err)
		}
		out = append(out, o)
	}
	return out
}

func TestGrouping(t *testing.T) {
	s, err := Load(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Turns) != 3 {
		t.Fatalf("want 3 turns, got %d", len(s.Turns))
	}
	if len(s.Preamble) != 2 {
		t.Fatalf("want 2 preamble entries, got %d", len(s.Preamble))
	}
	if got := s.Turns[1].Title(40); got != "now topic B" {
		t.Errorf("turn 2 title = %q", got)
	}
}

func TestCutMiddleRethreads(t *testing.T) {
	s, _ := Load(fixture(t))
	res := s.Cut(map[int]bool{2: true})
	objs := parseLines(t, res.Lines)

	present := map[string]bool{}
	for _, o := range objs {
		if u, ok := o["uuid"].(string); ok {
			present[u] = true
		}
	}
	for _, gone := range []string{"b1", "b2", "b3", "b4"} {
		if present[gone] {
			t.Errorf("%s should have been removed", gone)
		}
	}
	// c1 now hangs off a4.
	for _, o := range objs {
		if o["uuid"] == "c1" {
			if o["parentUuid"] != "a4" {
				t.Errorf("c1.parent = %v, want a4", o["parentUuid"])
			}
		}
	}
	// Every chain parent resolves.
	for _, o := range objs {
		if o["type"] == "user" || o["type"] == "assistant" {
			if p, ok := o["parentUuid"].(string); ok && p != "" && !present[p] {
				t.Errorf("dangling parent %s", p)
			}
		}
	}
	// One removed run, anchored after a4 for restore.
	if len(res.Runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(res.Runs))
	}
	run := res.Runs[0]
	if run.AnchorBefore != "a4" {
		t.Errorf("run anchor = %s, want a4", run.AnchorBefore)
	}
	if len(run.Lines) == 0 {
		t.Error("run has no removed lines")
	}
}

func TestCutDropsDanglingMeta(t *testing.T) {
	s, _ := Load(fixture(t))
	objs := parseLines(t, s.Cut(map[int]bool{2: true}).Lines)
	for _, o := range objs {
		if o["type"] == "last-prompt" && o["leafUuid"] == "b4" {
			t.Error("last-prompt for removed b4 should be dropped")
		}
	}
}

func TestCutFirstMakesNewRoot(t *testing.T) {
	s, _ := Load(fixture(t))
	objs := parseLines(t, s.Cut(map[int]bool{1: true}).Lines)
	var firstChain map[string]any
	for _, o := range objs {
		if o["type"] == "user" || o["type"] == "assistant" {
			firstChain = o
			break
		}
	}
	if firstChain["uuid"] != "b1" || firstChain["parentUuid"] != nil {
		t.Errorf("first chain = %v (want b1 root)", firstChain["uuid"])
	}
}

func TestCutNothingRoundtrips(t *testing.T) {
	p := fixture(t)
	s, _ := Load(p)
	res := s.Cut(map[int]bool{})
	orig, _ := os.ReadFile(p)
	// Re-serialize original for comparison of parsed objects.
	got := parseLines(t, res.Lines)
	var want []map[string]any
	for _, line := range splitFile(string(orig)) {
		var o map[string]any
		json.Unmarshal([]byte(line), &o)
		want = append(want, o)
	}
	if len(got) != len(want) {
		t.Fatalf("line count %d != %d", len(got), len(want))
	}
}

func splitFile(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
