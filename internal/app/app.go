// Package app wires the CLI: resolve a session, run cuts and restores, and
// launch the TUI.
package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GuitarWag/bisturi/internal/session"
	"github.com/GuitarWag/bisturi/internal/surgery"
)

// version is overridden at release time via -ldflags "-X ...app.version=<tag>".
var version = "dev"

type options struct {
	file          string
	sessionQuery  string
	project       string
	list          bool
	printTurns    bool
	asJSON        bool
	cut           string
	inPlace       bool
	dryRun        bool
	restoreID     string
	listSurgeries bool
	all           bool
	reverse       bool
	showVersion   bool
}

// Run is the entry point. Returns a process exit code.
func Run(args []string) int {
	opts, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if opts.showVersion {
		fmt.Println("bisturi", version)
		return 0
	}

	if opts.listSurgeries {
		return listSurgeries(opts)
	}
	if opts.restoreID != "" {
		return doRestore(opts)
	}

	path, done, err := resolveSession(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if done {
		return 0 // --list output or a cancelled picker; already handled
	}

	sess, err := session.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load %s: %v\n", path, err)
		return 1
	}
	if len(sess.Turns) == 0 {
		fmt.Fprintln(os.Stderr, "no user prompts found — nothing to cut.")
		return 1
	}

	switch {
	case opts.asJSON:
		printJSON(sess)
		return 0
	case opts.printTurns:
		printTurns(sess)
		return 0
	case opts.cut != "":
		nums, err := parseNums(opts.cut, len(sess.Turns))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return applyCut(sess, nums, opts.inPlace, opts.dryRun)
	}

	// Interactive TUI path — needs a real terminal.
	if !isTTY() {
		printTurns(sess)
		fmt.Fprintln(os.Stderr, "\nno interactive terminal here — the TUI needs a real TTY.")
		rel := relOrAbs(path)
		fmt.Fprintf(os.Stderr, "• inside Claude Code, run it yourself:  !bisturi %s\n", rel)
		fmt.Fprintf(os.Stderr, "• or cut non-interactively:  bisturi %s --cut <nums>\n", rel)
		return 2
	}
	return runTUI(sess)
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("bisturi", flag.ContinueOnError)
	fs.StringVar(&o.sessionQuery, "session", "", "select a session by id/title (substring ok)")
	fs.StringVar(&o.sessionQuery, "s", "", "shorthand for --session")
	fs.StringVar(&o.project, "project", "", "project working-dir or slug dir (default: cwd)")
	fs.BoolVar(&o.list, "list", false, "list sessions and exit")
	fs.BoolVar(&o.printTurns, "print", false, "print the turn breakdown and exit")
	fs.BoolVar(&o.asJSON, "json", false, "emit the turn breakdown as JSON and exit")
	fs.StringVar(&o.cut, "cut", "", "comma/range turn numbers to cut (skips the TUI)")
	fs.BoolVar(&o.inPlace, "in-place", false, "replace the original (a .bak-* backup is written first)")
	fs.BoolVar(&o.dryRun, "dry-run", false, "with --cut, report but write nothing")
	fs.StringVar(&o.restoreID, "restore", "", "restore a saved surgery by id (undo a cut)")
	fs.BoolVar(&o.listSurgeries, "surgeries", false, "list saved surgeries (undo history)")
	fs.BoolVar(&o.all, "all", false, "consider sessions across all projects, not just the cwd's")
	fs.BoolVar(&o.all, "a", false, "shorthand for --all")
	fs.BoolVar(&o.reverse, "reverse", false, "list sessions oldest-first (default newest-first)")
	fs.BoolVar(&o.reverse, "r", false, "shorthand for --reverse")
	fs.BoolVar(&o.showVersion, "version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "bisturi — surgically cut topics from a Claude Code session\n\n")
		fmt.Fprintf(os.Stderr, "usage:\n  bisturi [-s NAME | FILE] [flags]\n\nflags:\n")
		fs.PrintDefaults()
	}
	// Go's flag stops at the first positional, so a path before flags would
	// swallow the rest. Separate flags from positionals so order is free.
	flagArgs, positionals := splitArgs(args)
	if err := fs.Parse(flagArgs); err != nil {
		return o, err
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) > 0 {
		o.file = positionals[0]
	}
	return o, nil
}

