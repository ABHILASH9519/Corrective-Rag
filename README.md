# Corrective RAG Help Desk (Go)

A help desk that **checks its own retrieval** before answering, written in Go with zero third-party dependencies. It follows the Corrective RAG (CRAG) idea (Yan et al., 2024): grade what the retriever returned, then decide where the answer should come from.

Plain RAG trusts the top-k passages even when they only *look* related. Ask an Indian Railways bot about IndiGo baggage and it serves train-luggage notes. CRAG catches that and goes to the web instead.

```
Question -> Retrieve (TF-IDF cosine, top 3) -> Evaluator (Groq, one batched grade call)
              CORRECT   -> Refine (keep useful sentences) ----\
              AMBIGUOUS -> Refine + Web search (concurrent) ---> Generate (cited) -> Answer
              INCORRECT -> Web search (query rewrite first) --/
```

| Verdict | Meaning | Action |
|---|---|---|
| CORRECT | at least one passage answers the question | keep only the sentences that matter |
| INCORRECT | nothing retrieved helps | rewrite the query, search the web |
| AMBIGUOUS | some passages help partly | do both at once and merge |

Every fact carries a source tag, `[KB:<id>]` or `[WEB:<url>]`, so answers can be audited. If neither source has the answer, the bot says it doesn't know instead of bluffing.

## Why it is built this way

- **Single static binary, stdlib only.** No Python runtime, no vector database, no model download. `go build` and ship. The Docker image is distroless and runs as non-root.
- **Concurrency where it pays.** The ambiguous branch runs KB refinement and web search in parallel goroutines.
- **Resilient by default.** Groq calls retry 429/5xx honouring `Retry-After`, with jittered backoff and context cancellation. A broken web search degrades to "less knowledge", never a failed request.
- **Safe to expose.** Request size and question length limits, strict JSON decoding, a concurrency cap that returns `429` instead of melting the rate limit, per-request timeout, graceful shutdown, and no provider details in error responses.
- **Testable.** The LLM and the web searcher are interfaces. 40+ tests run offline in about a second, with the race detector on.
- **Honest limits.** Retrieval is lexical (TF-IDF), not semantic embeddings. That is a deliberate trade-off for zero dependencies, and the evaluator is what catches retrieval mistakes. See *Roadmap*.

## Quick start

Requires Go 1.24+ and a free [Groq API key](https://console.groq.com/keys).

```bash
git clone https://github.com/ABHILASH9519/Corrective-Rag
cd Corrective-Rag
cp .env.example .env            # put your GROQ_API_KEY in it
make build

./bin/crag ask -v "What time does Tatkal booking open for AC classes?"
./bin/crag ask -v "How much check-in baggage can I carry on an IndiGo domestic flight?"
./bin/crag serve                # http://localhost:8000
```

`-v` prints each passage's similarity and grade, the verdict and the web query used.

### HTTP API

```bash
curl -s localhost:8000/ask -H 'content-type: application/json' \
  -d '{"question": "My train ran 5 hours late and I did not travel. How do I get a refund?"}'
```

Response: `answer`, `verdict`, `sources`, per-passage `passages` (similarity + label) and `web_query` when the web was used. Status codes: `400` bad body, `422` question too short/long, `429` busy, `502` upstream failure, `504` timeout. `GET /health` reports version and model.

### Docker

```bash
docker build -t corrective-rag .
docker run --rm -p 8000:8000 --env-file .env corrective-rag
```

## Use your own knowledge base

Point `CRAG_KB_PATH` at a JSON file:

```json
[{"id": "refund-policy", "text": "Refunds are issued within 7 days. Shipping is not refunded."}]
```

IDs become citation tags, so keep them short and meaningful. Then set `CRAG_HELPDESK_NAME` and `CRAG_REWRITE_HINT` so web queries match your domain. The bundled notes are simplified Indian Railways examples, **not an official IRCTC source**.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `GROQ_API_KEY` | required | Groq API key |
| `CRAG_MODEL` | `openai/gpt-oss-120b` | grading and answering model |
| `CRAG_TOP_K` | `3` | passages sent to the evaluator |
| `CRAG_WEB_RESULTS` | `4` | web snippets per fallback |
| `CRAG_KB_PATH` | built-in | knowledge base file |
| `CRAG_ADDR` | `:8000` | HTTP listen address |
| `CRAG_MAX_CONCURRENT` | `4` | simultaneous questions before `429` |
| `CRAG_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `CRAG_HELPDESK_NAME` | `Indian Railways` | domain name used in prompts |
| `CRAG_REWRITE_HINT` | railway hint | extra rule for the query rewriter |
| `GROQ_BASE_URL` | Groq endpoint | any OpenAI-compatible API |

## Project layout

```
cmd/crag/             CLI: ask, serve, version
data/                 knowledge_base.json, embedded into the binary
internal/config/      env and .env loading, validation
internal/kb/          JSON KB, TF-IDF index, cosine retrieval
internal/llm/         Groq client (strict JSON schema, retries) + Client interface
internal/evaluator/   grade passages, decide CORRECT / AMBIGUOUS / INCORRECT
internal/refine/      split into sentence strips, keep useful ones
internal/web/         query rewrite + DuckDuckGo search + Searcher interface
internal/pipeline/    the CRAG flow, concurrency, citations
internal/server/      HTTP API, limits, logging
internal/llmtest/     fake LLM for tests
```

## Development

```bash
make test     # go test -race ./...
make vet
make fmt
```

CI runs gofmt, vet, race tests, build and a Docker build on every push.

## Known limitations

- DuckDuckGo's HTML endpoint is unofficial and can throttle or change markup. The parser is unit-tested against a fixture; swap in a paid search API by implementing `web.Searcher`.
- Lexical retrieval misses synonyms ("refund" vs "money back"). The evaluator then routes to the web rather than answering wrongly.
- Web snippets are untrusted text. The answer prompt restricts the model to cited knowledge, but treat web-sourced answers accordingly.

## Roadmap

- Pluggable embedding retriever behind an interface (keep TF-IDF as the zero-dependency default)
- Streaming responses
- Evaluation harness with a labelled question set

## Credits

Based on *Corrective Retrieval Augmented Generation* (Yan et al., 2024). LLM inference by Groq.

## License

MIT
