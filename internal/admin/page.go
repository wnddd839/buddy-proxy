package admin

import (
	"html"
	"strings"
)

// UIRevision is the admin console markup/script stamp. Bump it on every
// meaningful change to PageHTML so operators can tell the rebuilt binary
// carries the new UI (independent of the release tag injected via ldflags).
const UIRevision = "2026.10.20-attention-account-menu+egress"

func PageHTML() string {
	return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<meta name="cbp-ui-revision" content="` + UIRevision + `"/>
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
  display:flex;align-items:center;gap:0;flex-wrap:nowrap;flex-shrink:0;
  font-family:var(--sans);font-size:11px;letter-spacing:.15em;text-transform:uppercase;
  color:var(--fg-60);
}
.pill{display:inline-flex;align-items:center;gap:6px;flex:0 0 auto;white-space:nowrap}
.pill + .pill::before{content:"·";margin:0 10px;color:var(--fg-40)}
/* 更新控件单独一行，避免顶栏 pillrow 溢出 */
.updatebar{
  display:flex;align-items:center;gap:10px;flex-wrap:wrap;
  margin:0 0 16px;font-family:var(--sans);font-size:11px;letter-spacing:.15em;
  text-transform:uppercase;color:var(--fg-60);
}
.updatebar button{padding:6px 12px;font-size:11px}
/* 状态文案保持原文大小写并允许换行 */
#updateMessage{text-transform:none;letter-spacing:0;white-space:normal;overflow-wrap:anywhere}
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
/* 弹窗（出口设置） */
.modal{position:fixed;inset:0;z-index:80;display:flex;align-items:center;justify-content:center;padding:20px;background:rgba(0,0,0,.6)}
.modal[hidden]{display:none}
.modal-card{
  width:min(680px,100%);max-height:86vh;overflow:auto;padding:22px 24px 18px;
  border:1px solid var(--fg-20);background:var(--bg);box-shadow:0 24px 60px rgba(0,0,0,.45);
}
.modal-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}
.modal-head h3{margin:0;font-size:15px;font-weight:600;letter-spacing:.02em}
.modal-x{border:none;background:transparent;color:var(--fg-40);font-size:22px;line-height:1;cursor:pointer;padding:0 2px}
.modal-x:hover{color:var(--fg)}
.modal-row{display:flex;align-items:center;gap:10px}
.modal-row label{margin:0;font-size:10.5px;letter-spacing:.16em;text-transform:uppercase;color:var(--fg-40)}
.modal-row select{width:auto;min-width:170px;font-size:13px;padding:6px 0}
.modal-actions{display:flex;justify-content:flex-end;gap:10px;margin-top:16px}
/* 账号出口面板 */
.egress-panel{margin:16px 0 8px;padding:12px 14px;border:1px solid var(--fg-10);border-radius:10px;background:rgba(28,28,28,.015)}
.egress-head{display:flex;align-items:baseline;justify-content:space-between;gap:12px;flex-wrap:wrap}
.egress-head strong{font-size:13px;font-weight:600}
.egress-hint{font-size:11px;color:var(--fg-40)}
.egress-list{margin-top:8px}
.egress-row{display:flex;align-items:center;gap:12px;padding:8px 0;border-top:1px solid var(--fg-10)}
.egress-row:first-child{border-top:none}
.egress-id{flex:1;min-width:0}
.egress-id strong{display:block;font-size:12.5px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.egress-id span{display:block;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:11px;color:var(--fg-40);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.egress-count{font-size:11px;color:var(--fg-60);white-space:nowrap}
.egress-add{display:flex;gap:8px;margin-top:10px;flex-wrap:wrap}
.egress-add input{flex:1;min-width:180px}
.test-model-label{display:inline-flex;align-items:center;gap:8px;font-size:12px;color:var(--fg-60);white-space:nowrap}
.test-model-label select{min-width:160px;font-size:13px}
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
/* 从 OAuth 回调带 #codebuddy 进来时，滚动目标不要被吸顶栏盖住 */
#codebuddy{scroll-margin-top:88px}
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
.bound-keys{margin-top:18px;display:flex;flex-direction:column;gap:0}
.bound-key{
  display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;
  padding:12px 0;border-top:1px solid var(--fg-10);
}
.bound-key .meta{display:flex;flex-wrap:wrap;gap:8px;align-items:center}
.bound-keys-new{margin-top:16px}
.bound-keys-new label{
  display:block;font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--fg-40);margin-bottom:8px;
}
.bound-keys-new select{
  width:100%;padding:8px 10px;border:1px solid var(--fg-20);background:transparent;font:inherit;
}
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
.usage-filters{display:flex;flex-wrap:wrap;gap:16px 24px;margin:16px 0 0;align-items:end}
.usage-filters label{display:flex;flex-direction:column;gap:6px;font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--fg-40)}
.usage-filters select{min-width:180px;font-size:13px;text-transform:none;letter-spacing:0}
.usage-models{margin-top:16px;overflow:auto;border:1px solid var(--fg-10)}
.usage-models table{width:100%;border-collapse:collapse;font-size:12px}
.usage-models th,.usage-models td{padding:8px 12px;border-bottom:1px solid var(--fg-10);text-align:left}
.usage-models th{font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--fg-40);font-weight:500}
.usage-models td.mono{font-family:var(--mono);font-size:11px}
.usage-models .low{color:var(--fg-40)}

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

.log-table-wrap{margin-top:16px;border:1px solid var(--fg-10)}
.log-table-window{max-height:min(340px,42vh);overflow:auto}
.log-table{width:100%;border-collapse:collapse;font-size:12px;min-width:880px}
.log-pager{
  display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;
  padding:10px 12px;border-top:1px solid var(--fg-10);font-size:12px;color:var(--fg-60);
}
.log-pager .actions button{padding:6px 12px;font-size:11px}
.log-table th,.log-table td{padding:10px 12px;border-bottom:1px solid var(--fg-10);text-align:left;vertical-align:top}
.log-table th{
  font-size:10px;letter-spacing:.15em;text-transform:uppercase;color:var(--fg-40);font-weight:500;
  position:sticky;top:0;background:var(--bg);
}
.log-table td.mono{font-family:var(--mono);font-size:11px}
.log-table tr:hover td{background:rgba(28,28,28,.02)}
.log-table .idbtn{
  border:none;background:transparent;padding:0;font-family:var(--mono);font-size:11px;
  color:var(--fg);cursor:pointer;text-decoration:underline;text-underline-offset:3px;
}
.log-table .idbtn:hover{font-style:italic}

.usage-chart{
  margin-top:20px;padding:16px 12px 8px;border:1px solid var(--fg-10);
}
.usage-chart-head{
  display:flex;align-items:baseline;justify-content:space-between;gap:12px;flex-wrap:wrap;
  margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid var(--fg-10);
}
.usage-chart-head h3{
  margin:0;font-family:var(--display);font-size:16px;font-weight:400;color:var(--fg);
}
.usage-chart-legend{
  display:flex;gap:16px;font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--fg-60);
}
.usage-chart-legend span{display:inline-flex;align-items:center;gap:6px}
.usage-chart-legend .swatch{width:18px;height:0;border-top:2px solid var(--fg)}
.usage-chart-legend .swatch.dash{border-top-style:dashed}
.chart-empty{padding:32px 12px;text-align:center;color:var(--fg-40);font-size:13px}
.usage-chart svg{display:block;width:100%;height:auto;max-height:200px}
.usage-chart .axis{stroke:var(--fg-10);stroke-width:1}
.usage-chart .line-tokens{fill:none;stroke:var(--fg);stroke-width:1.5}
.usage-chart .line-rate{fill:none;stroke:var(--fg-60);stroke-width:1.5;stroke-dasharray:5 4}
.usage-chart .lbl{fill:var(--fg-40);font-family:var(--sans);font-size:10px}

