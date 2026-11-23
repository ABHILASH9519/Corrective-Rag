"""HTTP API with readiness probes, optional bearer auth and request telemetry."""
from __future__ import annotations
import hmac, logging, os, time, uuid
from contextlib import asynccontextmanager
from pathlib import Path
from typing import AsyncIterator
from fastapi import Depends, FastAPI, Header, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field, field_validator
from starlette.responses import Response
from crag import __version__
from crag.config import SETTINGS
from crag.graph import ask, sources_of
log = logging.getLogger(__name__)

@asynccontextmanager
async def lifespan(_: FastAPI) -> AsyncIterator[None]:
    log.info("service_starting", extra={"service":"corrective-rag","version":__version__})
    yield
    log.info("service_stopping", extra={"service":"corrective-rag"})

app = FastAPI(title="Corrective RAG API", version=__version__,
              description="Retrieval evaluation, evidence refinement, web fallback and cited generation.",
              lifespan=lifespan)
_origins = [v.strip() for v in os.getenv("CRAG_ALLOWED_ORIGINS","").split(",") if v.strip()]
if _origins:
    app.add_middleware(CORSMiddleware, allow_origins=_origins, allow_credentials=True,
        allow_methods=["GET","POST"], allow_headers=["Authorization","Content-Type","X-Request-ID"],
        expose_headers=["X-Request-ID"])

@app.middleware("http")
async def request_context(request: Request, call_next) -> Response:
    request_id = request.headers.get("x-request-id","").strip()[:128] or str(uuid.uuid4())
    started = time.perf_counter()
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    log.info("http_request", extra={"request_id":request_id,"method":request.method,
        "path":request.url.path,"status_code":response.status_code,
        "duration_ms":round((time.perf_counter()-started)*1000,2)})
    return response

def verify_access(authorization: str | None = Header(default=None)) -> None:
    expected = os.getenv("CRAG_API_TOKEN","").strip()
    if not expected:
        return
    scheme, _, token = (authorization or "").partition(" ")
    if scheme.lower() != "bearer" or not hmac.compare_digest(token,expected):
        raise HTTPException(status_code=401,detail="A valid bearer token is required.")

class AskRequest(BaseModel):
    question: str = Field(min_length=3,max_length=SETTINGS.max_question_chars)
    @field_validator("question")
    @classmethod
    def question_must_not_be_blank(cls,value: str) -> str:
        value=value.strip()
        if len(value)<3: raise ValueError("question must contain at least 3 non-whitespace characters")
        return value

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

@app.get("/health",tags=["operations"])
async def health() -> dict[str,str]:
    return {"status":"ok","version":__version__}

@app.get("/health/live",tags=["operations"])
async def liveness() -> dict[str,str]:
    return {"status":"alive"}

@app.get("/health/ready",tags=["operations"])
async def readiness() -> dict[str,str]:
    missing=[]
    if not os.getenv("GROQ_API_KEY","").strip(): missing.append("GROQ_API_KEY")
    if not Path(SETTINGS.kb_path).is_file(): missing.append("knowledge_base")
    if missing: raise HTTPException(status_code=503,detail={"status":"not_ready","missing":missing})
    return {"status":"ready","model":SETTINGS.model}

@app.post("/ask",response_model=AskResponse,tags=["inference"],dependencies=[Depends(verify_access)])
async def ask_endpoint(request: AskRequest) -> AskResponse:
    try: state=await ask(request.question)
    except Exception:  # noqa: BLE001
        log.exception("pipeline_failed",extra={"question_length":len(request.question)})
        raise HTTPException(status_code=502,detail="The retrieval pipeline could not complete this request.") from None
    return AskResponse(answer=state["answer"],verdict=state["verdict"],sources=sources_of(state),
        passages=[PassageGrade(id=d.id,similarity=d.similarity,
            label=state.get("labels",{}).get(d.id,"unknown")) for d in state.get("docs",[])],
        web_query=state.get("web_query"))
