package admin

import (
	"html"
	"strings"
)

func PageHTML() string {
	return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>CodeBuddy Proxy · Console</title>
<style>
:root{
  --bg:#F9F8F6;
  --fg:#1C1C1C;
  --fg-80:rgba(28,28,28,.80);
  --fg-60:rgba(28,28,28,.60);
  --fg-40:rgba(28,28,28,.40);
  --fg-20:rgba(28,28,28,.20);
  --fg-10:rgba(28,28,28,.10);
  --invert-bg:#1C1C1C;
  --invert-fg:#F9F8F6;
  --display:Georgia,"Iowan Old Style","Palatino Linotype",Palatino,"Songti SC","Noto Serif SC",serif;
  --sans:system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;
  --mono:ui-monospace,"SF Mono",Menlo,Consolas,monospace;
}
*{box-sizing:border-box}
html,body{margin:0;min-height:100%}
body{
  font-family:var(--sans);
  font-size:14px;line-height:1.625;
  color:var(--fg);
  background:var(--bg);
  letter-spacing:-.01em;
}
a{color:inherit;text-decoration:none}
.shell{max-width:1200px;margin:0 auto;padding:0 24px 64px}

/* 顶栏 */
.topbar{
  position:sticky;top:0;z-index:50;
  display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;
  min-height:58px;padding:0 0 16px;margin-bottom:24px;
  background:rgba(249,248,246,.90);backdrop-filter:blur(8px);
  border-bottom:1px solid var(--fg-10);
}
.brand{display:flex;align-items:baseline;gap:12px}
.brand strong{
  font-family:var(--display);font-size:18px;font-weight:400;
  letter-spacing:.2em;text-transform:uppercase;color:var(--fg);
}
.brand span{
  font-family:var(--sans);font-size:11px;
  letter-spacing:.15em;text-transform:uppercase;color:var(--fg-40);
}
.mark{display:none}
.pillrow{
  display:flex;align-items:center;gap:0;
  font-family:var(--sans);font-size:11px;letter-spacing:.15em;text-transform:uppercase;
  color:var(--fg-60);
}
.pill{display:inline-flex;align-items:center;gap:6px}
.pill + .pill::before{content:"·";margin:0 10px;color:var(--fg-40)}
.pill .dot{
  width:6px;height:6px;background:var(--fg);border:1px solid var(--fg);
}
.pill .dot.bad{background:transparent;border:1px solid var(--fg-40)}

/* 核心切页导航条 (Editorial Chapter Navigation) */
.tabs-nav{
  display:flex;
  border-top:1px solid var(--fg-10);
  border-bottom:1px solid var(--fg-10);
  margin-bottom:32px;
  overflow-x:auto;
}
.tab-link{
  flex:1;min-width:140px;
  padding:14px 18px;background:transparent;border:none;
  border-right:1px solid var(--fg-10);
  font-family:var(--sans);font-size:13px;letter-spacing:.05em;
  color:var(--fg-60);cursor:pointer;text-align:left;
  transition:color 200ms ease,background-color 200ms ease;
  display:flex;align-items:baseline;gap:8px;
}
.tab-link:last-child{border-right:none}
.tab-link:hover{color:var(--fg);background:rgba(28,28,28,.02)}
.tab-link.active{
  color:var(--fg);font-weight:500;
  box-shadow:inset 0 -2px 0 var(--fg);
  background:rgba(28,28,28,.03);
}
.tab-idx{
  font-family:var(--display);font-style:italic;font-size:15px;color:var(--fg-40);
}
.tab-link.active .tab-idx{color:var(--fg)}

/* 切页容器 */
.tab-panel{display:none}
.tab-panel.active{
  display:block;
  animation:panelFade 250ms ease-out;
}
@keyframes panelFade{
  from{opacity:0;transform:translateY(4px)}
  to{opacity:1;transform:translateY(0)}
}

/* 卡片与面板 */
.panel{
  border:1px solid var(--fg-10);background:transparent;
  padding:24px;margin-bottom:24px;transition:border-color 200ms ease;
}
.panel:hover{border-color:var(--fg-40)}
.panel-inner{height:100%}
.eyebrow{
  font-size:11px;font-family:var(--sans);letter-spacing:.2em;text-transform:uppercase;
  color:var(--fg-40);margin-bottom:8px;
}
h1{
  margin:0 0 8px;font-family:var(--display);font-weight:400;letter-spacing:-.02em;
  font-size:clamp(24px,3vw,32px);color:var(--fg);
}
.lede{margin:0;color:var(--fg-60);font-size:13.5px;line-height:1.6;max-width:52ch}
.checkin-hint{margin:12px 0 0;font-size:11.5px;line-height:1.5;color:var(--fg-40);max-width:58ch}
.checkin-detail{margin:12px 0 0;padding:10px 12px;border:1px solid var(--fg-10);font-family:var(--mono);font-size:11.5px;line-height:1.5;color:var(--fg-80);white-space:pre-wrap}
.metrics{
  display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px;margin-top:24px;
}
@media (max-width:980px){.metrics{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media (max-width:720px){.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}
.metric{padding:16px;border:1px solid var(--fg-10);background:transparent}
.metric .k{font-size:11px;font-family:var(--sans);letter-spacing:.15em;text-transform:uppercase;color:var(--fg-40)}
.metric .v{
  margin-top:8px;font-family:var(--mono);font-size:24px;font-weight:400;
  letter-spacing:-.02em;color:var(--fg);text-align:right;
}
.metric .v.sm{font-size:13.5px;line-height:1.4}
.section-head{
  display:flex;align-items:flex-end;justify-content:space-between;gap:12px;flex-wrap:wrap;
  margin:0 0 16px;padding-bottom:12px;border-bottom:1px solid var(--fg-10);
}
.section-head h2{
  margin:0;font-family:var(--display);font-size:18px;font-weight:400;
  letter-spacing:-.01em;color:var(--fg);
}
.section-head p{margin:4px 0 0;color:var(--fg-60);font-size:12px}
.actions{display:flex;gap:10px;flex-wrap:wrap}
button,.btn,.linkbtn{
  appearance:none;border:1px solid var(--fg-20);background:transparent;
  color:var(--fg-80);cursor:pointer;
  display:inline-flex;align-items:center;justify-content:center;gap:8px;
  padding:8px 16px;font-family:var(--sans);font-size:12px;
  letter-spacing:.06em;text-transform:uppercase;
  transition:color 200ms ease,border-color 200ms ease,background-color 200ms ease;
}
button:hover,.btn:hover,.linkbtn:hover{
  color:var(--fg);border-color:var(--fg);background:rgba(28,28,28,.03);
}
button:active,.btn:active,.linkbtn:active{opacity:.85}
button:focus-visible,.btn:focus-visible,.linkbtn:focus-visible,select:focus-visible,input:focus-visible{
  outline:2px solid var(--fg);outline-offset:2px;
}
button.primary,.btn.primary,button.teal{
  background:var(--invert-bg);color:var(--invert-fg);border-color:var(--invert-bg);font-weight:500;
}
button.primary:hover,.btn.primary:hover,button.teal:hover{
  background:var(--fg);color:var(--invert-fg);border-color:var(--fg);opacity:.9;
}
button.ghost,.linkbtn{background:transparent;color:var(--fg-80);border-color:var(--fg-20)}
button.danger{color:var(--fg-60);border-color:var(--fg-20)}
button.danger:hover{background:var(--invert-bg);color:var(--invert-fg);border-color:var(--invert-bg)}
.field-grid{display:grid;grid-template-columns:1fr 1.4fr;gap:16px;margin:8px 0 16px}
@media (max-width:640px){.field-grid{grid-template-columns:1fr}}
label{
  display:block;font-size:11px;font-family:var(--sans);letter-spacing:.18em;
  text-transform:uppercase;color:var(--fg-40);margin-bottom:6px;
}
select,input{
  width:100%;padding:8px 0;border:none;border-bottom:1px solid var(--fg-20);
  background:transparent;color:var(--fg);font-family:var(--sans);font-size:14px;
  outline:none;transition:border-color 200ms ease;
}
select:focus,input:focus{border-bottom-color:var(--fg)}
.oauth-status{
  margin-top:16px;padding:12px 14px;border:1px solid var(--fg-10);background:transparent;
}
.oauth-status .title{
  font-size:10.5px;letter-spacing:.18em;text-transform:uppercase;color:var(--fg-40);margin-bottom:6px;
}
.oauth-status .msg{font-size:13px;line-height:1.5;color:var(--fg-80)}

.account{
  display:grid;gap:10px;padding:18px;border:1px solid var(--fg-10);
  background:transparent;transition:border-color 200ms ease;margin-bottom:12px;
}
.account:hover{border-color:var(--fg-40)}
.account-top{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;align-items:center}
.account-title{display:flex;flex-wrap:wrap;gap:8px;align-items:center}
.account-title strong{
  font-family:var(--display);font-size:16px;font-weight:400;color:var(--fg);letter-spacing:-.01em;
}
.badge{
  display:inline-flex;align-items:center;padding:2px 7px;border:1px solid var(--fg-20);
  font-size:10.5px;font-family:var(--sans);letter-spacing:.1em;text-transform:uppercase;
  color:var(--fg-60);background:transparent;
}
.badge.site{border-color:var(--fg-40);color:var(--fg)}
.badge.muted{border-color:var(--fg-10);color:var(--fg-40)}
.badge.on{border-color:var(--fg);color:var(--fg);font-weight:500}
.badge.off{border-color:var(--fg-20);color:var(--fg-40)}
.seg{display:inline-flex;gap:0;border:1px solid var(--fg-20)}
.seg button{
  border:none;border-right:1px solid var(--fg-20);background:transparent;color:var(--fg-60);
  padding:6px 14px;font-size:12px;letter-spacing:.08em;text-transform:uppercase;cursor:pointer;
}
.seg button:last-child{border-right:none}
.seg button.active{background:var(--invert-bg);color:var(--invert-fg);font-weight:500}
.seg-hint{margin:10px 0 16px;color:var(--fg-40);font-size:12px}
.meta{color:var(--fg-60);font-size:12.5px;line-height:1.5}
.meta code,.mono{font-family:var(--mono);font-size:12px}
.err{margin-top:4px;color:var(--fg);font-family:var(--mono);font-size:12px;border-left:2px solid var(--fg);padding-left:8px}
.chips{display:flex;flex-wrap:wrap;gap:8px}
.chip{
  padding:6px 12px;border:1px solid var(--fg-10);font-size:12px;font-family:var(--mono);
  color:var(--fg);background:transparent;transition:border-color 200ms ease,font-style 500ms ease;
}
.chip.btnish{cursor:pointer}
.chip.btnish:hover{border-color:var(--fg-40);font-style:italic}
.empty{
  padding:32px 16px;border:1px dashed var(--fg-20);text-align:center;color:var(--fg-40);
  font-family:var(--sans);font-size:13px;
}
details.raw{margin-top:12px;border:1px solid var(--fg-10);background:transparent}
details.raw summary{
  cursor:pointer;list-style:none;padding:10px 14px;font-size:11px;letter-spacing:.18em;
  text-transform:uppercase;color:var(--fg-40);font-family:var(--sans);transition:color 200ms ease;
}
details.raw summary:hover{color:var(--fg)}
details.raw summary::-webkit-details-marker{display:none}
pre{
  margin:0;padding:0 14px 14px;white-space:pre-wrap;word-break:break-word;
  font-family:var(--mono);font-size:11.5px;line-height:1.6;color:var(--fg-80);
  max-height:280px;overflow:auto;
}
.copyline{display:grid;grid-template-columns:1fr auto;gap:12px;align-items:center}
.copyline input{font-family:var(--mono);font-size:12.5px}
.secret-hint{margin-top:12px;font-size:12px;color:var(--fg-40);line-height:1.5}
.toast{
  position:fixed;right:24px;bottom:24px;z-index:100;min-width:180px;max-width:min(420px,92vw);
  padding:12px 20px;background:var(--invert-bg);color:var(--invert-fg);font-size:12px;
  letter-spacing:.08em;text-transform:uppercase;font-family:var(--sans);font-weight:500;
  border:1px solid var(--invert-bg);opacity:0;pointer-events:none;transition:opacity 300ms ease;
}
.toast.show{opacity:1}
.toast.err{background:var(--invert-bg);color:var(--invert-fg);border-color:var(--invert-bg)}
.usage-line{margin-top:4px;font-size:12px}
.usage-line .pill{display:inline-flex;padding:2px 8px;font-family:var(--mono);font-size:11px;border:1px solid var(--fg-20);color:var(--fg)}
.usage-line .pill.good{border-color:var(--fg);color:var(--fg);font-weight:500}
.usage-line .pill.warn{border-color:var(--fg-40);color:var(--fg-80)}
.usage-line .pill.bad{border-color:var(--fg-20);color:var(--fg-40)}
.account .actions button{padding:4px 10px;font-size:11px;color:var(--fg-60);border-color:var(--fg-10)}
.account .actions button:hover{color:var(--fg);border-color:var(--fg-40)}

/* 概览下方双栏注释卡片 */
.overview-notes{
  display:grid;grid-template-columns:1fr 1fr;gap:20px;margin-top:24px;
}
@media (max-width:800px){.overview-notes{grid-template-columns:1fr}}
.note-item{
  border-top:1px solid var(--fg-10);padding-top:14px;
}
.note-item h3{
  font-family:var(--display);font-size:16px;font-weight:400;margin-bottom:6px;
}
.note-item p{
  font-size:12.5px;color:var(--fg-60);line-height:1.6;margin:0;
}

#statusBox,#oauthBox,#modelsBox{display:none}
@media (prefers-reduced-motion: reduce){
  *,*::before,*::after{animation:none !important;transition:none !important}
}
</style>
</head>
<body>
<div class="shell">
  <header class="topbar">
    <div class="brand">
      <div class="mark" aria-hidden="true"></div>
      <strong>CodeBuddy</strong>
      <span>protocol_direct</span>
    </div>
    <div class="pillrow">
      <span class="pill"><span class="dot" id="healthDot"></span><span id="healthText">检查中</span></span>
      <span class="pill">transport · <span class="mono" id="pillTransport">—</span></span>
      <span class="pill">site · <span class="mono" id="pillSite">—</span></span>
    </div>
  </header>

  <!-- 章节目录切页导航 (Chapter Tabs) -->
  <nav class="tabs-nav" role="tablist">
    <button type="button" class="tab-link active" data-tab="tab-overview">
      <span class="tab-idx">01 /</span> 概览与监控
    </button>
    <button type="button" class="tab-link" data-tab="tab-pool">
      <span class="tab-idx">02 /</span> 账号池与授权
    </button>
    <button type="button" class="tab-link" data-tab="tab-client">
      <span class="tab-idx">03 /</span> 客户端接入
    </button>
    <button type="button" class="tab-link" data-tab="tab-models">
      <span class="tab-idx">04 /</span> 模型与快照
    </button>
  </nav>

  <!-- ========================================================
       TAB 01: 概览与监控 (专注运行态与统计，不堆叠表单)
       ======================================================== -->
  <div class="tab-panel active" id="tab-overview">
    <section class="panel">
      <div class="panel-inner">
        <div class="eyebrow">Gateway Console · Overview</div>
        <h1>账号、模型与网关状态</h1>
        <p class="lede">监控 OAuth 登录、账号池状态与请求健康度。</p>
        <div class="metrics">
          <div class="metric"><div class="k">登录态</div><div class="v sm" id="mLogin">未登录</div></div>
          <div class="metric"><div class="k">Credits 余额</div><div class="v sm" id="mCredits">—</div></div>
          <div class="metric"><div class="k">启用账号</div><div class="v" id="mEnabled">0</div></div>
          <div class="metric"><div class="k">成功 / 失败</div><div class="v sm" id="mSF">0 / 0</div></div>
          <div class="metric"><div class="k">总 Tokens</div><div class="v sm" id="mTokens">0</div></div>
        </div>
        <div class="actions" style="margin-top:24px">
          <button class="primary" id="btnRefresh" type="button">刷新状态</button>
          <button class="ghost" id="btnCheckin" type="button" disabled aria-describedby="checkinHint">每日签到</button>
          <button class="ghost" id="btnModels" type="button">拉取模型</button>
        </div>
        <p class="checkin-hint" id="checkinHint">一键为当前号池全部已启用账号签到（国内站约 100 积分/天）。</p>
        <pre class="checkin-detail" id="checkinRaw" hidden></pre>
      </div>
    </section>

    <div class="overview-notes">
      <div class="note-item">
        <h3>429 限频隔离与自动熔断</h3>
        <p>纯 429 / rate limit 短冷却 2 分钟并自动换号；配额类错误会查询官方余额并对齐至 CycleEnd / SlicePeriod 结束。111xx 策略码仍为 5 分钟，部分 5xx 为 30 秒。</p>
      </div>
      <div class="note-item">
        <h3>Reasoning 思考链透传</h3>
        <p>支持客户端 <code>reasoning_effort</code> / <code>reasoning</code> 入站映射；流式与非流式可回传 <code>reasoning_content</code>（视上游模型是否开启思考）。</p>
      </div>
    </div>
  </div>

  <!-- ========================================================
       TAB 02: 账号池与授权 (专注号池调度与 OAuth)
       ======================================================== -->
  <div class="tab-panel" id="tab-pool">
    <section class="panel">
      <div class="panel-inner">
        <div class="section-head">
          <div>
            <h2>账号池集群</h2>
            <p>一键切换国内 / 国际号池；请求只会使用当前区域账号。</p>
          </div>
          <div class="actions">
            <div class="seg" id="poolSiteSeg" role="group" aria-label="号池区域">
              <button type="button" data-site="domestic" id="btnPoolDomestic">国内</button>
              <button type="button" data-site="global" id="btnPoolGlobal">国际</button>
            </div>
          </div>
        </div>
        <p class="seg-hint" id="poolSiteHint">当前号池：—</p>
        <div id="accounts"></div>
      </div>
    </section>

    <section class="panel" id="codebuddy">
      <div class="panel-inner">
        <div class="section-head">
          <div>
            <h2>OAuth 快速授权</h2>
            <p>选择站点并开始认证，完成后账号自动归入池中。</p>
          </div>
        </div>
        <div class="field-grid">
          <div>
            <label for="site">站点</label>
            <select id="site">
              <option value="domestic">国内站 · domestic</option>
              <option value="global">国际站 · global</option>
            </select>
          </div>
          <div>
            <label for="label">账号标签</label>
            <input id="label" placeholder="例如：办公主力号" value="CodeBuddy OAuth"/>
          </div>
        </div>
        <div class="actions">
          <button class="primary" id="btnStart" type="button">开始认证</button>
          <button class="teal" id="btnPoll" type="button">检查登录</button>
          <a class="linkbtn" id="launchLink" href="#" target="_blank" rel="noreferrer">打开登录页</a>
        </div>
        <div class="oauth-status">
          <div class="title">会话状态</div>
          <div class="msg" id="oauthMsg">OAuth 会话空闲（账号池登录态见上方「登录态」；这里只反映进行中的认证流程）</div>
        </div>
        <details class="raw">
          <summary>原始 OAuth 响应</summary>
          <pre id="oauthRaw">idle</pre>
        </details>
      </div>
    </section>
  </div>

  <!-- ========================================================
       TAB 03: 客户端接入配置 (专注 OpenAI 端点与验证)
       ======================================================== -->
  <div class="tab-panel" id="tab-client">
    <section class="panel" id="client-config">
      <div class="panel-inner">
        <div class="section-head">
          <div>
            <h2>OpenAI 兼容接入</h2>
            <p>给下游客户端填 Base URL + API Key；不会暴露 CodeBuddy OAuth token。</p>
          </div>
          <div class="actions">
            <button class="ghost" id="btnRefreshClient" type="button">刷新接入信息</button>
            <button class="teal" id="btnGenerateKey" type="button">生成 API Key</button>
          </div>
        </div>
        <div class="field-grid" style="grid-template-columns:1fr 1fr">
          <div>
            <label for="openAiBaseUrl">Base URL</label>
            <div class="copyline">
              <input id="openAiBaseUrl" readonly placeholder="加载中…"/>
              <button class="ghost" id="copyBaseUrl" type="button">复制</button>
            </div>
          </div>
          <div>
            <label for="openAiChatUrl">Chat Completions</label>
            <div class="copyline">
              <input id="openAiChatUrl" readonly placeholder="加载中…"/>
              <button class="ghost" id="copyChatUrl" type="button">复制</button>
            </div>
          </div>
          <div>
            <label for="openAiApiKey">API Key（网关层）</label>
            <div class="copyline">
              <input id="openAiApiKey" class="secret-input" type="password" readonly placeholder="未配置"/>
              <button class="ghost" id="copyApiKey" type="button">复制</button>
            </div>
          </div>
          <div>
            <label for="openAiModel">推荐模型</label>
            <div class="copyline">
              <input id="openAiModel" readonly value="auto"/>
              <button class="ghost" id="copyModel" type="button">复制</button>
            </div>
          </div>
        </div>
        <div class="secret-hint" id="clientConfigHint">API Key 点击复制时才会读取明文。生成的新 Key 仅当前进程生效，重启需写入 CODEBUDDY_PROXY_API_KEY。</div>
      </div>
    </section>
  </div>

  <!-- ========================================================
       TAB 04: 模型与诊断 (专注模型芯片与底层 JSON)
       ======================================================== -->
  <div class="tab-panel" id="tab-models">
    <section class="panel">
      <div class="panel-inner">
        <div class="section-head">
          <div>
            <h2>可用模型编目</h2>
            <p>展示上游模型名（不含 codebuddy/ 前缀）；点击模型芯片可复制。</p>
          </div>
        </div>
        <div class="chips" id="modelChips"><div class="empty">点击「拉取模型」加载</div></div>
        <details class="raw">
          <summary>原始模型响应</summary>
          <pre id="modelsRaw">[]</pre>
        </details>
      </div>
    </section>

    <section class="panel">
      <div class="panel-inner">
        <div class="section-head">
          <div>
            <h2>运行快照诊断</h2>
            <p>完整 status JSON，便于排障。</p>
          </div>
        </div>
        <details class="raw" open>
          <summary>status payload</summary>
          <pre id="statusRaw">loading…</pre>
        </details>
      </div>
    </section>
  </div>
</div>

<div class="toast" id="toast" role="status" aria-live="polite"></div>

<!-- hidden compatibility targets for existing helpers -->
<pre id="statusBox"></pre>
<pre id="oauthBox"></pre>
<pre id="modelsBox"></pre>

<script>
const $ = (id) => document.getElementById(id);

/* 切页逻辑 (Tab Switcher) */
function switchTab(tabId){
  document.querySelectorAll('.tab-link').forEach(function(b){ b.classList.remove('active'); });
  document.querySelectorAll('.tab-panel').forEach(function(p){ p.classList.remove('active'); });
  var btn = document.querySelector('.tab-link[data-tab="' + tabId + '"]');
  if (btn) btn.classList.add('active');
  var p = $(tabId);
  if (p) p.classList.add('active');
}
document.querySelectorAll('.tab-link').forEach(function(btn){
  btn.addEventListener('click', function(){
    switchTab(btn.getAttribute('data-tab'));
  });
});
if (window.location.hash === '#codebuddy') {
  switchTab('tab-pool');
} else if (window.location.hash === '#client-config') {
  switchTab('tab-client');
}
window.addEventListener('hashchange', function(){
  if (window.location.hash === '#codebuddy') switchTab('tab-pool');
  if (window.location.hash === '#client-config') switchTab('tab-client');
});

async function api(path, opts={}) {
  const headers = Object.assign({'content-type':'application/json'}, opts.headers||{});
  const res = await fetch(path, Object.assign({}, opts, {headers: headers, credentials:'same-origin'}));
  const text = await res.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch (e) { data = {raw:text}; }
  if (!res.ok) throw new Error((data && data.error && data.error.message) || data.message || data.error || ('HTTP '+res.status));
  return data;
}

function escapeHtml(s){
  return String(s||'').replace(/[&<>"']/g, function(c){
    return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]);
  });
}

function fmtUptime(ms){
  ms = Number(ms||0);
  if (!ms || ms < 0) return '—';
  const s = Math.floor(ms/1000);
  const h = Math.floor(s/3600);
  const m = Math.floor((s%3600)/60);
  const r = s%60;
  if (h > 0) return h + 'h ' + m + 'm';
  if (m > 0) return m + 'm ' + r + 's';
  return r + 's';
}

function setHealth(ok, text){
  const dot = $('healthDot');
  dot.className = 'dot' + (ok ? '' : ' bad');
  $('healthText').textContent = text;
}

function formatResetAt(ms){
  if (!ms) return '-';
  try { return new Date(ms).toLocaleString(); } catch (e) { return String(ms); }
}

const usageByAccount = {};
function renderAccounts(summary, activeSite) {
  const box = $('accounts');
  const accounts = (summary && summary.accounts) || [];
  activeSite = normalizeSite(activeSite || (summary && summary.activeSite) || '');
  if (!accounts.length) {
    box.innerHTML = '<div class="empty">暂无账号。请先在下方完成 OAuth 登录。</div>';
    return;
  }
  box.innerHTML = accounts.map(function(a) {
    const name = escapeHtml(a.userNickname || a.userName || a.userId || '未命名用户');
    const label = escapeHtml(a.label || a.id);
    const logged = a.loggedIn && a.hasCredentials;
    const site = normalizeSite(a.site);
    const inPool = !activeSite || site === activeSite;
    let poolStateHtml = '';
    if (a.quotaExhausted && a.quotaResetAt) {
      poolStateHtml = '<div class="usage-line"><span class="pill bad">配额耗尽 · ' + escapeHtml(formatResetAt(a.quotaResetAt)) + ' 恢复</span></div>';
    } else if (a.cooldownUntil && a.cooldownUntil > Date.now()) {
      poolStateHtml = '<div class="usage-line"><span class="pill warn">冷却中 · ' + escapeHtml(formatResetAt(a.cooldownUntil)) + ' 恢复</span></div>';
    } else if (a.quotaRemaining != null && Number(a.quotaRemaining) <= 0) {
      poolStateHtml = '<div class="usage-line"><span class="pill warn">配额可能已耗尽</span></div>';
    }
    const usage = usageByAccount[a.id];
    let usageHtml = '<div class="usage-line"><button class="ghost" data-act="usage" data-id="' + escapeHtml(a.id) + '" type="button">查余额</button></div>';
    if (usage && usage.error) {
      usageHtml = '<div class="usage-line"><span class="pill bad" title="' + escapeHtml(usage.error) + '">查询失败</span> ' +
        '<button class="ghost" data-act="usage" data-id="' + escapeHtml(a.id) + '" type="button">重试</button></div>';
    } else if (usage && usage.credits) {
      const c = usage.credits;
      const remaining = c.unlimited ? '不限量' : (c.remaining == null ? '-' : String(c.remaining));
      const total = c.unlimited ? '不限量' : (c.total == null ? '-' : String(c.total));
      let level = 'good';
      if (!c.unlimited && Number(c.total) > 0) {
        const ratio = Number(c.remaining) / Number(c.total);
        if (ratio <= 0.05 || Number(c.remaining) <= 0) level = 'bad';
        else if (ratio <= 0.2) level = 'warn';
      }
      if (usage.notify && usage.notify.level === 'bad') level = 'bad';
      else if (usage.notify && usage.notify.level === 'warn' && level === 'good') level = 'warn';
      usageHtml = '<div class="usage-line"><span class="pill ' + level + '">' + escapeHtml(remaining + ' / ' + total) + ' Credits</span> ' +
        '<button class="ghost" data-act="usage" data-id="' + escapeHtml(a.id) + '" type="button">刷新余额</button> ' +
        '<a href="' + escapeHtml(usage.officialUsageUrl || 'https://www.codebuddy.cn/profile/plan') + '" target="_blank" rel="noreferrer">官网套餐</a></div>';
    }
    return '<div class="account">' +
      '<div class="account-top">' +
        '<div class="account-title">' +
          '<strong>' + label + '</strong>' +
          '<span class="badge site">' + escapeHtml(siteLabel(site)) + '</span>' +
          '<span class="badge ' + (inPool?'on':'muted') + '">' + (inPool?'当前号池':'其他区域') + '</span>' +
          '<span class="badge ' + (logged?'on':'off') + '">' + (logged?'已登录':'未登录') + '</span>' +
          '<span class="badge ' + (a.enabled?'on':'off') + '">' + (a.enabled?'enabled':'disabled') + '</span>' +
        '</div>' +
        '<div class="actions">' +
          '<button class="ghost" data-act="usage" data-id="' + escapeHtml(a.id) + '" type="button">查余额</button>' +
          '<button class="ghost" data-act="toggle" data-id="' + escapeHtml(a.id) + '" data-enabled="' + (a.enabled?0:1) + '" type="button">' + (a.enabled?'禁用':'启用') + '</button>' +
          '<button class="ghost" data-act="refresh" data-id="' + escapeHtml(a.id) + '" type="button">刷新 Token</button>' +
          '<button class="danger" data-act="delete" data-id="' + escapeHtml(a.id) + '" type="button">删除</button>' +
        '</div>' +
      '</div>' +
      '<div class="meta">' + name +
        ' · <span class="mono">' + escapeHtml(a.authType||'') + '</span>' +
        ' · success ' + (a.successRequests||0) + ' / fail ' + (a.failedRequests||0) +
        (a.tokenExpired ? ' · <span class="err">token expired</span>' : '') +
      '</div>' +
      usageHtml +
      poolStateHtml +
      (a.lastError ? ('<div class="err">' + escapeHtml(a.lastError) + '</div>') : '') +
    '</div>';
  }).join('');
  box.querySelectorAll('button[data-act]').forEach(function(btn){ btn.addEventListener('click', onAccountAction); });
}

function bareModelId(raw){
  const id = String(raw||'').trim();
  if (!id) return 'auto';
  return id.replace(/^codebuddy[/:]/i, '') || 'auto';
}
function renderModels(data){
  const chips = $('modelChips');
  const list = Array.isArray(data) ? data
    : (Array.isArray(data && data.models) ? data.models
    : (Array.isArray(data && data.data) ? data.data : []));
  if (!list.length) {
    chips.innerHTML = '<div class="empty">暂无模型数据</div>';
    return;
  }
  chips.innerHTML = list.map(function(m){
    const raw = typeof m === 'string' ? m : (m.modelId || m.upstreamId || m.id || m.name || m.model || 'model');
    const id = bareModelId(raw);
    const baseLabel = typeof m === 'string' ? id : bareModelId(m.displayName || m.name || id);
    const credits = (typeof m === 'object' && m && m.credits) ? String(m.credits) : '';
    const free = typeof m === 'object' && m && (m.free === true || /x0(\.0+)?\s*credits/i.test(credits));
    const mult = (typeof m === 'object' && m && m.creditMultiplier != null) ? m.creditMultiplier : null;
    const badge = credits ? (' · ' + (free ? '免费' : credits.replace(/\s*credits$/i,''))) : '';
    const tip = [id, credits ? ('倍率 ' + credits) : '', (m && m.description) ? m.description : ''].filter(Boolean).join(' | ');
    return '<button type="button" class="chip btnish" data-copy="' + escapeHtml(id) + '" title="' + escapeHtml(tip || '点击复制') + '">' + escapeHtml(baseLabel + badge) + (free ? ' <span class="badge on">免费</span>' : (mult!=null && credits ? ' <span class="badge site">' + escapeHtml(String(mult)+'x') + '</span>' : '')) + '</button>';
  }).join('');
  chips.querySelectorAll('[data-copy]').forEach(function(btn){
    btn.addEventListener('click', function(){ copyText(btn.getAttribute('data-copy'), '模型', btn); });
  });
}

function normalizeSite(site){
  site = String(site||'').toLowerCase().trim();
  if (site === 'domestic' || site === 'cn' || site === 'china' || site === 'internal') return 'domestic';
  return 'global';
}
function siteLabel(site){
  return normalizeSite(site) === 'domestic' ? '国内' : '国际';
}
// 签到按钮跟随当前激活号池；paintPoolSite 每次刷新状态时同步。
// 空字符串表示尚未从 /status 拿到号池，按钮保持 disabled，避免首屏误打国际站。
let activeCheckinSite = '';
let checkinBusy = false;

function paintPoolSite(site, accounts){
  site = normalizeSite(site);
  activeCheckinSite = site;
  const checkinBtn = $('btnCheckin');
  if (checkinBtn && !checkinBusy) checkinBtn.disabled = false;
  const domesticBtn = $('btnPoolDomestic');
  const globalBtn = $('btnPoolGlobal');
  if (domesticBtn) domesticBtn.className = site === 'domestic' ? 'active' : '';
  if (globalBtn) globalBtn.className = site === 'global' ? 'active' : '';
  const domestic = accounts && accounts.domesticCount != null ? accounts.domesticCount : '—';
  const global = accounts && accounts.globalCount != null ? accounts.globalCount : '—';
  const activeEnabled = accounts && accounts.activeEnabledCount != null ? accounts.activeEnabledCount : '—';
  if ($('poolSiteHint')) {
    $('poolSiteHint').textContent = '当前号池：' + siteLabel(site) + ' · 可用启用账号 ' + activeEnabled + ' · 国内账号 ' + domestic + ' / 国际账号 ' + global + '（仅当前区域会参与请求）';
  }
  // 切号池 / 刷新状态时清掉上一次的签明细，避免与新号池并存造成误导。
  // 签到进行中不清理：runPoolCheckin 会在 refreshStatus 之后重新写入。
  const checkinRaw = $('checkinRaw');
  if (checkinRaw && !checkinBusy) {
    checkinRaw.hidden = true;
    checkinRaw.textContent = '';
  }
  if ($('checkinHint')) {
    $('checkinHint').textContent = activeCheckinSite === 'global'
      ? '一键为国际号池全部已启用账号签到；国际站通常无签到活动，会提示不支持。账号多时整体耗时约 20s × 账号数（上限 5 分钟）。'
      : '一键为国内号池全部已启用账号签到（约 100 积分/天，连续第 7 天可达 1000）；账号多时整体耗时约 20s × 账号数（上限 5 分钟）。';
  }
}

async function switchPoolSite(site){
  site = normalizeSite(site);
  const data = await api('/direct-admin/api/pool-site', {method:'POST', body: JSON.stringify({site: site})});
  paintStatus(data);
  if (data.note) showToast(data.note);
  refreshModels().catch(function(){});
  return data;
}

function paintStatus(data){
  const stats = data.stats || {};
  const accounts = data.accounts || {};
  const cfg = data.config || {};
  const enabledCount = accounts.enabledCount != null ? accounts.enabledCount : ((accounts.accounts||[]).filter(function(a){return a.enabled;}).length);
  $('mEnabled').textContent = String(enabledCount);
  $('mSF').textContent = (stats.successRequests||0) + ' / ' + (stats.failedRequests||0);
  const totalTokens = stats.totalTokens || 0;
  const cachedTokens = stats.totalCachedTokens || 0;
  $('mTokens').textContent = cachedTokens > 0
    ? (totalTokens + ' · cache ' + cachedTokens)
    : String(totalTokens);
  const loggedIn = !!accounts.loggedIn;
  const primary = accounts.primary || ((accounts.accounts||[])[0] || null);
  $('mLogin').textContent = loggedIn
    ? ('已登录' + (primary && (primary.userNickname || primary.userName || primary.userId) ? (' · ' + (primary.userNickname || primary.userName || primary.userId)) : ''))
    : '未登录';
  $('pillTransport').textContent = data.transport || 'protocol_direct';
  const poolSite = normalizeSite(data.poolSite || cfg.poolSite || cfg.site || 'global');
  $('pillSite').textContent = siteLabel(poolSite);
  paintPoolSite(poolSite, accounts);
  if ($('site') && !$('site').dataset.userTouched) {
    $('site').value = poolSite === 'domestic' ? 'domestic' : 'global';
  }
  const activeEnabled = accounts.activeEnabledCount != null ? accounts.activeEnabledCount : enabledCount;
  $('mEnabled').textContent = String(activeEnabled);
  setHealth(!!data.ok, data.ok ? (loggedIn ? ('服务正常 · ' + siteLabel(poolSite) + '号池') : ('服务正常 · ' + siteLabel(poolSite) + '号池未登录')) : '状态异常');
  if (primary && primary.id && primary.hasCredentials && !usageByAccount[primary.id] && !paintStatus._usageKick) {
    paintStatus._usageKick = true;
    fetchAccountUsage(primary.id, true).catch(function(){});
  }
  const usage = primary && usageByAccount[primary.id];
  if (usage && usage.credits) {
    const c = usage.credits;
    $('mCredits').textContent = c.unlimited ? '不限量' : ((c.remaining == null ? '-' : c.remaining) + ' / ' + (c.total == null ? '-' : c.total));
  } else if (usage && usage.error) {
    $('mCredits').textContent = '查询失败';
  } else {
    $('mCredits').textContent = loggedIn ? '点击查余额' : '—';
  }
  const raw = JSON.stringify(data, null, 2);
  $('statusBox').textContent = raw;
  $('statusRaw').textContent = raw;
  renderAccounts(accounts, poolSite);
}

function paintOAuth(data){
  const session = (data && data.session) || {};
  const login = (data && data.login) || {};
  const status = session.status || login.status || (data && data.ok ? 'ok' : 'idle');
  const err = session.error || login.message || '';
  let msg = '状态：' + status;
  if (session.label) msg += ' · ' + session.label;
  if (session.site) msg += ' · ' + session.site;
  if (err) msg += ' · ' + err;
  $('oauthMsg').textContent = msg;
  const raw = JSON.stringify(data, null, 2);
  $('oauthBox').textContent = raw;
  $('oauthRaw').textContent = raw;
}

let clientConfig = { apiKey: '', baseUrl: '', chatCompletionsUrl: '', recommendedModel: 'auto' };
function showToast(msg, kind){
  const el = $('toast');
  el.textContent = msg;
  el.className = 'toast show' + (kind === 'error' ? ' err' : '');
  clearTimeout(showToast._t);
  showToast._t = setTimeout(function(){ el.className = 'toast'; }, 2200);
}
async function copyText(text, label, button){
  const value = String(text||'');
  if (!value) { showToast((label||'内容') + '为空', 'error'); return; }
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(value);
    } else {
      const ta = document.createElement('textarea');
      ta.value = value; document.body.appendChild(ta); ta.select();
      document.execCommand('copy'); ta.remove();
    }
    const old = button ? button.textContent : '';
    if (button) button.textContent = '已复制';
    showToast((label||'内容') + ' 已复制');
    if (button) setTimeout(function(){ button.textContent = old; }, 1200);
  } catch (e) {
    showToast('复制失败：' + (e && e.message ? e.message : e), 'error');
  }
}
function paintClientConfig(cfg){
  clientConfig = cfg || clientConfig;
  const base = cfg.baseUrl || cfg.apiBase || '';
  const chat = cfg.chatCompletionsUrl || (base ? (base.replace(/\/$/,'') + '/chat/completions') : '');
  const model = bareModelId(cfg.recommendedModel || 'auto');
  $('openAiBaseUrl').value = base;
  $('openAiChatUrl').value = chat;
  $('openAiModel').value = model;
  const configured = !!cfg.apiKeyConfigured || !!cfg.apiKey;
  $('openAiApiKey').value = configured ? (cfg.apiKeyPreview || '已配置 · 点击复制') : '';
  $('openAiApiKey').placeholder = configured ? '' : 'API Key 未配置';
  $('copyApiKey').disabled = !configured;
  if (cfg.note) $('clientConfigHint').textContent = cfg.note;
  clientConfig.apiKey = cfg.apiKey || '';
  clientConfig.baseUrl = base;
  clientConfig.chatCompletionsUrl = chat;
  clientConfig.recommendedModel = model;
}
async function refreshClientConfig(){
  const data = await api('/direct-admin/api/client-config');
  paintClientConfig(data);
  return data;
}
async function generateApiKey(){
  if (!confirm('生成新的网关 API Key？\n\n旧 Key 会立即失效，新 Key 会写入 .env 并在重启后继续生效。\n请同步更新 ZCode / NewAPI 等客户端配置。')) return;
  const data = await api('/direct-admin/api/client-config/generate-key', {method:'POST', body:'{}'});
  paintClientConfig(data);
  if (data.apiKey) await copyText(data.apiKey, '新 API Key', $('btnGenerateKey'));
  if (data.note) showToast(data.note);
}

