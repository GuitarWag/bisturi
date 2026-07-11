// Package session parses a Claude Code session transcript, groups it into
// turns, and cuts turns while keeping the result resumable.
//
// A Claude Code session is a JSONL file at
// ~/.claude/projects/<slugged-cwd>/<session-id>.jsonl. Each line is one JSON
// object. Objects that carry a "uuid" (user, assistant, attachment, system, …)
// are threaded by "parentUuid". Parallel tool calls, retries, sidechains and
// compaction make the real structure a *forest* — many roots and leaf tips —
// so there is no single reliable "active path" to reason about across all
// sessions.
//
// Instead we treat the file as what it is: an append-ordered (chronological)
// log. Cutting removes the entries of the chosen turns and *relinks* the
// survivors into one clean linear chain — each surviving uuid node points at the
// previous surviving uuid node. That always yields a single-rooted, fully
// connected transcript with no dangling references, whatever the original shape,
// and it resumes correctly whether Claude Code walks parentUuid from the leaf or
// reads the log in order.
//
// A *turn* is one real user prompt plus the entries that follow it up to the
// next real prompt. Injected prompts (hook feedback, skill preambles; isMeta)
// fold into their surrounding turn, so you select whole topics.
package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// Entry is one physical line of the JSONL file. Raw is kept verbatim so
// untouched lines round-trip byte-for-byte; Obj is the parsed form.
type Entry struct {
	Index int
	Raw   string
	Obj   map[string]any
}

func (e Entry) Type() string      { return str(e.Obj["type"]) }
func (e Entry) UUID() string      { return str(e.Obj["uuid"]) }
func (e Entry) Parent() string    { return str(e.Obj["parentUuid"]) }
func (e Entry) HasUUID() bool     { return e.UUID() != "" }
func (e Entry) Timestamp() string { return str(e.Obj["timestamp"]) }

// IsPrompt reports whether this is a real user prompt (text the user typed),
// not a tool_result carrier or an injected/meta message.
func (e Entry) IsPrompt() bool {
	if e.Type() != "user" {
		return false
	}
	if b, _ := e.Obj["isMeta"].(bool); b {
		return false
	}
	switch c := messageContent(e.Obj).(type) {
	case string:
		return strings.TrimSpace(c) != ""
	case []any:
		hasText, isToolResult := false, false
		for _, blk := range c {
			m, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			switch str(m["type"]) {
			case "text":
				if strings.TrimSpace(str(m["text"])) != "" {
					hasText = true
				}
			case "tool_result":
				isToolResult = true
			}
		}
		return hasText && !isToolResult
	}
	return false
}

// Turn is a real prompt and every entry that belongs to it, in file order.
type Turn struct {
	Number  int
	Entries []Entry
}

func (t Turn) Prompt() Entry      { return t.Entries[0] }
func (t Turn) Timestamp() string  { return t.Prompt().Timestamp() }
func (t Turn) PromptText() string { return cleanText(textOf(t.Prompt().Obj)) }

// CharSize is the total raw byte size of the turn (with newlines).
func (t Turn) CharSize() int {
	n := 0
	for _, e := range t.Entries {
		n += len(e.Raw) + 1
	}
	return n
}

// TokenEstimate is a rough ~4-chars-per-token estimate, for relative sizing.
func (t Turn) TokenEstimate() int { return t.CharSize() / 4 }

// Session is a parsed session grouped into a preamble plus turns.
type Session struct {
	Path     string
	Entries  []Entry
	Preamble []Entry
	Turns    []Turn
}

// Load reads and parses a session file.
func Load(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	i := 0
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		var obj map[string]any
		_ = json.Unmarshal([]byte(line), &obj)
		entries = append(entries, Entry{Index: i, Raw: line, Obj: obj})
		i++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	s := &Session{Path: path, Entries: entries}
	s.group()
	return s, nil
}

// group partitions entries into a leading preamble and one Turn per real prompt.
func (s *Session) group() {
	first := -1
	for i, e := range s.Entries {
		if e.IsPrompt() {
			first = i
			break
		}
	}
	if first == -1 {
		s.Preamble = s.Entries
		return
	}
	s.Preamble = s.Entries[:first]
	var cur *Turn
	for _, e := range s.Entries[first:] {
		if e.IsPrompt() {
			s.Turns = append(s.Turns, Turn{Number: len(s.Turns) + 1, Entries: []Entry{e}})
			cur = &s.Turns[len(s.Turns)-1]
		} else if cur != nil {
			cur.Entries = append(cur.Entries, e)
		}
	}
}