/* 号池批量操作行 */
.ops-row{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 4px}
/* 概览「需要关注」 */
.attention{border-top:1px solid var(--fg-10);margin-top:4px}
.attention-row{
  display:flex;align-items:center;justify-content:space-between;gap:8px 16px;flex-wrap:wrap;
  padding:12px 0;border-bottom:1px solid var(--fg-10);
}
.attention-row .who{font-size:13px;color:var(--fg);min-width:0;overflow-wrap:anywhere}
.attention-row .who .meta{color:var(--fg-60);font-size:12.5px}
.reasons{display:flex;gap:6px;flex-wrap:wrap}
.badge.warn{border-color:var(--fg-40);color:var(--fg)}
.badge.bad{background:var(--invert-bg);border-color:var(--invert-bg);color:var(--invert-fg)}
/* 账号卡的次要操作收纳 */
.more{position:relative;display:inline-flex}
.more summary{
  list-style:none;cursor:pointer;border:1px solid var(--fg-10);padding:4px 10px;
  font-size:11px;color:var(--fg-60);letter-spacing:.08em;text-transform:uppercase;
}
.more summary::-webkit-details-marker{display:none}
.more summary:hover,.more[open] summary{color:var(--fg);border-color:var(--fg-40)}
.more-menu{
  position:absolute;right:0;top:calc(100% + 4px);z-index:20;min-width:148px;
  background:var(--bg);border:1px solid var(--fg-20);display:flex;flex-direction:column;
}
.more-menu button{border:none;padding:8px 12px;justify-content:flex-start;font-size:11px;color:var(--fg-60)}
/* 账号卡片的 .actions button 优先级更高，这里显式覆盖，避免菜单项带上按钮边框和内边距 */
.account .actions .more-menu button{border:none;padding:8px 12px;color:var(--fg-60)}
.account .actions .more-menu button:hover{border:none;background:rgba(28,28,28,.03);color:var(--fg)}
.more-menu button + button{border-top:1px solid var(--fg-10)}
.more-menu button:hover{background:rgba(28,28,28,.03);color:var(--fg);border-color:transparent}
.more-menu button.danger:hover{background:var(--invert-bg);color:var(--invert-fg)}

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
      <span class="pill">product · <span class="mono" id="pillProduct">—</span></span>
      <span class="pill">version · <span class="mono" id="pillVersion">—</span></span>
    </div>
  </header>

  <!-- 手动更新入口：仅点击时访问 GitHub，不自动检查 -->
  <div class="updatebar">
    <button type="button" id="updateCheck">检查更新</button>
    <button type="button" id="updateInstall" hidden>更新并重启</button>
    <button type="button" id="updateCancel" hidden>取消下载</button>
    <span id="updateMessage" role="status" aria-live="polite"></span>
  </div>

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
    <button type="button" class="tab-link" data-tab="tab-usage">
      <span class="tab-idx">05 /</span> 用量与明细
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
        </div>

        <div class="section-head" style="margin-top:28px">
          <div>
            <h2>需要关注</h2>
            <p>当前号池中未登录、冷却、配额耗尽、余额偏低或最近报错的启用账号。</p>
          </div>
          <div class="actions">
            <button class="linkbtn" type="button" id="btnGoAccounts">去账号池处理</button>
          </div>
        </div>
        <div class="attention" id="attentionList"><div class="empty">加载中…</div></div>
      </div>
    </section>

    <div class="overview-notes">
      <div class="note-item">
        <h3>429 限频隔离与自动熔断</h3>
        <p>同一会话钉在一个账号上。新会话只补缺失或超过 5 分钟的额度快照，再按剩余最大选号。纯 429 / rate limit 短冷却 2 分钟，松钉后并行刷新候选额度并选剩余最大者；配额类错误会查询官方余额并对齐至 CycleEnd / SlicePeriod 结束。111xx 策略码仍为 5 分钟，部分 5xx 为 30 秒。</p>
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
            <p>一键切换国内 / 国际号池，以及 CodeBuddy / WorkBuddy 上游；token 共用，请求跟随当前选择。</p>
          </div>
          <div class="actions">
            <div class="seg" id="poolSiteSeg" role="group" aria-label="号池区域">
              <button type="button" data-site="domestic" id="btnPoolDomestic">国内</button>
              <button type="button" data-site="global" id="btnPoolGlobal">国际</button>
            </div>
            <div class="seg" id="poolProductSeg" role="group" aria-label="上游产品">
              <button type="button" data-product="codebuddy" id="btnProductCodeBuddy">CodeBuddy</button>
              <button type="button" data-product="workbuddy" id="btnProductWorkBuddy">WorkBuddy</button>
            </div>
          </div>
        </div>
        <p class="seg-hint" id="poolSiteHint">当前号池：—</p>
        <div class="ops-row">
          <button class="ghost" id="btnCheckin" type="button" disabled aria-describedby="checkinHint">每日签到</button>
          <button class="ghost" id="btnPoolTest" type="button" disabled aria-describedby="poolTestHint">批量测试</button>
          <label class="test-model-label" for="testModelSelect">测试模型
            <select id="testModelSelect" aria-label="账号测试使用的模型">
              <option value="">自动（最低倍率）</option>
            </select>
          </label>
        </div>
        <p class="checkin-hint" id="checkinHint">一键为当前号池全部已启用账号签到（国内站约 100 积分/天）。</p>
        <p class="checkin-hint" id="poolTestHint">对当前号池已启用账号发最小 chat，验证可用性与延迟；默认选最低倍率模型。</p>
        <pre class="checkin-detail" id="checkinRaw" hidden></pre>
        <pre class="checkin-detail" id="poolTestRaw" hidden></pre>
        <div class="egress-panel" id="egressPanel">
          <div class="egress-head">
            <strong>账号出口</strong>
            <span class="egress-hint" id="egressHint">未绑定出口的账号走本机直连</span>
          </div>
          <div class="egress-list" id="egressList"></div>
          <div class="egress-add">
            <input id="egressNewUrl" placeholder="socks5h://user:pass@host:port" autocomplete="off" spellcheck="false"/>
            <input id="egressNewLabel" placeholder="备注（可选）" autocomplete="off" style="flex:0 0 140px"/>
            <button class="ghost" id="btnEgressAdd" type="button">加入候选</button>
            <button class="ghost" id="btnEgressBalance" type="button">均衡分配</button>
          </div>
        </div>

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
            <div class="secret-hint">ZCode / OpenCode / 多数 SDK 走这条。</div>
          </div>
          <div>
            <label for="openAiResponsesUrl">Responses</label>
            <div class="copyline">
              <input id="openAiResponsesUrl" readonly placeholder="加载中…"/>
              <button class="ghost" id="copyResponsesUrl" type="button">复制</button>
            </div>
            <div class="secret-hint">Codex CLI 走这条（wire_api = responses）。</div>
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
        <div class="bound-keys" id="boundApiKeys"></div>
        <div class="bound-keys-new">
          <label for="boundKeySite">新建绑定 Key</label>
          <div class="copyline">
            <select id="boundKeySite" aria-label="绑定区域">
              <option value="domestic">国内 domestic</option>
              <option value="global">国际 global</option>
            </select>
            <select id="boundKeyAccount" aria-label="绑定账号">
              <option value="">区域内自动选号</option>
            </select>
            <button class="teal" id="btnNewBoundKey" type="button">新建绑定 Key</button>
          </div>
        </div>
        <div class="secret-hint" id="clientConfigHint">API Key 点击复制时才会读取明文。Key 默认写入 ~/.codebuddy/proxy.env，与账号池同目录，换文件夹启动也会沿用同一把。绑定 Key 换区不改主 Key。</div>
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
          <div class="actions">
            <button class="ghost" id="btnModels" type="button">重新拉取</button>
          </div>
        </div>
        <div class="chips" id="modelChips"><div class="empty">加载中…</div></div>
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

  <!-- ========================================================
       TAB 05: 用量与明细
       ======================================================== -->
  <div class="tab-panel" id="tab-usage">
    <section class="panel">
      <div class="panel-inner">
        <div class="eyebrow">Usage · Request Log</div>
        <h1>Token 与缓存统计</h1>
        <p class="lede">汇总 Token 总量与 prompt 缓存命中率；明细保留请求追踪与 Credits。数据默认写入与账号池同目录的 <code>proxy-usage.json</code>（最近约 400 条明细 + 90 日按日汇总），重启后保留。</p>
        <div class="section-head" style="margin-top:20px;border:none;padding:0">
          <div class="seg" id="usageRangeSeg" role="group" aria-label="统计周期">
            <button type="button" data-range="day" class="active">今日</button>
            <button type="button" data-range="week">7 日</button>
            <button type="button" data-range="month">30 日</button>
          </div>
          <div class="actions">
            <button class="ghost" id="btnRefreshUsage" type="button">刷新明细</button>
          </div>
        </div>
        <div class="metrics">
          <div class="metric"><div class="k">请求数</div><div class="v" id="uRequests">0</div></div>
          <div class="metric"><div class="k">失败</div><div class="v" id="uFailed">0</div></div>
          <div class="metric"><div class="k">Token 总量</div><div class="v" id="uTotalTokens">0</div></div>
          <div class="metric"><div class="k">缓存命中率</div><div class="v sm" id="uCacheHit">—</div></div>
          <div class="metric"><div class="k">Credits Σ</div><div class="v sm" id="uCredits">—</div></div>
        </div>
        <div class="usage-filters">
          <label>账号
            <select id="usageAccountFilter" aria-label="按账号筛选">
              <option value="">全部账号</option>
            </select>
          </label>
          <label>模型
            <select id="usageModelFilter" aria-label="按模型筛选">
              <option value="">全部模型</option>
            </select>
          </label>
        </div>
        <div class="usage-models" id="usageByModel" hidden></div>
        <div class="usage-chart" id="usageChartBox" aria-hidden="false">
          <div class="usage-chart-head">
            <h3>趋势</h3>
            <div class="usage-chart-legend">
              <span><i class="swatch"></i> Token 总量</span>
              <span><i class="swatch dash"></i> 缓存命中率 %</span>
            </div>
          </div>
          <div id="usageChart"><div class="chart-empty">加载中…</div></div>
        </div>
        <p class="checkin-hint" id="usageCreditHint">命中率 = 缓存 token ÷ prompt token（所有模型同一公式）。按模型表用来对比 hy3 / DeepSeek 等差异。Credits 仅在上游 usage 返回 <code>credit</code> 时累加。</p>
        <div class="log-table-wrap">
          <div class="log-table-window">
            <table class="log-table" aria-label="请求明细">
              <thead>
                <tr>
                  <th>时间</th>
                  <th>请求 ID</th>
                  <th>上游 Request</th>
                  <th>会话</th>
                  <th>模型</th>
                  <th>Token 总量</th>
                  <th>缓存命中</th>
                  <th>Credit</th>
                  <th>耗时</th>
                  <th>账号</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody id="usageRows">
                <tr><td colspan="11" class="empty" style="border:none">加载中…</td></tr>
              </tbody>
            </table>
          </div>
          <div class="log-pager">
            <span id="usagePagerMeta">—</span>
            <div class="actions">
              <button type="button" class="ghost" id="btnUsagePrev" disabled>上一页</button>
              <button type="button" class="ghost" id="btnUsageNext" disabled>下一页</button>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</div>