async function refreshStatus(){
  const data = await api('/direct-admin/api/status');
  paintStatus(data);
}

function formatCheckinSummary(data){
  const s = data.summary || {};
  const parts = [];
  if (s.checkedIn > 0) parts.push('新签 ' + s.checkedIn);
  if (s.alreadyDone > 0) parts.push('已签 ' + s.alreadyDone);
  if (s.skipped > 0) parts.push('跳过 ' + s.skipped);
  if (s.unsupported > 0) parts.push('不支持 ' + s.unsupported);
  if (s.failed > 0) parts.push('失败 ' + s.failed);
  if (!parts.length) return data.note || '当前号池没有可签到的账号';
  return parts.join(' · ');
}

async function runPoolCheckin(){
  if (!activeCheckinSite) {
    showToast('号池状态尚未加载，请稍后再试', 'error');
    return;
  }
  const btn = $('btnCheckin');
  checkinBusy = true;
  if (btn) { btn.disabled = true; btn.textContent = '签到中…'; }
  try {
    const data = await api('/direct-admin/api/codebuddy/checkin', {method:'POST', body: JSON.stringify({site: activeCheckinSite})});
    const summary = formatCheckinSummary(data);
    showToast(summary, data.ok ? undefined : 'error');
    const results = data.results || [];
    for (const item of results) {
      if (item.ok && item.accountId && !item.alreadyCheckedIn) {
        await fetchAccountUsage(item.accountId, true);
      }
    }
    await refreshStatus();
    if (results.length && $('checkinRaw')) {
      const lines = results.map(function(item){
        const name = item.label || item.accountId || '账号';
        if (!item.supported) return name + '：' + (item.message || '不支持');
        if (item.ok && item.alreadyCheckedIn) return name + '：今日已签';
        if (item.ok) return name + '：' + (item.message || ('+' + (item.rewardCredits || 0) + ' 积分'));
        return name + '：' + (item.message || '失败');
      });
      $('checkinRaw').textContent = lines.join('\n');
      $('checkinRaw').hidden = false;
    }
  } catch (e) {
    showToast('签到失败：' + (e.message || e), 'error');
  } finally {
    checkinBusy = false;
    if (btn) { btn.disabled = false; btn.textContent = '每日签到'; }
  }
}

