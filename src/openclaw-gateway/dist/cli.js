#!/usr/bin/env node
'use strict';

const http = require('http');
const fs = require('fs');
const { streamChat } = require('./llm');
const { handleAdminAPI } = require('./control');
const pkg = require('../package.json');

function esc(s) {
  return String(s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function dashboardHTML(o) {
  var BT = String.fromCharCode(96);
  return '<!DOCTYPE html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n' +
'<meta name="viewport" content="width=device-width, initial-scale=1">\n' +
'<title>OpenClaw Agent</title>\n' +
'<link rel="preconnect" href="https://fonts.googleapis.com">\n' +
'<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>\n' +
'<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=Outfit:wght@300;400;500;600&display=swap" rel="stylesheet">\n' +
'<style>\n' +
':root {\n' +
'  --bg: #080b12;\n' +
'  --surface-1: #0f1420;\n' +
'  --surface-2: #161d2e;\n' +
'  --surface-3: #1c2538;\n' +
'  --accent: #00d4aa;\n' +
'  --accent-hover: #00f0c0;\n' +
'  --accent-glow: rgba(0,212,170,0.15);\n' +
'  --accent-dim: rgba(0,212,170,0.08);\n' +
'  --user-bg: #0d2847;\n' +
'  --user-border: #1a3d6e;\n' +
'  --text-1: #e4e8f1;\n' +
'  --text-2: #6b7a94;\n' +
'  --text-3: #4a5568;\n' +
'  --border: #1e2a3f;\n' +
'  --code-bg: #0a0e18;\n' +
'  --error: #ff4757;\n' +
'  --success: #2ed573;\n' +
'  --warn: #ffa502;\n' +
'  --font-body: "Outfit", system-ui, -apple-system, "Segoe UI", sans-serif;\n' +
'  --font-mono: "IBM Plex Mono", "SF Mono", "Cascadia Code", "Fira Code", "Consolas", monospace;\n' +
'}\n' +
'*, *::before, *::after { margin:0; padding:0; box-sizing:border-box; }\n' +
'html, body { height:100%; overflow:hidden; }\n' +
'body { font-family:var(--font-body); background:radial-gradient(ellipse at 50% 0%,#0f1a2e 0%,#080b12 50%); color:var(--text-1); display:flex; flex-direction:column; height:100vh; }\n' +
'\n' +
'.header { background:var(--surface-1); padding:14px 24px; display:flex; align-items:center; gap:14px; border-bottom:1px solid var(--border); position:relative; overflow:hidden; flex-shrink:0; }\n' +
'.header::after { content:""; position:absolute; top:0; right:0; width:200px; height:100%; background:repeating-linear-gradient(-45deg,transparent,transparent 8px,rgba(0,212,170,0.03) 8px,rgba(0,212,170,0.03) 9px); pointer-events:none; }\n' +
'.header .logo { color:var(--accent); display:flex; }\n' +
'.header h1 { font-size:17px; font-weight:600; letter-spacing:-0.3px; }\n' +
'.badge { background:var(--accent); color:#080b12; padding:3px 10px; border-radius:100px; font-size:10px; font-weight:600; font-family:var(--font-mono); letter-spacing:0.6px; text-transform:uppercase; display:flex; align-items:center; gap:6px; }\n' +
'.badge::before { content:""; display:block; width:5px; height:5px; background:#080b12; border-radius:50%; animation:pulse 2s ease-in-out infinite; }\n' +
'@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }\n' +
'\n' +
'.tab-bar { background:var(--surface-1); border-bottom:1px solid var(--border); display:flex; gap:0; padding:0 24px; flex-shrink:0; }\n' +
'.tab { padding:10px 20px; font-family:var(--font-mono); font-size:12px; font-weight:500; color:var(--text-3); cursor:pointer; border-bottom:2px solid transparent; transition:all 0.15s; letter-spacing:0.3px; }\n' +
'.tab:hover { color:var(--text-2); }\n' +
'.tab.active { color:var(--accent); border-bottom-color:var(--accent); }\n' +
'\n' +
'.info-bar { padding:7px 24px; background:var(--surface-1); font-family:var(--font-mono); font-size:11px; color:var(--text-3); border-bottom:1px solid var(--border); display:flex; gap:6px; align-items:center; flex-wrap:wrap; flex-shrink:0; }\n' +
'.info-bar .lbl { color:var(--text-2); }\n' +
'.info-bar .val { color:var(--accent); }\n' +
'.info-bar .sep { color:var(--border); margin:0 2px; }\n' +
'\n' +
'.tab-content { display:none; flex:1; min-height:0; flex-direction:column; }\n' +
'.tab-content.active { display:flex; }\n' +
'\n' +
'/* ── Chat tab ── */\n' +
'.chat { flex:1; overflow-y:auto; padding:24px 16px; }\n' +
'.chat::-webkit-scrollbar { width:5px; }\n' +
'.chat::-webkit-scrollbar-track { background:transparent; }\n' +
'.chat::-webkit-scrollbar-thumb { background:var(--border); border-radius:3px; }\n' +
'\n' +
'@keyframes msgIn { from{opacity:0;transform:translateY(10px)} to{opacity:1;transform:translateY(0)} }\n' +
'.msg { max-width:780px; margin:0 auto 16px; animation:msgIn 0.25s ease-out; }\n' +
'.msg-user { display:flex; justify-content:flex-end; }\n' +
'.msg-user .msg-content { background:var(--user-bg); border:1px solid var(--user-border); border-radius:16px 16px 4px 16px; padding:11px 16px; max-width:70%; line-height:1.55; font-size:14px; white-space:pre-wrap; word-break:break-word; }\n' +
'.msg-assistant { display:flex; gap:10px; }\n' +
'.msg-avatar { width:30px; height:30px; background:var(--accent-dim); border-radius:8px; display:flex; align-items:center; justify-content:center; flex-shrink:0; color:var(--accent); }\n' +
'.msg-body { flex:1; min-width:0; }\n' +
'.msg-label { font-size:11px; font-weight:500; color:var(--accent); margin-bottom:4px; font-family:var(--font-mono); letter-spacing:0.3px; }\n' +
'.msg-body .msg-content { background:var(--surface-2); border-left:2px solid var(--accent); border-radius:4px 12px 12px 4px; padding:13px 16px; line-height:1.6; font-size:14px; word-break:break-word; }\n' +
'.msg-content p { margin:0 0 8px; } .msg-content p:last-child { margin:0; }\n' +
'.msg-content strong { color:#fff; font-weight:600; }\n' +
'.msg-content em { color:var(--text-2); font-style:italic; }\n' +
'.msg-content h2,.msg-content h3,.msg-content h4 { color:#fff; margin:12px 0 6px; font-weight:600; } .msg-content h2{font-size:16px;} .msg-content h3{font-size:15px;} .msg-content h4{font-size:14px;}\n' +
'\n' +
'.code-block { background:var(--code-bg); border:1px solid #1a2540; border-radius:8px; margin:10px 0; overflow:hidden; }\n' +
'.code-header { display:flex; justify-content:space-between; align-items:center; padding:5px 12px; background:#0e1525; border-bottom:1px solid #1a2540; font-family:var(--font-mono); font-size:11px; color:var(--text-3); }\n' +
'.copy-btn { background:none; border:1px solid #1a2540; color:var(--text-3); padding:2px 8px; border-radius:4px; cursor:pointer; font-size:10px; font-family:var(--font-mono); transition:all 0.15s; }\n' +
'.copy-btn:hover { border-color:var(--accent); color:var(--accent); }\n' +
'.code-block pre { margin:0; padding:12px; overflow-x:auto; font-family:var(--font-mono); font-size:13px; line-height:1.6; color:#c8d0e0; }\n' +
'.code-block code { font-family:inherit; }\n' +
'.inline-code { background:#1a2235; padding:2px 6px; border-radius:4px; font-family:var(--font-mono); font-size:0.88em; color:var(--accent); }\n' +
'.list-item { display:flex; align-items:flex-start; gap:8px; padding:2px 0; }\n' +
'.list-bullet { color:var(--accent); flex-shrink:0; font-size:12px; line-height:1.6; }\n' +
'.error-text { color:var(--error); }\n' +
'\n' +
'.typing { display:flex; gap:5px; padding:6px 0; }\n' +
'.typing-dot { width:6px; height:6px; background:var(--accent); border-radius:50%; animation:bounce 1.4s ease-in-out infinite; }\n' +
'.typing-dot:nth-child(2){animation-delay:0.2s} .typing-dot:nth-child(3){animation-delay:0.4s}\n' +
'@keyframes bounce { 0%,60%,100%{transform:translateY(0);opacity:0.4} 30%{transform:translateY(-6px);opacity:1} }\n' +
'\n' +
'.cursor { display:inline-block; width:2px; height:1em; background:var(--accent); animation:blink 1s step-end infinite; margin-left:1px; vertical-align:text-bottom; }\n' +
'@keyframes blink { 50%{opacity:0} }\n' +
'\n' +
'.input-area { padding:14px 24px 18px; background:var(--surface-1); border-top:1px solid var(--border); flex-shrink:0; }\n' +
'.input-row { max-width:780px; margin:0 auto; display:flex; gap:10px; }\n' +
'.input-row textarea { flex:1; padding:11px 14px; background:var(--bg); border:1px solid var(--border); border-radius:10px; color:var(--text-1); font-family:var(--font-body); font-size:14px; resize:none; outline:none; min-height:44px; max-height:120px; line-height:1.45; transition:border-color 0.2s,box-shadow 0.2s; }\n' +
'.input-row textarea:focus { border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-glow); }\n' +
'.input-row textarea::placeholder { color:var(--text-3); }\n' +
'.send-btn { width:44px; height:44px; background:var(--accent); border:none; border-radius:10px; cursor:pointer; display:flex; align-items:center; justify-content:center; color:#080b12; transition:background 0.15s; flex-shrink:0; }\n' +
'.send-btn:hover { background:var(--accent-hover); }\n' +
'.send-btn.stop { background:var(--error); }\n' +
'.send-btn.stop:hover { background:#ff6b7a; }\n' +
'.input-hint { max-width:780px; margin:6px auto 0; font-size:11px; color:var(--text-3); font-family:var(--font-mono); }\n' +
'\n' +
'/* ── Admin tab ── */\n' +
'.admin-panel { flex:1; overflow-y:auto; padding:24px 20px; }\n' +
'.grid { max-width:1100px; margin:0 auto; display:grid; grid-template-columns:repeat(auto-fit, minmax(340px, 1fr)); gap:16px; }\n' +
'.card { background:var(--surface-1); border:1px solid var(--border); border-radius:10px; overflow:hidden; }\n' +
'.card-header { padding:12px 16px; border-bottom:1px solid var(--border); display:flex; align-items:center; gap:8px; }\n' +
'.card-header h2 { font-size:13px; font-weight:600; letter-spacing:0.3px; }\n' +
'.card-header .icon { color:var(--accent); font-size:14px; }\n' +
'.card-body { padding:16px; }\n' +
'.card.full-width { grid-column: 1 / -1; }\n' +
'\n' +
'.kv { display:grid; grid-template-columns:auto 1fr; gap:4px 16px; font-size:13px; }\n' +
'.kv .label { color:var(--text-2); font-family:var(--font-mono); font-size:12px; white-space:nowrap; }\n' +
'.kv .value { color:var(--text-1); font-family:var(--font-mono); font-size:12px; word-break:break-all; }\n' +
'.kv .value.accent { color:var(--accent); }\n' +
'.kv .value.success { color:var(--success); }\n' +
'.kv .value.error { color:var(--error); }\n' +
'.kv .value.warn { color:var(--warn); }\n' +
'\n' +
'.btn { padding:8px 16px; border:1px solid var(--border); border-radius:6px; background:var(--surface-2); color:var(--text-1); font-family:var(--font-mono); font-size:12px; cursor:pointer; transition:all 0.15s; }\n' +
'.btn:hover { border-color:var(--accent); color:var(--accent); }\n' +
'.btn.primary { background:var(--accent); color:#080b12; border-color:var(--accent); font-weight:600; }\n' +
'.btn.primary:hover { background:var(--accent-hover); }\n' +
'.btn.danger { border-color:var(--error); color:var(--error); }\n' +
'.btn.danger:hover { background:var(--error); color:#fff; }\n' +
'.btn:disabled { opacity:0.5; cursor:not-allowed; }\n' +
'.btn-row { display:flex; gap:8px; margin-top:12px; flex-wrap:wrap; }\n' +
'\n' +
'.log-box { background:var(--code-bg); border:1px solid #1a2540; border-radius:6px; padding:12px; font-family:var(--font-mono); font-size:11px; line-height:1.6; color:#c8d0e0; max-height:300px; overflow:auto; white-space:pre-wrap; word-break:break-all; }\n' +
'.log-controls { display:flex; gap:8px; align-items:center; margin-bottom:10px; }\n' +
'.log-controls select { padding:4px 8px; background:var(--surface-2); border:1px solid var(--border); border-radius:4px; color:var(--text-1); font-family:var(--font-mono); font-size:11px; }\n' +
'\n' +
'.test-result { margin-top:12px; padding:10px; border-radius:6px; font-family:var(--font-mono); font-size:12px; display:none; }\n' +
'.test-result.show { display:block; }\n' +
'.test-result.pass { background:rgba(46,213,115,0.1); border:1px solid rgba(46,213,115,0.3); color:var(--success); }\n' +
'.test-result.fail { background:rgba(255,71,87,0.1); border:1px solid rgba(255,71,87,0.3); color:var(--error); }\n' +
'\n' +
'.spinner { display:inline-block; width:12px; height:12px; border:2px solid var(--border); border-top-color:var(--accent); border-radius:50%; animation:spin 0.8s linear infinite; margin-right:6px; vertical-align:middle; }\n' +
'@keyframes spin { to { transform:rotate(360deg); } }\n' +
'\n' +
'@media (max-width:720px) { .grid { grid-template-columns:1fr; } }\n' +
'</style>\n' +
'</head>\n' +
'<body>\n' +
'\n' +
'<header class="header">\n' +
'  <div class="logo"><svg width="24" height="24" viewBox="0 0 24 24" fill="none"><path d="M7 3C6 8 5 13 8 21" stroke="currentColor" stroke-width="2" stroke-linecap="round"/><path d="M12 3C12 8 12 13 12 21" stroke="currentColor" stroke-width="2" stroke-linecap="round"/><path d="M17 3C18 8 19 13 16 21" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></div>\n' +
'  <h1>OpenClaw Agent</h1>\n' +
'  <span class="badge">Online</span>\n' +
'</header>\n' +
'\n' +
'<nav class="tab-bar">\n' +
'  <div class="tab active" data-tab="chat">Chat</div>\n' +
'  <div class="tab" data-tab="admin">Admin</div>\n' +
'</nav>\n' +
'\n' +
'<div class="info-bar">\n' +
'  <span class="lbl">Instance</span><span class="val">' + esc(o.instanceId) + '</span><span class="sep">/</span>\n' +
'  <span class="lbl">Plan</span><span class="val">' + esc(o.planName) + '</span><span class="sep">/</span>\n' +
'  <span class="lbl">Owner</span><span class="val">' + esc(o.owner) + '</span><span class="sep">/</span>\n' +
'  <span class="val">v' + esc(o.version) + '</span>\n' +
'</div>\n' +
'\n' +
'<!-- Chat Tab -->\n' +
'<div class="tab-content active" id="tab-chat">\n' +
'  <main class="chat" id="chat"></main>\n' +
'  <div class="input-area">\n' +
'    <div class="input-row">\n' +
'      <textarea id="input" placeholder="Send a message..." rows="1"></textarea>\n' +
'      <button class="send-btn" id="send-btn" aria-label="Send">\n' +
'        <svg id="send-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2L11 13"/><path d="M22 2L15 22L11 13L2 9L22 2Z"/></svg>\n' +
'        <svg id="stop-icon" width="18" height="18" viewBox="0 0 24 24" fill="currentColor" style="display:none"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>\n' +
'      </button>\n' +
'    </div>\n' +
'    <div class="input-hint">Enter to send &middot; Shift+Enter for new line</div>\n' +
'  </div>\n' +
'</div>\n' +
'\n' +
'<!-- Admin Tab -->\n' +
'<div class="tab-content" id="tab-admin">\n' +
'  <div class="admin-panel">\n' +
'    <div class="grid">\n' +
'      <div class="card">\n' +
'        <div class="card-header"><span class="icon">&#9679;</span><h2>Instance Status</h2></div>\n' +
'        <div class="card-body">\n' +
'          <div class="kv" id="status-kv"><span class="label">Loading...</span><span class="value"></span></div>\n' +
'          <div class="btn-row"><button class="btn" onclick="loadStatus()">Refresh</button></div>\n' +
'        </div>\n' +
'      </div>\n' +
'      <div class="card">\n' +
'        <div class="card-header"><span class="icon">&#9889;</span><h2>LLM Health Test</h2></div>\n' +
'        <div class="card-body">\n' +
'          <p style="font-size:12px;color:var(--text-2);margin-bottom:8px;">Send a test prompt to verify LLM connectivity.</p>\n' +
'          <button class="btn primary" id="llm-test-btn" onclick="testLLM()">Run Test</button>\n' +
'          <div class="test-result" id="llm-result"></div>\n' +
'        </div>\n' +
'      </div>\n' +
'      <div class="card full-width">\n' +
'        <div class="card-header"><span class="icon">&#9776;</span><h2>Log Viewer</h2></div>\n' +
'        <div class="card-body">\n' +
'          <div class="log-controls">\n' +
'            <select id="log-file"><option value="stdout">stdout</option><option value="stderr">stderr</option></select>\n' +
'            <select id="log-lines"><option value="100">100 lines</option><option value="200" selected>200 lines</option><option value="500">500 lines</option></select>\n' +
'            <button class="btn" onclick="loadLogs()">Fetch Logs</button>\n' +
'          </div>\n' +
'          <div class="log-box" id="log-content">Click "Fetch Logs" to load...</div>\n' +
'        </div>\n' +
'      </div>\n' +
'      <div class="card">\n' +
'        <div class="card-header"><span class="icon">&#9881;</span><h2>Process Management</h2></div>\n' +
'        <div class="card-body">\n' +
'          <p style="font-size:12px;color:var(--text-2);margin-bottom:8px;">Restart the gateway process (BPM will respawn it).</p>\n' +
'          <button class="btn danger" onclick="restartProcess()">Restart Process</button>\n' +
'        </div>\n' +
'      </div>\n' +
'      <div class="card">\n' +
'        <div class="card-header"><span class="icon">&#9733;</span><h2>Request Stats</h2></div>\n' +
'        <div class="card-body">\n' +
'          <div class="kv" id="stats-kv"><span class="label">Loading...</span><span class="value"></span></div>\n' +
'          <div class="btn-row"><button class="btn" onclick="loadStats()">Refresh</button></div>\n' +
'        </div>\n' +
'      </div>\n' +
'    </div>\n' +
'  </div>\n' +
'</div>\n' +
'\n' +
'<script>\n' +
'(function() {\n' +
'  /* ── Tab switching ── */\n' +
'  var tabs = document.querySelectorAll(".tab");\n' +
'  var panes = document.querySelectorAll(".tab-content");\n' +
'  for (var t = 0; t < tabs.length; t++) {\n' +
'    tabs[t].addEventListener("click", function() {\n' +
'      for (var i = 0; i < tabs.length; i++) { tabs[i].classList.remove("active"); }\n' +
'      for (var i = 0; i < panes.length; i++) { panes[i].classList.remove("active"); }\n' +
'      this.classList.add("active");\n' +
'      document.getElementById("tab-" + this.dataset.tab).classList.add("active");\n' +
'      if (this.dataset.tab === "admin" && !adminLoaded) { adminLoaded = true; loadStatus(); loadStats(); }\n' +
'      if (this.dataset.tab === "chat") { inputEl.focus(); }\n' +
'    });\n' +
'  }\n' +
'  var adminLoaded = false;\n' +
'\n' +
'  /* ── Chat ── */\n' +
'  var chatEl = document.getElementById("chat");\n' +
'  var inputEl = document.getElementById("input");\n' +
'  var sendBtn = document.getElementById("send-btn");\n' +
'  var sendIcon = document.getElementById("send-icon");\n' +
'  var stopIcon = document.getElementById("stop-icon");\n' +
'  var messages = [];\n' +
'  var isStreaming = false;\n' +
'  var abortCtrl = null;\n' +
'\n' +
'  var BT = String.fromCharCode(96);\n' +
'  var TRIPLE = BT + BT + BT;\n' +
'\n' +
'  function escH(s) {\n' +
'    return s.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;");\n' +
'  }\n' +
'\n' +
'  function renderMd(text) {\n' +
'    var html = escH(text);\n' +
'    var blocks = [];\n' +
'    var idx = 0;\n' +
'\n' +
'    var cbRe = new RegExp(TRIPLE + "(\\\\w*)\\\\n([\\\\s\\\\S]*?)" + TRIPLE, "g");\n' +
'    html = html.replace(cbRe, function(m, lang, code) {\n' +
'      var k = "%%CB" + idx + "%%";\n' +
'      var hdr = lang || "code";\n' +
'      blocks.push({k: k, h: \'<div class="code-block"><div class="code-header"><span>\' + escH(hdr) + \'</span><button class="copy-btn">Copy</button></div><pre><code>\' + code + \'</code></pre></div>\'});\n' +
'      idx++;\n' +
'      return k;\n' +
'    });\n' +
'\n' +
'    var icRe = new RegExp(BT + "([^" + BT + "\\\\n]+)" + BT, "g");\n' +
'    html = html.replace(icRe, function(m, code) {\n' +
'      var k = "%%IC" + idx + "%%";\n' +
'      blocks.push({k: k, h: \'<code class="inline-code">\' + code + \'</code>\'});\n' +
'      idx++;\n' +
'      return k;\n' +
'    });\n' +
'\n' +
'    html = html.replace(/\\*\\*(.+?)\\*\\*/g, "<strong>$1</strong>");\n' +
'    html = html.replace(/\\*(.+?)\\*/g, "<em>$1</em>");\n' +
'    html = html.replace(/^### (.+)$/gm, "<h4>$1</h4>");\n' +
'    html = html.replace(/^## (.+)$/gm, "<h3>$1</h3>");\n' +
'    html = html.replace(/^# (.+)$/gm, "<h2>$1</h2>");\n' +
'    html = html.replace(/^- (.+)$/gm, \'<div class="list-item"><span class="list-bullet">&#9656;</span>$1</div>\');\n' +
'    html = html.replace(/^\\d+\\. (.+)$/gm, \'<div class="list-item"><span class="list-bullet">&#9656;</span>$1</div>\');\n' +
'    html = html.replace(/\\n\\n/g, "<br><br>");\n' +
'    html = html.replace(/\\n/g, "<br>");\n' +
'\n' +
'    for (var i = 0; i < blocks.length; i++) {\n' +
'      html = html.replace(blocks[i].k, blocks[i].h);\n' +
'    }\n' +
'    return html;\n' +
'  }\n' +
'\n' +
'  var avatarSvg = \'<svg width="16" height="16" viewBox="0 0 24 24" fill="none"><path d="M7 3C6 8 5 13 8 21" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/><path d="M12 3C12 8 12 13 12 21" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/><path d="M17 3C18 8 19 13 16 21" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/></svg>\';\n' +
'\n' +
'  function addMsg(role, html, isRaw) {\n' +
'    var div = document.createElement("div");\n' +
'    div.className = "msg msg-" + role;\n' +
'    if (role === "assistant") {\n' +
'      div.innerHTML = \'<div class="msg-avatar">\' + avatarSvg + \'</div><div class="msg-body"><div class="msg-label">OpenClaw</div><div class="msg-content">\' + (isRaw ? html : renderMd(html)) + \'</div></div>\';\n' +
'    } else {\n' +
'      div.innerHTML = \'<div class="msg-content">\' + escH(html) + \'</div>\';\n' +
'    }\n' +
'    chatEl.appendChild(div);\n' +
'    scrollBottom();\n' +
'    return div;\n' +
'  }\n' +
'\n' +
'  function showTyping() {\n' +
'    var div = document.createElement("div");\n' +
'    div.className = "msg msg-assistant";\n' +
'    div.innerHTML = \'<div class="msg-avatar">\' + avatarSvg + \'</div><div class="msg-body"><div class="msg-label">OpenClaw</div><div class="msg-content"><div class="typing"><div class="typing-dot"></div><div class="typing-dot"></div><div class="typing-dot"></div></div></div></div>\';\n' +
'    chatEl.appendChild(div);\n' +
'    scrollBottom();\n' +
'    return div;\n' +
'  }\n' +
'\n' +
'  function scrollBottom() {\n' +
'    chatEl.scrollTop = chatEl.scrollHeight;\n' +
'  }\n' +
'\n' +
'  function setStreamMode(on) {\n' +
'    isStreaming = on;\n' +
'    sendIcon.style.display = on ? "none" : "";\n' +
'    stopIcon.style.display = on ? "" : "none";\n' +
'    sendBtn.classList.toggle("stop", on);\n' +
'    sendBtn.setAttribute("aria-label", on ? "Stop" : "Send");\n' +
'    inputEl.disabled = on;\n' +
'  }\n' +
'\n' +
'  function send() {\n' +
'    if (isStreaming) { if (abortCtrl) abortCtrl.abort(); return; }\n' +
'    var text = inputEl.value.trim();\n' +
'    if (!text) return;\n' +
'    messages.push({role: "user", content: text});\n' +
'    addMsg("user", text);\n' +
'    inputEl.value = "";\n' +
'    inputEl.style.height = "auto";\n' +
'    setStreamMode(true);\n' +
'    var typingEl = showTyping();\n' +
'\n' +
'    abortCtrl = new AbortController();\n' +
'    fetch("/api/chat", {\n' +
'      method: "POST",\n' +
'      headers: {"Content-Type": "application/json"},\n' +
'      body: JSON.stringify({messages: messages}),\n' +
'      signal: abortCtrl.signal\n' +
'    }).then(function(res) {\n' +
'      if (!res.ok) throw new Error("HTTP " + res.status);\n' +
'      typingEl.remove();\n' +
'      var msgEl = addMsg("assistant", "", true);\n' +
'      var contentEl = msgEl.querySelector(".msg-content");\n' +
'      var fullText = "";\n' +
'      var reader = res.body.getReader();\n' +
'      var decoder = new TextDecoder();\n' +
'      var buf = "";\n' +
'\n' +
'      function finish() {\n' +
'        contentEl.innerHTML = renderMd(fullText);\n' +
'        if (fullText) messages.push({role: "assistant", content: fullText});\n' +
'        setStreamMode(false);\n' +
'        abortCtrl = null;\n' +
'        scrollBottom();\n' +
'        inputEl.focus();\n' +
'      }\n' +
'\n' +
'      function pump(result) {\n' +
'        if (result.done) { finish(); return; }\n' +
'        buf += decoder.decode(result.value, {stream: true});\n' +
'        var lines = buf.split("\\n");\n' +
'        buf = lines.pop();\n' +
'        for (var i = 0; i < lines.length; i++) {\n' +
'          var ln = lines[i].trim();\n' +
'          if (ln.indexOf("data: ") !== 0) continue;\n' +
'          try {\n' +
'            var d = JSON.parse(ln.substring(6));\n' +
'            if (d.token) {\n' +
'              fullText += d.token;\n' +
'              contentEl.innerHTML = renderMd(fullText) + \'<span class="cursor"></span>\';\n' +
'              scrollBottom();\n' +
'            }\n' +
'            if (d.done) { finish(); return; }\n' +
'            if (d.error) {\n' +
'              contentEl.innerHTML = \'<span class="error-text">\' + escH(d.error) + \'</span>\';\n' +
'              setStreamMode(false); abortCtrl = null;\n' +
'              return;\n' +
'            }\n' +
'          } catch(e) {}\n' +
'        }\n' +
'        reader.read().then(pump);\n' +
'      }\n' +
'      reader.read().then(pump);\n' +
'    }).catch(function(err) {\n' +
'      typingEl.remove();\n' +
'      if (err.name !== "AbortError") {\n' +
'        addMsg("assistant", \'<span class="error-text">Connection error: \' + escH(err.message) + \'</span>\', true);\n' +
'      }\n' +
'      setStreamMode(false);\n' +
'      abortCtrl = null;\n' +
'    });\n' +
'  }\n' +
'\n' +
'  chatEl.addEventListener("click", function(e) {\n' +
'    if (e.target.classList.contains("copy-btn")) {\n' +
'      var codeEl = e.target.closest(".code-block").querySelector("code");\n' +
'      if (codeEl && navigator.clipboard) {\n' +
'        navigator.clipboard.writeText(codeEl.textContent).then(function() {\n' +
'          e.target.textContent = "Copied!";\n' +
'          setTimeout(function() { e.target.textContent = "Copy"; }, 2000);\n' +
'        });\n' +
'      }\n' +
'    }\n' +
'  });\n' +
'\n' +
'  inputEl.addEventListener("input", function() {\n' +
'    this.style.height = "auto";\n' +
'    this.style.height = Math.min(this.scrollHeight, 120) + "px";\n' +
'  });\n' +
'\n' +
'  inputEl.addEventListener("keydown", function(e) {\n' +
'    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); }\n' +
'  });\n' +
'\n' +
'  sendBtn.addEventListener("click", send);\n' +
'\n' +
'  addMsg("assistant", "Hello! I am your OpenClaw AI agent. How can I help you today?");\n' +
'  inputEl.focus();\n' +
'\n' +
'  /* ── Admin panel ── */\n' +
'  function api(method, path) {\n' +
'    return fetch(path, { method: method, credentials: "same-origin" }).then(function(r) { return r.json(); });\n' +
'  }\n' +
'\n' +
'  function kvHTML(pairs) {\n' +
'    var h = "";\n' +
'    for (var i = 0; i < pairs.length; i++) {\n' +
'      var cls = pairs[i][2] || "";\n' +
'      h += \'<span class="label">\' + pairs[i][0] + \'</span><span class="value \' + cls + \'">\' + pairs[i][1] + \'</span>\';\n' +
'    }\n' +
'    return h;\n' +
'  }\n' +
'\n' +
'  function fmtUptime(s) {\n' +
'    if (s < 60) return s + "s";\n' +
'    if (s < 3600) return Math.floor(s/60) + "m " + (s%60) + "s";\n' +
'    var h = Math.floor(s/3600); var m = Math.floor((s%3600)/60);\n' +
'    return h + "h " + m + "m";\n' +
'  }\n' +
'\n' +
'  window.loadStatus = function() {\n' +
'    api("GET", "/api/status").then(function(d) {\n' +
'      document.getElementById("status-kv").innerHTML = kvHTML([\n' +
'        ["Version", "v" + d.version, "accent"],\n' +
'        ["Uptime", fmtUptime(d.uptime), ""],\n' +
'        ["Instance", d.instance.id, ""],\n' +
'        ["Owner", d.instance.owner, ""],\n' +
'        ["Plan", d.instance.plan, ""],\n' +
'        ["LLM Provider", d.llm.provider, "accent"],\n' +
'        ["LLM Endpoint", d.llm.endpoint, ""],\n' +
'        ["LLM Model", d.llm.model, ""],\n' +
'        ["Memory (RSS)", d.memory.rss_mb + " MB", ""],\n' +
'        ["Heap Used", d.memory.heap_used_mb + " MB", ""],\n' +
'        ["Node.js", d.node_version, ""]\n' +
'      ]);\n' +
'    }).catch(function(e) {\n' +
'      document.getElementById("status-kv").innerHTML = \'<span class="label">Error</span><span class="value error">\' + e.message + \'</span>\';\n' +
'    });\n' +
'  };\n' +
'\n' +
'  window.testLLM = function() {\n' +
'    var btn = document.getElementById("llm-test-btn");\n' +
'    var result = document.getElementById("llm-result");\n' +
'    btn.disabled = true;\n' +
'    btn.innerHTML = \'<span class="spinner"></span>Testing...\';\n' +
'    result.className = "test-result";\n' +
'\n' +
'    api("POST", "/api/llm-test").then(function(d) {\n' +
'      btn.disabled = false;\n' +
'      btn.textContent = "Run Test";\n' +
'      if (d.success) {\n' +
'        result.className = "test-result show pass";\n' +
'        result.textContent = "PASS (" + d.latency_ms + "ms) — " + d.response;\n' +
'      } else {\n' +
'        result.className = "test-result show fail";\n' +
'        result.textContent = "FAIL (" + d.latency_ms + "ms) — " + d.error;\n' +
'      }\n' +
'    }).catch(function(e) {\n' +
'      btn.disabled = false;\n' +
'      btn.textContent = "Run Test";\n' +
'      result.className = "test-result show fail";\n' +
'      result.textContent = "Error: " + e.message;\n' +
'    });\n' +
'  };\n' +
'\n' +
'  window.loadLogs = function() {\n' +
'    var file = document.getElementById("log-file").value;\n' +
'    var lines = document.getElementById("log-lines").value;\n' +
'    var box = document.getElementById("log-content");\n' +
'    box.textContent = "Loading...";\n' +
'    api("GET", "/api/logs?file=" + encodeURIComponent(file) + "&lines=" + lines).then(function(d) {\n' +
'      box.textContent = d.content || "(empty)";\n' +
'      box.scrollTop = box.scrollHeight;\n' +
'    }).catch(function(e) {\n' +
'      box.textContent = "Error: " + e.message;\n' +
'    });\n' +
'  };\n' +
'\n' +
'  window.restartProcess = function() {\n' +
'    if (!confirm("Restart the gateway process? BPM will respawn it.")) return;\n' +
'    api("POST", "/api/restart").then(function() {\n' +
'      document.body.innerHTML = \'<div style="text-align:center;padding:60px;font-family:var(--font-mono);color:var(--accent);">Restarting... page will reload shortly.</div>\';\n' +
'      setTimeout(function() { location.reload(); }, 5000);\n' +
'    });\n' +
'  };\n' +
'\n' +
'  window.loadStats = function() {\n' +
'    api("GET", "/api/stats").then(function(d) {\n' +
'      document.getElementById("stats-kv").innerHTML = kvHTML([\n' +
'        ["Requests", String(d.request_count), "accent"],\n' +
'        ["Uptime", fmtUptime(d.uptime), ""]\n' +
'      ]);\n' +
'    }).catch(function(e) {\n' +
'      document.getElementById("stats-kv").innerHTML = \'<span class="label">Error</span><span class="value error">\' + e.message + \'</span>\';\n' +
'    });\n' +
'  };\n' +
'})();\n' +
'</script>\n' +
'</body>\n' +
'</html>';
}

function loginPageHTML(o) {
  return '<!DOCTYPE html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n' +
'<meta name="viewport" content="width=device-width, initial-scale=1">\n' +
'<title>OpenClaw Agent — Authenticate</title>\n' +
'<link rel="preconnect" href="https://fonts.googleapis.com">\n' +
'<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>\n' +
'<link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;600;700&family=IBM+Plex+Mono:wght@400;500&display=swap" rel="stylesheet">\n' +
'<style>\n' +
'*{margin:0;padding:0;box-sizing:border-box}\n' +
':root{--bg:#0c1222;--surface:#151d32;--border:#1e2a45;--text:#e2e8f0;--text-dim:#8892a6;--accent:#3b82f6;--accent-glow:rgba(59,130,246,.15)}\n' +
'body{font-family:"Outfit",sans-serif;background:var(--bg);color:var(--text);min-height:100vh;display:flex;align-items:center;justify-content:center}\n' +
'.card{background:var(--surface);border:1px solid var(--border);border-radius:16px;padding:48px 40px;max-width:420px;width:90%;text-align:center}\n' +
'.logo{font-size:28px;font-weight:700;margin-bottom:6px;letter-spacing:-.5px}\n' +
'.logo span{color:var(--accent)}\n' +
'.sub{color:var(--text-dim);font-size:13px;margin-bottom:32px;font-family:"IBM Plex Mono",monospace}\n' +
'.field{position:relative;margin-bottom:20px}\n' +
'.field input{width:100%;background:var(--bg);border:1px solid var(--border);border-radius:10px;padding:14px 16px;color:var(--text);font-family:"IBM Plex Mono",monospace;font-size:14px;outline:none;transition:border-color .2s}\n' +
'.field input:focus{border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-glow)}\n' +
'.field input::placeholder{color:var(--text-dim)}\n' +
'.btn{width:100%;padding:14px;background:var(--accent);color:#fff;border:none;border-radius:10px;font-family:"Outfit",sans-serif;font-size:15px;font-weight:600;cursor:pointer;transition:opacity .2s}\n' +
'.btn:hover{opacity:.9}\n' +
'.err{color:#f87171;font-size:13px;margin-top:12px;display:none;font-family:"IBM Plex Mono",monospace}\n' +
'.meta{margin-top:28px;padding-top:20px;border-top:1px solid var(--border);color:var(--text-dim);font-size:11px;font-family:"IBM Plex Mono",monospace}\n' +
'</style>\n</head>\n<body>\n' +
'<div class="card">\n' +
'<div class="logo">Open<span>Claw</span> Agent</div>\n' +
'<div class="sub">Authentication required</div>\n' +
'<form id="f" onsubmit="return go()">\n' +
'<div class="field"><input id="t" type="password" placeholder="Enter access token" autocomplete="off" autofocus></div>\n' +
'<button class="btn" type="submit">Authenticate</button>\n' +
'<div class="err" id="e">Invalid token. Check your service key credentials.</div>\n' +
'</form>\n' +
'<div class="meta">Instance ' + esc(o.instanceId) + ' &middot; v' + esc(o.version) + '</div>\n' +
'</div>\n' +
'<script>\n' +
'function go(){var t=document.getElementById("t").value.trim();if(!t){document.getElementById("e").style.display="block";return false}window.location.href="/?token="+encodeURIComponent(t);return false}\n' +
'</script>\n' +
'</body>\n</html>';
}

/* ── Commands ── */

var cmd = process.argv[2];

if (cmd === 'gateway') {
  var cfgIdx = process.argv.indexOf('--config');
  var cfgPath = cfgIdx > -1 ? process.argv[cfgIdx + 1] : null;
  var config = {};
  if (cfgPath && fs.existsSync(cfgPath)) {
    try { config = JSON.parse(fs.readFileSync(cfgPath, 'utf8')); } catch (e) { console.error('Config parse error:', e.message); }
  }

  var gwPort = (config.gateway && config.gateway.port) || 18789;
  var gwBind = (config.gateway && config.gateway.bind_address) || '0.0.0.0';
  var webchatPort = (config.webchat && config.webchat.port) || 8080;
  var instanceId = (config.instance && config.instance.id) || 'unknown';
  var owner = (config.instance && config.instance.owner) || 'unknown';
  var planName = (config.instance && config.instance.plan) || 'unknown';
  var llmProvider = (config.llm && config.llm.provider) || 'none';
  var llmEndpoint = (config.llm && config.llm.genai && config.llm.genai.endpoint) || 'not set';

  var ssoEnabled = config.sso && config.sso.enabled;
  console.log('SSO: enabled=' + !!ssoEnabled + ' webchatPort=' + webchatPort);

  var html = dashboardHTML({ instanceId: instanceId, planName: planName, owner: owner, version: pkg.version });
  var requestCount = 0;
  var startedAt = Date.now();
  var gwToken = (config.gateway && config.gateway.token) || '';

  // Unified auth: Bearer header, ?token= param (sets cookie), or oc_token cookie.
  // SSO (oauth2-proxy) handles auth upstream when enabled.
  function checkAuth(req, res) {
    if (ssoEnabled) return true;
    if (!gwToken) return true; // no token configured = open access

    // Check Authorization: Bearer <token>
    var authHeader = req.headers['authorization'] || '';
    if (authHeader.indexOf('Bearer ') === 0 && authHeader.slice(7) === gwToken) return true;

    // Check ?token= query param — set HttpOnly cookie for subsequent requests
    var urlObj;
    try { urlObj = new URL(req.url, 'http://localhost'); } catch (e) { /* ignore */ }
    if (urlObj) {
      var qToken = urlObj.searchParams.get('token');
      if (qToken === gwToken) {
        res.setHeader('Set-Cookie', 'oc_token=' + gwToken + '; HttpOnly; Path=/; SameSite=Strict');
        return true;
      }
    }

    // Check oc_token cookie
    var cookies = (req.headers.cookie || '').split(';');
    for (var i = 0; i < cookies.length; i++) {
      var parts = cookies[i].trim().split('=');
      if (parts[0] === 'oc_token' && parts.slice(1).join('=') === gwToken) return true;
    }

    return false;
  }

  var systemPrompt = 'You are OpenClaw, an AI agent running on instance "' + instanceId +
    '" (plan: ' + planName + ', owner: ' + owner +
    '). Be helpful, clear, and concise. Use markdown formatting when it improves readability.';

  // Unified HTTP server
  var webchatServer = http.createServer(function (req, res) {
    // Parse pathname for routing
    var pathname;
    try { pathname = new URL(req.url, 'http://localhost').pathname; } catch (e) { pathname = req.url.split('?')[0]; }

    if (pathname === '/healthz' || pathname === '/health') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ status: 'ok', version: pkg.version, instance: instanceId }));
    } else if (pathname === '/api/chat' && req.method === 'POST') {
      if (!checkAuth(req, res)) {
        res.writeHead(401, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: 'Unauthorized' }));
        return;
      }
      requestCount++;
      handleChat(req, res, config, systemPrompt);
    } else if (pathname.indexOf('/api/') === 0) {
      // Admin API endpoints (status, stats, llm-test, logs, restart)
      if (!checkAuth(req, res)) {
        res.writeHead(401, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: 'Unauthorized' }));
        return;
      }
      var handled = handleAdminAPI(req, res, pathname, config, { requestCount: requestCount, startedAt: startedAt });
      if (handled === false) {
        res.writeHead(404, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ error: 'Unknown API endpoint' }));
      }
    } else {
      // Dashboard (root and any other path)
      if (!checkAuth(req, res)) {
        res.writeHead(401, { 'Content-Type': 'text/html; charset=utf-8' });
        res.end(loginPageHTML({ instanceId: instanceId, version: pkg.version }));
        return;
      }
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end(html);
    }
  });
  webchatServer.listen(webchatPort, gwBind, function () {
    console.log('OpenClaw Dashboard listening on ' + gwBind + ':' + webchatPort);
  });

  // Gateway status server
  var gwServer = http.createServer(function (req, res) {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ gateway: 'openclaw', version: pkg.version, status: 'ready' }));
  });
  gwServer.listen(gwPort, gwBind, function () {
    console.log('OpenClaw Gateway listening on ' + gwBind + ':' + gwPort);
    console.log('OpenClaw Agent v' + pkg.version + ' started');
    console.log('LLM: provider=' + llmProvider + ' endpoint=' + llmEndpoint);
  });

  function shutdown() {
    console.log('Shutting down...');
    webchatServer.close();
    gwServer.close();
    process.exit(0);
  }
  process.on('SIGTERM', shutdown);
  process.on('SIGINT', shutdown);

} else if (cmd === 'registry') {
  var sub = process.argv[3];
  if (sub === 'serve') {
    var portIdx = process.argv.indexOf('--port');
    var port = portIdx > -1 ? process.argv[portIdx + 1] : '8081';
    http.createServer(function (req, res) {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end('{"skills":[]}');
    }).listen(port, function () { console.log('OpenClaw registry on port ' + port); });
  } else if (sub === 'sync') {
    console.log('OpenClaw registry sync complete (placeholder)');
  } else if (sub === 'verify-signature') {
    console.log('Signature OK (placeholder)');
  }

} else {
  console.log('OpenClaw CLI v' + pkg.version);
}

/* ── Chat handler ── */

function handleChat(req, res, config, systemPrompt) {
  var body = '';
  req.on('data', function (chunk) { body += chunk; });
  req.on('end', function () {
    var data;
    try {
      data = JSON.parse(body);
    } catch (e) {
      res.writeHead(400, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: 'Invalid JSON: ' + e.message }));
      return;
    }

    var msgs = data.messages || [];
    var fullMessages = [{ role: 'system', content: systemPrompt }].concat(msgs);

    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no'
    });
    // Flush headers immediately with SSE comment
    res.write(':ok\n\n');

    var done = false;

    streamChat(
      config,
      fullMessages,
      function onToken(token) {
        if (!done) {
          res.write('data: ' + JSON.stringify({ token: token }) + '\n\n');
        }
      },
      function onDone() {
        if (!done) {
          done = true;
          res.write('data: ' + JSON.stringify({ done: true }) + '\n\n');
          res.end();
        }
      },
      function onError(err) {
        console.error('[chat] LLM error:', err.message);
        if (!done) {
          done = true;
          res.write('data: ' + JSON.stringify({ error: err.message }) + '\n\n');
          res.end();
        }
      }
    );

    res.on('close', function () { done = true; });
  });
}
