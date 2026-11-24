# RAGForge design system

The web UI is one embedded file, `internal/server/ui/index.html`, with no build step and no dependencies. This document records the choices in it so later changes stay consistent.

## Concept

RAGForge grades its own sources before answering. The interface shows that: a question travels a visible route, and a **signal lamp** (red, amber, green) reports the grade. The lamp is the one memorable element. Everything around it stays quiet.

Audience: someone asking a help-desk question who wants to know whether to trust the answer. Primary job: show the answer, where it came from, and how it was decided.

## Color

Tokens live on `:root` and are redefined for dark mode, both by system preference and by an explicit `data-theme` on `<html>`.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--bg` | `#e9eef1` | `#0b1218` | page |
| `--plate` | `#f9fbfc` | `#121c25` | cards, inputs, route nodes |
| `--ink` | `#0e1b26` | `#e8eef3` | text, primary button |
| `--muted` | `#52616e` | `#92a1ae` | secondary text |
| `--rail` | `#c4cdd4` | `#2a3844` | borders, idle wires |
| `--kb` | `#1d4ed8` | `#8ab0ff` | notes lane, similarity bars, note citations |
| `--web` | `#7c3aed` | `#bda1ff` | web lane, web citations |
| `--ok` / `--warn` / `--bad` | green / amber / red | lighter equivalents | verdicts and grades |

Rules:
- Blue always means the knowledge base, violet always means the web. Never reuse them for anything else.
- Verdict color is never the only signal: every verdict has a plain-language label and a lit lamp position.
- Body text on `--plate` meets WCAG AA contrast in both themes.

## Type

One system sans stack, no web fonts, so the page paints at once and works offline. A mono stack is used only for data that is genuinely code-like: note ids, similarity scores, timings, citation chips and the rewritten search query.

- Headline: `clamp(2.1rem, 5.6vw, 3.5rem)`, weight 780, tracking `-0.035em`, `text-wrap: balance`.
- Body: 16px, line height 1.55. Answer text 1.1rem, capped at 68ch.
- Numbers that change (timings, counters, similarity) use `font-variant-numeric: tabular-nums` so they do not jitter.
- Copy is sentence case, active voice, and names things by what the person understands ("Use the notes", "Search the web"), not by how the system is built.

## Layout

Single column, max 920px, left aligned. Order: header, headline, question box, example chips, recent questions, route diagram, result, footer.

The route diagram is four nodes. Desktop is a row: Retrieve, Grade, a stacked pair of lanes (notes above web), Answer. Below 760px it becomes a column with the two lanes side by side. Wires between nodes are drawn with CSS only.

## Motion

Motion is used to show state, not to decorate. Nothing animates on its own except two things: the lamp sweep once on page load, and the scanning bar on the node that is currently working.

| Moment | Treatment |
|---|---|
| Node starts working | 3px scanning bar slides across its top edge |
| Node finishes | border takes its lane color; the wire leading out lights up |
| Lane not needed | node goes dashed and fades to 50% |
| Grade arrives | each note label flips in with the spring curve; lamp pops lit |
| Answer arrives | words rise in one after another, capped at about 1.4s total |
| Note expands | `grid-template-rows` 0fr to 1fr over 400ms |
| Press | scale to 0.97 in 80ms, release on the spring |

Easing:
- `--ease: cubic-bezier(.2, .8, .2, 1)` for most transitions.
- `--spring` is a damped spring (stiffness 260, damping 22) sampled into a CSS `linear()` curve. Browsers without `linear()` keep the cubic-bezier.

Rules:
- Animate only `transform`, `opacity`, and `grid-template-rows`. Never `transition: all`.
- Everything that moves is wrapped in `@media (prefers-reduced-motion: no-preference)`. With reduced motion on, content appears at once and state changes are still visible through color and text.
- Animations are interruptible: transitions, not keyframes, wherever a user action can reverse them.

## Components

- **Ask form**: 54px field and button. The button becomes **Stop** while a question runs, and shows **Wait N s** after a rate limit. `/` focuses the field, `Esc` stops a run or clears the field.
- **Chip**: pill button for example and recent questions.
- **Route node**: title, one line of status, and a timing taken from the server's own timestamps, not a client estimate.
- **Verdict chip**: lamp dot plus plain text. Raw verdict (`CORRECT`, `AMBIGUOUS`, `INCORRECT`) is in the tooltip.
- **Passage row**: note id, similarity, grade label, similarity bar. Expands to show the note text. Irrelevant notes dim.
- **Citation chip**: blue button opens the matching note and scrolls to it; violet link opens the web source in a new tab. Only `http(s)` URLs ever become links.
- **Toast**: bottom center, 2.2s, announced politely to screen readers.

## Accessibility

- Visible `:focus-visible` ring on every interactive element; skip link to the question box.
- A hidden live region (`role="status"`) announces each stage and the final answer.
- Results container sets `aria-busy` while a question is running.
- Touch targets are at least 44px on coarse pointers.
- Works without a mouse; no hover-only information.

## Security notes

- The page is served with `default-src 'none'`, inline style and script only, `connect-src 'self'`, and `frame-ancestors 'none'`.
- All server text is inserted with `textContent` or DOM nodes, never as HTML.
- `localStorage` access is wrapped in try/catch; the page works without it.

## Changing this file's subject

When adding UI: reuse the tokens above, keep one memorable element, and ask whether a new animation shows a change the person caused or is only decoration. If it is decoration, leave it out.
