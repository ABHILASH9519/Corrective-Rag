# Corrective RAG Help Desk

A help desk that **checks its own retrieval** before answering. Built on the Corrective RAG (CRAG) idea (Yan et al., 2024): grade what the retriever returned, then decide where the answer should come from.

Plain RAG trusts the top-k passages even when they only *look* related. Ask an Indian Railways bot about IndiGo baggage and it happily serves train-luggage notes. CRAG catches that and goes to the web instead.

```
Question -> Retrieve (Chroma, top 3) -> Evaluator (Groq, per-passage grade)
              CORRECT   -> Refine (keep useful sentences) ----\
              AMBIGUOUS -> Refine + Web search (in parallel) --> Generate (cited) -> Answer
              INCORRECT -> Web search (query rewrite first) --/
```

| Verdict | Meaning | Action |
|---|---|---|
| CORRECT | at least one passage answers the question | keep only the sentences that matter |
| INCORRECT | nothing retrieved helps | rewrite the query, search the web |
| AMBIGUOUS | some passages help partly | do both and merge |

Every fact in the answer carries a source tag, `[KB:<id>]` or `[WEB:<url>]`, so it can be audited. If neither source has the answer, the bot says it doesn't know instead of bluffing.

## Features

- Batched LLM evaluator with strict JSON-schema output (one call grades all passages)
- Sentence-level knowledge refinement with citations that survive the cut
- DuckDuckGo fallback with domain-aware query rewriting, rate-limit retries and graceful failure
- LangGraph pipeline with parallel branches for ambiguous cases
- Bring your own knowledge base: a plain JSON file, optional persistent Chroma index
- CLI and FastAPI service (`/ask`, `/health`), Dockerfile, CI, tests
- Grading and routing are separate: `decide()` is a plain, unit-tested rule

## Quick start

Requires Python 3.12+, [uv](https://docs.astral.sh/uv/) and a free [Groq API key](https://console.groq.com/keys).

```bash
git clone https://github.com/ABHILASH9519/corrective-rag
cd corrective-rag
cp .env.example .env        # then put your GROQ_API_KEY in it
uv sync --extra dev

uv run crag ask -v "What time does Tatkal booking open for AC classes?"
uv run crag ask -v "How much check-in baggage can I carry on an IndiGo domestic flight?"
uv run crag serve           # http://localhost:8000/docs
```

`-v` prints each passage's similarity and grade plus the verdict.

### API

```bash
curl -s localhost:8000/ask -H 'content-type: application/json' \
  -d '{"question": "My train ran 5 hours late and I did not travel. How do I get a refund?"}'
```

Returns `answer`, `verdict`, `sources`, per-passage `passages` (similarity + label) and the `web_query` used, if any.

### Docker

```bash
docker build -t corrective-rag .
docker run --rm -p 8000:8000 --env-file .env corrective-rag
```

## Use your own knowledge base

Edit `data/knowledge_base.json` (or point `CRAG_KB_PATH` at another file):

```json
[{"id": "refund-policy", "text": "Refunds are issued within 7 days..."}]
```

IDs become citation tags, so keep them short and meaningful. Set `CRAG_CHROMA_DIR` to persist the index; it is re-synced with the file on every start. Update the domain wording in `src/crag/web.py` (`REWRITE_SYSTEM`) and `src/crag/graph.py` (`ANSWER_SYSTEM`) for your own domain.

The bundled notes are simplified Indian Railways examples, **not an official IRCTC source**.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `GROQ_API_KEY` | required | Groq API key |
| `CRAG_MODEL` | `openai/gpt-oss-120b` | grading / answering model |
| `CRAG_TOP_K` | `3` | passages sent to the evaluator |
| `CRAG_WEB_RESULTS` | `4` | web snippets per fallback |
| `CRAG_KB_PATH` | `data/knowledge_base.json` | knowledge base file |
| `CRAG_CHROMA_DIR` | empty (in-memory) | persistent index directory |
| `CRAG_LOG_LEVEL` | `INFO` | logging level |

## Project layout

```
src/crag/
  config.py     settings and API-key check
  kb.py         JSON KB -> Chroma (cosine) -> retrieve()
  llm.py        ask_json (strict schema) / ask_text
  evaluator.py  grade passages, decide CORRECT / AMBIGUOUS / INCORRECT
  refine.py     split into sentence strips, keep useful ones
  web.py        query rewrite + DuckDuckGo search
  graph.py      LangGraph wiring and ask()
  cli.py        `crag ask`, `crag serve`
  api.py        FastAPI app
tests/          unit tests (LLM and network stubbed)
```

## Tests

```bash
uv run pytest -q
```

Tests stub all model and network calls, so they run offline without a key.

## Troubleshooting

- **Empty or truncated grading reply**: `gpt-oss-120b` reasons before writing JSON; raise `grade_max_tokens` in `config.py`.
- **429 from Groq**: the free tier is 8,000 tokens/minute; the client already retries 5 times.
- **Web search returns nothing**: DuckDuckGo throttles bursts; the bot then says it doesn't know instead of failing.
- **First run is slow**: Chroma downloads its ~80 MB ONNX embedding model once.

## Credits

Based on *Corrective Retrieval Augmented Generation* (Yan et al., 2024). Stack: LangGraph, Chroma, Groq, DuckDuckGo (`ddgs`), FastAPI.

## License

MIT