// valueFlags are the flags that consume the following argument as their value.
var valueFlags = map[string]bool{
	"session": true, "s": true, "project": true, "cut": true, "restore": true,
}

// splitArgs partitions argv into flag tokens and positionals, so flags may
// appear after the file path (`bisturi file.jsonl --cut 3`).
func splitArgs(args []string) (flags, positionals []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue
			}
			if valueFlags[name] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positionals = append(positionals, a)
	}
	return flags, positionals
}

// resolveSession turns flags into a concrete file path. done=true means the
// command already completed successfully without a session (e.g. --list
// printed, or the user cancelled the picker).
func resolveSession(o options) (path string, done bool, err error) {
	if o.file != "" {
		if !fileExists(o.file) {
			return "", false, fmt.Errorf("no such file: %s", o.file)
		}
		return o.file, false, nil
	}

	metas, where := gatherMetas(o)
	if len(metas) == 0 {
		return "", false, fmt.Errorf("no sessions found in %s\npass a .jsonl path, --project <dir>, or -a for all projects", where)
	}
	if o.reverse {
		for i, j := 0, len(metas)-1; i < j; i, j = i+1, j-1 {
			metas[i], metas[j] = metas[j], metas[i]
		}
	}

	if o.sessionQuery != "" {
		hits := session.Match(metas, o.sessionQuery)
		if len(hits) == 0 {
			return "", false, fmt.Errorf("no session matches %q in %s", o.sessionQuery, where)
		}
		if len(hits) == 1 {
			return hits[0].Path, false, nil
		}
		// Ambiguous: let the user pick if interactive, else list and stop.
		if isTTY() {
			return pickOrCancel(hits, fmt.Sprintf("%q matches %d sessions", o.sessionQuery, len(hits)))
		}
		fmt.Fprintf(os.Stderr, "%q matches %d sessions:\n", o.sessionQuery, len(hits))
		printMetas(hits)
		return "", false, fmt.Errorf("be more specific, or run interactively to pick")
	}

	if o.list {
		printMetas(metas)
		return "", true, nil
	}
	if len(metas) == 1 {
		return metas[0].Path, false, nil
	}
	// No selector, several sessions: pick interactively, else list and stop.
	if isTTY() {
		return pickOrCancel(metas, "select a session")
	}
	printMetas(metas)
	return "", false, fmt.Errorf("multiple sessions — pass -s <name>, a path, or run interactively")
}

// pickOrCancel wraps the picker: a cancelled picker is a successful no-op.
func pickOrCancel(metas []session.Meta, title string) (string, bool, error) {
	p, err := runPicker(metas, title)
	if err != nil {
		return "", false, err
	}
	if p == "" {
		return "", true, nil // user cancelled
	}
	return p, false, nil
}

// gatherMetas returns the candidate sessions and a human label for where they
// came from. With -a (or when the cwd's project has none), it spans all projects.
func gatherMetas(o options) ([]session.Meta, string) {
	if o.all {
		return session.AllSessions(), "all projects"
	}
	dir := projectDir(o.project)
	metas := session.ListSessions(dir)
	if len(metas) == 0 && o.project == "" {
		// Run from a folder with no sessions of its own — fall back to all.
		if all := session.AllSessions(); len(all) > 0 {
			return all, "all projects (none in cwd)"
		}
	}
	return metas, dir
}

