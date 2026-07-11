package session

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSession(t *testing.T, dir, id string, lines ...string) {
	t.Helper()
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(joinLines(lines)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func joinLines(ls []string) string {
	out := ""
	for _, l := range ls {
		out += l + "\n"
	}
	return out
}

func TestPeekNamesLastWins(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s1",
		`{"type":"ai-title","aiTitle":"First auto title"}`,
		`{"type":"custom-title","customTitle":"old-name","sessionId":"s1"}`,
		`{"type":"ai-title","aiTitle":"Newer auto title"}`,
		`{"type":"custom-title","customTitle":"353","sessionId":"s1"}`,
	)
	metas := ListSessions(dir)
	if len(metas) != 1 {
		t.Fatalf("want 1 session, got %d", len(metas))
	}
	if metas[0].Name != "353" {
		t.Errorf("Name = %q, want 353 (last custom-title wins)", metas[0].Name)
	}
	if metas[0].Title != "Newer auto title" {
		t.Errorf("Title = %q, want the last aiTitle", metas[0].Title)
	}
}

func TestMatchPrefersRenameName(t *testing.T) {
	metas := []Meta{
		{ID: "aaaa1111", Name: "353", Title: "Something about slack"},
		{ID: "bbbb2222", Name: "week-ahead", Title: "353 tokens discussion"}, // title contains 353
	}
	hits := Match(metas, "353")
	if len(hits) == 0 || hits[0].ID != "aaaa1111" {
		t.Fatalf("expected the /rename '353' to rank first, got %+v", hits)
	}
}

func TestMatchByIDPrefixAndTitle(t *testing.T) {
	metas := []Meta{
		{ID: "abc12345", Name: "", Title: "Fix connection health check"},
		{ID: "def67890", Name: "deploy", Title: ""},
	}
	if hits := Match(metas, "abc12"); len(hits) != 1 || hits[0].ID != "abc12345" {
		t.Errorf("id-prefix match failed: %+v", hits)
	}
	if hits := Match(metas, "connection"); len(hits) != 1 || hits[0].ID != "abc12345" {
		t.Errorf("title substring match failed: %+v", hits)
	}
	if hits := Match(metas, "deploy"); len(hits) != 1 || hits[0].ID != "def67890" {
		t.Errorf("name match failed: %+v", hits)
	}
}

func TestDisplayFallback(t *testing.T) {
	if got := (Meta{ID: "abcd1234efgh"}).Display(); got != "abcd1234" {
		t.Errorf("Display fallback = %q, want short id", got)
	}
	if got := (Meta{ID: "x", Title: "the title"}).Display(); got != "the title" {
		t.Errorf("Display should prefer title, got %q", got)
	}
	if got := (Meta{ID: "x", Name: "the-name", Title: "t"}).Display(); got != "the-name" {
		t.Errorf("Display should prefer name, got %q", got)
	}
}
