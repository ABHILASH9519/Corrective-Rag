"""Settings for the CRAG help desk, read from environment variables / .env.

Nothing here touches the network. A missing API key only fails when the key is
first needed (see ``require_api_key``), so tests and imports stay cheap.
"""

from __future__ import annotations

import logging
import os
import sys
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv

load_dotenv()

# Groq replies can contain characters (e.g. U+2011) that crash a cp1252 console.
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, "reconfigure"):
        _stream.reconfigure(encoding="utf-8")

PROJECT_ROOT = Path(__file__).resolve().parents[2]


def _int(name: str, default: int, minimum: int = 1) -> int:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        raise SystemExit(f"{name} must be an integer, got {raw!r}") from None
    if value < minimum:
        raise SystemExit(f"{name} must be >= {minimum}, got {value}")
    return value


@dataclass(frozen=True)
class Settings:
    model: str = "openai/gpt-oss-120b"
    top_k: int = 3  # passages handed to the evaluator
    web_results: int = 4  # web snippets fetched when the KB is not enough
    grade_max_tokens: int = 2000  # gpt-oss spends completion tokens reasoning before the JSON
    answer_max_tokens: int = 600
    max_retries: int = 5  # free tier is rate limited; retry 429s
    kb_path: Path = PROJECT_ROOT / "data" / "knowledge_base.json"
    chroma_dir: str = ""  # empty = in-memory
    max_question_chars: int = 500
    log_level: str = "INFO"


def load_settings() -> Settings:
    kb_path = Path(os.getenv("CRAG_KB_PATH", "").strip() or Settings.kb_path)
    return Settings(
        model=os.getenv("CRAG_MODEL", "").strip() or Settings.model,
        top_k=_int("CRAG_TOP_K", Settings.top_k),
        web_results=_int("CRAG_WEB_RESULTS", Settings.web_results),
        kb_path=kb_path,
        chroma_dir=os.getenv("CRAG_CHROMA_DIR", "").strip(),
        log_level=os.getenv("CRAG_LOG_LEVEL", "").strip().upper() or Settings.log_level,
    )


SETTINGS = load_settings()

logging.basicConfig(
    level=getattr(logging, SETTINGS.log_level, logging.INFO),
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)


def require_api_key() -> str:
    """Return the Groq key, or stop with a message that says how to get one."""
    key = os.getenv("GROQ_API_KEY", "").strip()
    if not key:
        raise SystemExit(
            "GROQ_API_KEY is missing. Create a free key at https://console.groq.com/keys "
            "and put it in .env as GROQ_API_KEY=..."
        )
    return key


if __name__ == "__main__":
    print(f"model    {SETTINGS.model}")
    print(f"top-k    {SETTINGS.top_k}  web results {SETTINGS.web_results}")
    print(f"kb       {SETTINGS.kb_path}")
    print(f"groq key {'set' if os.getenv('GROQ_API_KEY') else 'MISSING'}")
