"""Browser regression against an isolated real binary; requires Python Playwright.

  go build -o .tmp/buddy-ui.exe ./cmd/codebuddy-proxy
  python scripts/test-admin-e2e.py --binary .tmp/buddy-ui.exe

Only account/status/model display data is stubbed in the browser. Configuration
GET/POST requests and persistence use the real Go server and temporary files.
No real credentials or upstream calls are needed.
"""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.request

from playwright.sync_api import sync_playwright, expect


def run(binary, output):
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="buddy-admin-e2e-") as tmp:
        home = Path(tmp)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env = {k: v for k, v in os.environ.items() if not k.startswith(("CODEBUDDY_", "CURSOR_"))}
        env.update({"HOME": tmp, "USERPROFILE": tmp,
                    "CODEBUDDY_PROXY_HOST": "127.0.0.1", "CODEBUDDY_PROXY_PORT": str(port),
                    "CODEBUDDY_PROXY_API_KEY": "e2e-not-a-real-secret",
                    "CODEBUDDY_PROXY_ENV_FILE": str(home / "proxy.env"),
                    "CODEBUDDY_PROXY_ACCOUNTS_PATH": str(home / "accounts.json"),
                    "CODEBUDDY_PROXY_USAGE_PATH": str(home / "usage.json"),
                    "CODEBUDDY_PROXY_CHECKIN_AUTO": "false"})
        (home / "proxy.env").write_text("", encoding="utf-8")
        base = f"http://127.0.0.1:{port}"
        with (output / "server.log").open("w", encoding="utf-8") as log:
            process = subprocess.Popen([str(binary)], cwd=tmp, env=env, stdout=log, stderr=log)
            try:
                for _ in range(100):
                    try:
                        with urllib.request.urlopen(base + "/health", timeout=1):
                            break
                    except OSError:
                        if process.poll() is not None:
                            raise RuntimeError("server exited; see server.log")
                        time.sleep(.1)
                else:
                    raise RuntimeError("server startup timed out")
                with sync_playwright() as pw:
                    browser = pw.chromium.launch()
                    page = browser.new_page(viewport={"width": 1366, "height": 900}, reduced_motion="reduce")
                    errors = []
                    external = []
                    page.on("pageerror", lambda e: errors.append(str(e)))
                    page.on("request", lambda r: external.append(r.url) if not r.url.startswith(base) else None)
                    page.goto(base + "/direct-admin/#checkin")
                    expect(page.locator("#checkinSaveState")).to_have_text("签到配置已保存")
                    expect(page.locator("#tab-checkin")).to_be_visible()
                    expect(page.locator("#checkinRules")).to_contain_text("账号池还没有账号")
                    expect(page.locator("#updateInstall")).to_be_hidden()
                    expect(page.locator("#concurrencyBar")).to_be_hidden()  # separate overview chapter
                    assert page.locator(".tab-link").count() == 6
                    assert page.locator("#tab-client #checkinAuto").count() == 0
                    ids = page.locator("[id]").evaluate_all("els => els.map(el => el.id)")
                    assert len(ids) == len(set(ids)), "duplicate DOM IDs"

                    accounts = [
                        {"id": "cn-test", "label": "本地测试 · <img src=x onerror=alert(1)>", "site": "domestic", "enabled": True, "hasCredentials": True, "checkinToday": "done"},
                        {"id": "global-test", "label": "国际测试", "site": "global", "enabled": True, "hasCredentials": True, "checkinToday": "failed"},
                    ]
                    status = {"ok": True, "poolSite": "domestic", "transport": "protocol_direct", "version": "local-e2e",
                              "accounts": {"accounts": accounts, "enabledCount": 2, "loggedIn": True},
                              "accountUsages": [{"accountId": a["id"], "credits": {"remaining": 47, "total": 100}} for a in accounts]}
                    page.route("**/api/status*", lambda route: route.fulfill(json=status))
                    page.evaluate("refreshStatus()")
                    expect(page.locator(".checkin-rule")).to_have_count(2)
                    assert page.locator("#checkinRules img").count() == 0, "account label was not escaped"
                    page.locator("#checkinAuto").select_option("on")
                    page.locator("#checkinFallback").select_option("on")
                    page.locator("#checkinRetryMin").select_option("30")
                    page.locator("#checkinRetryMax").select_option("2")
                    page.locator("#btnCheckinAll").click()
                    page.locator("#checkinPreset").select_option("0900")
                    start = page.locator(".checkin-rule").first.locator('[data-f="start"]')
                    expect(start).to_be_visible()
                    start.fill("09:37")
                    expect(page.locator("#checkinSaveState")).to_have_attribute("data-state", "dirty")
                    page.evaluate("refreshStatus()")
                    expect(start).to_have_value("09:37")
                    expect(page.locator("#checkinFallback")).to_have_value("on")
                    expect(page.locator("#checkinRetryMin")).to_have_value("30")
                    page.locator("#btnCheckinSave").click()
                    expect(page.locator("#checkinSaveState")).to_have_text("签到配置已保存并生效")
                    saved = page.request.get(base + "/direct-admin/api/checkin-config").json()
                    assert saved["auto"] and saved["allowDirectFallback"] and saved["checkinRetryMin"] == 30 and saved["checkinRetryMax"] == 2
                    assert len(saved["rules"]) == 2 and saved["rules"][0]["start"] == "09:37"
                    page.locator('#checkinSiteSeg [data-cksite="domestic"]').click()
                    expect(page.locator("#ckDone")).to_have_text("1")
                    expect(page.locator("#ckFailed")).to_have_text("0")
                    page.locator("#btnCheckinNone").click()
                    page.locator("#btnCheckinSave").click()
                    expect(page.locator("#checkinSaveState")).to_have_text("签到配置已保存并生效")
                    saved = page.request.get(base + "/direct-admin/api/checkin-config").json()
                    assert not saved["rules"][0]["enabled"] and saved["rules"][1]["enabled"], "filter dropped the other site's rule"

                    def fail_save(route):
                        if route.request.method == "POST":
                            route.fulfill(status=503, json={"error": "simulated save failure"})
                        else:
                            route.continue_()
                    page.route("**/api/checkin-config", fail_save)
                    start.fill("10:24")
                    page.locator("#btnCheckinSave").click()
                    expect(page.locator("#checkinSaveState")).to_contain_text("保存失败")
                    expect(start).to_have_value("10:24")
                    page.unroute("**/api/checkin-config", fail_save)
                    page.locator("#btnCheckinSave").click()
                    expect(page.locator("#checkinSaveState")).to_have_text("签到配置已保存并生效")
                    page.reload()
                    expect(page.locator("#checkinSaveState")).to_have_text("签到配置已保存")
                    expect(page.locator("#checkinFallback")).to_have_value("on")
                    expect(page.locator("#checkinRetryMin")).to_have_value("30")
                    page.screenshot(path=str(output / "checkin-desktop.png"), full_page=True)

                    page.locator('[data-tab="tab-pool"]').click()
                    page.locator("#quotaFloor").fill("18")
                    expect(page.locator("#quotaFloorState")).to_have_attribute("data-state", "dirty")
                    page.locator("#btnQuotaFloorSave").click()
                    expect(page.locator("#quotaFloorState")).to_contain_text("低于 18")
                    assert page.request.get(base + "/direct-admin/api/quota-floor").json()["quotaFloor"] == 18
                    page.evaluate("scrollTo(0,0)")
                    page.screenshot(path=str(output / "accounts-desktop.png"), full_page=True)
                    page.locator('[data-tab="tab-models"]').click()
                    page.locator("#btnRouteAdd").click()
                    page.locator('[data-rf="match"]').fill("alias")
                    page.locator('[data-rf="target"]').fill("upstream")
                    page.locator("#btnRouteSave").click()
                    expect(page.locator("#routeSaveState")).to_have_text("路由规则已保存并生效")
                    assert page.request.get(base + "/direct-admin/api/model-routes").json()["rules"][0]["target"] == "upstream"
                    for anchor, panel in [("#codebuddy", "tab-pool"), ("#client-config", "tab-client"), ("#usage", "tab-usage")]:
                        page.goto(base + "/direct-admin/" + anchor)
                        expect(page.locator("#" + panel)).to_be_visible()
                    page.set_viewport_size({"width": 390, "height": 844})
                    for chapter in ["overview", "pool", "checkin", "client", "models", "usage"]:
                        page.evaluate("tab => switchTab('tab-' + tab)", chapter)
                        assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), f"mobile overflow: {chapter}"
                    page.evaluate("switchTab('tab-checkin')")
                    page.evaluate("scrollTo(0,0)")
                    page.screenshot(path=str(output / "checkin-mobile.png"), full_page=True)
                    assert not errors, errors
                    assert not external, external
                    browser.close()
                # Product page is also entirely local; verify six client tabs and small-laptop CTA.
                with sync_playwright() as pw:
                    browser = pw.chromium.launch()
                    page = browser.new_page(viewport={"width": 1366, "height": 768}, reduced_motion="reduce")
                    page.goto((Path(__file__).resolve().parents[1] / "docs/index.html").as_uri())
                    assert page.locator(".tab-trigger").count() == 6
                    assert page.locator(".article-item").count() == 8
                    cta = page.locator(".hero .hero-btn-row").bounding_box()
                    assert cta["y"] + cta["height"] <= 768, "hero CTA below small-laptop viewport"
                    page.screenshot(path=str(output / "landing-desktop.png"), full_page=True)
                    page.set_viewport_size({"width": 390, "height": 844})
                    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "product page mobile overflow"
                    browser.close()
                print("PASS: six chapters, real config persistence, draft/error recovery, filters, legacy anchors, XSS escaping, desktop/mobile, offline product page")
            finally:
                process.terminate()
                process.wait(timeout=10)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=Path(".tmp/editorial-e2e"))
    args = parser.parse_args()
    run(args.binary.resolve(), args.output.resolve())
