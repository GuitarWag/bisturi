---
description: Surgically cut a topic/block of turns out of a Claude Code session's context
argument-hint: "[session-id | path | --project DIR]  (defaults to recent sessions of the current project)"
allowed-tools: Bash(bisturi:*), Bash(./bisturi:*), Bash(python3:*), Bash(ls:*), Bash(find:*), AskUserQuestion
---

You are driving **ctx-bisturi**, a tool that removes selected turns (topic
blocks) from a Claude Code session transcript and repairs the `uuid` chain so the
trimmed session still resumes. The user wants to see the blocks and confirm a
cut from inside this interface.

The `bisturi` command lives in the ctx-bisturi repo. Use `bisturi` if it's on
PATH, otherwise `./bisturi` from the repo, otherwise
`python3 -m ctxbisturi.cli` from the repo root.

User argument (may be empty): `$ARGUMENTS`

Follow these steps:

1. **Pick the session to operate on.**
   - If the argument is a `.jsonl` path, use it.
   - If it's a session id, resolve it under `~/.claude/projects/<slug>/`.
   - If it names a project or is empty, run `bisturi --list` (optionally with
     `--project`) and show the recent sessions. If more than one, ask the user
     which via AskUserQuestion (offer the 3–4 most recent by time + id).
   - IMPORTANT: the **currently-running** session cannot be safely edited while
     live — Claude Code may overwrite it on exit. If the target is the active
     session, tell the user the cut applies on the next resume/restart, and
     prefer writing a `.cut.jsonl` (not `--in-place`).

2. **Show the blocks.** Run `bisturi <path> --json` and present the turns as a
   compact numbered list: `#`, time, ~tokens, and the title. Give the user two
   ways to choose which to cut:
   - **Visual TUI:** tell them to run `!bisturi <path>` — the `!` runs it in
     their real terminal so the checkbox TUI opens here in the session (space to
     select, enter to confirm). When they paste back the result, continue.
   - **In-chat:** they tell you the numbers, or you use AskUserQuestion
     (multiSelect) with the most likely blocks as options and each block's
     prompt text as the option preview, so they pick and confirm inline.

3. **Preview the impact.** Before writing anything, run
   `bisturi <path> --cut <nums> --dry-run` and show how many turns and ~tokens
   would be removed.

4. **Confirm, then apply.** After the user confirms, run the real cut:
   `bisturi <path> --cut <nums>` (writes a sibling `*.cut.jsonl`, original
   untouched), or add `--in-place` if they explicitly want to replace the
   original (a `.bak-*` backup is written first). Report the exact output path.

5. **Explain resume.** Tell them how to load the trimmed session:
   - `--in-place`: reopen with `claude --resume <session-id>`.
   - `.cut.jsonl`: review it, then copy it over the original id (or use
     `--in-place`) so `--resume` picks it up.

Be concrete and safe: never overwrite without confirmation, always show the
dry-run first, and never touch a live session in place.
