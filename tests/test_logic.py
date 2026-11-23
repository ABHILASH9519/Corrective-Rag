"""Pure-logic tests: no network, no API key, no heavy dependencies."""

import asyncio
import json

import pytest

from crag import evaluator, refine
from crag.kb import Doc, load_passages


def test_decide_correct_when_any_relevant():
    assert evaluator.decide({"a": "relevant", "b": "irrelevant"}) == "CORRECT"


def test_decide_incorrect_when_all_irrelevant():
    assert evaluator.decide({"a": "irrelevant", "b": "irrelevant"}) == "INCORRECT"


def test_decide_incorrect_when_nothing_retrieved():
    assert evaluator.decide({}) == "INCORRECT"


def test_decide_ambiguous_for_partial():
    assert evaluator.decide({"a": "partial", "b": "irrelevant"}) == "AMBIGUOUS"


def test_split_strips_numbers_across_docs_and_tags_source():
    docs = [Doc("one", "First. Second!", 0.9), Doc("two", "Third?", 0.8)]
    strips = refine.split_strips(docs)
    assert strips == {
        "s1": "First. [KB:one]",
        "s2": "Second! [KB:one]",
        "s3": "Third? [KB:two]",
    }


def test_refine_keeps_only_marked_strips_in_order(monkeypatch):
    async def fake_ask_json(system, user, name, schema):
        return {"strips": [{"id": "s1", "keep": False}, {"id": "s2", "keep": True}]}

    monkeypatch.setattr(refine, "ask_json", fake_ask_json)
    docs = [Doc("one", "Nope. Yes.", 0.9)]
    assert asyncio.run(refine.refine("q", docs)) == ["Yes. [KB:one]"]


def test_grade_defaults_skipped_passages_to_irrelevant(monkeypatch):
    async def fake_ask_json(system, user, name, schema):
        return {"grades": [{"id": "a", "label": "relevant"}]}

    monkeypatch.setattr(evaluator, "ask_json", fake_ask_json)
    docs = [Doc("a", "x", 0.5), Doc("b", "y", 0.4)]
    assert asyncio.run(evaluator.grade("q", docs)) == {"a": "relevant", "b": "irrelevant"}


def test_load_passages_validates(tmp_path):
    good = tmp_path / "kb.json"
    good.write_text(json.dumps([{"id": "a", "text": " hello "}]))
    assert load_passages(good) == {"a": "hello"}

    dup = tmp_path / "dup.json"
    dup.write_text(json.dumps([{"id": "a", "text": "x"}, {"id": "a", "text": "y"}]))
    with pytest.raises(SystemExit):
        load_passages(dup)

    bad = tmp_path / "bad.json"
    bad.write_text(json.dumps([{"id": "a"}]))
    with pytest.raises(SystemExit):
        load_passages(bad)


def test_shipped_knowledge_base_is_valid():
    from crag.config import SETTINGS

    passages = load_passages(SETTINGS.kb_path)
    assert len(passages) == 15
