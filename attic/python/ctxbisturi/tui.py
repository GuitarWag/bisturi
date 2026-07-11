"""A stdlib-curses TUI for selecting which turns to cut from a session.

Left column: the turn list with checkboxes and token estimates.
Right column: a scrollable preview of the highlighted turn.
No third-party dependencies — runs anywhere Python and a terminal exist.
"""

from __future__ import annotations

import curses
from typing import Callable

from .session import Session, Turn

HELP = "↑/↓ move · space select · a preview-scroll · enter APPLY · q quit"


class _State:
    def __init__(self, session: Session):
        self.session = session
        self.turns: list[Turn] = session.turns
        self.cursor = 0
        self.selected: set[int] = set()
        self.preview_scroll = 0
        self.applied = False

    def toggle(self) -> None:
        if not self.turns:
            return
        n = self.turns[self.cursor].number
        if n in self.selected:
            self.selected.discard(n)
        else:
            self.selected.add(n)

    def move(self, delta: int) -> None:
        if not self.turns:
            return
        self.cursor = max(0, min(len(self.turns) - 1, self.cursor + delta))
        self.preview_scroll = 0

    def kept_tokens(self) -> int:
        return sum(
            t.token_estimate() for t in self.turns if t.number not in self.selected
        )


def run(session: Session) -> set[int]:
    """Show the TUI; return the set of turn numbers the user chose to cut.

    An empty set means the user quit without applying.
    """
    state = _State(session)
    curses.wrapper(_loop, state)
    return state.selected if state.applied else set()


def _loop(stdscr: "curses._CursesWindow", state: _State) -> None:
    curses.curs_set(0)
    stdscr.keypad(True)
    _init_colors()
    while True:
        _draw(stdscr, state)
        key = stdscr.getch()
        if key in (ord("q"), 27):  # q or Esc
            return
        if key in (curses.KEY_UP, ord("k")):
            state.move(-1)
        elif key in (curses.KEY_DOWN, ord("j")):
            state.move(1)
        elif key == ord(" "):
            state.toggle()
        elif key == ord("a"):
            state.preview_scroll += 1
        elif key == ord("z"):
            state.preview_scroll = max(0, state.preview_scroll - 1)
        elif key in (curses.KEY_ENTER, 10, 13):
            if state.selected and _confirm(stdscr, state):
                state.applied = True
                return


def _init_colors() -> None:
    if not curses.has_colors():
        return
    curses.start_color()
    curses.use_default_colors()
    curses.init_pair(1, curses.COLOR_CYAN, -1)     # header
    curses.init_pair(2, curses.COLOR_RED, -1)      # selected-to-cut
    curses.init_pair(3, curses.COLOR_YELLOW, -1)   # cursor
    curses.init_pair(4, curses.COLOR_GREEN, -1)    # footer / kept


def _cp(n: int) -> int:
    try:
        return curses.color_pair(n) if curses.has_colors() else 0
    except curses.error:  # colors not initialized (e.g. outside a live screen)
        return 0


def _draw(stdscr: "curses._CursesWindow", state: _State) -> None:
    stdscr.erase()
    h, w = stdscr.getmaxyx()
    left_w = max(32, w // 2)

    total = state.session.total_tokens()
    kept = state.kept_tokens()
    cut_n = len(state.selected)
    header = f" ctx-bisturi  ·  {len(state.turns)} turns  ·  ~{total} tok total"
    _addstr(stdscr, 0, 0, header.ljust(w), _cp(1) | curses.A_BOLD)

    status = f" cutting {cut_n} turn(s) → keeps ~{kept}/{total} tok (~{total - kept} removed) "
    _addstr(stdscr, 1, 0, status.ljust(w), _cp(4))

    # Turn list (left)
    list_top = 3
    list_h = h - list_top - 1
    start = max(0, state.cursor - list_h + 1)
    for row, turn in enumerate(state.turns[start : start + list_h]):
        y = list_top + row
        idx = start + row
        mark = "[x]" if turn.number in state.selected else "[ ]"
        ts = turn.timestamp[11:16] if len(turn.timestamp) >= 16 else ""
        label = f" {mark} {turn.number:>2}. {ts} ~{turn.token_estimate():>5}t {turn.title(left_w - 22)}"
        attr = 0
        if turn.number in state.selected:
            attr = _cp(2)
        if idx == state.cursor:
            attr = _cp(3) | curses.A_REVERSE
        _addstr(stdscr, y, 0, label[:left_w].ljust(left_w), attr)

    # Divider
    for y in range(list_top, h - 1):
        _addstr(stdscr, y, left_w, "│", 0)

    # Preview (right)
    if state.turns:
        preview = state.turns[state.cursor].preview()
        pv_x = left_w + 2
        pv_w = w - pv_x - 1
        wrapped: list[str] = []
        for line in preview:
            wrapped.extend(_wrap(line, pv_w))
        view = wrapped[state.preview_scroll : state.preview_scroll + list_h]
        for row, line in enumerate(view):
            attr = _cp(1) if line.startswith("──") else 0
            _addstr(stdscr, list_top + row, pv_x, line[:pv_w], attr)

    _addstr(stdscr, h - 1, 0, HELP[:w].ljust(w), _cp(4) | curses.A_REVERSE)
    stdscr.refresh()


def _confirm(stdscr: "curses._CursesWindow", state: _State) -> bool:
    h, w = stdscr.getmaxyx()
    nums = ", ".join(str(n) for n in sorted(state.selected))
    msg = f" Cut turns {nums}?  y = yes, any other key = no "
    _addstr(stdscr, h - 1, 0, msg[:w].ljust(w), _cp(2) | curses.A_REVERSE)
    stdscr.refresh()
    return stdscr.getch() in (ord("y"), ord("Y"))


def _wrap(line: str, width: int) -> list[str]:
    if width <= 0:
        return [line]
    if len(line) <= width:
        return [line]
    out = []
    while len(line) > width:
        out.append(line[:width])
        line = line[width:]
    out.append(line)
    return out


def _addstr(win: "curses._CursesWindow", y: int, x: int, text: str, attr: int) -> None:
    h, w = win.getmaxyx()
    if y < 0 or y >= h or x >= w:
        return
    try:
        win.addstr(y, x, text[: max(0, w - x - 1)], attr)
    except curses.error:
        pass