func applyCut(sess *session.Session, nums map[int]bool, inPlace, dryRun bool) int {
	res := sess.Cut(nums)
	var removedTokens int
	var titles []string
	var cutList []int
	for _, t := range sess.Turns {
		if nums[t.Number] {
			removedTokens += t.TokenEstimate()
			titles = append(titles, t.Title(60))
			cutList = append(cutList, t.Number)
		}
	}
	sort.Ints(cutList)
	nice := joinInts(cutList)

	if dryRun {
		fmt.Printf("[dry-run] would cut turns %s\n", nice)
		fmt.Printf("[dry-run] %d turns kept, ~%d tokens removed, %d lines remain\n",
			len(sess.Turns)-len(nums), removedTokens, len(res.Lines))
		return 0
	}

	now := time.Now()
	out := sess.Path
	var bak string
	if inPlace {
		var err error
		bak, err = backup(sess.Path, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
			return 1
		}
	} else {
		out = cutSiblingPath(sess.Path)
	}
	if err := writeLines(out, res.Lines); err != nil {
		fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
		return 1
	}

	// Persist the surgery so it can be restored later.
	runs := make([]surgery.Run, len(res.Runs))
	for i, r := range res.Runs {
		runs[i] = surgery.Run{AnchorBefore: r.AnchorBefore, Lines: r.Lines}
	}
	rec := surgery.Record{
		ID:            surgery.NewID(now, sessionID(sess.Path)),
		SessionID:     sessionID(sess.Path),
		SessionPath:   sess.Path,
		CreatedAt:     now.Format(time.RFC3339),
		CutTurns:      cutList,
		Titles:        titles,
		RemovedTokens: removedTokens,
		Runs:          runs,
		BackupPath:    bak,
	}
	recPath, err := surgery.Save(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: surgery not saved (%v) — restore won't be available\n", err)
	}

	fmt.Printf("cut turns %s — ~%d tokens removed.\n", nice, removedTokens)
	if bak != "" {
		fmt.Printf("backup:  %s\n", bak)
	}
	fmt.Printf("wrote:   %s\n", out)
	if recPath != "" {
		fmt.Printf("surgery: %s  (undo:  bisturi --restore %s)\n", rec.ID, rec.ID)
	}

	id := sessionID(sess.Path)
	if inPlace {
		// The cut is on disk, but a running/paused session keeps its context in
		// memory — Claude Code only reloads the trimmed transcript on resume.
		fmt.Println()
		if isLiveWarn(sess.Path) {
			fmt.Println("⚠  RESTART REQUIRED — this is the session you're in right now.")
			fmt.Println("   The cut is saved, but your live context won't shrink until you reload it.")
		} else {
			fmt.Println("⚠  RESTART REQUIRED to take effect — a running session holds its")
			fmt.Println("   context in memory; Claude Code reloads the transcript only on resume.")
		}
		fmt.Printf("   →  exit Claude Code, then:  claude --resume %s\n", id)
	} else {
		fmt.Println("\nwrote a .cut.jsonl sibling — the original session is untouched.")
		fmt.Println("to use it: re-run with --in-place (or copy it over the original id), then")
		fmt.Printf("restart Claude Code:  claude --resume %s\n", id)
	}
	return 0
}

func doRestore(o options) int {
	rec, err := surgery.Load(o.restoreID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no surgery %q: %v\n", o.restoreID, err)
		return 1
	}
	target := rec.SessionPath
	if o.file != "" {
		target = o.file
	}
	current, err := readLines(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", target, err)
		return 1
	}
	restored, err := surgery.Restore(current, rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore failed: %v\n", err)
		return 1
	}
	// Restore is an undo, so it writes the session file itself (with a backup
	// first) — a "restored copy" sibling would just be confusing.
	bak, err := backup(target, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		return 1
	}
	if err := writeLines(target, restored); err != nil {
		fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
		return 1
	}
	fmt.Printf("restored surgery %s (%d lines re-inserted).\n", rec.ID, rec.RemovedLineCount())
	fmt.Printf("backup: %s\n", bak)
	fmt.Printf("wrote:  %s\n", target)
	fmt.Println("\n⚠  RESTART REQUIRED to take effect — a running session holds its context")
	fmt.Println("   in memory; Claude Code reloads the transcript only on resume.")
	fmt.Printf("   →  exit Claude Code, then:  claude --resume %s\n", sessionID(target))
	return 0
}

