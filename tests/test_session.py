"""Tests for parsing, turn grouping, and safe cutting.

The fixture mirrors a real Claude Code session: a preamble of metadata rows, a
linear user/assistant chain threaded by uuid/parentUuid, attachments and
tool_result rows hanging off prompts, and trailing metadata that references
message uuids by leafUuid.
"""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from ctxbisturi.session import Session  # noqa: E402


def _user_prompt(uuid, parent, text):
    return {
        "type": "user", "uuid": uuid, "parentUuid": parent,
        "timestamp": "2026-07-09T17:00:00.000Z",
        "message": {"role": "user", "content": [{"type": "text", "text": text}]},
    }


def _assistant(uuid, parent, text):
    return {
        "type": "assistant", "uuid": uuid, "parentUuid": parent,
        "message": {"role": "assistant", "content": [{"type": "text", "text": text}]},
    }


def _tool_result(uuid, parent):
    return {
        "type": "user", "uuid": uuid, "parentUuid": parent,
        "message": {"role": "user", "content": [{"type": "tool_result", "content": "ok"}]},
    }


def _attachment(parent):
    return {"type": "attachment", "parentUuid": parent, "attachment": {"x": 1}}


def _meta_leaf(leaf):
    return {"type": "last-prompt", "leafUuid": leaf, "lastPrompt": "..."}


def build_fixture():
    rows = [
        {"type": "mode", "mode": "default"},            # preamble
        {"type": "file-history-snapshot", "messageId": "s0"},
        # Turn A
        _user_prompt("a1", None, "work on topic A"),
        _attachment("a1"),
        _assistant("a2", "a1", "doing A, tool time"),
        _tool_result("a3", "a2"),
        _assistant("a4", "a3", "A done"),
        _meta_leaf("a4"),
        # Turn B  (the one we want to excise)
        _user_prompt("b1", "a4", "now topic B related to A"),
        _assistant("b2", "b1", "doing B"),
        _tool_result("b3", "b2"),
        _assistant("b4", "b3", "B done"),
        _meta_leaf("b4"),
        # Turn C
        _user_prompt("c1", "b4", "topic C now"),
        _assistant("c2", "c1", "doing C"),
        _meta_leaf("c2"),
    ]
    return "\n".join(json.dumps(r) for r in rows) + "\n"


def write_fixture(tmpdir):
    p = os.path.join(tmpdir, "sess.jsonl")
    with open(p, "w") as fh:
        fh.write(build_fixture())
    return p


def test_grouping(tmp_path):
    s = Session.load(write_fixture(tmp_path))
    assert len(s.turns) == 3
    assert [t.number for t in s.turns] == [1, 2, 3]
    assert s.turns[0].title().startswith("work on topic A")
    assert s.turns[1].title().startswith("now topic B")
    # preamble holds the leading metadata, not any prompt
    assert all(not e.is_prompt() for e in s.preamble)
    assert len(s.preamble) == 2


def test_cut_middle_turn_rethreads_chain(tmp_path):
    s = Session.load(write_fixture(tmp_path))
    lines = s.cut({2})  # remove topic B
    objs = [json.loads(l) for l in lines]

    uuids = {o.get("uuid") for o in objs if o.get("type") in ("user", "assistant")}
    # None of B's messages survive.
    assert uuids == {"a1", "a2", "a3", "a4", "c1", "c2"}

    # C1 must now hang off A4 (the gap is closed), not the deleted b4.
    c1 = next(o for o in objs if o.get("uuid") == "c1")
    assert c1["parentUuid"] == "a4"

    # The whole chain is walkable: every non-root parentUuid resolves.
    chain = [o for o in objs if o.get("type") in ("user", "assistant")]
    present = {o["uuid"] for o in chain}
    for o in chain:
        if o["parentUuid"] is not None:
            assert o["parentUuid"] in present


def test_cut_drops_dangling_meta_and_attachments(tmp_path):
    s = Session.load(write_fixture(tmp_path))
    objs = [json.loads(l) for l in s.cut({2})]
    # The last-prompt row pointing at b4 must be gone; the ones for a4/c2 stay.
    leaves = [o.get("leafUuid") for o in objs if o.get("type") == "last-prompt"]
    assert "b4" not in leaves
    assert set(leaves) == {"a4", "c2"}


def test_cut_first_turn_makes_new_root(tmp_path):
    s = Session.load(write_fixture(tmp_path))
    objs = [json.loads(l) for l in s.cut({1})]
    chain = [o for o in objs if o.get("type") in ("user", "assistant")]
    # First surviving message becomes the root (parentUuid None).
    assert chain[0]["uuid"] == "b1"
    assert chain[0]["parentUuid"] is None


def test_cut_nothing_roundtrips(tmp_path):
    p = write_fixture(tmp_path)
    s = Session.load(p)
    lines = s.cut(set())
    reparsed = [json.loads(l) for l in lines]
    original = [json.loads(l) for l in build_fixture().splitlines()]
    assert reparsed == original


def test_untouched_lines_are_byte_identical(tmp_path):
    # Cutting turn 3 (the tail) must not rewrite turn-1 lines at all.
    p = write_fixture(tmp_path)
    s = Session.load(p)
    lines = s.cut({3})
    original_lines = build_fixture().splitlines()
    # a1..a4 lines appear verbatim.
    for original in original_lines[2:8]:
        assert original in lines
