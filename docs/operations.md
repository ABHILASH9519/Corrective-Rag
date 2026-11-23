# Operations

## Local development
```bash
cp .env.example .env
# Set GROQ_API_KEY in .env
uv sync --extra dev
uv run pytest -q
uv run ruff check .
uv run crag serve --host 127.0.0.1 --port 8000
```

## Docker Compose
```bash
cp .env.example .env
# Set GROQ_API_KEY and a strong CRAG_API_TOKEN
docker compose up --build -d
docker compose ps
docker compose logs -f corrective-rag
```

Compose binds to loopback. Put the service behind TLS-enabled ingress before exposing it. Chroma persists in a named volume; the JSON knowledge file remains the source of truth.

## Probes and access
- Use /health/live for liveness and /health/ready for readiness.
- Set CRAG_API_TOKEN to require Authorization: Bearer <token> on POST /ask.
- Set CRAG_ALLOWED_ORIGINS only to explicit trusted browser origins.
- Do not log API keys, bearer tokens, authorization headers or sensitive prompts.

The service calls external LLM and web providers. Apply upstream timeouts, retry budgets, request quotas and cost monitoring at the deployment boundary. Web fallback may fail independently; the pipeline should then return an evidence-limited or explicit no-answer response.
