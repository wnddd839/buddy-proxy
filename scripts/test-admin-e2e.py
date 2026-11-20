"""Browser regression against an isolated real binary; requires Python Playwright.

  go build -o .tmp/buddy-ui.exe ./cmd/codebuddy-proxy
  python scripts/test-admin-e2e.py --binary .tmp/buddy-ui.exe

Only account/status/model display data is stubbed in the browser. Configuration
GET/POST requests and persistence use the real Go server and temporary files.
No real credentials or upstream calls are needed.
"""
import argparse
import os
from pathlib import Path
import re
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
        env.update({
            "HOME": tmp,
            "USERPROFILE": tmp,
            "CODEBUDDY_PROXY_HOST": "127.0.0.1",
            "CODEBUDDY_PROXY_PORT": str(port),
            "CODEBUDDY_PROXY_API_KEY": "e2e-not-a-real-secret",
            "CODEBUDDY_PROXY_ENV_FILE": str(home / "proxy.env"),
            "CODEBUDDY_PROXY_ACCOUNTS_PATH": str(home / "accounts.json"),
            "CODEBUDDY_PROXY_USAGE_PATH": str(home / "usage.json"),
            "CODEBUDDY_PROXY_CHECKIN_AUTO": "false",
        })
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
                        time.sleep(0.1)
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
                    assert page.locator(".tab-link").count() == 7
                    ids = page.locator("[id]").evaluate_all("els => els.map(el => el.id)")
                    assert len(ids) == len(set(ids)), f"duplicate DOM IDs: {ids}"

                    accounts = [
                        {
                            "id": "cn-test",
                            "label": "本地测试 · <img src=x onerror=alert(1)>",
                            "site": "domestic",
                            "enabled": True,
                            "hasCredentials": True,
                            "loggedIn": True,
                            "checkinToday": "done",
                            "checkinCount48h": 2,
                        },
                        {
                            "id": "global-test",
                            "label": "国际测试",
                            "site": "global",
                            "enabled": True,
                            "hasCredentials": True,
                            "loggedIn": True,
                            "checkinToday": "failed",
                            "checkinCount48h": 0,
                        },
                    ]
                    status = {
                        "ok": True,
                        "poolSite": "domestic",
                        "transport": "protocol_direct",
                        "version": "local-e2e",
                        "accounts": {
                            "accounts": accounts,
                            "enabledCount": 2,
                            "activeEnabledCount": 1,
                            "activeAvailableCount": 1,
                            "domesticCount": 1,
                            "globalCount": 1,
                            "loggedIn": True,
                        },
                        "accountUsages": [
                            {"accountId": a["id"], "credits": {"remaining": 47, "total": 100}}
                            for a in accounts
                        ],
                    }
                    page.route("**/api/status*", lambda route: route.fulfill(json=status))
                    page.route(
                        "**/api/codebuddy/models*",
                        lambda route: route.fulfill(
                            json={"models": [{"id": "test-model", "sites": ["domestic", "global"], "credits": "1x credits"}]}
                        ),
                    )
                    page.locator("#btnRefresh").click()
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

                    # Refreshing status must not clobber unsaved checkin draft.
                    page.locator("#btnRefresh").click()
                    expect(start).to_have_value("09:37")
                    expect(page.locator("#checkinFallback")).to_have_value("on")
                    expect(page.locator("#checkinRetryMin")).to_have_value("30")

                    page.locator("#btnCheckinSave").click()
                    expect(page.locator("#checkinSaveState")).to_have_text("签到配置已保存并生效")
                    saved = page.request.get(base + "/direct-admin/api/checkin-config").json()
                    assert saved["auto"] and saved["allowDirectFallback"] and saved["checkinRetryMin"] == 30 and saved["checkinRetryMax"] == 2
                    assert len(saved["rules"]) == 2 and saved["rules"][0]["start"] == "09:37"

                    # Site filter narrows visible list & stats, yet saving preserves the other site's rule.
                    page.locator('#checkinSiteSeg [data-cksite="domestic"]').click()
                    expect(page.locator("#ckDone")).to_have_text("1")
                    expect(page.locator("#ckFailed")).to_have_text("0")
                    expect(page.locator(".checkin-rule")).to_have_count(1)
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

                    # Pool + Quota floor drawer
                    page.locator('[data-tab="tab-pool"]').click()
                    expect(page.locator("#tab-pool")).to_be_visible()
                    page.screenshot(path=str(output / "accounts-desktop.png"), full_page=True)
                    page.locator("#btnStrategyOpen").click()
                    page.locator("#quotaFloor").fill("18")
                    expect(page.locator("#quotaFloorState")).to_have_attribute("data-state", "dirty")
                    page.locator("#btnQuotaFloorSave").click()
                    expect(page.locator("#quotaFloorState")).to_contain_text("低于 18")
                    assert page.request.get(base + "/direct-admin/api/quota-floor").json()["quotaFloor"] == 18
                    page.keyboard.press("Escape")

                    # Models & directional routes + live availability badge
                    page.locator('[data-tab="tab-models"]').click()
                    expect(page.locator("#tab-models")).to_be_visible()
                    page.locator("#btnRouteAdd").click()
                    page.locator('[data-rf="match"]').fill("alias")
                    page.locator('[data-rf="target"]').fill("upstream")
                    page.locator("#btnRouteSave").click()
                    expect(page.locator("#routeSaveState")).to_have_text("路由规则已保存并生效")
                    saved_route = page.request.get(base + "/direct-admin/api/model-routes").json()["rules"][0]
                    assert saved_route["target"] == "upstream" and saved_route["mode"] == "onExhausted"
                    page.locator('[data-rf="mode"]').select_option("always")
                    page.locator("#btnRouteSave").click()
                    expect(page.locator("#routeSaveState")).to_have_text("路由规则已保存并生效")
                    assert page.request.get(base + "/direct-admin/api/model-routes").json()["rules"][0]["mode"] == "always"

                    now = int(time.time() * 1000)
                    accounts[0].update({"quotaRemaining": 3, "quotaCheckedAt": now})
                    accounts[1]["modelCooldowns"] = {"test-model": now + 600000}
                    page.locator("#btnRefresh").click()
                    page.locator("#btnModelsBoth").click()
                    badge = page.locator('#modelChips [data-copy="test-model"] .model-avail')
                    expect(badge).to_have_text("可用 0 · 冷却 1 · 额度不足 1")
                    accounts[0]["quotaRemaining"] = 50
                    page.locator("#btnRefresh").click()
                    expect(badge).to_have_text("可用 1 · 冷却 1")
                    page.screenshot(path=str(output / "models-desktop.png"), full_page=True)

                    # System Prompt developer panel
                    page.locator('[data-tab="tab-system-prompt"]').click()
                    expect(page.locator("#tab-system-prompt")).to_be_visible()
                    assert page.locator('[aria-label="system 提示词实时事件"] thead th').count() == 11
                    with page.expect_response(
                        lambda r: r.url.endswith("/direct-admin/api/system-prompt") and r.request.method == "POST"
                    ):
                        page.locator('#spModeSeg [data-mode="strip"]').click()
                    expect(page.locator('#spModeSeg [data-mode="strip"]')).to_have_class(re.compile(r"\bactive\b"))
                    assert page.request.get(base + "/direct-admin/api/system-prompt").json()["mode"] == "strip"
                    page.reload()
                    expect(page.locator("#tab-system-prompt")).to_be_visible()
                    expect(page.locator('#spModeSeg [data-mode="strip"]')).to_have_class(re.compile(r"\bactive\b"))
                    with page.expect_response(
                        lambda r: r.url.endswith("/direct-admin/api/system-prompt") and r.request.method == "POST"
                    ):
                        page.locator('#spModeSeg [data-mode="auto"]').click()
                    expect(page.locator('#spModeSeg [data-mode="auto"]')).to_have_class(re.compile(r"\bactive\b"))
                    assert page.request.get(base + "/direct-admin/api/system-prompt").json()["mode"] == "auto"
                    page.screenshot(path=str(output / "system-prompt-desktop.png"), full_page=True)

                    # Overview screenshot in light & dark themes + hero chart + persistent sidebar check
                    usage_sample = {
                        "ok": True,
                        "summary": {
                            "requests": 128,
                            "failed": 2,
                            "totalTokens": 1420800,
                            "cacheHitRate": 68.4,
                            "credits": 12.45,
                            "creditRows": 96,
                        },
                        "series": [
                            {"at": 1, "label": "00:00", "totalTokens": 42000, "cacheHitRate": 48},
                            {"at": 2, "label": "03:00", "totalTokens": 18000, "cacheHitRate": 52},
                            {"at": 3, "label": "06:00", "totalTokens": 65000, "cacheHitRate": 61},
                            {"at": 4, "label": "09:00", "totalTokens": 215000, "cacheHitRate": 71},
                            {"at": 5, "label": "12:00", "totalTokens": 310000, "cacheHitRate": 74},
                            {"at": 6, "label": "15:00", "totalTokens": 284000, "cacheHitRate": 69},
                            {"at": 7, "label": "18:00", "totalTokens": 248000, "cacheHitRate": 72},
                            {"at": 8, "label": "21:00", "totalTokens": 172000, "cacheHitRate": 66},
                            {"at": 9, "label": "23:00", "totalTokens": 66800, "cacheHitRate": 64},
                        ],
                        "byModel": [
                            {"model": "claude-4-sonnet", "requests": 64, "promptTokens": 520000, "totalTokens": 780000, "cachedTokens": 395000, "cacheHitRate": 76.0},
                            {"model": "gpt-5.4", "requests": 42, "promptTokens": 310000, "totalTokens": 440800, "cachedTokens": 198000, "cacheHitRate": 63.9},
                            {"model": "gemini-2.5-pro", "requests": 22, "promptTokens": 140000, "totalTokens": 200000, "cachedTokens": 72000, "cacheHitRate": 51.4},
                        ],
                        "requests": [],
                        "requestsTotal": 0,
                        "accounts": [],
                        "models": [],
                    }
                    page.route("**/api/usage*", lambda route: route.fulfill(json=usage_sample))
                    page.locator('[data-tab="tab-overview"]').click()
                    expect(page.locator("#tab-overview")).to_be_visible()
                    expect(page.locator('#overviewHeroChart svg[role="img"]')).to_be_visible()
                    expect(page.locator('#overviewHeroChart svg[role="img"] path')).to_have_count(3)
                    expect(page.locator("#appSidebar")).to_be_visible()
                    for sel in ["#btnPoolDomestic", "#btnPoolGlobal", "#btnProductCodeBuddy", "#btnProductWorkBuddy", "#updateCheck"]:
                        box = page.locator(sel).bounding_box()
                        assert box and box["height"] <= 36, f"control wrapped vertically ({sel}): {box}"
                    page.screenshot(path=str(output / "overview-light.png"), full_page=True)
                    page.locator('button[aria-label="切换至深色模式"]').click()
                    page.screenshot(path=str(output / "overview-dark.png"), full_page=True)
                    page.locator('button[aria-label="切换至浅色模式"]').click()
                    page.unroute("**/api/usage*")

                    # Client & Usage desktop screenshots
                    page.locator('[data-tab="tab-client"]').click()
                    expect(page.locator("#tab-client")).to_be_visible()
                    page.screenshot(path=str(output / "client-desktop.png"), full_page=True)
                    page.locator('[data-tab="tab-usage"]').click()
                    expect(page.locator("#tab-usage")).to_be_visible()
                    page.screenshot(path=str(output / "usage-desktop.png"), full_page=True)

                    # Real site toggle round-trip + half-screen 840px persistent sidebar check
                    page.locator('[data-tab="tab-overview"]').click()
                    with page.expect_response(
                        lambda r: r.url.endswith("/direct-admin/api/pool-site") and r.request.method == "POST"
                    ):
                        page.locator("#btnPoolGlobal").click()
                    expect(page.locator("#btnPoolGlobal")).to_have_attribute("aria-pressed", "true")
                    page.set_viewport_size({"width": 840, "height": 768})
                    expect(page.locator("#appSidebar")).to_be_visible()
                    for sel in ["#btnPoolDomestic", "#btnPoolGlobal", "#btnProductCodeBuddy", "#btnProductWorkBuddy"]:
                        box = page.locator(sel).bounding_box()
                        assert box and box["height"] <= 36, f"control wrapped at 840px ({sel}): {box}"
                    page.screenshot(path=str(output / "overview-840.png"), full_page=True)
                    with page.expect_response(
                        lambda r: r.url.endswith("/direct-admin/api/pool-site") and r.request.method == "POST"
                    ):
                        page.locator("#btnPoolDomestic").click()
                    expect(page.locator("#btnPoolDomestic")).to_have_attribute("aria-pressed", "true")
                    page.set_viewport_size({"width": 1366, "height": 900})

                    # Legacy anchors
                    for anchor, panel in [
                        ("#codebuddy", "tab-pool"),
                        ("#client-config", "tab-client"),
                        ("#system-prompt", "tab-system-prompt"),
                        ("#usage", "tab-usage"),
                    ]:
                        page.goto(base + "/direct-admin/" + anchor)
                        expect(page.locator("#" + panel)).to_be_visible()

                    # Mobile viewport (390x844) zero horizontal overflow across all 7 chapters
                    page.set_viewport_size({"width": 390, "height": 844})
                    for anchor, panel in [
                        ("#overview", "tab-overview"),
                        ("#accounts", "tab-pool"),
                        ("#checkin", "tab-checkin"),
                        ("#client-config", "tab-client"),
                        ("#models", "tab-models"),
                        ("#system-prompt", "tab-system-prompt"),
                        ("#usage", "tab-usage"),
                    ]:
                        page.goto(base + "/direct-admin/" + anchor)
                        expect(page.locator("#" + panel)).to_be_visible()
                        assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), f"mobile overflow: {panel}"
                    page.screenshot(path=str(output / "usage-mobile.png"), full_page=True)

                    assert not errors, errors
                    assert not external, external
                    browser.close()

                # Product page verification
                with sync_playwright() as pw:
                    browser = pw.chromium.launch()
                    page = browser.new_page(viewport={"width": 1366, "height": 768}, reduced_motion="reduce")
                    page.goto((Path(__file__).resolve().parents[1] / "docs/index.html").as_uri())
                    assert page.locator(".tab-trigger").count() == 6
                    assert page.locator(".article-item").count() == 8
                    cta = page.locator(".hero .hero-btn-row").bounding_box()
                    assert cta["y"] + cta["height"] <= 768, "hero CTA below small-laptop viewport"
                    page.set_viewport_size({"width": 390, "height": 844})
                    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "product page mobile overflow"
                    browser.close()

                print("PASS: 7 chapters, real config persistence, draft/error recovery, filters, legacy anchors, XSS escaping, light/dark, desktop/mobile, zero external requests")
            finally:
                process.terminate()
                process.wait(timeout=10)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=Path(".tmp/atelier-e2e"))
    args = parser.parse_args()
    run(args.binary.resolve(), args.output.resolve())
