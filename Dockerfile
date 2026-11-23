FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1
WORKDIR /app

COPY pyproject.toml README.md ./
COPY src ./src
COPY data ./data
RUN pip install .

# Chroma's ONNX embedding model is fetched on first run; keep it across restarts with a volume.
ENV CRAG_KB_PATH=/app/data/knowledge_base.json
EXPOSE 8000
HEALTHCHECK CMD python -c "import urllib.request as u; u.urlopen('http://localhost:8000/health')" || exit 1
CMD ["crag", "serve", "--host", "0.0.0.0", "--port", "8000"]