<div class="toast" id="toast" role="status" aria-live="polite"></div>

<!-- 设置单个账号的出口 -->
<div class="modal" id="egressModal" hidden>
  <div class="modal-card" role="dialog" aria-modal="true" aria-labelledby="egressTitle">
    <div class="modal-head">
      <h3 id="egressTitle">设置账号出口</h3>
      <button class="modal-x" id="btnEgressClose" type="button" aria-label="关闭">×</button>
    </div>
    <p class="checkin-hint" style="margin:6px 0 0">绑死后该账号的请求固定走这条出口（不轮换、不自动切换）。选「直连」则用本机 IP。</p>
    <div class="modal-row">
      <label for="egressSelect">出口</label>
      <select id="egressSelect"></select>
    </div>
    <div class="modal-row" id="egressCustomRow" hidden>
      <label for="egressCustom">自定义</label>
      <input id="egressCustom" placeholder="socks5h://user:pass@host:port" autocomplete="off" spellcheck="false"/>
    </div>
    <pre class="checkin-detail" id="egressCheckRaw" hidden></pre>
    <div class="modal-actions">
      <button class="ghost" id="btnEgressCheck" type="button">检测</button>
      <button class="ghost" id="btnEgressCancel" type="button">取消</button>
      <button class="primary" id="btnEgressSave" type="button">保存</button>
    </div>
  </div>
