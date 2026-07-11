// Package surgery persists each cut so it can be undone later, even after the
// live session has grown with new turns.
//
// A cut removes one or more contiguous blocks from the active leaf→root path.
// For each block we keep the removed lines (they already carry their original
// parentUuids), the uuid of the surviving node just *before* the block, and the
// surviving node just *after* it whose parent we rewired across the gap.
// Restore re-inserts the lines after the "before" anchor and repoints the
// "after" node to the block's tail. Because anchors are located by uuid,
// later-added turns elsewhere in the file don't interfere.
package surgery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wagnersilva/ctx-bisturi/internal/session"
)

// Run is one removed contiguous block plus the file anchor to re-insert after.
type Run struct {
	AnchorBefore string   `json:"anchor_before"` // uuid to re-insert after ("" = front)
	Lines        []string `json:"lines"`         // removed raw lines, in original order
}

// Record is one saved surgery.
type Record struct {
	ID            string   `json:"id"`
	SessionID     string   `json:"session_id"`
	SessionPath   string   `json:"session_path"`
	CreatedAt     string   `json:"created_at"`
	CutTurns      []int    `json:"cut_turns"`
	Titles        []string `json:"titles"`
	RemovedTokens int      `json:"removed_tokens"`
	Runs          []Run    `json:"runs"`
	BackupPath    string   `json:"backup_path,omitempty"`
}

// removedCount is a small convenience for reporting.
func (r Record) removedCount() int {
	n := 0
	for _, run := range r.Runs {
		n += len(run.Lines)
	}
	return n
}

// RemovedLineCount reports how many raw lines this surgery took out.
func (r Record) RemovedLineCount() int { return r.removedCount() }

// Dir is where surgery records live.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "ctx-bisturi", "surgeries")
}

func recordPath(id string) string { return filepath.Join(Dir(), id+".json") }

// NewID builds a sortable id from a time and session.
func NewID(t time.Time, sessionID string) string {
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("%s-%s", t.Format("20060102-150405"), short)
}

// Save writes a record to disk.
func Save(r Record) (string, error) {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	p := recordPath(r.ID)
	if err := atomicWrite(p, string(b)+"\n"); err != nil {
		return "", err
	}
	return p, nil
}

// Load reads one record by id.
func Load(id string) (Record, error) {
	var r Record
	b, err := os.ReadFile(recordPath(id))
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}

// List returns all records, newest first, optionally filtered by session id.
func List(sessionID string) []Record {
	ents, err := os.ReadDir(Dir())
	if err != nil {
		return nil
	}
	var out []Record
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		var r Record
		b, err := os.ReadFile(filepath.Join(Dir(), de.Name()))
		if err != nil || json.Unmarshal(b, &r) != nil {
			continue
		}
		if sessionID != "" && r.SessionID != sessionID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// Restore re-inserts a surgery's removed blocks into the current session lines
// (which may have grown since) and relinks the result into a clean chain. It
// returns the rebuilt lines without writing.
func Restore(current []string, r Record) ([]string, error) {
	if len(r.Runs) == 0 {
		return nil, fmt.Errorf("surgery %s has nothing to restore", r.ID)
	}
	uuidAt := map[string]bool{}
	for _, line := range current {
		if u := fieldOf(line, "uuid"); u != "" {
			uuidAt[u] = true
		}
	}

	// Bucket runs by their insertion anchor.
	afterMap := map[string][][]string{}
	var frontRuns [][]string
	for _, run := range r.Runs {
		if run.AnchorBefore == "" {
			frontRuns = append(frontRuns, run.Lines)
			continue
		}
		if !uuidAt[run.AnchorBefore] {
			return nil, fmt.Errorf("cannot restore: anchor %s not found in current session", short(run.AnchorBefore))
		}
		afterMap[run.AnchorBefore] = append(afterMap[run.AnchorBefore], run.Lines)
	}

	firstChain := firstChainIndex(current)
	spliced := make([]string, 0, len(current)+r.removedCount())
	for i, line := range current {
		if len(frontRuns) > 0 && i == firstChain {
			for _, fr := range frontRuns {
				spliced = append(spliced, fr...)
			}
			frontRuns = nil
		}
		spliced = append(spliced, line)
		if runs, ok := afterMap[fieldOf(line, "uuid")]; ok {
			for _, rl := range runs {
				spliced = append(spliced, rl...)
			}
		}
	}
	for _, fr := range frontRuns { // no chain node to precede: append at end
		spliced = append(spliced, fr...)
	}

	// Relink so the re-inserted blocks thread cleanly with everything else.
	return session.Relink(spliced), nil
}

// --- helpers ---

func firstChainIndex(lines []string) int {
	for i, line := range lines {
		if t := fieldOf(line, "type"); t == "user" || t == "assistant" {
			return i
		}
	}
	return len(lines)
}

func fieldOf(line, key string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(line), &obj) != nil {
		return ""
	}
	s, _ := obj[key].(string)
	return s
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func atomicWrite(path, payload string) error {
	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	if err := os.WriteFile(tmp, []byte(payload), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
