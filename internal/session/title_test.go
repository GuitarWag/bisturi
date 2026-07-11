package session

import "testing"

func TestCleanText(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			in:   "<command-name>/goal</command-name>\n  <command-args>have a tool to cut context</command-args>",
			want: "/goal have a tool to cut context",
		},
		{
			in:   "<command-message>ctx-cut</command-message>\n<command-name>/ctx-cut</command-name>",
			want: "/ctx-cut",
		},
		{
			in:   "<local-command-stdout>Goal set: refactor in Go and improve the TUI</local-command-stdout>",
			want: "refactor in Go and improve the TUI",
		},
		{
			in:   "plain user message with no tags",
			want: "plain user message with no tags",
		},
		{
			in:   "line one\n\n  line   two  ",
			want: "line one line two",
		},
	}
	for _, c := range cases {
		if got := cleanText(c.in); got != c.want {
			t.Errorf("cleanText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTitleTruncates(t *testing.T) {
	turn := Turn{Entries: []Entry{{Obj: map[string]any{
		"type":    "user",
		"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "abcdefghij"}}},
	}}}}
	if got := turn.Title(5); got != "abcd…" {
		t.Errorf("Title(5) = %q, want abcd…", got)
	}
	if got := turn.Title(0); got != "abcdefghij" {
		t.Errorf("Title(0) = %q, want full", got)
	}
}
