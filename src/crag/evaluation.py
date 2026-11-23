"""Offline retrieval evaluation metrics and golden-set runner.

The evaluation path measures retrieval only; it does not call an LLM or web provider.
"""
from __future__ import annotations
import json
import math
from pathlib import Path
from typing import Any
from crag.kb import retrieve


def retrieval_metrics(retrieved_ids: list[str], relevant_ids: list[str], k: int) -> dict[str, float]:
    """Compute binary Recall@k, reciprocal rank and normalized DCG."""
    if k < 1:
        raise ValueError("k must be at least 1")
    relevant = set(relevant_ids)
    if not relevant:
        raise ValueError("relevant_ids must contain at least one expected passage")
    ranked = retrieved_ids[:k]
    hits = [1 if item in relevant else 0 for item in ranked]
    recall = sum(hits) / len(relevant)
    reciprocal_rank = next((1.0 / rank for rank, hit in enumerate(hits, 1) if hit), 0.0)
    dcg = sum(hit / math.log2(rank + 1) for rank, hit in enumerate(hits, 1))
    ideal_hits = min(len(relevant), k)
    ideal_dcg = sum(1.0 / math.log2(rank + 1) for rank in range(1, ideal_hits + 1))
    return {
        f"recall@{k}": round(recall, 4),
        "mrr": round(reciprocal_rank, 4),
        f"ndcg@{k}": round(dcg / ideal_dcg if ideal_dcg else 0.0, 4),
    }


def evaluate_dataset(dataset_path: Path, k: int = 5) -> dict[str, Any]:
    """Run a JSON golden set against the configured vector index."""
    if k < 1:
        raise ValueError("k must be at least 1")
    try:
        payload = json.loads(Path(dataset_path).read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise ValueError(f"Evaluation dataset not found: {dataset_path}") from None
    except json.JSONDecodeError as error:
        raise ValueError(f"Evaluation dataset is invalid JSON: {error}") from None
    queries = payload.get("queries") if isinstance(payload, dict) else None
    if not isinstance(queries, list) or not queries:
        raise ValueError("Evaluation dataset must contain a non-empty 'queries' array")

    rows = []
    metric_totals: dict[str, float] = {}
    for index, item in enumerate(queries):
        if not isinstance(item, dict):
            raise ValueError(f"Evaluation query #{index} must be an object")
        question = item.get("question")
        relevant = item.get("relevant_ids")
        if not isinstance(question, str) or not question.strip():
            raise ValueError(f"Evaluation query #{index} needs a non-empty question")
        if not isinstance(relevant, list) or not relevant or not all(isinstance(v, str) and v for v in relevant):
            raise ValueError(f"Evaluation query #{index} needs non-empty relevant_ids")
        docs = retrieve(question, k=k)
        metrics = retrieval_metrics([doc.id for doc in docs], relevant, k)
        for name, value in metrics.items():
            metric_totals[name] = metric_totals.get(name, 0.0) + value
        rows.append({
            "id": str(item.get("id", index + 1)),
            "question": question,
            "expected_ids": relevant,
            "retrieved_ids": [doc.id for doc in docs],
            "metrics": metrics,
        })
    count = len(rows)
    return {
        "queries": count,
        "k": k,
        "aggregate": {name: round(total / count, 4) for name, total in metric_totals.items()},
        "results": rows,
    }
