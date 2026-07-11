---
description: Surgically cut a topic/block of turns out of a Claude Code session's context
argument-hint: "[-s name | session-id | path | --project DIR]  (defaults to recent sessions of the current project)"
allowed-tools: Bash(bisturi:*), Bash(ls:*), Bash(find:*), AskUserQuestion
---

You are driving **ctx-bisturi** (a Go CLI), which removes selected turns (topic
blocks) from a Claude Code session transcript and relinks it so the trimmed
session still resumes. Every cut is also saved as a restorable surgery. The user
wants to see the blocks and confirm a cut from inside this interface.

Use the `bisturi` command (on PATH). If it's missing, build it: from the
ctx-bisturi repo run `go build -o bisturi .` and use `./bisturi`.

User argument (may be empty): `$ARGUMENTS`

Follow these steps:

1. **Pick the session to operate on.**
   - If the argument is a `.jsonl` path, use it.
   - If it's `-s <name>` or a bare word, pass it as `bisturi -s <name>` (matches
     session id or ai-title, substring ok). If it names a project, add
     `--project <dir>`.
   - If empty, run `bisturi --list` and show recent sessions. If more than one,
     ask which via AskUserQuestion (offer the 3–4 most recent by time + title).
   - IMPORTANT: the **currently-running** session cannot be safely edited while
     live — Claude Code may overwrite it on exit. If the target is the active
     session, tell the user the cut applies on the next resume/restart, and
     prefer writing a `.cut.jsonl` (not `--in-place`).

2. **Show the blocks.** Run `bisturi <selector> --json` and present the turns as a
   compact numbered list: `#`, time, ~tokens, title. Give two ways to choose:
   - **Visual TUI:** tell them to run `!bisturi <selector>` — the `!` runs it in
     their real terminal so the checkbox TUI opens here (space select, `d` diff,
     `y` apply). When they paste back the result, continue.
   - **In-chat:** they tell you the numbers, or you use AskUserQuestion
     (multiSelect) with the likely blocks as options and each block's prompt text
     as the preview, so they pick and confirm inline.

3. **Preview the impact.** Before writing, run `bisturi <selector> --cut <nums>
   --dry-run` and show how many turns and ~tokens would go.

4. **Confirm, then apply.** After confirmation, run `bisturi <selector> --cut
   <nums>` (writes a sibling `*.cut.jsonl`, original untouched), or add
   `--in-place` to replace the original (a `.bak-*` backup is written first).
   Report the output path and the surgery id it prints.

5. **Explain undo + resume.**
   - Undo: `bisturi --restore <surgery-id> --in-place` puts the blocks back
     (works even if the session grew since). `bisturi --surgeries` lists them.
   - Resume: with `--in-place`, reopen via `claude --resume <session-id>`; with a
     `.cut.jsonl`, review it then copy it over the original id.

Be concrete and safe: never overwrite without confirmation, always show the
dry-run first, and never touch a live session in place.
