# Architecture

## Request lifecycle
1. **Retrieve:** Chroma embeds the question and returns top-k passages using cosine distance.
2. **Evaluate:** one structured LLM call grades each passage as relevant, partial or irrelevant.
3. **Route:** deterministic logic selects an action: CORRECT refines local evidence; INCORRECT rewrites and searches; AMBIGUOUS does both concurrently.
4. **Refine:** sentence-level filtering keeps only useful evidence and preserves its knowledge-base citation.
5. **Generate:** answer only from surviving evidence and preserve exact [KB:<id>] and [WEB:<url>] source tags.
6. **Return:** answer, verdict, passage grades and cited sources are exposed through the API.

## Trust boundaries
- The knowledge base is application-owned input and is validated before indexing.
- Retrieved passages and web snippets are untrusted evidence, not instructions.
- Provider credentials are injected through environment variables; deployments should use a secret manager.
- CRAG_API_TOKEN enables bearer authentication. Production also requires TLS, ingress request limits and network policies.
- Web results may be stale or inaccurate. Citations enable traceability, not an assertion of authority.

## Interfaces
- POST /ask — synchronous question answering.
- GET /health/live — process liveness.
- GET /health/ready — provider-key and knowledge-file readiness.
- GET /health — backwards-compatible liveness.

## Current constraints
This is a single-process service with a file-backed knowledge source and an in-process Chroma index. Multi-tenant isolation, durable request history, distributed rate limiting, evaluation datasets and persistent telemetry remain follow-on engineering work; they are not claimed as current capabilities.
