"""Two thin wrappers over AsyncGroq: one returns parsed JSON, one returns plain text.

Inputs:  a system prompt, a user prompt, and (for JSON) a schema
Outputs: a dict that matches the schema, or the model's text reply
"""

from __future__ import annotations

import json

from crag.config import SETTINGS, require_api_key

_client = None


def client():
    """Create the Groq client once, after .env has been loaded."""
    global _client
    if _client is None:
        from groq import AsyncGroq

        # The free tier allows 8,000 tokens a minute, so retry 429s more than the SDK default.
        _client = AsyncGroq(
            api_key=require_api_key(), max_retries=SETTINGS.max_retries, timeout=60.0
        )
    return _client


async def ask_json(system: str, user: str, name: str, schema: dict) -> dict:
    """Ask for a reply that must match the schema. Strict mode makes Groq enforce it."""
    response = await client().chat.completions.create(
        model=SETTINGS.model,
        messages=[{"role": "system", "content": system}, {"role": "user", "content": user}],
        response_format={
            "type": "json_schema",
            "json_schema": {"name": name, "strict": True, "schema": schema},
        },
        temperature=0,
        max_completion_tokens=SETTINGS.grade_max_tokens,
    )
    content = response.choices[0].message.content
    if not content:
        raise RuntimeError("Model returned an empty reply (token budget spent on reasoning?)")
    return json.loads(content)


async def ask_text(system: str, user: str) -> str:
    """Ask for a free-text reply."""
    response = await client().chat.completions.create(
        model=SETTINGS.model,
        messages=[{"role": "system", "content": system}, {"role": "user", "content": user}],
        temperature=0,
        max_completion_tokens=SETTINGS.answer_max_tokens,
    )
    return (response.choices[0].message.content or "").strip()
