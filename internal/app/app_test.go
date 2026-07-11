package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseNumsRejectsEmptyAndBadRanges(t *testing.T) {
	for _, spec := range []string{"", " ", ",", "3-1"} {
		if _, err := parseNums(spec, 5); err == nil {
			t.Errorf("parseNums(%q) should error, got nil", spec)
		}
	}
	nums, err := parseNums("2,4-5", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(nums) != 3 || !nums[2] || !nums[4] || !nums[5] {
		t.Errorf("parseNums(2,4-5) = %v", nums)
	}
}

func TestWriteLinesPreservesRestrictiveMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sess.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeLines(p, []string{`{"a":1}`}); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("rewritten session mode = %o, want 0600", fi.Mode().Perm())
	}

	// New files (no prior mode to preserve) default to private too.
	fresh := filepath.Join(dir, "new.jsonl")
	if err := writeLines(fresh, []string{`{"a":1}`}); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(fresh)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("new file mode = %o, want 0600", fi.Mode().Perm())
	}
}

func TestBackupPreservesMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sess.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bak, err := backup(p, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(bak)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %o, want 0600", fi.Mode().Perm())
	}
}
