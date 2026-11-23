"""Domain-neutral web fallback with bounded rate-limit retries."""
from __future__ import annotations
import asyncio, logging, time
from crag.config import SETTINGS
from crag.llm import ask_text
log=logging.getLogger(__name__)
REWRITE_SYSTEM="""Rewrite the user's question as a concise web search query of 4 to 10 keywords.
Preserve names, products, organizations, places, dates and numbers. Use relevant
domain terminology when clear. Return only the query, without quotes or explanation."""
async def rewrite_query(question: str) -> str:
    query=await ask_text(REWRITE_SYSTEM,question)
    return query.strip().strip('"') or question
def _search(query: str) -> list[dict]:
    from ddgs import DDGS
    from ddgs.exceptions import RatelimitException
    for attempt in range(3):
        try: return DDGS().text(query,max_results=SETTINGS.web_results)
        except RatelimitException:
            if attempt==2: raise
            time.sleep(2*(attempt+1))
    return []
async def web_search(query: str) -> list[str]:
    from ddgs.exceptions import DDGSException
    try: results=await asyncio.to_thread(_search,query)
    except DDGSException as error:
        log.warning("web_search_failed",extra={"error_type":type(error).__name__})
        return []
    return [f"{r['title']}: {r['body']} [WEB:{r['href']}]" for r in results
            if r.get("title") and r.get("body") and r.get("href")]