async function refreshModels(){
  const data = await api('/direct-admin/api/codebuddy/models?fresh=1');
  const raw = JSON.stringify(data, null, 2);
  $('modelsBox').textContent = raw;
  $('modelsRaw').textContent = raw;
  renderModels(data);
}

async function startOAuth(){
  $('oauthMsg').textContent = '正在创建登录会话…';
  const data = await api('/direct-admin/api/codebuddy/oauth/start', {
    method:'POST',
    body: JSON.stringify({site:$('site').value, label:$('label').value, reuseExisting:false})
  });
  paintOAuth(data);
  const url = (data.login && data.login.url) || (data.session && data.session.url) || '#';
  const launch = (data.login && data.login.launchUrl) || (data.session && data.session.launchUrl) || url;
  $('launchLink').href = launch;
  if (url && url !== '#') window.open(url, '_blank', 'noopener');
}

async function pollOAuth(){
  $('oauthMsg').textContent = '正在检查登录…';
  const data = await api('/direct-admin/api/codebuddy/oauth/poll', {method:'POST', body:'{}'});
  paintOAuth(data);
  await refreshStatus();
}

async function fetchAccountUsage(accountId, silent){
  try {
    const result = await api('/direct-admin/api/codebuddy/accounts/'+encodeURIComponent(accountId)+'/usage');
    usageByAccount[accountId] = result;
    if (!silent) {
      const credits = result.credits || {};
      showToast(credits.display || credits.label || '余额已更新');
    }
  } catch (e) {
    usageByAccount[accountId] = { error: e.message || String(e) };
    if (!silent) showToast('查余额失败：' + (e.message || e), 'error');
  }
  await refreshStatus();
}
async function onAccountAction(ev){
  const btn = ev.currentTarget;
  const id = btn.getAttribute('data-id');
  const act = btn.getAttribute('data-act');
  if (act === 'usage') {
    await fetchAccountUsage(id, false);
    return;
  }
  if (act === 'delete') {
    if (!confirm('确认删除该账号？')) return;
    await api('/direct-admin/api/codebuddy/accounts/'+encodeURIComponent(id), {method:'DELETE'});
    delete usageByAccount[id];
  } else if (act === 'toggle') {
    const enabled = btn.getAttribute('data-enabled') === '1';
    await api('/direct-admin/api/codebuddy/accounts/'+encodeURIComponent(id)+'/'+(enabled?'enable':'disable'), {method:'POST', body:'{}'});
  } else if (act === 'refresh') {
    await api('/direct-admin/api/codebuddy/accounts/'+encodeURIComponent(id)+'/refresh-token', {method:'POST', body:'{}'});
  }
  await refreshStatus();
}

