package surgery

import "testing"

// TestRestoreDropsCompactSummary: undoing a --compact cut must remove the
// summary node and put the original blocks back — never leave both.
func TestRestoreDropsCompactSummary(t *testing.T) {
	// Session after a compact cut of B: the summary replaced b1/b2.
	summary := `{"type":"user","uuid":"sum1","parentUuid":"a2","isCompactSummary":true,"message":{"role":"user","content":"summary"}}`
	current := []string{
		chainMsg("a1", nil),
		chainMsg("a2", "a1"),
		summary,
		chainMsg("c1", "sum1"),
	}
	rec := Record{
		ID:          "20260101-000000-cmp",
		SummaryUUID: "sum1",
		Runs:        []Run{{AnchorBefore: "a2", Lines: []string{chainMsg("b1", "a2"), chainMsg("b2", "b1")}}},
	}
	out, err := Restore(current, rec)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(out))
	for _, l := range out {
		got = append(got, uuidOf(t, l))
	}
	want := []string{"a1", "a2", "b1", "b2", "c1"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (summary should be gone, blocks back)", got, want)
		}
	}
}
