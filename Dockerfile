FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1
WORKDIR /app

COPY pyproject.toml README.md ./
COPY src ./src
COPY data ./data
RUN pip install . \
    && useradd --system --create-home --uid 10001 app \
    && mkdir -p /var/lib/corrective-rag \
    && chown -R app:app /app /var/lib/corrective-rag

ENV CRAG_KB_PATH=/app/data/knowledge_base.json
EXPOSE 8000
HEALTHCHECK CMD python -c "import urllib.request as u; u.urlopen('http://localhost:8000/health/live', timeout=3)" || exit 1

USER app
CMD ["crag", "serve", "--host", "0.0.0.0", "--port", "8000"]
