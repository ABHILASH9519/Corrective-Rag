"""Validated JSON knowledge source and Chroma cosine retrieval."""
from __future__ import annotations

import json
import logging
import threading
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from crag.config import SETTINGS

log = logging.getLogger(__name__)


@dataclass
class Doc:
    id: str
    text: str
    similarity: float


def load_passages(path: Path) -> dict[str, str]:
    try:
        raw: Any = json.loads(Path(path).read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise SystemExit(f"Knowledge base not found: {path}") from None
    except json.JSONDecodeError as error:
        raise SystemExit(f"Knowledge base {path} is not valid JSON: {error}") from None
    if not isinstance(raw, list):
        raise SystemExit(f"Knowledge base {path} must contain a JSON array")
    passages = {}
    for position, item in enumerate(raw):
        if not isinstance(item, dict):
            raise SystemExit(f"Knowledge base entry #{position} must be an object")
        identifier, content = item.get("id"), item.get("text")
        if not isinstance(identifier, str) or not identifier.strip():
            raise SystemExit(f"Knowledge base entry #{position} needs a non-empty string 'id'")
        if not isinstance(content, str) or not content.strip():
            raise SystemExit(f"Knowledge base entry #{position} needs a non-empty string 'text'")
        identifier = identifier.strip()
        if identifier in passages:
            raise SystemExit(f"Duplicate knowledge base id: {identifier}")
        passages[identifier] = content.strip()
    if not passages:
        raise SystemExit(f"Knowledge base {path} is empty")
    return passages


def build_collection(passages: dict[str, str]):
    import chromadb

    client = (
        chromadb.PersistentClient(path=SETTINGS.chroma_dir)
        if SETTINGS.chroma_dir
        else chromadb.EphemeralClient()
    )
    collection = client.get_or_create_collection(
        "knowledge", configuration={"hnsw": {"space": "cosine"}}
    )
    collection.upsert(ids=list(passages), documents=list(passages.values()))
    stale = set(collection.get()["ids"]) - set(passages)
    if stale:
        collection.delete(ids=sorted(stale))
    log.info("knowledge_base_ready", extra={"passage_count": len(passages)})
    return collection


_collection = None
_lock = threading.Lock()


def _get_collection():
    global _collection
    with _lock:
        if _collection is None:
            _collection = build_collection(load_passages(SETTINGS.kb_path))
        return _collection


def retrieve(question: str, k: int | None = None) -> list[Doc]:
    if not question.strip():
        return []
    k = k or SETTINGS.top_k
    if k < 1:
        raise ValueError("k must be at least 1")
    collection = _get_collection()
    count = collection.count()
    if not count:
        return []
    result = collection.query(query_texts=[question], n_results=min(k, count))
    return [
        Doc(identifier, text, round(max(0.0, min(1.0, 1.0 - distance)), 4))
        for identifier, text, distance in zip(
            result["ids"][0], result["documents"][0], result["distances"][0]
        )
    ]


if __name__ == "__main__":
    for doc in retrieve("What is in the knowledge base?"):
        print(f"{doc.similarity:.4f} {doc.id}")
