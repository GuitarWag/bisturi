"""Command-line entry point for ctx-bisturi.

Typical use::

    bisturi                       # pick a session from the current project, then TUI
    bisturi --project ~/code/app  # sessions for another project
    bisturi path/to/session.jsonl # operate on a file directly
    bisturi session.jsonl --cut 2,4 --in-place   # non-interactive

Cutting writes a sibling ``*.cut.jsonl`` by default. Pass ``--in-place`` to
replace the original (a timestamped ``.bak-*`` copy is made first).
"""

from __future__ import annotations

import argparse
import os
import sys

from . import __version__
from .session import Session
from .storage import (
    SessionFile,
    list_sessions,
    project_dir_for,
    write_cut,
)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="bisturi",
        description="Surgically cut topics out of a Claude Code session transcript.",
    )
    parser.add_argument("file", nargs="?", help="a session .jsonl file to operate on")
    parser.add_argument("--project", metavar="DIR",
                        help="pick a session from this project's dir (default: cwd)")
    parser.add_argument("--list", action="store_true", help="list sessions and exit")
    parser.add_argument("--print", dest="print_turns", action="store_true",
                        help="print the turn breakdown and exit (no TUI)")
    parser.add_argument("--json", action="store_true",
                        help="emit the turn breakdown as JSON and exit (for tools/commands)")
    parser.add_argument("--cut", metavar="NUMS",
                        help="comma-separated turn numbers to cut (skips the TUI)")
    parser.add_argument("--in-place", action="store_true",
                        help="replace the original (a .bak-* backup is written first)")
    parser.add_argument("--dry-run", action="store_true",
                        help="with --cut, report what would change but write nothing")
    parser.add_argument("--version", action="version", version=f"ctx-bisturi {__version__}")
    args = parser.parse_args(argv)

    path = args.file or _pick_session(args)
    if path is None:
        return 1
    if not os.path.isfile(path):
        print(f"no such file: {path}", file=sys.stderr)
        return 1

    session = Session.load(path)
    if not session.turns:
        print("no user prompts found — nothing to cut.", file=sys.stderr)
        return 1

    if args.json:
        _print_json(session)
        return 0

    if args.print_turns or args.list and args.file:
        _print_turns(session)
        return 0

    if args.cut:
        to_cut = _parse_nums(args.cut, len(session.turns))
        if to_cut is None:
            return 1
        return _apply(session, to_cut, in_place=args.in_place, dry_run=args.dry_run)

    # Interactive path — needs a real terminal. When invoked without one
    # (e.g. from an agent's captured shell), guide the caller instead of crashing.
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        _print_turns(session)
        print("\nno interactive terminal here — the TUI needs a real TTY.", file=sys.stderr)
        print("• inside Claude Code, run it yourself with:  !bisturi "
              f"{os.path.relpath(path) if not os.path.isabs(path) else path}", file=sys.stderr)
        print("• or cut non-interactively:  bisturi <file> --cut <nums> [--in-place]",
              file=sys.stderr)
        return 2

    from .tui import run as run_tui  # imported lazily so --list works without a tty

    to_cut = run_tui(session)
    if not to_cut:
        print("nothing cut.")
        return 0
    return _apply(session, to_cut, in_place=args.in_place, dry_run=args.dry_run)


def _pick_session(args: argparse.Namespace) -> str | None:
    project = args.project or os.getcwd()
    project_dir = project if os.path.basename(project).startswith("-Users") else project_dir_for(
        os.path.abspath(os.path.expanduser(project))
    )
    sessions = list_sessions(project_dir)
    if not sessions:
        print(f"no sessions found in {project_dir}", file=sys.stderr)
        print("pass a .jsonl path directly, or --project <dir>.", file=sys.stderr)
        return None

    if args.list:
        _print_sessions(sessions)
        return None

    if len(sessions) == 1:
        return sessions[0].path

    _print_sessions(sessions)
    try:
        choice = input(f"\nselect session [1-{len(sessions)}] (q to quit): ").strip()
    except EOFError:
        return None
    if choice.lower() in ("q", ""):
        return None
    if choice.isdigit() and 1 <= int(choice) <= len(sessions):
        return sessions[int(choice) - 1].path
    print("invalid selection.", file=sys.stderr)
    return None


def _print_sessions(sessions: list[SessionFile]) -> None:
    import time

    print(f"{'#':>3}  {'modified':<16}  {'size':>8}  session")
    for i, s in enumerate(sessions, 1):
        when = time.strftime("%Y-%m-%d %H:%M", time.localtime(s.mtime))
        print(f"{i:>3}  {when:<16}  {s.size:>8}  {s.session_id}")


def _print_turns(session: Session) -> None:
    total = session.total_tokens()
    print(f"{os.path.basename(session.path)} — {len(session.turns)} turns, ~{total} tokens\n")
    for t in session.turns:
        ts = t.timestamp[:19].replace("T", " ")
        print(f"{t.number:>3}. {ts}  ~{t.token_estimate():>6}t  {t.title(70)}")


def _print_json(session: Session) -> None:
    import json

    payload = {
        "path": session.path,
        "session_id": os.path.splitext(os.path.basename(session.path))[0],
        "total_tokens": session.total_tokens(),
        "turns": [
            {
                "number": t.number,
                "timestamp": t.timestamp,
                "title": t.title(80),
                "prompt": t.prompt_text[:500],
                "token_estimate": t.token_estimate(),
                "entries": len(t.entries),
            }
            for t in session.turns
        ],
    }
    print(json.dumps(payload, ensure_ascii=False, indent=2))


def _parse_nums(spec: str, count: int) -> set[int] | None:
    nums: set[int] = set()
    for part in spec.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:  # allow ranges like 2-4
            a, _, b = part.partition("-")
            try:
                lo, hi = int(a), int(b)
            except ValueError:
                print(f"bad range: {part!r}", file=sys.stderr)
                return None
            nums.update(range(lo, hi + 1))
        elif part.isdigit():
            nums.add(int(part))
        else:
            print(f"bad turn number: {part!r}", file=sys.stderr)
            return None
    bad = [n for n in nums if n < 1 or n > count]
    if bad:
        print(f"turn(s) out of range 1-{count}: {bad}", file=sys.stderr)
        return None
    return nums


def _apply(session: Session, to_cut: set[int], *, in_place: bool, dry_run: bool) -> int:
    lines = session.cut(to_cut)
    kept = len(session.turns) - len(to_cut)
    removed_tokens = sum(
        t.token_estimate() for t in session.turns if t.number in to_cut
    )
    nums = ", ".join(str(n) for n in sorted(to_cut))
    if dry_run:
        print(f"[dry-run] would cut turns {nums}")
        print(f"[dry-run] {kept} turns kept, ~{removed_tokens} tokens removed, "
              f"{len(lines)} lines remain")
        return 0
    out, backup = write_cut(session.path, lines, in_place=in_place)
    print(f"cut turns {nums} — ~{removed_tokens} tokens removed.")
    if backup:
        print(f"backup: {backup}")
    print(f"wrote:  {out}")
    if not in_place:
        print("\nreview it, then replace the original yourself, or re-run with --in-place.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
