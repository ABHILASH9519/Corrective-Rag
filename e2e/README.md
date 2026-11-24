# Browser tests

These drive the real UI in headless Chromium against the real `crag` binary. A small fake model server stands in for Groq, so the tests need no API key and no network, and each verdict (correct, ambiguous, incorrect) can be triggered on purpose.

```bash
pip install playwright
playwright install chromium    # or: export CHROME=/path/to/chrome
make e2e
```

What they cover: the route diagram moving through stages in order, each verdict lighting the right lamp, the notes and web lanes, citation chips, copy buttons, theme persistence, recent questions, stop with `Esc`, the `/` shortcut, fallback to `/ask` when streaming is missing, network failure messages, the rate-limit countdown, reduced motion, and mobile layout with no horizontal scroll.

Set `E2E_SHOTS=./shots` to keep a screenshot of each step. Ports can be changed with `APP_PORT`, `LIMITED_PORT` and `LLM_PORT`.

`fake_llm.py` picks grades from keywords in the question: "tatkal" gives a relevant note, "baggage" gives none, anything else gives partial notes.
