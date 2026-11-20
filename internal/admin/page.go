package admin

import (
	_ "embed"
	"html"
	"strings"
)

// UIRevision is the admin console markup/script stamp. Bump it on every
// meaningful change to the embedded console bundle so operators can tell
// the rebuilt binary carries the new UI (independent of the release tag
// injected via ldflags).
const UIRevision = "2026.11.20-atelier-v3"

//go:embed dist/index.html
var embeddedConsoleHTML string

var renderedPageHTML = strings.Replace(embeddedConsoleHTML, "__CBP_UI_REVISION__", UIRevision, 1)

// PageHTML returns the self-contained single-file admin console HTML.
func PageHTML() string {
	return renderedPageHTML
}

// LaunchPage renders the standalone OAuth callback landing page with the
// same Atelier visual tokens and automatic dark mode support.
func LaunchPage(message string, success bool) string {
	stateLabel := "认证未完成"
	toneClass := "err"
	if success {
		stateLabel = "认证成功"
		toneClass = "ok"
	}
	return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Buddy Proxy · OAuth</title>
<style>
:root{
  --canvas:#F5F2EB;
  --surface:#FCFBF8;
  --border:rgba(32,28,24,.12);
  --border-strong:rgba(32,28,24,.24);
  --fg:#1D1A17;
  --fg-2:#575049;
  --fg-3:#8C8378;
  --accent:#1D1A17;
  --accent-fg:#FCFBF8;
  --ok:#1E7A56;
  --ok-bg:rgba(30,122,86,.10);
  --err:#B83A3A;
  --err-bg:rgba(184,58,58,.10);
  --serif:Georgia,"Iowan Old Style","Palatino Linotype",Palatino,"Songti SC","Noto Serif SC",serif;
  --sans:system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;
  --mono:ui-monospace,"SF Mono",Menlo,Consolas,monospace;
}
@media (prefers-color-scheme: dark){
  :root{
    --canvas:#141311;
    --surface:#1E1D1A;
    --border:rgba(242,236,225,.10);
    --border-strong:rgba(242,236,225,.22);
    --fg:#EFECE5;
    --fg-2:#ABA396;
    --fg-3:#756E64;
    --accent:#EAE3D5;
    --accent-fg:#161513;
    --ok:#4DC492;
    --ok-bg:rgba(77,196,146,.14);
    --err:#E66C6C;
    --err-bg:rgba(230,108,108,.14);
  }
}
*{box-sizing:border-box}
body{
  margin:0;min-height:100dvh;display:grid;place-items:center;padding:24px;
  font-family:var(--sans);font-size:14px;line-height:1.6;
  color:var(--fg);background:var(--canvas);-webkit-font-smoothing:antialiased;
}
.card{
  width:min(480px,100%);padding:28px 30px;
  background:var(--surface);border:1px solid var(--border);border-radius:6px;
  box-shadow:0 8px 30px rgba(0,0,0,.06);
}
.head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:18px}
.brand{display:flex;align-items:center;gap:10px;font-family:var(--serif);font-size:15px}
.logo{
  width:28px;height:28px;border-radius:5px;border:1px solid var(--border-strong);
  display:grid;place-items:center;
}
.badge{
  display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:999px;
  font-size:11px;font-weight:500;
}
.badge.ok{background:var(--ok-bg);color:var(--ok)}
.badge.err{background:var(--err-bg);color:var(--err)}
.dot{width:6px;height:6px;border-radius:999px;background:currentColor}
h1{margin:0 0 10px;font-family:var(--serif);font-weight:400;font-size:24px;letter-spacing:-.02em}
p{margin:0 0 22px;color:var(--fg-2);font-size:13px;word-break:break-word}
a.btn{
  display:inline-flex;align-items:center;justify-content:center;
  padding:9px 18px;border-radius:4px;text-decoration:none;font-size:13px;font-weight:500;
  background:var(--accent);color:var(--accent-fg);transition:opacity 150ms ease;
}
a.btn:hover{opacity:.88}
.foot{margin-top:18px;padding-top:14px;border-top:1px solid var(--border);font-family:var(--mono);font-size:11px;color:var(--fg-3)}
</style>
</head>
<body>
<div class="card">
  <div class="head">
    <div class="brand">
      <div class="logo" aria-hidden="true">
        <svg width="15" height="15" viewBox="0 0 256 256" fill="none" stroke="currentColor" stroke-width="22" stroke-linecap="round" stroke-linejoin="round"><path d="M50 84 122 128 50 172"/><path d="M122 128h84"/></svg>
      </div>
      <span>Buddy Proxy</span>
    </div>
    <span class="badge ` + toneClass + `"><span class="dot"></span>` + stateLabel + `</span>
  </div>
  <h1>` + stateLabel + `</h1>
  <p>` + html.EscapeString(message) + `</p>
  <a class="btn" href="/direct-admin/#codebuddy">返回控制台</a>
  <div class="foot">protocol_direct · OAuth Callback</div>
</div>
</body>
</html>`
}
