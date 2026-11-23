"""Knowledge base: JSON notes embedded into a Chroma collection (cosine distance).

Inputs:  data/knowledge_base.json - a list of {"id": str, "text": str}
Outputs: the top-k closest passages as Doc objects, with cosine similarity

Point CRAG_KB_PATH at your own file to run the help desk on your own content.
Set CRAG_CHROMA_DIR to persist the index between runs.
"""

from __future__ import annotations

import json
import logging
import threading
from dataclasses import dataclass
from pathlib import Path

from crag.config import SETTINGS

log = logging.getLogger(__name__)


@dataclass
class Doc:
    """One retrieved passage and how close it was to the question."""

    id: str
    text: str
    similarity: float


def load_passages(path: Path) -> dict[str, str]:
    """Read and validate the knowledge base file."""
    try:
        raw = json.loads(Path(path).read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise SystemExit(f"Knowledge base not found: {path}") from None
    except json.JSONDecodeError as error:
        raise SystemExit(f"Knowledge base {path} is not valid JSON: {error}") from None

    passages: dict[str, str] = {}
    for position, item in enumerate(raw):
        if not isinstance(item, dict) or not item.get("id") or not item.get("text"):
            raise SystemExit(f"Knowledge base entry #{position} needs non-empty 'id' and 'text'")
        if item["id"] in passages:
            raise SystemExit(f"Duplicate knowledge base id: {item['id']}")
        passages[str(item["id"])] = str(item["text"]).strip()
    if not passages:
        raise SystemExit(f"Knowledge base {path} is empty")
    return passages


def build_collection(passages: dict[str, str]):
    """Embed passages into a Chroma collection that uses cosine distance."""
    import chromadb

    client = (
        chromadb.PersistentClient(path=SETTINGS.chroma_dir)
        if SETTINGS.chroma_dir
        else chromadb.EphemeralClient()
    )
    collection = client.get_or_create_collection(
        "knowledge", configuration={"hnsw": {"space": "cosine"}}
    )
    # upsert keeps a persistent index in sync with the file on every start.
    collection.upsert(ids=list(passages), documents=list(passages.values()))
    stale = set(collection.get()["ids"]) - set(passages)
    if stale:
        collection.delete(ids=sorted(stale))
    log.info("knowledge base ready: %d passages", len(passages))
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
    """Return the k passages closest to the question, best first."""
    k = k or SETTINGS.top_k
    collection = _get_collection()
    result = collection.query(query_texts=[question], n_results=min(k, collection.count()))
    ids, texts, distances = result["ids"][0], result["documents"][0], result["distances"][0]
    # Chroma returns a cosine DISTANCE; similarity is 1 minus it.
    return [Doc(i, t, round(1 - d, 2)) for i, t, d in zip(ids, texts, distances)]


if __name__ == "__main__":
    for q in [
        "What time does Tatkal booking open for AC classes?",
        "How much check-in baggage can I carry on an IndiGo domestic flight?",
    ]:
        print(f"\nQ: {q}")
        for doc in retrieve(q):
            print(f"  {doc.similarity:.2f}  {doc.id}")
