"""Browser tests for the RAGForge UI against a real crag server (see run.sh)."""
import os, sys, tempfile
from playwright.sync_api import sync_playwright

BASE = os.environ.get("E2E_BASE", "http://localhost:8100")
SHOTS = os.environ.get("E2E_SHOTS") or tempfile.mkdtemp(prefix="ragforge-shots-")
os.makedirs(SHOTS, exist_ok=True)
CHROME = os.environ.get("CHROME")  # optional path to a Chrome/Chromium binary


def launch_browser(pw):
    # Talk to localhost directly even when a system proxy is configured.
    kw = {"args": ["--no-sandbox", "--proxy-server=direct://", "--proxy-bypass-list=*"]}
    if CHROME:
        kw["executable_path"] = CHROME
    return pw.chromium.launch(**kw)


TRACE = """
window.__log = [];
const t0 = performance.now();
new MutationObserver((muts) => {
  for (const m of muts) {
    if (m.type === 'attributes' && m.attributeName === 'data-state' && m.target.dataset.node)
      window.__log.push([Math.round(performance.now() - t0), m.target.dataset.node, m.target.dataset.state]);
  }
}).observe(document, {attributes: true, subtree: true, attributeFilter: ['data-state']});
"""

problems = []
def check(cond, msg):
    print(("PASS " if cond else "FAIL ") + msg)
    if not cond: problems.append(msg)

def new_page(pw, w=1280, h=900, scheme="light", **kw):
    b = launch_browser(pw)
    ctx = b.new_context(viewport={"width": w, "height": h}, color_scheme=scheme, **kw)
    page = ctx.new_page()
    errs = []
    page.on("pageerror", lambda e: errs.append("pageerror: " + str(e)))
    page.on("console", lambda m: errs.append("console." + m.type + ": " + m.text) if m.type in ("error", "warning") else None)
    page.add_init_script(TRACE)
    return b, page, errs

def states(page):
    return page.evaluate("window.__log")

