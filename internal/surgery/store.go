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

	"github.com/GuitarWag/bisturi/internal/session"
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
	return filepath.Join(home, ".claude", "bisturi", "surgeries")
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

// Save writes a record to disk. Records contain the removed conversation
// text, so the directory and files are kept private (0700/0600).
func Save(r Record) (string, error) {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
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
//
// Restore is idempotent: a run whose messages are already present in the file
// (surgery restored before, or the cut was never applied to this file) is
// skipped, so restoring can never duplicate transcript content. If every run is
// already present it returns an error instead of rewriting the file.
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

	// Keep only runs that are actually missing from the file (idempotency).
	var pending []Run
	for _, run := range r.Runs {
		if runAlreadyPresent(run, uuidAt) {
			continue
		}
		pending = append(pending, run)
	}
	if len(pending) == 0 {
		return nil, fmt.Errorf("surgery %s is already present in the session — nothing to restore", r.ID)
	}

	// Compute an insertion index for each run. A run goes after its anchor
	// line *and* after any uuid-less metadata lines that immediately follow it
	// (those belong to the anchor's turn), so restored blocks land exactly
	// between turns.
	insertBefore := map[int][][]string{} // line index -> blocks to insert before it
	appendAtEnd := [][]string{}
	lineUUID := make([]string, len(current))
	for i, line := range current {
		lineUUID[i] = fieldOf(line, "uuid")
	}
	for _, run := range pending {
		if run.AnchorBefore == "" {
			insertBefore[firstChainIndex(current)] = append(insertBefore[firstChainIndex(current)], run.Lines)
			continue
		}
		anchor := -1
		for i, u := range lineUUID {
			if u == run.AnchorBefore {
				anchor = i
				break
			}
		}
		if anchor == -1 {
			return nil, fmt.Errorf("cannot restore: anchor %s not found in current session", short(run.AnchorBefore))
		}
		j := anchor + 1
		for j < len(current) && lineUUID[j] == "" {
			j++ // skip the anchor turn's trailing metadata
		}
		if j >= len(current) {
			appendAtEnd = append(appendAtEnd, run.Lines)
		} else {
			insertBefore[j] = append(insertBefore[j], run.Lines)
		}
	}

	spliced := make([]string, 0, len(current)+r.removedCount())
	for i, line := range current {
		for _, block := range insertBefore[i] {
			spliced = append(spliced, block...)
		}
		spliced = append(spliced, line)
	}
	for _, block := range appendAtEnd {
		spliced = append(spliced, block...)
	}

	// Relink so the re-inserted blocks thread cleanly with everything else.
	return session.Relink(spliced), nil
}

// runAlreadyPresent reports whether a run's message content is already in the
// file. A run counts as present when every uuid it carries already exists; a
// run with no uuids at all (pure metadata) is treated as present so it is
// never blindly duplicated.
func runAlreadyPresent(run Run, uuidAt map[string]bool) bool {
	for _, l := range run.Lines {
		if u := fieldOf(l, "uuid"); u != "" && !uuidAt[u] {
			return false
		}
	}
	return true
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
	if err := os.WriteFile(tmp, []byte(payload), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
