"""Pure retrieval-metric tests; no vector database or provider is required."""
import pytest
from crag.evaluation import retrieval_metrics


def test_metrics_perfect_ranking():
    assert retrieval_metrics(["a", "b", "c"], ["a", "b"], 3) == {
        "recall@3": 1.0, "mrr": 1.0, "ndcg@3": 1.0
    }


def test_metrics_reward_relevant_passage_near_the_top():
    first = retrieval_metrics(["target", "noise"], ["target"], 2)
    second = retrieval_metrics(["noise", "target"], ["target"], 2)
    assert first["mrr"] > second["mrr"]
    assert first["ndcg@2"] > second["ndcg@2"]
    assert first["recall@2"] == second["recall@2"] == 1.0


def test_metrics_zero_when_no_relevant_passage_is_retrieved():
    assert retrieval_metrics(["x", "y"], ["target"], 2) == {
        "recall@2": 0.0, "mrr": 0.0, "ndcg@2": 0.0
    }


def test_metrics_validate_inputs():
    with pytest.raises(ValueError):
        retrieval_metrics(["a"], ["a"], 0)
    with pytest.raises(ValueError):
        retrieval_metrics(["a"], [], 3)
