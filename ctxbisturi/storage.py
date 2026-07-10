"""Locate Claude Code session files and write cut results without losing data."""

from __future__ import annotations

import os
import shutil
import time
from dataclasses import dataclass


def projects_root() -> str:
    return os.path.expanduser("~/.claude/projects")


def slug_for_cwd(cwd: str) -> str:
    """Claude Code slugs a working directory by replacing os.sep and '.' with '-'."""
    return cwd.replace(os.sep, "-").replace(".", "-")


@dataclass
class SessionFile:
    path: str
    mtime: float
    size: int

    @property
    def session_id(self) -> str:
        return os.path.splitext(os.path.basename(self.path))[0]


def list_sessions(project_dir: str) -> list[SessionFile]:
    if not os.path.isdir(project_dir):
        return []
    out = []
    for name in os.listdir(project_dir):
        if not name.endswith(".jsonl"):
            continue
        p = os.path.join(project_dir, name)
        try:
            st = os.stat(p)
        except OSError:
            continue
        out.append(SessionFile(path=p, mtime=st.st_mtime, size=st.st_size))
    out.sort(key=lambda s: s.mtime, reverse=True)
    return out


def project_dir_for(cwd: str) -> str:
    return os.path.join(projects_root(), slug_for_cwd(cwd))


def backup_path(path: str) -> str:
    # A fixed-per-second suffix keeps this deterministic within a call.
    stamp = time.strftime("%Y%m%d-%H%M%S", time.localtime())
    return f"{path}.bak-{stamp}"


def write_cut(
    src_path: str,
    lines: list[str],
    *,
    in_place: bool = False,
) -> tuple[str, str | None]:
    """Write the cut transcript.

    Returns ``(output_path, backup_path)``. When ``in_place`` is False (default)
    the original is untouched and a sibling ``.cut.jsonl`` is written. When True,
    a timestamped ``.bak-*`` copy is made first, then the original is replaced.
    """
    payload = "\n".join(lines) + ("\n" if lines else "")
    if in_place:
        bak = backup_path(src_path)
        shutil.copy2(src_path, bak)
        _atomic_write(src_path, payload)
        return src_path, bak
    base, ext = os.path.splitext(src_path)
    out = f"{base}.cut{ext}"
    _atomic_write(out, payload)
    return out, None


def _atomic_write(path: str, payload: str) -> None:
    tmp = f"{path}.tmp-{os.getpid()}"
    with open(tmp, "w", encoding="utf-8") as fh:
        fh.write(payload)
        fh.flush()
        os.fsync(fh.fileno())
    os.replace(tmp, path)
