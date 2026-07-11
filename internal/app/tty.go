package app

import (
	"os"
	"strings"
)

// isTTY reports whether both stdin and stdout are attached to a terminal, so
// the full-screen TUI can run. When invoked from a captured shell (an agent's
// tool call) this is false and the caller degrades gracefully.
func isTTY() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// isLiveWarn best-effort detects that the target is the session we're running
// inside, so the TUI can warn that a cut only takes effect on the next resume.
// Returns false when it can't tell (no false alarms).
func isLiveWarn(path string) bool {
	id := os.Getenv("CLAUDE_SESSION_ID")
	if id == "" {
		return false
	}
	return strings.Contains(path, id)
}
