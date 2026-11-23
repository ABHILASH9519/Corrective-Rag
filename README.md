# Corrective RAG — Go

Native Go implementation of Corrective Retrieval-Augmented Generation. The service retrieves candidate passages using BM25, evaluates evidence with a structured LLM response, routes to local evidence or web fallback, and generates source-attributed answers.

## Architecture

Question → Validation → BM25 retrieval → LLM evidence grading → CORRECT / AMBIGUOUS / INCORRECT routing → evidence refinement and/or web fallback → cited answer.

## Engineering capabilities

- Native Go runtime, CLI and HTTP API.
- BM25 lexical retrieval with inverse-document-frequency and document-length normalization.
- Structured relevance grading (relevant, partial, irrelevant).
- Evidence-grounded answer generation with [KB:id] and [WEB:url] citation convention.
- Adaptive web fallback, request bounds, bearer-token authentication and CORS allow-list.
- Liveness/readiness endpoints and graceful server shutdown.
- Docker multi-stage build with a non-root distroless runtime.
- Go unit-test and CI foundation.

## Run locally

Requires Go 1.23+ and a Groq API key.

```bash
export GROQ_API_KEY="your-key"
go test ./...
go run ./cmd/crag ask --verbose "When does Tatkal booking open for AC classes?"
go run ./cmd/crag serve
```

## API

```bash
curl -X POST http://localhost:8080/ask -H 'Content-Type: application/json' -d '{"question":"When does Tatkal booking open for AC classes?"}'
```

Endpoints: GET /health/live, GET /health/ready, POST /ask. Set CRAG_API_TOKEN to require a bearer token.

## Configuration

GROQ_API_KEY, GROQ_API_BASE, CRAG_MODEL, CRAG_ADDR, CRAG_API_TOKEN, CRAG_ALLOWED_ORIGINS, CRAG_KB_PATH, CRAG_TOP_K.

## Quality and limitations

Run `go test ./...` and `go vet ./...`. The bundled corpus is illustrative and must be replaced with reviewed source material for real decisions. Current retrieval is BM25 lexical search; dense embeddings, reranking, durable traces, multi-tenant isolation, distributed rate limiting and production SLOs are future work. This repository does not claim production certification.
