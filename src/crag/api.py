"""HTTP API: POST /ask returns a cited answer plus how the pipeline reached it."""

from __future__ import annotations

import logging

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from crag import __version__
from crag.config import SETTINGS, require_api_key
from crag.graph import ask, sources_of

log = logging.getLogger(__name__)

app = FastAPI(
    title="Corrective RAG Help Desk",
    version=__version__,
    description="Grades its own retrieval, then answers from the KB, the web, or both.",
)


class AskRequest(BaseModel):
    question: str = Field(min_length=3, max_length=SETTINGS.max_question_chars)


class PassageGrade(BaseModel):
    id: str
    similarity: float
    label: str


class AskResponse(BaseModel):
    answer: str
    verdict: str
    sources: list[str]
    passages: list[PassageGrade]
    web_query: str | None = None


@app.on_event("startup")
async def _check_key() -> None:
    require_api_key()


@app.get("/health")
async def health() -> dict:
    return {"status": "ok", "version": __version__, "model": SETTINGS.model}


@app.post("/ask", response_model=AskResponse)
async def ask_endpoint(request: AskRequest) -> AskResponse:
    question = request.question.strip()
    try:
        state = await ask(question)
    except Exception:  # noqa: BLE001 - never leak provider internals to clients
        log.exception("pipeline failed for question %r", question)
        raise HTTPException(status_code=502, detail="The help desk could not answer right now.")
    return AskResponse(
        answer=state["answer"],
        verdict=state["verdict"],
        sources=sources_of(state),
        passages=[
            PassageGrade(id=d.id, similarity=d.similarity, label=state["labels"].get(d.id, "?"))
            for d in state.get("docs", [])
        ],
        web_query=state.get("web_query"),
    )