if ($('btnPoolDomestic')) $('btnPoolDomestic').onclick = function(){ switchPoolSite('domestic').catch(function(e){ showToast(e.message, 'error'); }); };
if ($('btnPoolGlobal')) $('btnPoolGlobal').onclick = function(){ switchPoolSite('global').catch(function(e){ showToast(e.message, 'error'); }); };
if ($('site')) $('site').addEventListener('change', function(){ $('site').dataset.userTouched = '1'; });
$('btnRefresh').onclick = function(){ refreshStatus().catch(function(e){ $('statusRaw').textContent = e.message; setHealth(false, '刷新失败'); }); };
if ($('btnCheckin')) $('btnCheckin').onclick = function(){ runPoolCheckin().catch(function(e){ showToast(e.message, 'error'); }); };
$('btnModels').onclick = function(){ refreshModels().catch(function(e){ $('modelsRaw').textContent = e.message; $('modelChips').innerHTML = '<div class="empty">' + escapeHtml(e.message) + '</div>'; }); };
$('btnStart').onclick = function(){ startOAuth().catch(function(e){ $('oauthMsg').textContent = e.message; $('oauthRaw').textContent = e.message; }); };
$('btnPoll').onclick = function(){ pollOAuth().catch(function(e){ $('oauthMsg').textContent = e.message; $('oauthRaw').textContent = e.message; }); };
$('btnRefreshClient').onclick = function(){ refreshClientConfig().catch(function(e){ showToast(e.message, 'error'); }); };
$('btnGenerateKey').onclick = function(){ generateApiKey().catch(function(e){ showToast(e.message, 'error'); }); };
$('copyBaseUrl').onclick = function(){ copyText($('openAiBaseUrl').value, 'Base URL', $('copyBaseUrl')); };
$('copyChatUrl').onclick = function(){ copyText($('openAiChatUrl').value, 'Chat Completions', $('copyChatUrl')); };
$('copyModel').onclick = function(){ copyText($('openAiModel').value, '模型', $('copyModel')); };
$('copyApiKey').onclick = function(){
  refreshClientConfig().then(function(cfg){
    if (!cfg.apiKey) { showToast('API Key 未配置', 'error'); return; }
    $('openAiApiKey').value = cfg.apiKeyPreview || '已配置 · 点击复制';
    return copyText(cfg.apiKey, 'API Key', $('copyApiKey'));
  }).catch(function(e){ showToast(e.message, 'error'); });
};
refreshStatus().catch(function(e){ $('statusRaw').textContent = e.message; setHealth(false, '无法连接'); });
refreshClientConfig().catch(function(e){ showToast(e.message, 'error'); });
refreshModels().catch(function(){});
setInterval(function(){ refreshStatus().catch(function(){}); }, 15000);
</script>
</body>
</html>`
}

func LaunchPage(message string, success bool) string {
	stateLabel := "认证未完成"
	if success {
		stateLabel = "认证成功"
	}
	return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>CodeBuddy OAuth</title>
<style>
:root{
  --bg:#F9F8F6;
  --fg:#1C1C1C;
  --fg-60:rgba(28,28,28,0.60);
  --fg-20:rgba(28,28,28,0.20);
  --display:Georgia,"Iowan Old Style","Palatino Linotype",Palatino,"Songti SC","Noto Serif SC",serif;
  --sans:system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;
}
*{box-sizing:border-box}
body{
  margin:0;min-height:100dvh;display:grid;place-items:center;padding:24px;
  font-family:var(--sans);font-size:14px;line-height:1.625;color:var(--fg);
  background:var(--bg);
}
.card{
  width:min(520px,100%);padding:32px;
  background:transparent;border:1px solid var(--fg-20);
}
.meta{
  font-size:11px;letter-spacing:.2em;text-transform:uppercase;color:var(--fg-60);margin-bottom:12px;
}
h1{
  margin:0 0 12px;font-family:var(--display);font-size:26px;letter-spacing:-.02em;font-weight:400;color:var(--fg);
}
p{
  margin:0 0 24px;color:var(--fg-60);font-size:14px;line-height:1.6;
}
a{
  display:inline-flex;align-items:center;justify-content:center;
  padding:10px 20px;text-decoration:none;font-size:12px;letter-spacing:.08em;text-transform:uppercase;
  color:var(--bg);background:var(--fg);border:1px solid var(--fg);
  transition:background .2s, color .2s;
}
a:hover{background:#333;color:var(--bg)}
a:focus-visible{outline:2px solid var(--fg);outline-offset:2px}
</style>
</head>
<body>
<div class="card">
  <div class="meta">CodeBuddy Proxy · OAuth</div>
  <h1>` + html.EscapeString(stateLabel) + `</h1>
  <p>` + html.EscapeString(message) + `</p>
  <a href="/direct-admin/#codebuddy">返回管理台</a>
</div>
</body>
</html>`
}

func Compact(value string) string { return strings.TrimSpace(value) }
