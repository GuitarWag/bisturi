package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// writeLines writes JSONL content atomically (temp file + rename).
func writeLines(path string, lines []string) error {
	payload := strings.Join(lines, "\n")
	if len(lines) > 0 {
		payload += "\n"
	}
	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(payload); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// backup copies src to a timestamped sibling and returns its path.
func backup(src string, now time.Time) (string, error) {
	dst := fmt.Sprintf("%s.bak-%s", src, now.Format("20060102-150405"))
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// cutSiblingPath returns "<base>.cut.jsonl" next to the original.
func cutSiblingPath(src string) string {
	ext := filepath.Ext(src)
	return strings.TrimSuffix(src, ext) + ".cut" + ext
}

// readLines loads a file into raw lines (no trailing empty line).
func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw := strings.Split(string(data), "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}