</div>

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
    const tab = btn.getAttribute('data-tab');
    switchTab(tab);
    if (tab === 'tab-usage') refreshUsage().catch(function(e){ showToast(e.message, 'error'); });
  });
});
if (window.location.hash === '#codebuddy') {
  switchTab('tab-pool');
} else if (window.location.hash === '#client-config') {
  switchTab('tab-client');
} else if (window.location.hash === '#usage') {
  switchTab('tab-usage');
}
window.addEventListener('hashchange', function(){
  if (window.location.hash === '#codebuddy') switchTab('tab-pool');
  if (window.location.hash === '#client-config') switchTab('tab-client');
  if (window.location.hash === '#usage') switchTab('tab-usage');
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

// 页面加载和常规轮询均不检查 Release；只有用户点击才会访问 GitHub。
let updatePolling = false;
// 更新请求单独加 AbortController 超时：重启期间旧连接可能长时间挂起。
// 默认 3 秒适合本地状态轮询；检查更新要访问 GitHub，单独放宽。
async function updateFetch(path, opts={}, timeoutMs=3000) {
  const ctrl = new AbortController();
  const timer = setTimeout(function(){ ctrl.abort(); }, timeoutMs);
  try {
    const headers = Object.assign({'content-type':'application/json'}, opts.headers||{});
    const res = await fetch(path, Object.assign({}, opts, {headers: headers, credentials:'same-origin', signal: ctrl.signal}));
    const text = await res.text();
    let data = {};
    try { data = text ? JSON.parse(text) : {}; } catch (e) { data = {raw:text}; }
    if (!res.ok) throw new Error((data && data.error && data.error.message) || data.message || data.error || ('HTTP '+res.status));
    return data;
  } catch(e) {
    // 中止错误统一成可读中文，由调用方决定显示“等待服务重新连接”还是具体原因。
    if (e && e.name === 'AbortError') throw new Error('请求超时（'+Math.round(timeoutMs/1000)+' 秒）');
    throw e;
  } finally {
    clearTimeout(timer);
  }
}
// 只读取本地内存状态，让 Docker/dev 用户在打开页面时看到操作说明。
updateFetch('/direct-admin/api/update').then(renderUpdate).catch(function(){});
function renderUpdate(s) {
  $('updateMessage').textContent = s.message || (s.available ? ('可更新至 '+s.latest) : (s.latest ? '已是最新版本' : ''));
  $('updateInstall').hidden = !s.available;
  const busy = ['checking','downloading','restarting'].includes(s.phase);
  $('updateInstall').disabled = busy;
  $('updateCheck').disabled = busy;
  $('updateCancel').hidden = s.phase !== 'downloading';
}
$('updateCheck').addEventListener('click', async function(){
  this.disabled = true;
  $('updateMessage').textContent = '正在检查更新…';
  try { renderUpdate(await updateFetch('/direct-admin/api/update/check', {method:'POST'}, 30000)); }
  catch(e) { $('updateMessage').textContent = e.message; this.disabled = false; }
});
$('updateCancel').addEventListener('click', async function(){
  try { await updateFetch('/direct-admin/api/update/cancel', {method:'POST'}); }
  catch(e) { $('updateMessage').textContent = e.message; }
});
$('updateInstall').addEventListener('click', async function(){
  if (!confirm('更新将排空当前请求并重启，仅替换可执行文件。继续？')) return;
  const before = (await updateFetch('/direct-admin/api/update')).current;
  this.disabled = true;
  try {
    renderUpdate(await updateFetch('/direct-admin/api/update/install', {method:'POST'}));
    if (updatePolling) return;
    updatePolling = true;
    const deadline = Date.now()+240000;
    while (Date.now()<deadline) {
      await new Promise(resolve => setTimeout(resolve,1000));
      try {
        const s = await updateFetch('/direct-admin/api/update');
        if (s.current !== before) { $('updateMessage').textContent = '更新完成：'+s.current; location.reload(); return; }
        if (s.phase === 'error') { renderUpdate(s); return; }
        if (s.phase === 'idle') { renderUpdate(s); $('updateMessage').textContent = s.message || '服务已重连但版本未变化，请检查安装目录更新日志（可能已回滚）'; return; }
        renderUpdate(s);
      } catch(e) { $('updateMessage').textContent = '等待服务重新连接…'; }
    }
    $('updateMessage').textContent = '重连超时，请检查安装目录 .buddy-update-helper-*.exe.log 和原程序备份；不要重复启动服务';
  } catch(e) { $('updateMessage').textContent = e.message; }
  finally { updatePolling = false; this.disabled = false; $('updateCheck').disabled = false; }
});

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
let lastAccounts = [];
// 套餐时间由后端按北京时间解析为毫秒；前端只显示绝对时间，不解析无时区字符串。
function packageExpiryView(usage, now){
  const packages = ((usage.credits || {}).packages || []).slice().sort(function(a,b){
    return (a.expiresAt || Infinity) - (b.expiresAt || Infinity);
  });
  let expiring = 0, amount = 0, remaining = 0;
  const rows = packages.map(function(p){
    const expiry = Number(p.expiresAt) || 0;
    const left = Number(p.remaining);
    if (Number.isFinite(left) && left > 0) remaining += left;
    const delta = expiry - now;
    let label = '到期未知', level = 'muted';
    if (expiry && delta <= 0) { label = '已过期'; level = 'bad'; }
    else if (expiry && delta <= 86400000) { label = '24h 内'; level = 'bad'; }
    else if (expiry && delta <= 3*86400000) { label = '3 天内'; level = 'warn'; }
    else if (expiry && delta <= 7*86400000) { label = '7 天内'; level = 'warn'; }
    else if (expiry) label = '有效';
    if (expiry && delta > 0 && delta <= 7*86400000 && Number.isFinite(left) && left > 0) {
      expiring++; amount += left;
    }
    const number = function(value){ return value == null ? '未知' : (Number(value) === -1 ? '不限量' : String(value)); };
    return '<tr><td>' + escapeHtml(expiry ? new Date(expiry).toLocaleString() : '未知') +
      '</td><td><span class="pill ' + level + '">' + label + '</span></td><td>' +
      escapeHtml(p.packageName || '未命名套餐') + '</td><td>' + escapeHtml(number(p.remaining) + ' / ' + number(p.total)) +
      '</td><td>' + escapeHtml(number(p.used)) + '</td></tr>';
  }).join('');
  const checked = usage.quota && usage.quota.checkedAt;
  return '<details class="package-expiry"><summary>积分到期明细 · ' + packages.length + ' 包 · 快照剩余 ' + escapeHtml(remaining.toFixed(2)) + ' 分 · 未来 7 天 ' +
    expiring + ' 包 / ' + escapeHtml(amount.toFixed(2)) + ' 分</summary><p class="meta">余额为查询快照，非实时；数据时间：' +
    escapeHtml(checked ? new Date(checked).toLocaleString() : '未知') +
    '。到期时间按浏览器本地时区显示。</p><div style="overflow-x:auto"><table><thead><tr>' +
    '<th>到期时间</th><th>状态</th><th>套餐</th><th>剩余 / 总量</th><th>已用</th></tr></thead><tbody>' +
    (rows || '<tr><td colspan="5">暂无套餐明细，已过期历史包可能不在上游有效套餐接口中。</td></tr>') +
    '</tbody></table></div></details>';
}

function renderAccounts(summary, activeSite) {
  const box = $('accounts');
  const all = (summary && summary.accounts) || [];
  lastAccounts = all;
  const expanded = new Set(Array.from(box.querySelectorAll('.package-expiry[open]')).map(function(d){ return d.closest('.account').getAttribute('data-account-id'); }));
  // 15 秒轮询会重绘卡片；记住已展开的「更多」菜单，避免操作到一半被收起。
  const menuOpen = new Set(Array.from(box.querySelectorAll('.more[open]')).map(function(d){ return d.closest('.account').getAttribute('data-account-id'); }));
  const accountSelect = $('boundKeyAccount');
  if (accountSelect) {
    const selected = accountSelect.value;
    accountSelect.innerHTML = '<option value="">区域内自动选号</option>' + all.filter(function(a){
      return a.enabled && a.hasCredentials;
    }).map(function(a){
      return '<option value="' + escapeHtml(a.id) + '">' + escapeHtml((a.label || a.id) + ' · ' + siteLabel(a.site)) + '</option>';
    }).join('');
    accountSelect.value = selected;
  }
  activeSite = normalizeSite(activeSite || (summary && summary.activeSite) || '');
  if (!all.length) {
    box.innerHTML = '<div class="empty">暂无账号。请在下方 OAuth 区域完成登录。</div>';
    renderAttention(summary, activeSite);
    return;
  }
  const accounts = all.filter(function(a){ return normalizeSite(a.site) === activeSite; });
  if (!accounts.length) {
    box.innerHTML = '<div class="empty">当前号池没有账号，切到另一区域可管理其他号</div>';
    renderAttention(summary, activeSite);
    return;
  }
  box.innerHTML = accounts.map(function(a) {
    const id = escapeHtml(a.id);
    const name = escapeHtml(a.userNickname || a.userName || a.userId || '未命名用户');
    const label = escapeHtml(a.label || a.id);
    const logged = a.loggedIn && a.hasCredentials;
    const site = normalizeSite(a.site);
    const states = accountStates(a);
    const usage = usageByAccount[a.id];
    let usageHtml = '';
    if (usage && usage.error) {
      usageHtml = '<div class="usage-line"><span class="pill bad" title="' + escapeHtml(usage.error) + '">余额查询失败</span> ' +
        '<button class="ghost" data-act="usage" data-id="' + id + '" type="button">重试</button></div>';
    } else if (usage && usage.credits) {
      const c = usage.credits;
      const remaining = c.unlimited ? '不限量' : (c.remaining == null ? '-' : String(c.remaining));
      const total = c.unlimited ? '不限量' : (c.total == null ? '-' : String(c.total));
      usageHtml = '<div class="usage-line"><span class="pill ' + creditLevel(usage) + '">' + escapeHtml(remaining + ' / ' + total) + ' Credits</span> ' +
        '<a href="' + escapeHtml(safeHttpUrl(usage.officialUsageUrl, 'https://www.codebuddy.cn/profile/plan')) + '" target="_blank" rel="noreferrer">官网套餐</a></div>';
      usageHtml += packageExpiryView(usage, Date.now());
    }
    const poolStateHtml = states.map(function(s){
      return '<div class="usage-line"><span class="pill ' + s.level + '">' + escapeHtml(s.text) + '</span></div>';
    }).join('');
    return '<div class="account" data-account-id="' + id + '">' +
      '<div class="account-top">' +
        '<div class="account-title">' +
          '<strong>' + label + '</strong>' +
          '<span class="badge site">' + escapeHtml(siteLabel(site)) + '</span>' +
          '<span class="badge ' + (logged?'on':'off') + '">' + (logged?'已登录':'未登录') + '</span>' +
          (a.proxy ? '<span class="badge" title="' + escapeHtml(a.proxy) + '">出口 ' + escapeHtml(proxyHost(a.proxy)) + '</span>' : '') +
          (a.enabled ? '' : '<span class="badge off">已禁用</span>') +
          (a.tokenExpired ? '<span class="badge bad">Token 过期</span>' : '') +
        '</div>' +
        '<div class="actions">' +
          '<button class="ghost" data-act="test" data-id="' + id + '" type="button">测试</button>' +
          '<button class="ghost" data-act="usage" data-id="' + id + '" type="button">' + (usage && usage.credits ? '刷新余额' : '查余额') + '</button>' +
          '<details class="more"' + (menuOpen.has(a.id) ? ' open' : '') + '><summary aria-label="更多操作">更多</summary><div class="more-menu">' +
            '<button data-act="toggle" data-id="' + id + '" data-enabled="' + (a.enabled?0:1) + '" type="button">' + (a.enabled?'禁用账号':'启用账号') + '</button>' +
            '<button data-act="proxy" data-id="' + id + '" type="button">设置出口</button>' +
            '<button data-act="refresh" data-id="' + id + '" type="button">刷新 Token</button>' +
            '<button class="danger" data-act="delete" data-id="' + id + '" type="button">删除账号</button>' +
          '</div></details>' +
        '</div>' +
      '</div>' +
      '<div class="meta">' + name +
        ' · <span class="mono">' + escapeHtml(a.authType||'') + '</span>' +
        ' · success ' + (a.successRequests||0) + ' / fail ' + (a.failedRequests||0) +
      '</div>' +
      usageHtml +
      poolStateHtml +
      (a.lastError ? ('<div class="err">' + escapeHtml(a.lastError) + '</div>') : '') +
    '</div>';
  }).join('');
  box.querySelectorAll('.package-expiry').forEach(function(d){ d.open = expanded.has(d.closest('.account').getAttribute('data-account-id')); });
  box.querySelectorAll('button[data-act]').forEach(function(btn){ btn.addEventListener('click', onAccountAction); });
  renderAttention(summary, activeSite);
}

// 账号的调度阻塞状态；账号卡片和概览「需要关注」共用同一份判断。
function accountStates(a){
  const now = Date.now();
  const out = [];
  if (a.quotaExhausted && a.quotaResetAt) {
    out.push({level:'bad', text:'配额耗尽 · ' + formatResetAt(a.quotaResetAt) + ' 恢复'});
  } else if (a.cooldownUntil && a.cooldownUntil > now) {
    out.push({level:'warn', text:'冷却中 · ' + formatResetAt(a.cooldownUntil) + ' 恢复'});
  } else if (a.quotaRemaining != null && Number(a.quotaRemaining) <= 0) {
    out.push({level:'warn', text:'配额可能已耗尽'});
  }
  Object.entries(a.modelCooldowns || {}).forEach(function(entry) {
    if (entry[1] > now) out.push({level:'warn', text:'模型冷却 · ' + entry[0] + ' · ' + formatResetAt(entry[1]) + ' 恢复'});
  });
  return out;
}

function creditLevel(usage){
  const c = (usage && usage.credits) || {};
  let level = 'good';
  if (!c.unlimited && Number(c.total) > 0) {
    const ratio = Number(c.remaining) / Number(c.total);
    if (ratio <= 0.05 || Number(c.remaining) <= 0) level = 'bad';
    else if (ratio <= 0.2) level = 'warn';
  }
  if (usage && usage.notify && usage.notify.level === 'bad') level = 'bad';
  else if (usage && usage.notify && usage.notify.level === 'warn' && level === 'good') level = 'warn';
  return level;
}

// 上游给的链接放进 href 前只放行 http(s)。
function safeHttpUrl(raw, fallback){
  const s = String(raw || '').trim();
  return /^https?:\/\//i.test(s) ? s : fallback;
}

function renderAttention(summary, activeSite){
  const box = $('attentionList');
  if (!box) return;
  const all = (summary && summary.accounts) || [];
  if (!all.length) {
    box.innerHTML = '<div class="empty">号池为空。到「账号池与授权」页添加账号。</div>';
    return;
  }
  const site = normalizeSite(activeSite);
  const rows = all.filter(function(a){
    return a.enabled && normalizeSite(a.site) === site;
  }).map(function(a){
    const reasons = accountStates(a);
    if (!(a.loggedIn && a.hasCredentials)) reasons.unshift({level:'bad', text:'未登录'});
    if (a.tokenExpired) reasons.unshift({level:'bad', text:'Token 过期'});
    const usage = usageByAccount[a.id];
    if (usage && usage.credits && creditLevel(usage) !== 'good') {
      reasons.push({level: creditLevel(usage), text: '余额偏低'});
    }
    if (a.lastError) reasons.push({level:'muted', text:'最近报错'});
    return {a:a, reasons:reasons};
  }).filter(function(r){ return r.reasons.length; });
  if (!rows.length) {
    box.innerHTML = '<div class="empty">当前号池的启用账号都正常。</div>';
    return;
  }
  box.innerHTML = rows.map(function(r){
    return '<div class="attention-row">' +
      '<div class="who"><strong>' + escapeHtml(r.a.label || r.a.id) + '</strong>' +
        (r.a.lastError ? ' <span class="meta" title="' + escapeHtml(r.a.lastError) + '">· ' + escapeHtml(r.a.lastError) + '</span>' : '') +
      '</div>' +
      '<div class="reasons">' + r.reasons.map(function(s){
        return '<span class="badge ' + s.level + '">' + escapeHtml(s.text) + '</span>';
      }).join('') + '</div>' +
    '</div>';
  }).join('');
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
  fillTestModelSelect(list);
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

function fillTestModelSelect(list){
  const sel = $('testModelSelect');
  if (!sel) return;
  const prev = sel.value;
  const opts = ['<option value="">自动（最低倍率）</option>'];
  const seen = {};
  (list || []).forEach(function(m){
    const raw = typeof m === 'string' ? m : (m && (m.id || m.modelId || m.upstreamId || m.name));
    const id = bareModelId(raw);
    if (!id || id === 'auto' || seen[id]) return;
    seen[id] = true;
    const credits = (typeof m === 'object' && m && m.credits) ? String(m.credits) : '';
    const label = credits ? (id + ' · ' + credits.replace(/\s*credits$/i,'')) : id;
    opts.push('<option value="' + escapeHtml(id) + '">' + escapeHtml(label) + '</option>');
  });
  sel.innerHTML = opts.join('');
  if (prev && seen[prev]) sel.value = prev;
}

function selectedTestModel(){
  const sel = $('testModelSelect');
  return sel ? String(sel.value || '').trim() : '';
}

function normalizeSite(site){
  site = String(site||'').toLowerCase().trim();
  if (site === 'domestic' || site === 'cn' || site === 'china' || site === 'internal') return 'domestic';
  return 'global';
}
function siteLabel(site){
  return normalizeSite(site) === 'domestic' ? '国内' : '国际';
}
function normalizeProduct(product){
  product = String(product||'').toLowerCase().trim();
  if (product === 'workbuddy' || product === 'wb' || product === 'ide') return 'workbuddy';
  return 'codebuddy';
}
function productLabel(product){
  return normalizeProduct(product) === 'workbuddy' ? 'WorkBuddy' : 'CodeBuddy';
}
// 签到按钮跟随当前激活号池；paintPoolSite 每次刷新状态时同步。
// 空字符串表示尚未从 /status 拿到号池，按钮保持 disabled，避免首屏误打国际站。
let activeCheckinSite = '';
let activeProduct = 'codebuddy';
let lastPoolAccounts = null;
let checkinBusy = false;
let poolTestBusy = false;
let usageRange = 'day';
let usagePage = 1;
let usageAccount = '';
let usageModel = '';
const usagePageSize = 20;

function formatTime(ms){
  if (!ms) return '—';
  try { return new Date(ms).toLocaleString(); } catch(e) { return String(ms); }
}
function shortId(id){
  id = String(id||'');
  if (id.length <= 10) return id || '—';
  return id.slice(0, 8) + '…';
}
function formatHitRate(rate){
  if (rate == null || rate < 0) return '—';
  return rate.toFixed(1) + '%';
}
function rowTotalTokens(row){
  const p = row.promptTokens || 0;
  const c = row.completionTokens || 0;
  return row.totalTokens || (p + c);
}
function rowHitRate(row){
  const p = row.promptTokens || 0;
  if (p <= 0) return -1;
  return (row.cachedTokens || 0) / p * 100;
}
function paintUsageChart(series, summary){
  const host = $('usageChart');
  if (!host) return;
  const pts = series || [];
  const hasTokens = pts.some(function(p){ return (p.totalTokens||0) > 0; });
  const hasRate = pts.some(function(p){ return p.cacheHitRate != null && p.cacheHitRate >= 0; });
  const hasRequests = summary && (summary.requests || 0) > 0;
  if (!pts.length) {
    host.innerHTML = '<div class="chart-empty">暂无趋势数据</div>';
    return;
  }
  if (!hasTokens && !hasRate && !hasRequests) {
    host.innerHTML = '<div class="chart-empty">暂无趋势数据</div>';
    return;
  }
  const W = 720, H = 168, pad = {l: 44, r: 44, t: 14, b: 30};
  const innerW = W - pad.l - pad.r;
  const innerH = H - pad.t - pad.b;
  const maxTok = Math.max(1, pts.reduce(function(m, p){ return Math.max(m, p.totalTokens||0); }, 0));
  const n = pts.length;
  function xAt(i){ return pad.l + (n <= 1 ? innerW / 2 : (i / (n - 1)) * innerW); }
  function yTok(v){ return pad.t + innerH - (v / maxTok) * innerH; }
  function yRate(r){ if (r < 0) r = 0; return pad.t + innerH - (r / 100) * innerH; }
  let pathTok = '';
  let pathRate = '';
  pts.forEach(function(p, i){
    const x = xAt(i);
    const yt = yTok(p.totalTokens || 0);
    const yr = yRate(p.cacheHitRate != null ? p.cacheHitRate : -1);
    pathTok += (i ? ' L' : 'M') + x + ' ' + yt;
    if (p.cacheHitRate != null && p.cacheHitRate >= 0) {
      pathRate += (pathRate ? ' L' : 'M') + x + ' ' + yr;
    }
  });
  const labelStep = n <= 8 ? 1 : Math.max(1, Math.ceil(n / 8));
  const labels = pts.map(function(p, i){
    if (i % labelStep !== 0 && i !== n - 1) return '';
    const x = xAt(i);
    const anchor = i === 0 ? 'start' : (i === n - 1 ? 'end' : 'middle');
    return '<text class="lbl" x="' + x + '" y="' + (H - 8) + '" text-anchor="' + anchor + '">' + escapeHtml(p.label || '') + '</text>';
  }).join('');
  host.innerHTML =
    '<svg viewBox="0 0 ' + W + ' ' + H + '" role="img" aria-label="Token 与缓存命中率趋势">' +
    '<line class="axis" x1="' + pad.l + '" y1="' + (pad.t + innerH) + '" x2="' + (W - pad.r) + '" y2="' + (pad.t + innerH) + '"/>' +
    '<path class="line-tokens" d="' + pathTok + '"/>' +
    (pathRate ? '<path class="line-rate" d="' + pathRate + '"/>' : '') +
    labels +
    '</svg>';
}
function paintUsage(data){
  const sum = data.summary || {};
  $('uRequests').textContent = String(sum.requests || 0);
  $('uFailed').textContent = String(sum.failed || 0);
  $('uTotalTokens').textContent = String(sum.totalTokens || 0);
  $('uCacheHit').textContent = formatHitRate(sum.cacheHitRate);
  if (sum.creditRows > 0) {
    $('uCredits').textContent = (sum.credits || 0).toFixed(4) + ' · ' + sum.creditRows + ' 条';
  } else {
    $('uCredits').textContent = '—';
  }
  fillUsageFilter($('usageAccountFilter'), data.accounts || [], usageAccount, '全部账号');
  fillUsageFilter($('usageModelFilter'), data.models || [], usageModel, '全部模型');
  paintByModel(data.byModel || []);
  paintUsageChart(data.series || [], data.summary || {});
  const rows = data.requests || [];
  const tbody = $('usageRows');
  if (!tbody) return;
  if (!rows.length) {
    tbody.innerHTML = '<tr><td colspan="11" style="color:var(--fg-40);padding:24px;text-align:center">暂无记录</td></tr>';
    return;
  }
  tbody.innerHTML = rows.map(function(row){
    const proxyId = row.proxyRequestId || '';
    const upReq = row.upstreamConversationRequestId || '';
    const credit = row.credit != null ? String(row.credit) : '—';
    const account = row.accountLabel || shortId(row.accountId);
    const status = row.ok
      ? '<span class="badge on">OK</span>'
      : '<span class="badge off" title="' + escapeHtml(row.error||'') + '">FAIL</span>';
    const session = escapeHtml(row.sessionLabel || shortId(row.sessionKey));
    const totalTok = rowTotalTokens(row);
    const hit = formatHitRate(rowHitRate(row));
    return '<tr>' +
      '<td class="mono">' + escapeHtml(formatTime(row.at)) + '</td>' +
      '<td class="mono"><button type="button" class="idbtn" data-copy="' + escapeHtml(proxyId) + '">' + escapeHtml(shortId(proxyId)) + '</button></td>' +
      '<td class="mono">' + (upReq ? '<button type="button" class="idbtn" data-copy="' + escapeHtml(upReq) + '">' + escapeHtml(shortId(upReq)) + '</button>' : '—') + '</td>' +
      '<td>' + session + '</td>' +
      '<td class="mono">' + escapeHtml(row.model || '—') + '</td>' +
      '<td class="mono">' + String(totalTok) + '</td>' +
      '<td class="mono">' + escapeHtml(hit) + '</td>' +
      '<td class="mono">' + escapeHtml(credit) + '</td>' +
      '<td class="mono">' + (row.durationMs != null ? (row.durationMs + 'ms') : '—') + '</td>' +
      '<td>' + escapeHtml(account || '—') + '</td>' +
      '<td>' + status + '</td>' +
      '</tr>';
  }).join('');
  tbody.querySelectorAll('[data-copy]').forEach(function(btn){
    btn.addEventListener('click', function(){ copyText(btn.getAttribute('data-copy'), 'ID', btn); });
  });
}
function paintUsagePager(data){
  const total = data.requestsTotal != null ? data.requestsTotal : (data.requests || []).length;
  const limit = data.limit || usagePageSize;
  const offset = data.offset != null ? data.offset : (usagePage - 1) * limit;
  const pages = Math.max(1, Math.ceil(total / limit) || 1);
  usagePage = Math.floor(offset / limit) + 1;
  if ($('usagePagerMeta')) {
    $('usagePagerMeta').textContent = total
      ? ('第 ' + usagePage + ' / ' + pages + ' 页 · 本页 ' + (data.requests || []).length + ' 条 · 共 ' + total + ' 条')
      : '暂无明细';
  }
  const prev = $('btnUsagePrev');
  const next = $('btnUsageNext');
  if (prev) prev.disabled = usagePage <= 1;
  if (next) next.disabled = usagePage >= pages || total === 0;
}
async function refreshUsage(){
  const offset = (usagePage - 1) * usagePageSize;
  let url = '/direct-admin/api/usage?range=' + encodeURIComponent(usageRange) +
    '&limit=' + usagePageSize + '&offset=' + offset;
  if (usageAccount) url += '&account=' + encodeURIComponent(usageAccount);
  if (usageModel) url += '&model=' + encodeURIComponent(usageModel);
  const data = await api(url);
  paintUsage(data);
  paintUsagePager(data);
  return data;
}
function fillUsageFilter(sel, values, current, allLabel){
  if (!sel) return;
  const keep = current || '';
  const opts = ['<option value="">' + allLabel + '</option>'];
  (values || []).forEach(function(v){
    const selected = v === keep ? ' selected' : '';
    opts.push('<option value="' + escapeHtml(v) + '"' + selected + '>' + escapeHtml(v) + '</option>');
  });
  if (keep && (values || []).indexOf(keep) < 0) {
    opts.push('<option value="' + escapeHtml(keep) + '" selected>' + escapeHtml(keep) + '</option>');
  }
  sel.innerHTML = opts.join('');
}
function paintByModel(rows){
  const host = $('usageByModel');
  if (!host) return;
  if (!rows || !rows.length) {
    host.hidden = true;
    host.innerHTML = '';
    return;
  }
  host.hidden = false;
  host.innerHTML = '<table aria-label="按模型缓存命中"><thead><tr>' +
    '<th>模型</th><th>请求</th><th>Token</th><th>缓存命中</th></tr></thead><tbody>' +
    rows.map(function(row){
      const rate = formatHitRate(row.cacheHitRate);
      const low = (row.cacheHitRate == null || row.cacheHitRate < 5) ? ' class="low"' : '';
      return '<tr>' +
        '<td class="mono">' + escapeHtml(row.model || '—') + '</td>' +
        '<td class="mono">' + String(row.requests || 0) + '</td>' +
        '<td class="mono">' + String(row.totalTokens || 0) + '</td>' +
        '<td class="mono"' + low + '>' + escapeHtml(rate) + '</td>' +
        '</tr>';
    }).join('') +
    '</tbody></table>';
}
function paintUsageRange(range){
  usageRange = range || 'day';
  usagePage = 1;
  const seg = $('usageRangeSeg');
  if (!seg) return;
  seg.querySelectorAll('button[data-range]').forEach(function(btn){
    btn.className = btn.getAttribute('data-range') === usageRange ? 'active' : '';
  });
}

function paintPoolHint(site, product, accounts){
  const domestic = accounts && accounts.domesticCount != null ? accounts.domesticCount : '—';
  const global = accounts && accounts.globalCount != null ? accounts.globalCount : '—';
  const activeEnabled = accounts && accounts.activeEnabledCount != null ? accounts.activeEnabledCount : '—';
  const activeAvailable = accounts && accounts.activeAvailableCount != null ? accounts.activeAvailableCount : '—';
  if ($('poolSiteHint')) {
    $('poolSiteHint').textContent = '当前号池：' + siteLabel(site) + ' · 上游：' + productLabel(product) + ' · 启用账号 ' + activeEnabled + ' · 无整号阻塞 ' + activeAvailable + '（模型冷却另计） · 国内账号 ' + domestic + ' / 国际账号 ' + global + '（仅当前区域会参与请求）';
  }
}

function paintPoolSite(site, accounts){
  site = normalizeSite(site);
  activeCheckinSite = site;
  if (accounts) lastPoolAccounts = accounts;
  const checkinBtn = $('btnCheckin');
  if (checkinBtn && !checkinBusy) checkinBtn.disabled = false;
  const poolTestBtn = $('btnPoolTest');
  if (poolTestBtn && !poolTestBusy) poolTestBtn.disabled = false;
  const domesticBtn = $('btnPoolDomestic');
  const globalBtn = $('btnPoolGlobal');
  if (domesticBtn) domesticBtn.className = site === 'domestic' ? 'active' : '';
  if (globalBtn) globalBtn.className = site === 'global' ? 'active' : '';
  paintPoolHint(site, activeProduct, lastPoolAccounts || accounts);
  // 切号池 / 刷新状态时清掉上一次的签明细，避免与新号池并存造成误导。
  // 签到进行中不清理：runPoolCheckin 会在 refreshStatus 之后重新写入。
  const checkinRaw = $('checkinRaw');
  if (checkinRaw && !checkinBusy) {
    checkinRaw.hidden = true;
    checkinRaw.textContent = '';
  }
  const poolTestRaw = $('poolTestRaw');
  if (poolTestRaw && !poolTestBusy) {
    poolTestRaw.hidden = true;
    poolTestRaw.textContent = '';
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

async function switchPoolProduct(product){
  product = normalizeProduct(product);
  const data = await api('/direct-admin/api/pool-product', {method:'POST', body: JSON.stringify({product: product})});
  paintStatus(data);
  if (data.note) showToast(data.note);
  refreshModels().catch(function(){});
  return data;
}

function paintPoolProduct(product){
  product = normalizeProduct(product);
  activeProduct = product;
  const cbBtn = $('btnProductCodeBuddy');
  const wbBtn = $('btnProductWorkBuddy');
  if (cbBtn) cbBtn.className = product === 'codebuddy' ? 'active' : '';
  if (wbBtn) wbBtn.className = product === 'workbuddy' ? 'active' : '';
  if ($('pillProduct')) $('pillProduct').textContent = productLabel(product);
  paintPoolHint(activeCheckinSite || 'global', product, lastPoolAccounts);
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
  if ($('pillVersion')) {
    const ver = data.version || (data.build && data.build.version) || 'dev';
    const ui = document.querySelector('meta[name="cbp-ui-revision"]');
    const uiRev = ui ? (ui.getAttribute('content') || '') : '';
    $('pillVersion').textContent = uiRev ? (ver + ' · ui ' + uiRev) : ver;
  }
  const poolSite = normalizeSite(data.poolSite || cfg.poolSite || cfg.site || 'global');
  const poolProduct = normalizeProduct(data.poolProduct || data.product || cfg.poolProduct || cfg.product || 'codebuddy');
  $('pillSite').textContent = siteLabel(poolSite);
  paintPoolSite(poolSite, accounts);
  paintPoolProduct(poolProduct);
  if ($('site') && !$('site').dataset.userTouched) {
    $('site').value = poolSite === 'domestic' ? 'domestic' : 'global';
  }
  const activeEnabled = accounts.activeEnabledCount != null ? accounts.activeEnabledCount : enabledCount;
  $('mEnabled').textContent = String(activeEnabled);
  const up = data.upstream || {};
  var healthOk = !!data.ok;
  var healthText = healthOk ? (loggedIn ? ('服务正常 · ' + siteLabel(poolSite) + '号池 · ' + productLabel(poolProduct)) : ('服务正常 · ' + siteLabel(poolSite) + '号池未登录')) : '状态异常';
  if (up.probed && up.ok === false) {
    healthOk = false;
    healthText = '上游不可达' + (up.message ? (' · ' + String(up.message).slice(0, 48)) : '');
  } else if (up.probed && up.ok) {
    healthText = '服务正常 · 上游可达 · ' + siteLabel(poolSite) + '号池 · ' + productLabel(poolProduct);
  }
  setHealth(healthOk, healthText);
  if (Array.isArray(data.accountUsages)) {
    data.accountUsages.forEach(function(item){
      if (!item || !item.accountId) return;
      usageByAccount[item.accountId] = item;
    });
  }
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
  refreshEgress();
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
  const responses = cfg.responsesUrl || (base ? (base.replace(/\/$/,'') + '/responses') : '');
  const model = bareModelId(cfg.recommendedModel || 'auto');
  $('openAiBaseUrl').value = base;
  $('openAiChatUrl').value = chat;
  if ($('openAiResponsesUrl')) $('openAiResponsesUrl').value = responses;
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
  renderBoundApiKeys(cfg.apiKeys || []);
}
function renderBoundApiKeys(list){
  const box = $('boundApiKeys');
  if (!box) return;
  if (!list.length) {
    box.innerHTML = '<div class="empty">还没有区域绑定 Key。主 Key 仍可用；新建一把即可按区域接入。</div>';
    return;
  }
  box.innerHTML = list.map(function(item){
    const site = normalizeSite(item.site);
    const preview = escapeHtml(item.preview || '已配置');
    const key = String(item.key || '');
    return '<div class="bound-key">' +
      '<div class="meta"><span class="mono">' + preview + '</span>' +
        '<span class="badge site">' + escapeHtml(item.accountId ? ('固定账号 ' + item.accountId) : siteLabel(site)) + '</span></div>' +
      '<div class="actions">' +
        '<button class="ghost" data-act="copy-bound" data-key="' + escapeHtml(key) + '" type="button">复制</button>' +
        '<button class="danger" data-act="delete-bound" data-key="' + escapeHtml(key) + '" type="button">删除</button>' +
      '</div></div>';
  }).join('');
  box.querySelectorAll('button[data-act]').forEach(function(btn){
    btn.addEventListener('click', onBoundKeyAction);
  });
}
async function onBoundKeyAction(ev){
  const btn = ev.currentTarget;
  const act = btn.getAttribute('data-act');
  const key = btn.getAttribute('data-key') || '';
  if (act === 'copy-bound') {
    return copyText(key, '绑定 Key', btn);
  }
  if (act === 'delete-bound') {
    if (!confirm('删除这把区域绑定 Key？主 API Key 不会被删。')) return;
    const data = await api('/direct-admin/api/client-config/bound-keys', {method:'DELETE', body: JSON.stringify({key:key})});
    paintClientConfig(data);
    showToast('绑定 Key 已删除');
  }
}
async function generateBoundKey(){
  const site = $('boundKeySite') ? $('boundKeySite').value : 'global';
  const accountId = $('boundKeyAccount') ? $('boundKeyAccount').value : '';
  const binding = accountId ? {accountId:accountId} : {site:site};
  const data = await api('/direct-admin/api/client-config/bound-keys', {method:'POST', body: JSON.stringify(binding)});
  paintClientConfig(data);
  const created = (data.apiKeys || []).filter(function(item){ return item && item.key && (accountId ? item.accountId === accountId : !item.accountId && item.site === normalizeSite(site)); }).pop();
  if (created && created.key) await copyText(created.key, '绑定 Key', $('btnNewBoundKey'));
  if (data.note) showToast(data.note);
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

async function refreshStatus(fresh){
  const data = await api('/direct-admin/api/status' + (fresh ? '?fresh=1' : ''));
  paintStatus(data);
  if (fresh && data.creditsRefreshed) showToast('额度已从上游刷新');
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

function formatPoolTestSummary(data){
  const s = data.summary || {};
  const parts = [];
  if (s.passed > 0) parts.push('通过 ' + s.passed);
  if (s.failed > 0) parts.push('失败 ' + s.failed);
  if (s.skipped > 0) parts.push('跳过 ' + s.skipped);
  const model = data.model ? (' · ' + data.model) : '';
  if (!parts.length) return (data.note || '当前号池没有可测试的账号') + model;
  return parts.join(' · ') + model;
}

async function runPoolChatTest(){
  if (!activeCheckinSite) {
    showToast('号池状态尚未加载，请稍后再试', 'error');
    return;
  }
  const btn = $('btnPoolTest');
  poolTestBusy = true;
  if (btn) { btn.disabled = true; btn.textContent = '测试中…'; }
  try {
    const body = {site: activeCheckinSite};
    const model = selectedTestModel();
    if (model) body.model = model;
    const data = await api('/direct-admin/api/codebuddy/test', {method:'POST', body: JSON.stringify(body)});
    showToast(formatPoolTestSummary(data), data.ok ? undefined : 'error');
    const results = data.results || [];
    if (results.length && $('poolTestRaw')) {
      const lines = results.map(function(item){
        const name = item.label || item.accountId || '账号';
        if (item.ok) return name + '：ok · ' + (item.latencyMs || 0) + 'ms · ' + (item.model || '');
        return name + '：' + (item.message || '失败');
      });
      $('poolTestRaw').textContent = lines.join('\n');
      $('poolTestRaw').hidden = false;
    }
  } catch (e) {
    showToast('批量测试失败：' + (e.message || e), 'error');
  } finally {
    poolTestBusy = false;
    if (btn) { btn.disabled = false; btn.textContent = '批量测试'; }
  }
}

async function testAccountChat(accountId){
  const body = {};
  const model = selectedTestModel();
  if (model) body.model = model;
  const data = await api('/direct-admin/api/codebuddy/accounts/'+encodeURIComponent(accountId)+'/test', {
    method:'POST',
    body: JSON.stringify(body)
  });
  if (data.ok) {
    showToast((data.label || data.accountId || '账号') + ' · ' + (data.latencyMs || 0) + 'ms · ' + (data.model || ''));
  } else {
    showToast('测试失败：' + (data.message || 'unknown'), 'error');
  }
  return data;
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
  const menu = btn.closest('.more');
  if (menu) menu.open = false;
  const id = btn.getAttribute('data-id');
  const act = btn.getAttribute('data-act');
  if (act === 'usage') {
    await fetchAccountUsage(id, false);
    return;
  }
  if (act === 'test') {
    btn.disabled = true;
    try {
      await testAccountChat(id);
    } catch (e) {
      showToast('测试失败：' + (e.message || e), 'error');
    } finally {
      btn.disabled = false;
    }
    return;
  }
  if (act === 'proxy') {
    openEgressModal(id);
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

// ---------------- 账号出口 ----------------
let egressState = {candidates: [], used: [], direct: 0, total: 0};
let egressAccountID = '';

function proxyHost(url){
  const s = String(url || '');
  const at = s.lastIndexOf('@');
  const rest = at >= 0 ? s.slice(at + 1) : s.replace(/^[a-z0-9+.-]+:\/\//i, '');
  return rest.replace(/\/.*$/, '');
}

async function refreshEgress(){
  try {
    egressState = await api('/direct-admin/api/codebuddy/proxies');
    renderEgress();
  } catch (e) {
    // 出口面板只是附加信息，失败不打扰主流程。
  }
}

function renderEgress(){
  const box = $('egressList');
  if (!box) return;
  const cands = egressState.candidates || [];
  const counts = {};
  (egressState.used || []).forEach(function(u){ counts[u.proxy] = u.count || 0; });
  if (!cands.length) {
    box.innerHTML = '<div class="egress-hint" style="padding:6px 0">还没有候选出口，账号默认走本机直连。</div>';
  } else {
    box.innerHTML = cands.map(function(c){
      return '<div class="egress-row">' +
        '<div class="egress-id"><strong>' + escapeHtml(c.label || proxyHost(c.url)) + '</strong><span>' + escapeHtml(c.url) + '</span></div>' +
        '<div class="egress-count">' + (counts[c.url] || 0) + ' 个账号</div>' +
        '<div class="actions">' +
          '<button class="ghost" data-egress-act="check" data-id="' + escapeHtml(c.id) + '" type="button">检测</button>' +
          '<button class="danger" data-egress-act="del" data-id="' + escapeHtml(c.id) + '" type="button">删除</button>' +
        '</div>' +
      '</div>';
    }).join('');
    box.innerHTML += '<div class="egress-row"><div class="egress-id"><strong>直连（本机 IP）</strong><span>没有绑定出口的账号</span></div>' +
      '<div class="egress-count">' + (egressState.direct || 0) + ' 个账号</div><div class="actions"></div></div>';
    box.querySelectorAll('button[data-egress-act]').forEach(function(btn){ btn.onclick = onEgressRowAction; });
  }
  $('egressHint').textContent = cands.length + ' 条候选出口 · ' + (egressState.direct || 0) + ' 个账号仍直连';
}

async function onEgressRowAction(ev){
  const btn = ev.currentTarget;
  const id = btn.getAttribute('data-id');
  const act = btn.getAttribute('data-egress-act');
  btn.disabled = true;
  try {
    if (act === 'del') {
      if (!confirm('从候选清单删除这条出口？（已绑定它的账号不受影响）')) return;
      await api('/direct-admin/api/codebuddy/proxies/candidates', {method: 'DELETE', body: JSON.stringify({id: id})});
      showToast('已从候选清单删除');
    } else {
      const res = await api('/direct-admin/api/codebuddy/proxies/check', {method: 'POST', body: JSON.stringify({candidateId: id})});
      if (res.ok) {
        const direct = (res.direct && res.direct.ok) ? (' · 本机 ' + res.direct.ip) : '';
        showToast('出口 ' + res.ip + ' · ' + res.elapsedMs + 'ms' + direct);
      } else {
        showToast('出口不可用：' + (res.error || '未知错误'), 'error');
      }
    }
  } catch (e) {
    showToast(String(e.message || e), 'error');
  } finally {
    btn.disabled = false;
    await refreshEgress();
  }
}

async function addEgressCandidate(){
  const url = ($('egressNewUrl').value || '').trim();
  const label = ($('egressNewLabel').value || '').trim();
  if (!url) { showToast('先填出口 URL', 'error'); return; }
  await api('/direct-admin/api/codebuddy/proxies/candidates', {method: 'POST', body: JSON.stringify({url: url, label: label})});
  $('egressNewUrl').value = '';
  $('egressNewLabel').value = '';
  showToast('已加入候选出口');
  await refreshEgress();
}

function currentPoolSite(){
  const btn = document.querySelector('#poolSiteSeg button.active');
  return btn ? btn.getAttribute('data-site') : '';
}

async function balanceEgress(){
  if (!(egressState.candidates || []).length) { showToast('先加至少一条候选出口', 'error'); return; }
  if (!confirm('把当前号池已启用的账号轮流分配到候选出口？会覆盖这些账号现有的出口绑定。')) return;
  const res = await api('/direct-admin/api/codebuddy/proxies/balance', {method: 'POST', body: JSON.stringify({site: currentPoolSite()})});
  showToast('已把 ' + res.assigned + ' 个账号分到 ' + res.proxies + ' 条出口');
  await refreshStatus();
  await refreshEgress();
}

function openEgressModal(id){
  const account = (lastAccounts || []).filter(function(a){ return a.id === id; })[0] || {};
  egressAccountID = id;
  const opts = ['<option value="">直连（本机 IP）</option>'];
  let matched = false;
  (egressState.candidates || []).forEach(function(c){
    const isCurrent = !!account.proxy && account.proxy === c.url;
    if (isCurrent) matched = true;
    opts.push('<option value="' + escapeHtml(c.id) + '"' + (isCurrent ? ' selected' : '') + '>' +
      escapeHtml(c.label || proxyHost(c.url)) + ' · ' + escapeHtml(c.url) + '</option>');
  });
  if (account.proxy && !matched) {
    opts.push('<option value="__current__" selected>当前：' + escapeHtml(account.proxy) + '（不在候选清单）</option>');
  }
  opts.push('<option value="__custom__">自定义 URL…</option>');
  $('egressSelect').innerHTML = opts.join('');
  $('egressCustomRow').hidden = true;
  $('egressCustom').value = '';
  $('egressCheckRaw').hidden = true;
  $('egressTitle').textContent = '设置出口 · ' + (account.label || id);
  $('egressModal').hidden = false;
}

function closeEgressModal(){
  $('egressModal').hidden = true;
  egressAccountID = '';
}

async function saveEgressModal(){
  const id = egressAccountID;
  if (!id) return;
  const value = $('egressSelect').value;
  if (value === '__current__') { closeEgressModal(); return; }
  let body;
  if (value === '') {
    body = {clear: true};
  } else if (value === '__custom__') {
    const url = ($('egressCustom').value || '').trim();
    if (!url) { showToast('自定义出口 URL 不能为空', 'error'); return; }
    body = {proxy: url};
  } else {
    body = {candidateId: value};
  }
  await api('/direct-admin/api/codebuddy/accounts/' + encodeURIComponent(id) + '/proxy', {method: 'POST', body: JSON.stringify(body)});
  closeEgressModal();
  showToast('出口已更新');
  await refreshStatus();
  await refreshEgress();
}

async function checkEgressModal(){
  const value = $('egressSelect').value;
  const box = $('egressCheckRaw');
  box.hidden = false;
  box.textContent = '检测中…（走一次真实 HTTPS 请求）';
  let body;
  if (value === '__custom__') body = {url: ($('egressCustom').value || '').trim()};
  else if (value === '__current__') body = {accountId: egressAccountID};
  else if (value === '') body = {url: ''};
  else body = {candidateId: value};
  try {
    const res = await api('/direct-admin/api/codebuddy/proxies/check', {method: 'POST', body: JSON.stringify(body)});
    if (res.ok) {
      box.textContent = '出口 IP：' + res.ip + ' · ' + res.elapsedMs + 'ms' +
        (res.direct && res.direct.ok ? ('\n本机 IP：' + res.direct.ip + ' · ' + res.direct.elapsedMs + 'ms') : '');
    } else {
      box.textContent = '失败：' + (res.error || '未知错误');
    }
  } catch (e) {
    box.textContent = String(e.message || e);
  }
}

// 点击菜单外部时收起账号的「更多」菜单。
document.addEventListener('click', function(ev){
  document.querySelectorAll('.more[open]').forEach(function(d){ if (!d.contains(ev.target)) d.open = false; });
});

document.addEventListener('keydown', function(e){
  if (e.key !== 'Escape' && e.key !== 'Esc') return;
  if ($('egressModal') && !$('egressModal').hidden) closeEgressModal();
});
if ($('btnEgressAdd')) $('btnEgressAdd').onclick = function(){ addEgressCandidate().catch(function(e){ showToast(String(e.message||e), 'error'); }); };
if ($('btnEgressBalance')) $('btnEgressBalance').onclick = function(){ balanceEgress().catch(function(e){ showToast(String(e.message||e), 'error'); }); };
if ($('egressSelect')) $('egressSelect').onchange = function(){ $('egressCustomRow').hidden = this.value !== '__custom__'; };
if ($('btnEgressSave')) $('btnEgressSave').onclick = function(){ saveEgressModal().catch(function(e){ showToast(String(e.message||e), 'error'); }); };
if ($('btnEgressCheck')) $('btnEgressCheck').onclick = function(){ checkEgressModal().catch(function(e){ showToast(String(e.message||e), 'error'); }); };
if ($('btnEgressClose')) $('btnEgressClose').onclick = closeEgressModal;
if ($('btnEgressCancel')) $('btnEgressCancel').onclick = closeEgressModal;
if ($('egressModal')) $('egressModal').onclick = function(e){ if (e.target === this) closeEgressModal(); };

if ($('btnPoolDomestic')) $('btnPoolDomestic').onclick = function(){ switchPoolSite('domestic').catch(function(e){ showToast(e.message, 'error'); }); };
if ($('btnPoolGlobal')) $('btnPoolGlobal').onclick = function(){ switchPoolSite('global').catch(function(e){ showToast(e.message, 'error'); }); };
if ($('btnProductCodeBuddy')) $('btnProductCodeBuddy').onclick = function(){ switchPoolProduct('codebuddy').catch(function(e){ showToast(e.message, 'error'); }); };
if ($('btnProductWorkBuddy')) $('btnProductWorkBuddy').onclick = function(){ switchPoolProduct('workbuddy').catch(function(e){ showToast(e.message, 'error'); }); };
if ($('site')) $('site').addEventListener('change', function(){ $('site').dataset.userTouched = '1'; });
if ($('btnRefresh')) $('btnRefresh').onclick = function(){
  const btn = $('btnRefresh');
  btn.disabled = true;
  refreshStatus(true).catch(function(e){
    $('statusRaw').textContent = e.message;
    setHealth(false, '刷新失败');
  }).finally(function(){ btn.disabled = false; });
};
if ($('btnRefreshUsage')) $('btnRefreshUsage').onclick = function(){ refreshUsage().catch(function(e){ showToast(e.message, 'error'); }); };
if ($('usageRangeSeg')) $('usageRangeSeg').querySelectorAll('button[data-range]').forEach(function(btn){
  btn.addEventListener('click', function(){
    paintUsageRange(btn.getAttribute('data-range'));
    refreshUsage().catch(function(e){ showToast(e.message, 'error'); });
  });
});
if ($('btnUsagePrev')) $('btnUsagePrev').onclick = function(){
  if (usagePage <= 1) return;
  usagePage--;
  refreshUsage().catch(function(e){ showToast(e.message, 'error'); });
};
if ($('btnUsageNext')) $('btnUsageNext').onclick = function(){
  usagePage++;
  refreshUsage().catch(function(e){ showToast(e.message, 'error'); });
};
function onUsageFilterChange(){
  usageAccount = ($('usageAccountFilter') && $('usageAccountFilter').value) || '';
  usageModel = ($('usageModelFilter') && $('usageModelFilter').value) || '';
  usagePage = 1;
  refreshUsage().catch(function(e){ showToast(e.message, 'error'); });
}
if ($('usageAccountFilter')) $('usageAccountFilter').onchange = onUsageFilterChange;
if ($('usageModelFilter')) $('usageModelFilter').onchange = onUsageFilterChange;
if ($('btnCheckin')) $('btnCheckin').onclick = function(){ runPoolCheckin().catch(function(e){ showToast(e.message, 'error'); }); };
if ($('btnPoolTest')) $('btnPoolTest').onclick = function(){ runPoolChatTest().catch(function(e){ showToast(e.message, 'error'); }); };
if ($('btnModels')) $('btnModels').onclick = function(){ refreshModels().catch(function(e){ $('modelsRaw').textContent = e.message; $('modelChips').innerHTML = '<div class="empty">' + escapeHtml(e.message) + '</div>'; }); };
if ($('btnGoAccounts')) $('btnGoAccounts').onclick = function(){ switchTab('tab-pool'); };
$('btnStart').onclick = function(){ startOAuth().catch(function(e){ $('oauthMsg').textContent = e.message; $('oauthRaw').textContent = e.message; }); };
$('btnPoll').onclick = function(){ pollOAuth().catch(function(e){ $('oauthMsg').textContent = e.message; $('oauthRaw').textContent = e.message; }); };
$('btnRefreshClient').onclick = function(){ refreshClientConfig().catch(function(e){ showToast(e.message, 'error'); }); };
$('btnGenerateKey').onclick = function(){ generateApiKey().catch(function(e){ showToast(e.message, 'error'); }); };
if ($('btnNewBoundKey')) $('btnNewBoundKey').onclick = function(){ generateBoundKey().catch(function(e){ showToast(e.message, 'error'); }); };
$('copyBaseUrl').onclick = function(){ copyText($('openAiBaseUrl').value, 'Base URL', $('copyBaseUrl')); };
$('copyChatUrl').onclick = function(){ copyText($('openAiChatUrl').value, 'Chat Completions', $('copyChatUrl')); };
if ($('copyResponsesUrl')) $('copyResponsesUrl').onclick = function(){ copyText($('openAiResponsesUrl').value, 'Responses', $('copyResponsesUrl')); };
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
