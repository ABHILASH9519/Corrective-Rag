"""The CRAG graph: retrieve, grade, then refine the KB, search the web, or both.

Inputs:  a question
Outputs: the final state - labels, verdict, the knowledge used and a cited answer
"""

from __future__ import annotations

import asyncio
import operator
import re
from typing import Annotated, TypedDict

from langgraph.graph import END, START, StateGraph

from crag import evaluator, kb, llm, refine, web

ANSWER_SYSTEM = """You are a railway help desk assistant.
Answer only from the knowledge given. Each line ends with its source tag, such as [KB:tdr] or [WEB:https://...].
Cite the tags you used at the end of the sentence that uses them, copied exactly in square brackets.
If the knowledge does not answer the question, say you don't know. Keep it under 100 words."""

NO_ANSWER = "I don't know - I couldn't find reliable information to answer that."

_TAG = re.compile(r"\[(?:KB|WEB):[^\]]+\]")


class CragState(TypedDict, total=False):
    """Everything the graph knows about one question."""

    question: str
    docs: list[kb.Doc]
    labels: dict[str, str]
    verdict: str
    # refine and web can both write here in the same step, so their lists are added together.
    knowledge: Annotated[list[str], operator.add]
    web_query: str
    answer: str


async def retrieve_node(state: CragState) -> dict:
    docs = await asyncio.to_thread(kb.retrieve, state["question"])
    return {"docs": docs}


async def grade_node(state: CragState) -> dict:
    labels = await evaluator.grade(state["question"], state["docs"])
    return {"labels": labels, "verdict": evaluator.decide(labels)}


async def refine_node(state: CragState) -> dict:
    return {"knowledge": await refine.refine(state["question"], state["docs"])}


async def web_node(state: CragState) -> dict:
    query = await web.rewrite_query(state["question"])
    return {"web_query": query, "knowledge": await web.web_search(query)}


async def generate_node(state: CragState) -> dict:
    knowledge = state.get("knowledge") or []
    if not knowledge:
        return {"answer": NO_ANSWER}
    user = f"Question: {state['question']}\n\nKnowledge:\n" + "\n".join(knowledge)
    return {"answer": await llm.ask_text(ANSWER_SYSTEM, user) or NO_ANSWER}


def route(state: CragState) -> list[str]:
    """CORRECT -> refine, INCORRECT -> web, AMBIGUOUS -> both at once."""
    return {
        "CORRECT": ["refine"],
        "INCORRECT": ["web"],
        "AMBIGUOUS": ["refine", "web"],
    }[state["verdict"]]


def build_graph():
    graph = StateGraph(CragState)
    graph.add_node("retrieve", retrieve_node)
    graph.add_node("grade", grade_node)
    graph.add_node("refine", refine_node)
    graph.add_node("web", web_node)
    graph.add_node("generate", generate_node)

    graph.add_edge(START, "retrieve")
    graph.add_edge("retrieve", "grade")
    graph.add_conditional_edges("grade", route, ["refine", "web"])
    graph.add_edge("refine", "generate")
    graph.add_edge("web", "generate")
    graph.add_edge("generate", END)
    return graph.compile()


_app = None


async def ask(question: str) -> CragState:
    """Run one question through the whole pipeline."""
    global _app
    if _app is None:
        _app = build_graph()
    return await _app.ainvoke({"question": question, "knowledge": []})


def sources_of(state: CragState) -> list[str]:
    """Source tags (KB ids / URLs) the answer actually cites."""
    seen: dict[str, None] = {}
    for tag in _TAG.findall(state.get("answer", "")):
        seen[tag.strip("[]")] = None
    return list(seen)