with sync_playwright() as pw:
    # ---------- 1. CORRECT path, desktop light ----------
    b, page, errs = new_page(pw)
    page.goto(BASE)
    page.wait_for_timeout(2600)
    page.screenshot(path=f"{SHOTS}/01-idle-light.png", full_page=True)
    check(page.locator("h1").inner_text().startswith("Checks its own sources"), "hero headline renders")
    check(not page.locator("#recent").is_visible(), "recent list hidden when empty")

    page.locator("#chips button").first.click()
    check(page.locator("#go").get_attribute("data-mode") == "stop", "button becomes Stop while running")
    for i, ms in enumerate([350, 500, 500, 700]):
        page.wait_for_timeout(ms)
        page.screenshot(path=f"{SHOTS}/02-run-{i+1}.png", full_page=True)
    page.wait_for_selector(".answer .w", timeout=15000)
    page.wait_for_timeout(2200)  # let the word reveal finish
    page.screenshot(path=f"{SHOTS}/03-correct-done-light.png", full_page=True)

    log = states(page)
    seq = [(n, s) for _, n, s in log if n in ("retrieve", "grade", "kb", "answer")]
    print("   node transitions:", seq)
    order = [x for x in seq if x[1] in ("active", "done")]
    check(order.index(("retrieve", "done")) < order.index(("grade", "done")) < order.index(("kb", "done")) < order.index(("answer", "done")),
          "stages complete in pipeline order (retrieve < grade < notes lane < answer)")
    check(("web", "skip") in [(n, s) for _, n, s in log], "web lane is marked skipped for a CORRECT verdict")
    check(page.locator(".lamp.g.on").count() == 1 and page.locator(".lamps .lamp.on").count() == 1, "only the green lamp is lit")
    check("Notes were enough" in page.locator(".verdict").inner_text(), "verdict chip uses plain language")
    check("tatkal-timing" in page.locator(".cite.kb").first.inner_text(), "answer carries a KB citation chip")
    check(page.locator(".row").count() == 3, "three graded passages listed")
    check([page.locator(".row .label").nth(i).inner_text() for i in range(3)] == ["relevant", "irrelevant", "irrelevant"], "labels flipped to final grades")
    check(page.locator("#route [data-node=retrieve] .node-time").inner_text() != "", "stage timing shown from real server timestamps")
    check(page.locator("#recent").is_visible() and page.locator("#recent .chip").count() == 1, "question saved to recent list")

    # citation chip opens the matching note
    page.locator(".cite.kb").first.click()
    page.wait_for_timeout(700)
    check(page.locator(".row.open").count() == 1, "clicking a KB citation opens that note")
    check("Tatkal" in page.locator(".row.open .row-body p").inner_text() or len(page.locator(".row.open .row-body p").inner_text()) > 20, "opened note shows its text")
    page.screenshot(path=f"{SHOTS}/04-note-open.png", full_page=True)

    # copy button
    page.context.grant_permissions(["clipboard-read", "clipboard-write"], origin=BASE)
    page.get_by_role("button", name="Copy answer").click()
    page.wait_for_timeout(300)
    check(page.locator("#toast.show").count() == 1, "copy shows a toast")
    check("Tatkal booking" in page.evaluate("navigator.clipboard.readText()"), "clipboard holds the answer")

    # theme cycle
    before = page.locator("#theme").get_attribute("data-theme")
    page.locator("#theme").click()
    check(page.locator("#theme").get_attribute("data-theme") != before, "theme toggle cycles")
    page.locator("#theme").click()  # -> dark
    page.wait_for_timeout(400)
    check(page.evaluate("document.documentElement.dataset.theme") == "dark", "dark theme applied")
    page.screenshot(path=f"{SHOTS}/05-correct-done-dark.png", full_page=True)
    page.reload(); page.wait_for_timeout(600)
    check(page.evaluate("document.documentElement.dataset.theme") == "dark", "theme persists across reload")
    check(page.locator("#recent .chip").count() == 1, "recent list persists across reload")
    check(not [e for e in errs if "keep-alive" not in e], "no console errors/warnings: " + str(errs))
    b.close()

    # ---------- 2. INCORRECT path (web lane) ----------
    b, page, errs = new_page(pw)
    page.goto(BASE); page.wait_for_timeout(500)
    page.locator("#chips button").nth(1).click()
    page.wait_for_selector(".card.enter .query", timeout=20000)
    page.wait_for_timeout(900)
    page.screenshot(path=f"{SHOTS}/06-web-lane-running.png", full_page=True)
    page.wait_for_selector(".answer", timeout=40000)
    page.wait_for_timeout(2000)
    page.screenshot(path=f"{SHOTS}/07-incorrect-done.png", full_page=True)
    log = states(page)
    check(("kb", "skip") in [(n, s) for _, n, s in log], "notes lane skipped for INCORRECT")
    check(page.locator(".lamp.r.on").count() == 1, "red lamp lit for INCORRECT")
    check(page.locator("#result .query").inner_text() != "", "rewritten web query shown")
    print("   web card:", page.locator("#result .found").inner_text() if page.locator("#result .found").count() else "(none)")
    print("   answer:", page.locator(".answer").inner_text())
    check(not errs, "no console errors on web path: " + str(errs))
    b.close()

    # ---------- 3. mobile ----------
    b, page, errs = new_page(pw, 390, 844, "light", device_scale_factor=2, has_touch=True, is_mobile=True)
    page.goto(BASE); page.wait_for_timeout(2400)
    page.screenshot(path=f"{SHOTS}/08-mobile-idle.png", full_page=True)
    page.locator("#chips button").first.tap()
    page.wait_for_selector(".answer .w", timeout=15000)
    page.wait_for_timeout(2300)
    page.screenshot(path=f"{SHOTS}/09-mobile-done.png", full_page=True)
    overflow = page.evaluate("document.documentElement.scrollWidth - document.documentElement.clientWidth")
    check(overflow <= 0, f"no horizontal overflow on mobile (overflow={overflow}px)")
    check(not errs, "no console errors on mobile: " + str(errs))
    b.close()

    # ---------- 4. stop + keyboard ----------
    b, page, errs = new_page(pw)
    page.goto(BASE); page.wait_for_timeout(300)
    page.keyboard.press("/")
    check(page.evaluate("document.activeElement.id") == "q", "'/' focuses the question box")
    page.fill("#q", "What time does Tatkal booking open for AC classes?")
    check(page.locator("#count").inner_text() == "50/500", "character counter updates (shows " + page.locator("#count").inner_text() + ")")
    page.keyboard.press("Enter")
    page.wait_for_timeout(600)
    page.keyboard.press("Escape")
    page.wait_for_timeout(500)
    check("Stopped" in page.locator("#result").inner_text(), "Escape stops a running question")
    check(page.locator("#go").get_attribute("data-mode") == "ask" and not page.locator("#go").is_disabled(), "button is ready again after stopping")
    page.screenshot(path=f"{SHOTS}/10-stopped.png", full_page=True)
    b.close()

    # ---------- 5. fallback when stream endpoint is missing ----------
    b, page, errs = new_page(pw)
    page.route("**/ask/stream", lambda r: r.fulfill(status=404, body="not found"))
    page.goto(BASE); page.wait_for_timeout(300)
    page.locator("#chips button").first.click()
    page.wait_for_selector(".answer .w", timeout=15000)
    check("Notes were enough" in page.locator(".verdict").inner_text(), "falls back to /ask when /ask/stream is missing")
    b.close()

    # ---------- 6. network failure ----------
    b, page, errs = new_page(pw)
    page.route("**/ask/stream", lambda r: r.abort())
    page.goto(BASE); page.wait_for_timeout(300)
    page.locator("#chips button").first.click()
    page.wait_for_selector("#result .err", timeout=8000)
    check("Could not reach" in page.locator("#result .err").inner_text(), "network failure gives a clear message")
    check(page.get_by_role("button", name="Try again").count() == 1, "error offers Try again")
    b.close()

print("\nRESULT:", "ALL PASSED" if not problems else f"{len(problems)} FAILED: {problems}")
sys.exit(1 if problems else 0)
