"""API contract and security tests; inference is stubbed."""
import pytest
from fastapi.testclient import TestClient

from crag import api


@pytest.fixture
def client(monkeypatch):
    monkeypatch.delenv("CRAG_API_TOKEN", raising=False)
    return TestClient(api.app)


def test_liveness_is_available_without_provider_key(client):
    response = client.get("/health/live")
    assert response.status_code == 200
    assert response.json() == {"status": "alive"}
    assert response.headers.get("x-request-id")


def test_ask_rejects_whitespace_question(client):
    assert client.post("/ask", json={"question": "   "}).status_code == 422


def test_ask_returns_pipeline_result(client, monkeypatch):
    async def fake_ask(question):
        return {
            "answer": "Evidence says yes. [KB:policy]",
            "verdict": "CORRECT",
            "labels": {"policy": "relevant"},
            "docs": [type("Doc", (), {"id": "policy", "similarity": 0.91})()],
        }

    monkeypatch.setattr(api, "ask", fake_ask)
    response = client.post("/ask", json={"question": "Can I proceed?"})
    assert response.status_code == 200
    assert response.json()["verdict"] == "CORRECT"
    assert response.json()["sources"] == ["KB:policy"]
    assert response.json()["passages"][0]["similarity"] == 0.91


def test_configured_bearer_token_is_required(monkeypatch):
    monkeypatch.setenv("CRAG_API_TOKEN", "test-secret")

    async def fake_ask(question):
        return {"answer": "Evidence. [KB:policy]", "verdict": "CORRECT", "labels": {}, "docs": []}

    monkeypatch.setattr(api, "ask", fake_ask)
    client = TestClient(api.app)
    assert client.post("/ask", json={"question": "Can I proceed?"}).status_code == 401
    response = client.post(
        "/ask",
        json={"question": "Can I proceed?"},
        headers={"Authorization": "Bearer test-secret"},
    )
    assert response.status_code == 200
