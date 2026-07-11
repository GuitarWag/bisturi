package surgery

import (
	"encoding/json"
	"testing"
)

// chainMsg builds one linear chain message line.
func chainMsg(uuid string, parent any) string {
	m := map[string]any{"type": "assistant", "uuid": uuid, "parentUuid": parent,
		"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": uuid}}}}
	b, _ := json.Marshal(m)
	return string(b)
}

func parentOf(t *testing.T, line string) any {
	t.Helper()
	var o map[string]any
	if err := json.Unmarshal([]byte(line), &o); err != nil {
		t.Fatal(err)
	}
	return o["parentUuid"]
}

func uuidOf(t *testing.T, line string) string {
	t.Helper()
	var o map[string]any
	json.Unmarshal([]byte(line), &o)
	s, _ := o["uuid"].(string)
	return s
}

// TestRestoreAfterGrowth is the core guarantee: a block cut earlier can be put
// back even though the session has since grown with new turns.
func TestRestoreAfterGrowth(t *testing.T) {
	// Cut has already happened. Current (trimmed) session, then grown with D:
	//   a1 → a2 → c1(reparented to a2) → c2 → d1 → d2
	current := []string{
		chainMsg("a1", nil),
		chainMsg("a2", "a1"),
		chainMsg("c1", "a2"), // was reparented from b2 → a2 by the cut
		chainMsg("c2", "c1"),
		chainMsg("d1", "c2"), // grew after the surgery
		chainMsg("d2", "d1"),
	}
	rec := Record{
		ID:        "20260101-000000-test",
		SessionID: "test",
		Runs: []Run{{
			AnchorBefore: "a2",
			Lines: []string{
				chainMsg("b1", "a2"),
				chainMsg("b2", "b1"),
			},
		}},
	}

	out, err := Restore(current, rec)
	if err != nil {
		t.Fatal(err)
	}

	// Expected order: a1 a2 b1 b2 c1 c2 d1 d2
	wantOrder := []string{"a1", "a2", "b1", "b2", "c1", "c2", "d1", "d2"}
	if len(out) != len(wantOrder) {
		t.Fatalf("got %d lines, want %d", len(out), len(wantOrder))
	}
	present := map[string]any{}
	for i, line := range out {
		u := uuidOf(t, line)
		if u != wantOrder[i] {
			t.Errorf("line %d = %s, want %s", i, u, wantOrder[i])
		}
		present[u] = parentOf(t, line)
	}
	// c1 must be repointed back to b2.
	if present["c1"] != "b2" {
		t.Errorf("c1.parent = %v, want b2", present["c1"])
	}
	// b1 keeps its original parent a2; the whole chain must resolve.
	if present["b1"] != "a2" {
		t.Errorf("b1.parent = %v, want a2", present["b1"])
	}
	for u, p := range present {
		if ps, ok := p.(string); ok && ps != "" {
			if _, exists := present[ps]; !exists {
				t.Errorf("%s has dangling parent %s", u, ps)
			}
		}
	}
	// D survives untouched.
	if present["d2"] != "d1" {
		t.Errorf("grown turn d2.parent = %v, want d1", present["d2"])
	}
}

func TestRestoreFrontBlock(t *testing.T) {
	// The cut removed the very first block; AnchorBefore is empty.
	current := []string{
		chainMsg("b1", nil), // was reparented to root by the cut
		chainMsg("b2", "b1"),
	}
	rec := Record{
		Runs: []Run{{
			AnchorBefore: "",
			Lines: []string{
				chainMsg("a1", nil),
				chainMsg("a2", "a1"),
			},
		}},
	}
	out, err := Restore(current, rec)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a1", "a2", "b1", "b2"}
	for i, line := range out {
		if uuidOf(t, line) != want[i] {
			t.Errorf("line %d = %s, want %s", i, uuidOf(t, line), want[i])
		}
	}
	// b1 repointed back to a2.
	if parentOf(t, out[2]) != "a2" {
		t.Errorf("b1.parent = %v, want a2", parentOf(t, out[2]))
	}
}

// TestRestoreIsIdempotent guards against the worst failure mode: restoring a
// surgery twice (or against a file that never lost the content) must not
// duplicate transcript messages.
func TestRestoreIsIdempotent(t *testing.T) {
	current := []string{
		chainMsg("a1", nil),
		chainMsg("a2", "a1"),
		chainMsg("c1", "a2"),
	}
	rec := Record{
		ID:   "20260101-000000-idem",
		Runs: []Run{{AnchorBefore: "a2", Lines: []string{chainMsg("b1", "a2"), chainMsg("b2", "b1")}}},
	}
	once, err := Restore(current, rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(once) != 5 {
		t.Fatalf("first restore: got %d lines, want 5", len(once))
	}
	// Second restore against the already-restored file must refuse.
	if _, err := Restore(once, rec); err == nil {
		t.Fatal("second restore should error, not duplicate content")
	}
	// Restoring against a file that never lost the block must also refuse.
	full := []string{chainMsg("a1", nil), chainMsg("a2", "a1"), chainMsg("b1", "a2"), chainMsg("b2", "b1"), chainMsg("c1", "b2")}
	if _, err := Restore(full, rec); err == nil {
		t.Fatal("restore into a never-cut file should error")
	}
}

// TestRestoreInsertsAfterAnchorMetadata: uuid-less metadata directly after the
// anchor belongs to the anchor's turn, so the restored block must land after
// it, not between the anchor and its metadata.
func TestRestoreInsertsAfterAnchorMetadata(t *testing.T) {
	meta := `{"type":"last-prompt","leafUuid":"a2","lastPrompt":"x"}`
	current := []string{
		chainMsg("a1", nil),
		chainMsg("a2", "a1"),
		meta, // trailing metadata of turn A
		chainMsg("c1", "a2"),
	}
	rec := Record{
		ID:   "20260101-000000-meta",
		Runs: []Run{{AnchorBefore: "a2", Lines: []string{chainMsg("b1", "a2")}}},
	}
	out, err := Restore(current, rec)
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(out))
	for _, l := range out {
		if u := uuidOf(t, l); u != "" {
			order = append(order, u)
		} else {
			order = append(order, "meta")
		}
	}
	want := []string{"a1", "a2", "meta", "b1", "c1"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rec := Record{ID: "20260101-000000-abc", SessionID: "abc", CutTurns: []int{2}, RemovedTokens: 42,
		Runs: []Run{{AnchorBefore: "a2", Lines: []string{chainMsg("b1", "a2")}}}}
	if _, err := Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := Load(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "abc" || got.RemovedTokens != 42 || got.RemovedLineCount() != 1 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if list := List("abc"); len(list) != 1 {
		t.Errorf("List returned %d, want 1", len(list))
	}
}
