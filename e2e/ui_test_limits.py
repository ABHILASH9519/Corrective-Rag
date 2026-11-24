"""Rate-limit, reduced-motion and mobile-row browser tests (see run.sh)."""
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

LIMITED = os.environ.get("E2E_BASE_LIMITED", "http://localhost:8101")

problems = []
def check(cond, msg):
    print(("PASS " if cond else "FAIL ") + msg)
    if not cond: problems.append(msg)

def launch(pw, **ctx):
    b = launch_browser(pw)
    return b, b.new_context(**ctx).new_page()

with sync_playwright() as pw:
    # 429 countdown against the rate-limited instance (1 question per minute)
    b, page = launch(pw, viewport={"width": 1100, "height": 900})
    page.goto(LIMITED); page.wait_for_timeout(300)
    page.locator("#chips button").first.click()
    page.wait_for_selector(".answer .w", timeout=15000)
    page.get_by_role("button", name="Ask again").click()
    page.wait_for_selector("#result .err", timeout=8000)
    page.wait_for_timeout(500)
    label = page.locator("#go-label").inner_text()
    check(label.startswith("Wait "), f"button shows a countdown after a 429 (label: {label!r})")
    check(page.locator("#go").is_disabled(), "button is disabled during the countdown")
    check("too quickly" in page.locator("#result .err").inner_text(), "rate-limit message is shown")
    check(page.get_by_role("button", name="Try again").count() == 0, "no pointless Try again button while waiting")
    page.screenshot(path=f"{SHOTS}/11-rate-limited.png")
    page.wait_for_timeout(2200)
    check(page.locator("#go-label").inner_text() != label, "countdown ticks down")
    b.close()

    # reduced motion: nothing animates by itself, content is simply there
    b, page = launch(pw, viewport={"width": 1100, "height": 900}, reduced_motion="reduce")
    page.goto(BASE); page.wait_for_timeout(200)
    check(page.evaluate("getComputedStyle(document.querySelector('#lamps .lamp.r')).animationName") == "none", "no idle lamp sweep with reduced motion")
    page.locator("#chips button").first.click()
    page.wait_for_selector(".answer .w", timeout=15000)
    names = page.evaluate("[...document.querySelectorAll('.answer .w')].map(w => getComputedStyle(w).animationName)")
    check(all(n == "none" for n in names), "answer words appear at once with reduced motion")
    check(page.evaluate("getComputedStyle(document.querySelector('.node .scan i')).animationName") == "none", "no scanning bar animation with reduced motion")
    b.close()

    # mobile row layout after the similarity fix
    b, page = launch(pw, viewport={"width": 390, "height": 844}, device_scale_factor=2, is_mobile=True, has_touch=True)
    page.goto(BASE); page.wait_for_timeout(300)
    page.locator("#chips button").first.tap()
    page.wait_for_selector(".answer .w", timeout=15000); page.wait_for_timeout(2200)
    page.locator("#result .rows").scroll_into_view_if_needed()
    page.locator("#result .card").nth(1).screenshot(path=f"{SHOTS}/12-mobile-rows.png")
    h = page.evaluate("document.querySelector('.row-head').getBoundingClientRect().height")
    check(h < 70, f"mobile passage row stays compact (height {h:.0f}px)")
    check(page.evaluate("getComputedStyle(document.querySelectorAll('.row')[1].querySelector('.bar i')).opacity") in ("0.35", "0.349"), "irrelevant note's bar is dimmed")
    b.close()

print("\nRESULT:", "ALL PASSED" if not problems else f"{len(problems)} FAILED: {problems}")
sys.exit(1 if problems else 0)
