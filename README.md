# ctx-bisturi

Surgically excise a topic from a Claude Code session's context — keep the parts
you still need, cut out the middle you don't.

*Bisturi* is Italian/Portuguese for scalpel. That's the whole idea: not a blunt
`/clear`, not a lossy `/compact`, but a clean cut of a specific span.

## The problem it solves

You start a session on **topic A**. Then **B** comes up, related to A. Now you
want to work on **C** — but B is dead weight bloating the context and confusing
the model. You want B gone while A and C stay intact.

Claude Code's built-ins don't do this:

| Built-in | What it does | Why it's not this |
| --- | --- | --- |
| `/clear` | wipes the whole context | loses A and C too |
| `/compact` | LLM-summarizes everything | lossy, and you can't choose *what* to drop |
| `/rewind` (Esc·Esc) | rolls back to an earlier checkpoint | **linear** — to drop B it also drops C |

Rewind moves a single pointer backwards in time. It can't remove a span from the
*middle* and keep what came after. That middle-excision is the gap `ctx-bisturi`
fills. As far as I've found, no mainstream tool does it — the ecosystem tools
(`ccexp`, `claude-code-exporter`, various viewers) export or browse sessions,
they don't rewrite the chain.

## How it works

A Claude Code session lives at
`~/.claude/projects/<slugged-cwd>/<session-id>.jsonl`. Each line is one JSON
object; `user` and `assistant` rows form a linear chain via `uuid` /
`parentUuid`. `ctx-bisturi`:

1. Groups the transcript into **turns** — one real user prompt plus its
   attachments, tool results, assistant replies, and metadata, up to the next
   prompt. (Synthetic prompts like hook feedback or skill preambles fold into
   their surrounding turn, so you select whole topics, not noise.)
2. Lets you pick turns to cut in a TUI (or by number on the CLI).
3. Removes them and **re-threads the chain**: the survivor after the gap relinks
   to the survivor before it, and any attachment/metadata row that pointed at a
   removed message is dropped — so a resume never dereferences a hole.
4. Writes a sibling `*.cut.jsonl` by default; `--in-place` replaces the original
   after taking a timestamped `.bak-*` copy. Untouched lines round-trip
   byte-for-byte.

Resume the trimmed session with `claude --resume <session-id>` (use `--in-place`,
or copy the `.cut.jsonl` over the original id, so Claude Code picks it up).

## Usage

No dependencies — stdlib Python 3.10+ and a terminal.

```bash
./bisturi                        # pick a session in the current project, then TUI
./bisturi --project ~/code/app   # sessions for another project
./bisturi path/to/session.jsonl  # operate on a file directly
./bisturi --list                 # list sessions for the current project
```

Inspect and cut without the TUI:

```bash
./bisturi session.jsonl --print              # numbered turn breakdown + token estimates
./bisturi session.jsonl --cut 2,3 --dry-run  # show what would change, write nothing
./bisturi session.jsonl --cut 2-3            # writes session.cut.jsonl
./bisturi session.jsonl --cut 2 --in-place   # replace original, keep a .bak-* backup
```

## Inside Claude Code (`/ctx-cut`)

There's a slash command so you can do this from within a Claude session. A
slash command can't *host* a full-screen TUI itself (Claude's shell has no
interactive terminal), so `/ctx-cut` gives you two ways to see and confirm the
blocks, both inside the interface:

- **Visual TUI** — the command hands you `!bisturi <session>`. The `!` prefix
  runs it in your real terminal, so the checkbox TUI opens right there in the
  session; its result prints back into the chat.
- **In-chat** — Claude lists the numbered blocks and confirms your selection
  with Claude Code's native selectable question UI, then shows a dry-run before
  writing anything.

```
/ctx-cut                         # recent sessions of the current project
/ctx-cut <session-id>            # a specific session
/ctx-cut --project ~/code/app    # another project
```

Install: copy `.claude/commands/ctx-cut.md` to `~/.claude/commands/` (global) or
keep it in a project's `.claude/commands/`, and put `bisturi` on your `PATH`
(e.g. `ln -s "$PWD/bisturi" ~/.local/bin/bisturi`).

Note: you can't hot-edit the **currently running** session — Claude Code owns
that file while live. Cut a past session, or cut the current one and pick up the
trimmed context on the next `claude --resume`.

### TUI keys

| Key | Action |
| --- | --- |
| `↑`/`↓` or `k`/`j` | move between turns |
| `space` | mark/unmark a turn for cutting |
| `a` / `z` | scroll the preview pane down / up |
| `enter` | apply the cut (asks to confirm) |
| `q` / `Esc` | quit without changing anything |

Install as a command if you prefer: `pip install -e .` then `bisturi …`.

## Safety

- The original is never touched unless you pass `--in-place`, and even then a
  `.bak-<timestamp>` copy is written first.
- Writes are atomic (temp file + `os.replace`).
- The resulting chain is validated in tests to be single-rooted, fully
  reachable, and free of dangling `parentUuid`/`leafUuid` references.

## Caveats

- Re-threading assumes a **linear** session (the normal case). A session already
  branched by prior rewinds is linearized to file order on cut.
- Token counts are estimates (~4 chars/token), for relative sizing, not billing.
- Close the session in Claude Code before cutting in place, so it doesn't
  overwrite your changes on exit.

## Development

```bash
python3 -m pytest tests/ -q
```
