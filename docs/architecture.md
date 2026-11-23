# Go service architecture

The application is a native Go process. The request path is bounded HTTP input → BM25 lexical retrieval → structured LLM relevance grading → deterministic CRAG verdict → evidence refinement and/or web fallback → cited answer. The knowledge corpus is JSON and is loaded once at startup.

The Go standard library provides HTTP serving, JSON encoding, context cancellation, process signals and concurrency primitives. Groq's OpenAI-compatible API is called over HTTPS. DuckDuckGo Instant Answer is used as a best-effort web fallback; it is not a general-purpose search index and may return no results.

Trust boundaries: corpus and web content are untrusted evidence; prompts explicitly prohibit treating their contents as instructions. Secrets are environment-injected. Deploy behind TLS, quotas and a secret manager. BM25 is lexical retrieval, not dense semantic search.
