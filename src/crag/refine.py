"""Knowledge refinement: break passages into sentence strips and keep only useful ones.

Inputs:  the question and the retrieved Docs
Outputs: a list of strips (each tagged [KB:<id>]) that help answer the question
"""

from __future__ import annotations

import re

from crag.kb import Doc
from crag.llm import ask_json

SYSTEM = """You filter sentences for a help desk.
For each numbered sentence, set keep to true only if it helps answer the question.
Drop sentences that are on a nearby topic but do not help with this question."""

SCHEMA = {
    "type": "object",
    "properties": {
        "strips": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {"id": {"type": "string"}, "keep": {"type": "boolean"}},
                "required": ["id", "keep"],
                "additionalProperties": False,
            },
        }
    },
    "required": ["strips"],
    "additionalProperties": False,
}

_SENTENCE_END = re.compile(r"(?<=[.!?])\s+")


def split_strips(docs: list[Doc]) -> dict[str, str]:
    """Split every passage into sentences, numbered s1, s2, ... across all passages."""
    strips: dict[str, str] = {}
    for doc in docs:
        for sentence in _SENTENCE_END.split(doc.text):
            if sentence.strip():
                strips[f"s{len(strips) + 1}"] = f"{sentence.strip()} [KB:{doc.id}]"
    return strips


async def refine(question: str, docs: list[Doc]) -> list[str]:
    """Return only the strips the model marks as useful, in their original order."""
    strips = split_strips(docs)
    if not strips:
        return []
    numbered = "\n".join(f"{sid}: {text}" for sid, text in strips.items())
    reply = await ask_json(SYSTEM, f"Question: {question}\n\n{numbered}", "strips", SCHEMA)
    kept = {s["id"] for s in reply["strips"] if s["keep"]}
    return [text for sid, text in strips.items() if sid in kept]