func listSurgeries(o options) int {
	recs := surgery.List("")
	if len(recs) == 0 {
		fmt.Println("no saved surgeries.")
		return 0
	}
	fmt.Printf("%-24s  %-10s  %-6s  turns  title\n", "id", "session", "~tok")
	for _, r := range recs {
		title := ""
		if len(r.Titles) > 0 {
			title = r.Titles[0]
		}
		fmt.Printf("%-24s  %-10s  %-6d  %-5s  %s\n",
			r.ID, short(r.SessionID), r.RemovedTokens, joinInts(r.CutTurns), title)
	}
	return 0
}

// --- printing ---

func printTurns(s *session.Session) {
	fmt.Printf("%s — %d turns, ~%d tokens\n\n", filepath.Base(s.Path), len(s.Turns), s.TotalTokens())
	for _, t := range s.Turns {
		ts := tsShort(t.Timestamp())
		fmt.Printf("%3d. %s  ~%6dt  %s\n", t.Number, ts, t.TokenEstimate(), t.Title(70))
	}
}

func printJSON(s *session.Session) {
	type turnJSON struct {
		Number  int    `json:"number"`
		Time    string `json:"timestamp"`
		Title   string `json:"title"`
		Prompt  string `json:"prompt"`
		Tokens  int    `json:"token_estimate"`
		Entries int    `json:"entries"`
		Detail  string `json:"detail"`
	}
	out := struct {
		Path        string     `json:"path"`
		SessionID   string     `json:"session_id"`
		TotalTokens int        `json:"total_tokens"`
		Turns       []turnJSON `json:"turns"`
	}{Path: s.Path, SessionID: sessionID(s.Path), TotalTokens: s.TotalTokens()}
	for _, t := range s.Turns {
		p := t.PromptText()
		if len(p) > 500 {
			p = p[:500]
		}
		out.Turns = append(out.Turns, turnJSON{
			Number: t.Number, Time: t.Timestamp(), Title: t.Title(80),
			Prompt: p, Tokens: t.TokenEstimate(), Entries: len(t.Entries), Detail: t.Detail(),
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}

func printMetas(metas []session.Meta) {
	fmt.Printf("%3s  %-16s  %-8s  %-18s  %s\n", "#", "modified", "id", "name", "ai-title")
	for i, m := range metas {
		when := time.Unix(m.ModTime, 0).Format("2006-01-02 15:04")
		name := m.Name
		if name == "" {
			name = "—"
		}
		fmt.Printf("%3d  %-16s  %-8s  %-18s  %s\n", i+1, when, short(m.ID), truncate(name, 18), m.Title)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// --- small helpers ---

func projectDir(project string) string {
	if project == "" {
		cwd, _ := os.Getwd()
		return session.ProjectDirFor(cwd)
	}
	// Already a slug dir under projects/?
	if strings.HasPrefix(filepath.Base(project), "-") || fileExists(filepath.Join(project)) && isDir(project) && strings.Contains(project, ".claude") {
		return project
	}
	abs, _ := filepath.Abs(expand(project))
	return session.ProjectDirFor(abs)
}

func parseNums(spec string, count int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			lohi := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(lohi[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(lohi[1]))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("bad range: %q", part)
			}
			if lo > hi {
				return nil, fmt.Errorf("bad range: %q (start > end)", part)
			}
			for n := lo; n <= hi; n++ {
				out[n] = true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("bad turn number: %q", part)
		}
		out[n] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nothing to cut in %q — pass turn numbers like 2,4 or 2-4", spec)
	}
	for n := range out {
		if n < 1 || n > count {
			return nil, fmt.Errorf("turn %d out of range 1-%d", n, count)
		}
	}
	return out, nil
}

func sessionID(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func joinInts(ns []int) string {
	if len(ns) == 0 {
		return "-"
	}
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func tsShort(ts string) string {
	if len(ts) >= 16 {
		return strings.Replace(ts[:16], "T", " ", 1)
	}
	return ts
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
func isDir(p string) bool      { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

func expand(p string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}

func relOrAbs(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		if rel, err := filepath.Rel(mustGetwd(), abs); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return p
}

func mustGetwd() string { d, _ := os.Getwd(); return d }
