package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectsRoot is where Claude Code keeps per-project session folders.
func ProjectsRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// SlugForCwd reproduces Claude Code's directory slug: os separators and dots
// become dashes.
func SlugForCwd(cwd string) string {
	s := strings.ReplaceAll(cwd, string(os.PathSeparator), "-")
	return strings.ReplaceAll(s, ".", "-")
}

// ProjectDirFor returns the sessions folder for a working directory.
func ProjectDirFor(cwd string) string {
	return filepath.Join(ProjectsRoot(), SlugForCwd(cwd))
}

// Meta is a lightweight description of a session file (no full parse).
type Meta struct {
	Path    string
	ID      string
	Title   string // ai-generated title, if present
	ModTime int64
	Size    int64
}

// ListSessions returns the sessions in a project dir, newest first.
func ListSessions(projectDir string) []Meta {
	ents, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}
	var out []Meta
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".jsonl") {
			continue
		}
		// Skip our own cut output, not real sessions.
		if strings.HasSuffix(de.Name(), ".cut.jsonl") {
			continue
		}
		p := filepath.Join(projectDir, de.Name())
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, Meta{
			Path:    p,
			ID:      strings.TrimSuffix(de.Name(), ".jsonl"),
			Title:   peekTitle(p),
			ModTime: info.ModTime().Unix(),
			Size:    info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out
}

// AllSessions scans every project for sessions, newest first.
func AllSessions() []Meta {
	root := ProjectsRoot()
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Meta
	for _, de := range ents {
		if de.IsDir() {
			out = append(out, ListSessions(filepath.Join(root, de.Name()))...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out
}

// Match finds sessions whose id or title matches the query (case-insensitive
// substring; id prefix also counts). Ordered best-first: exact id, id prefix,
// title contains, then by recency.
func Match(metas []Meta, query string) []Meta {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return metas
	}
	type scored struct {
		m     Meta
		score int
	}
	var hits []scored
	for _, m := range metas {
		id := strings.ToLower(m.ID)
		title := strings.ToLower(m.Title)
		switch {
		case id == q:
			hits = append(hits, scored{m, 0})
		case strings.HasPrefix(id, q):
			hits = append(hits, scored{m, 1})
		case title != "" && title == q:
			hits = append(hits, scored{m, 2})
		case title != "" && strings.Contains(title, q):
			hits = append(hits, scored{m, 3})
		case strings.Contains(id, q):
			hits = append(hits, scored{m, 4})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		return hits[i].m.ModTime > hits[j].m.ModTime
	})
	out := make([]Meta, len(hits))
	for i, h := range hits {
		out[i] = h.m
	}
	return out
}

// peekTitle scans a file for the most recent ai-title row without a full parse.
func peekTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	title := ""
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"aiTitle"`) {
			continue
		}
		var obj map[string]any
		if json.Unmarshal(line, &obj) == nil {
			if t := str(obj["aiTitle"]); t != "" {
				title = t
			}
		}
	}
	return title
}
