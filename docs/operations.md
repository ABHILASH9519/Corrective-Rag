# Operations

Set GROQ_API_KEY before starting inference. Run go test ./..., go vet ./..., and go run ./cmd/crag evaluate. Start the API with go run ./cmd/crag serve. Configure CRAG_ADDR, CRAG_API_TOKEN, CRAG_ALLOWED_ORIGINS, CRAG_KB_PATH, CRAG_TOP_K and CRAG_MODEL through the environment.

GET /health/live is process liveness. GET /health/ready checks provider-key configuration. POST /ask accepts JSON {"question":"..."}. Configure bearer auth before exposing the service. Use TLS ingress, network restrictions, request quotas, provider spend limits and external secret management in deployment.

The process loads the knowledge base at startup. Restart after changing corpus files. Web fallback is best-effort. Keep a reviewed corpus and evaluation set for any real decision workflow.
