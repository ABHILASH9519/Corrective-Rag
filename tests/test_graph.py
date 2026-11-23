"""Graph routing tests with every external call stubbed out."""
import asyncio

import pytest

pytest.importorskip("langgraph")

from crag import evaluator, graph, kb, llm, refine, web
from crag.kb import Doc


@pytest.fixture(autouse=True)
def stubs(monkeypatch):
    calls = {"refine": 0, "web": 0}
    monkeypatch.setattr(kb, "retrieve", lambda q, k=None: [Doc("tdr", "A TDR is filed.", 0.5)])

    async def fake_refine(q, docs):
        calls["refine"] += 1
        return ["A TDR is filed. [KB:tdr]"]

    async def fake_rewrite(q):
        return "indian railways query"

    async def fake_web(query):
        calls["web"] += 1
        return ["Title: body [WEB:https://example.com]"]

    async def fake_text(system, user):
        return "Answer. [KB:tdr] [WEB:https://example.com]"

    monkeypatch.setattr(refine, "refine", fake_refine)
    monkeypatch.setattr(web, "rewrite_query", fake_rewrite)
    monkeypatch.setattr(web, "web_search", fake_web)
    monkeypatch.setattr(llm, "ask_text", fake_text)
    graph._app = None
    return calls


def _run(monkeypatch, label):
    async def fake_grade(q, docs):
        return {d.id: label for d in docs}

    monkeypatch.setattr(evaluator, "grade", fake_grade)
    return asyncio.run(graph.ask("question"))


def test_correct_only_refines(monkeypatch, stubs):
    state = _run(monkeypatch, "relevant")
    assert state["verdict"] == "CORRECT"
    assert (stubs["refine"], stubs["web"]) == (1, 0)


def test_incorrect_only_searches_web(monkeypatch, stubs):
    state = _run(monkeypatch, "irrelevant")
    assert state["verdict"] == "INCORRECT"
    assert (stubs["refine"], stubs["web"]) == (0, 1)


def test_ambiguous_does_both_and_merges_knowledge(monkeypatch, stubs):
    state = _run(monkeypatch, "partial")
    assert state["verdict"] == "AMBIGUOUS"
    assert (stubs["refine"], stubs["web"]) == (1, 1)
    assert len(state["knowledge"]) == 2


def test_no_knowledge_means_honest_dont_know(monkeypatch, stubs):
    async def empty_web(query):
        return []

    monkeypatch.setattr(web, "web_search", empty_web)
    state = _run(monkeypatch, "irrelevant")
    assert state["answer"] == graph.NO_ANSWER


def test_sources_of_extracts_cited_tags():
    state = {"answer": "x [KB:tdr] y [WEB:https://example.com] z [KB:tdr]"}
    assert graph.sources_of(state) == ["KB:tdr", "WEB:https://example.com"]