// TotalTokens sums the estimated tokens across turns.
func (s *Session) TotalTokens() int {
	n := 0
	for _, t := range s.Turns {
		n += t.TokenEstimate()
	}
	return n
}

// RemovedRun is one contiguous block taken out by a cut, with the file anchor
// needed to splice it back in later.
type RemovedRun struct {
	AnchorBefore string   `json:"anchor_before"` // uuid to re-insert after ("" = front)
	Lines        []string `json:"lines"`         // removed raw lines, in original order
}

// CutResult is the outcome of a cut: the relinked file lines plus what was
// removed (grouped into contiguous runs for restore).
type CutResult struct {
	Lines []string
	Runs  []RemovedRun
}

// Cut removes the given turn numbers and relinks the survivors into a clean
// linear chain. It does not touch disk.
func (s *Session) Cut(cut map[int]bool) CutResult {
	removedIdx := map[int]bool{}
	removedUUID := map[string]bool{}
	for _, t := range s.Turns {
		if !cut[t.Number] {
			continue
		}
		for _, e := range t.Entries {
			removedIdx[e.Index] = true
			if e.HasUUID() {
				removedUUID[e.UUID()] = true
			}
		}
	}

	survivors := make([]Entry, 0, len(s.Entries))
	for _, e := range s.Entries {
		if !removedIdx[e.Index] {
			survivors = append(survivors, e)
		}
	}
	lines := relink(survivors, removedUUID)

	return CutResult{Lines: lines, Runs: buildRuns(s.Entries, removedIdx)}
}

// relink walks entries in order and rethreads every uuid-bearing node to the
// previous surviving uuid node, so the chain is linear and gap-free. Untouched
// links are left byte-identical. Metadata rows that reference a removed uuid are
// dropped so nothing dangles.
func relink(entries []Entry, removedUUID map[string]bool) []string {
	out := make([]string, 0, len(entries))
	prev := ""
	for _, e := range entries {
		if e.Obj == nil {
			out = append(out, e.Raw)
			continue
		}
		if e.HasUUID() {
			desired := prev
			if e.Parent() != desired {
				clone := cloneObj(e.Obj)
				if desired == "" {
					clone["parentUuid"] = nil
				} else {
					clone["parentUuid"] = desired
				}
				out = append(out, marshal(clone))
			} else {
				out = append(out, e.Raw)
			}
			prev = e.UUID()
			continue
		}
		// No-uuid metadata: drop if it points at a removed message.
		if leaf := str(e.Obj["leafUuid"]); leaf != "" && removedUUID[leaf] {
			continue
		}
		if p := str(e.Obj["parentUuid"]); p != "" && removedUUID[p] {
			continue
		}
		out = append(out, e.Raw)
	}
	return out
}

// buildRuns groups removed entries into contiguous file-order runs and records,
// for each, the uuid of the surviving node immediately before it (the anchor to
// re-insert after on restore).
func buildRuns(all []Entry, removedIdx map[int]bool) []RemovedRun {
	var runs []RemovedRun
	var cur *RemovedRun
	lastSurvivorUUID := ""
	for _, e := range all {
		if removedIdx[e.Index] {
			if cur == nil {
				runs = append(runs, RemovedRun{AnchorBefore: lastSurvivorUUID})
				cur = &runs[len(runs)-1]
			}
			cur.Lines = append(cur.Lines, e.Raw)
			continue
		}
		cur = nil
		if e.HasUUID() {
			lastSurvivorUUID = e.UUID()
		}
	}
	return runs
}

// Relink rethreads a set of raw JSONL lines into a clean linear chain. Used on
// restore after removed blocks are spliced back in.
func Relink(rawLines []string) []string {
	entries := make([]Entry, 0, len(rawLines))
	for i, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var obj map[string]any
		_ = json.Unmarshal([]byte(line), &obj)
		entries = append(entries, Entry{Index: i, Raw: line, Obj: obj})
	}
	return relink(entries, map[string]bool{})
}

// --- helpers ---

func messageContent(obj map[string]any) any {
	msg, ok := obj["message"].(map[string]any)
	if !ok {
		return nil
	}
	return msg["content"]
}

func textOf(obj map[string]any) string {
	switch c := messageContent(obj).(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, blk := range c {
			if m, ok := blk.(map[string]any); ok && str(m["type"]) == "text" {
				parts = append(parts, str(m["text"]))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func str(v any) string { s, _ := v.(string); return s }

func cloneObj(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func marshal(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
