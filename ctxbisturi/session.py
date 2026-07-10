"""Parse a Claude Code session JSONL, group it into turns, and cut turns safely.

Claude Code stores each session as a JSONL file under
``~/.claude/projects/<slugged-cwd>/<session-id>.jsonl``. Every line is one JSON
object. The ones that matter for context are ``user`` and ``assistant`` rows,
which form a linear chain through ``uuid`` / ``parentUuid``. Other row types
(``attachment``, ``mode``, ``ai-title``, ``file-history-snapshot`` …) hang off
that chain or are standalone metadata.

A *turn* here is one real user prompt plus everything that follows it — its
attachments, the assistant replies, tool-result rows, and trailing metadata —
up to the next real user prompt. That is the unit the TUI lets you cut.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

# Row types that carry a place in the user/assistant conversation chain.
CHAIN_TYPES = ("user", "assistant")


@dataclass
class Entry:
    """One physical line of the JSONL file."""

    index: int          # 0-based line number in the original file
    raw: str            # exact original text (so untouched lines round-trip byte-for-byte)
    obj: dict[str, Any]  # parsed JSON (empty dict if the line failed to parse)

    @property
    def type(self) -> str:
        return self.obj.get("type", "")

    @property
    def uuid(self) -> str | None:
        return self.obj.get("uuid")

    @property
    def parent_uuid(self) -> str | None:
        return self.obj.get("parentUuid")

    def is_prompt(self) -> bool:
        """True for a *real* user prompt (text), not a tool_result carrier."""
        if self.type != "user":
            return False
        if self.obj.get("isMeta"):
            return False
        content = self.obj.get("message", {}).get("content")
        if isinstance(content, str):
            return content.strip() != ""
        if isinstance(content, list):
            has_text = any(
                isinstance(b, dict) and b.get("type") == "text" and b.get("text", "").strip()
                for b in content
            )
            is_tool_result = any(
                isinstance(b, dict) and b.get("type") == "tool_result" for b in content
            )
            return has_text and not is_tool_result
        return False


@dataclass
class Turn:
    """A real prompt and every entry that belongs to it, in file order."""

    number: int                       # 1-based turn number
    entries: list[Entry] = field(default_factory=list)

    @property
    def prompt_entry(self) -> Entry:
        return self.entries[0]

    @property
    def timestamp(self) -> str:
        return self.prompt_entry.obj.get("timestamp", "")

    @property
    def prompt_text(self) -> str:
        return _text_of(self.prompt_entry.obj)

    def title(self, width: int = 60) -> str:
        text = " ".join(self.prompt_text.split())
        if len(text) > width:
            text = text[: width - 1] + "…"
        return text or "(empty prompt)"

    def preview(self) -> list[str]:
        """Human-readable multi-line summary of everything in the turn."""
        lines: list[str] = []
        for e in self.entries:
            t = e.type
            if t == "user" and e.is_prompt():
                lines.append("── you ──")
                lines.extend(_text_of(e.obj).splitlines() or [""])
            elif t == "user":
                lines.append("  [tool result]")
            elif t == "assistant":
                for label in _assistant_labels(e.obj):
                    lines.append(label)
            elif t == "attachment":
                lines.append("  [attachment]")
            # metadata rows are omitted from the preview
        return lines

    def char_size(self) -> int:
        return sum(len(e.raw) for e in self.entries)

    def token_estimate(self) -> int:
        # ~4 chars per token is the usual rough rule for English/code.
        return self.char_size() // 4


def _text_of(obj: dict[str, Any]) -> str:
    content = obj.get("message", {}).get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for b in content:
            if isinstance(b, dict) and b.get("type") == "text":
                parts.append(b.get("text", ""))
        return "\n".join(parts)
    return ""


def _assistant_labels(obj: dict[str, Any]) -> list[str]:
    content = obj.get("message", {}).get("content")
    out: list[str] = []
    if isinstance(content, str):
        return ["── claude ──", *content.splitlines()]
    if isinstance(content, list):
        for b in content:
            if not isinstance(b, dict):
                continue
            bt = b.get("type")
            if bt == "text" and b.get("text", "").strip():
                out.append("── claude ──")
                out.extend(b.get("text", "").splitlines())
            elif bt == "thinking":
                out.append("  [thinking]")
            elif bt == "tool_use":
                out.append(f"  [tool: {b.get('name', '?')}]")
    return out


class Session:
    """A parsed session file, grouped into turns and cuttable by turn number."""

    def __init__(self, path: str, entries: list[Entry]):
        self.path = path
        self.entries = entries
        self.preamble, self.turns = _group(entries)

    @classmethod
    def load(cls, path: str) -> "Session":
        entries: list[Entry] = []
        with open(path, encoding="utf-8") as fh:
            for i, line in enumerate(fh):
                stripped = line.rstrip("\n")
                if not stripped.strip():
                    continue
                try:
                    obj = json.loads(stripped)
                except json.JSONDecodeError:
                    obj = {}
                entries.append(Entry(index=i, raw=stripped, obj=obj))
        return cls(path, entries)

    def total_tokens(self) -> int:
        return sum(t.token_estimate() for t in self.turns)

    def cut(self, turn_numbers: set[int]) -> list[str]:
        """Return new JSONL lines with the given turns removed and the chain repaired.

        Does not touch disk — callers decide where to write. See :func:`cut_entries`.
        """
        keep_entries = list(self.preamble)
        for turn in self.turns:
            if turn.number not in turn_numbers:
                keep_entries.extend(turn.entries)
        return _rethread(keep_entries)


def _group(entries: list[Entry]) -> tuple[list[Entry], list[Turn]]:
    """Split entries into a leading preamble and one Turn per real prompt."""
    first_prompt = next((i for i, e in enumerate(entries) if e.is_prompt()), None)
    if first_prompt is None:
        return list(entries), []
    preamble = entries[:first_prompt]
    turns: list[Turn] = []
    current: Turn | None = None
    for e in entries[first_prompt:]:
        if e.is_prompt():
            current = Turn(number=len(turns) + 1, entries=[e])
            turns.append(current)
        else:
            assert current is not None
            current.entries.append(e)
    return preamble, turns


def _rethread(entries: list[Entry]) -> list[str]:
    """Repair the uuid chain of a surviving set of entries and serialize to lines.

    Sessions are linear, so after removing a middle block we relink each
    surviving chain message's ``parentUuid`` to the previous surviving chain
    message. Rows (attachments, metadata) that reference a uuid no longer
    present are dropped so a resume never dereferences a hole.
    """
    surviving_uuids = {e.uuid for e in entries if e.type in CHAIN_TYPES and e.uuid}

    out_lines: list[str] = []
    prev_chain_uuid: str | None = None
    for e in entries:
        obj = e.obj
        if not obj:
            out_lines.append(e.raw)
            continue

        if e.type in CHAIN_TYPES:
            # Relink to the previous survivor; untouched if it already matches.
            if obj.get("parentUuid") != prev_chain_uuid:
                obj = dict(obj)
                obj["parentUuid"] = prev_chain_uuid
                out_lines.append(json.dumps(obj, ensure_ascii=False))
            else:
                out_lines.append(e.raw)
            prev_chain_uuid = e.uuid
            continue

        # Non-chain rows: drop if they point at a removed message.
        ref = obj.get("parentUuid")
        leaf = obj.get("leafUuid")
        if ref is not None and ref not in surviving_uuids:
            continue
        if leaf is not None and leaf not in surviving_uuids:
            continue
        out_lines.append(e.raw)

    return out_lines
