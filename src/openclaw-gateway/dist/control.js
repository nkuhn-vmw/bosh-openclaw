'use strict';

const fs = require('fs');
const { streamChat } = require('./llm');

function esc(s) {
  return String(s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/**
 * Token-based auth middleware for control UI.
 * Checks Authorization header, ?token= query param (sets cookie), or control_token cookie.
 * Returns true if authenticated, sends 401 and returns false if not.
 */
function createAuthMiddleware(config) {
  var controlConfig = config.control_ui || {};
  var token = (config.instance && config.instance.gateway_token) || '';

  return function controlAuth(req, res) {
    if (controlConfig.require_auth === false) return true;
    if (!token) {
      res.writeHead(401, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: 'No gateway token configured' }));
      return false;
    }

    // Check Authorization: Bearer <token>
    var authHeader = req.headers['authorization'] || '';
    if (authHeader.indexOf('Bearer ') === 0 && authHeader.slice(7) === token) {
      return true;
    }

    // Check ?token= query param — set HttpOnly cookie for subsequent requests
    var urlObj;
    try { urlObj = new URL(req.url, 'http://localhost'); } catch (e) { /* ignore */ }
    if (urlObj) {
      var qToken = urlObj.searchParams.get('token');
      if (qToken === token) {
        res.setHeader('Set-Cookie', 'control_token=' + token + '; HttpOnly; Path=/control; SameSite=Strict');
        return true;
      }
    }

    // Check control_token cookie
    var cookies = (req.headers.cookie || '').split(';');
    for (var i = 0; i < cookies.length; i++) {
      var parts = cookies[i].trim().split('=');
      if (parts[0] === 'control_token' && parts.slice(1).join('=') === token) {
        return true;
      }
    }

    res.writeHead(401, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: 'Unauthorized — provide token via Authorization header, ?token= param, or cookie' }));
    return false;
  };
}

/**
 * Handle control API requests.
 * @param {http.IncomingMessage} req
 * @param {http.ServerResponse} res
 * @param {string} pathname - parsed URL pathname
 * @param {object} config - full gateway config
 * @param {object} stats - { requestCount, startedAt }
 */
function handleControlAPI(req, res, pathname, config, stats) {
  var pkg = require('../package.json');

  if (req.method === 'GET' && pathname === '/control/api/status') {
    var mem = process.memoryUsage();
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      version: pkg.version,
      uptime: Math.floor((Date.now() - stats.startedAt) / 1000),
      instance: {
        id: (config.instance && config.instance.id) || 'unknown',
        owner: (config.instance && config.instance.owner) || 'unknown',
        plan: (config.instance && config.instance.plan) || 'unknown'
      },
      llm: {
        provider: (config.llm && config.llm.provider) || 'none',
        endpoint: (config.llm && config.llm.genai && config.llm.genai.endpoint) || 'not set',
        model: (config.llm && config.llm.model_override) || (config.llm && config.llm.genai && config.llm.genai.model) || 'auto'
      },
      memory: {
        rss_mb: Math.round(mem.rss / 1048576),
        heap_used_mb: Math.round(mem.heapUsed / 1048576),
        heap_total_mb: Math.round(mem.heapTotal / 1048576)
      },
      node_version: process.version
    }));
    return;
  }

  if (req.method === 'GET' && pathname === '/control/api/stats') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      request_count: stats.requestCount,
      uptime: Math.floor((Date.now() - stats.startedAt) / 1000)
    }));
    return;
  }

  if (req.method === 'POST' && pathname === '/control/api/llm-test') {
    var start = Date.now();
    var tokens = '';
    streamChat(
      config,
      [
        { role: 'system', content: 'You are a test assistant. Reply in exactly one short sentence.' },
        { role: 'user', content: 'Say hello and confirm you are working.' }
      ],
      function onToken(t) { tokens += t; },
      function onDone() {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          success: true,
          latency_ms: Date.now() - start,
          response: tokens.slice(0, 500)
        }));
      },
      function onError(err) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          success: false,
          latency_ms: Date.now() - start,
          error: err.message
        }));
      }
    );
    return;
  }

  if (req.method === 'GET' && pathname === '/control/api/logs') {
    var urlObj;
    try { urlObj = new URL(req.url, 'http://localhost'); } catch (e) { /* ignore */ }
    var logFile = (urlObj && urlObj.searchParams.get('file')) || 'stdout';
    var lines = parseInt((urlObj && urlObj.searchParams.get('lines')) || '200', 10);
    if (lines > 2000) lines = 2000;

    // Sanitize logFile to prevent path traversal
    logFile = logFile.replace(/[^a-zA-Z0-9._-]/g, '');

    var logDir = '/var/vcap/sys/log/openclaw-agent';
    var logPath = logDir + '/' + logFile + '.log';

    try {
      if (fs.existsSync(logPath)) {
        var content = fs.readFileSync(logPath, 'utf8');
        var allLines = content.split('\n');
        var tail = allLines.slice(-lines).join('\n');
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ file: logFile, lines: lines, content: tail }));
      } else {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ file: logFile, lines: 0, content: '(log file not found: ' + logPath + ')' }));
      }
    } catch (e) {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ file: logFile, lines: 0, content: 'Error reading log: ' + e.message }));
    }
    return;
  }

  if (req.method === 'POST' && pathname === '/control/api/restart') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ message: 'Restarting process...' }));
    setTimeout(function () { process.exit(1); }, 500);
    return;
  }

  res.writeHead(404, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ error: 'Unknown control API endpoint' }));
}

/**
 * Render the control UI HTML dashboard.
 */
function controlHTML(o) {
  return '<!DOCTYPE html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n' +
'<meta name="viewport" content="width=device-width, initial-scale=1">\n' +
'<title>OpenClaw Control Panel</title>\n' +
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
'html { height:100%; }\n' +
'body { font-family:var(--font-body); background:radial-gradient(ellipse at 50% 0%,#0f1a2e 0%,#080b12 50%); color:var(--text-1); min-height:100vh; }\n' +
'\n' +
'.header { background:var(--surface-1); padding:14px 24px; display:flex; align-items:center; gap:14px; border-bottom:1px solid var(--border); position:relative; overflow:hidden; }\n' +
'.header::after { content:""; position:absolute; top:0; right:0; width:200px; height:100%; background:repeating-linear-gradient(-45deg,transparent,transparent 8px,rgba(0,212,170,0.03) 8px,rgba(0,212,170,0.03) 9px); pointer-events:none; }\n' +
'.header .logo { color:var(--accent); display:flex; }\n' +
'.header h1 { font-size:17px; font-weight:600; letter-spacing:-0.3px; }\n' +
'.header .sub { font-size:12px; color:var(--text-2); font-family:var(--font-mono); }\n' +
'.badge { background:var(--accent); color:#080b12; padding:3px 10px; border-radius:100px; font-size:10px; font-weight:600; font-family:var(--font-mono); letter-spacing:0.6px; text-transform:uppercase; }\n' +
'.back-link { color:var(--accent); text-decoration:none; font-size:12px; font-family:var(--font-mono); margin-left:auto; }\n' +
'.back-link:hover { color:var(--accent-hover); }\n' +
'\n' +
'.grid { max-width:1100px; margin:24px auto; padding:0 20px; display:grid; grid-template-columns:repeat(auto-fit, minmax(340px, 1fr)); gap:16px; }\n' +
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
'  <div><h1>OpenClaw Control Panel</h1><div class="sub">' + esc(o.instanceId) + ' &middot; ' + esc(o.planName) + '</div></div>\n' +
'  <span class="badge">Admin</span>\n' +
'  <a href="/" class="back-link">&larr; WebChat</a>\n' +
'</header>\n' +
'\n' +
'<div class="grid">\n' +
'\n' +
'  <div class="card">\n' +
'    <div class="card-header"><span class="icon">&#9679;</span><h2>Instance Status</h2></div>\n' +
'    <div class="card-body">\n' +
'      <div class="kv" id="status-kv">\n' +
'        <span class="label">Loading...</span><span class="value"></span>\n' +
'      </div>\n' +
'      <div class="btn-row"><button class="btn" onclick="loadStatus()">Refresh</button></div>\n' +
'    </div>\n' +
'  </div>\n' +
'\n' +
'  <div class="card">\n' +
'    <div class="card-header"><span class="icon">&#9889;</span><h2>LLM Health Test</h2></div>\n' +
'    <div class="card-body">\n' +
'      <p style="font-size:12px;color:var(--text-2);margin-bottom:8px;">Send a test prompt to verify LLM connectivity.</p>\n' +
'      <button class="btn primary" id="llm-test-btn" onclick="testLLM()">Run Test</button>\n' +
'      <div class="test-result" id="llm-result"></div>\n' +
'    </div>\n' +
'  </div>\n' +
'\n' +
'  <div class="card full-width">\n' +
'    <div class="card-header"><span class="icon">&#9776;</span><h2>Log Viewer</h2></div>\n' +
'    <div class="card-body">\n' +
'      <div class="log-controls">\n' +
'        <select id="log-file"><option value="stdout">stdout</option><option value="stderr">stderr</option></select>\n' +
'        <select id="log-lines"><option value="100">100 lines</option><option value="200" selected>200 lines</option><option value="500">500 lines</option></select>\n' +
'        <button class="btn" onclick="loadLogs()">Fetch Logs</button>\n' +
'      </div>\n' +
'      <div class="log-box" id="log-content">Click "Fetch Logs" to load...</div>\n' +
'    </div>\n' +
'  </div>\n' +
'\n' +
'  <div class="card">\n' +
'    <div class="card-header"><span class="icon">&#9881;</span><h2>Process Management</h2></div>\n' +
'    <div class="card-body">\n' +
'      <p style="font-size:12px;color:var(--text-2);margin-bottom:8px;">Restart the gateway process (BPM will respawn it).</p>\n' +
'      <button class="btn danger" onclick="restartProcess()">Restart Process</button>\n' +
'    </div>\n' +
'  </div>\n' +
'\n' +
'  <div class="card">\n' +
'    <div class="card-header"><span class="icon">&#9733;</span><h2>Request Stats</h2></div>\n' +
'    <div class="card-body">\n' +
'      <div class="kv" id="stats-kv">\n' +
'        <span class="label">Loading...</span><span class="value"></span>\n' +
'      </div>\n' +
'      <div class="btn-row"><button class="btn" onclick="loadStats()">Refresh</button></div>\n' +
'    </div>\n' +
'  </div>\n' +
'\n' +
'</div>\n' +
'\n' +
'<script>\n' +
'(function() {\n' +
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
'    api("GET", "/control/api/status").then(function(d) {\n' +
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
'    api("POST", "/control/api/llm-test").then(function(d) {\n' +
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
'    api("GET", "/control/api/logs?file=" + encodeURIComponent(file) + "&lines=" + lines).then(function(d) {\n' +
'      box.textContent = d.content || "(empty)";\n' +
'      box.scrollTop = box.scrollHeight;\n' +
'    }).catch(function(e) {\n' +
'      box.textContent = "Error: " + e.message;\n' +
'    });\n' +
'  };\n' +
'\n' +
'  window.restartProcess = function() {\n' +
'    if (!confirm("Restart the gateway process? BPM will respawn it.")) return;\n' +
'    api("POST", "/control/api/restart").then(function() {\n' +
'      document.body.innerHTML = \'<div style="text-align:center;padding:60px;font-family:var(--font-mono);color:var(--accent);">Restarting... page will reload shortly.</div>\';\n' +
'      setTimeout(function() { location.reload(); }, 5000);\n' +
'    });\n' +
'  };\n' +
'\n' +
'  window.loadStats = function() {\n' +
'    api("GET", "/control/api/stats").then(function(d) {\n' +
'      document.getElementById("stats-kv").innerHTML = kvHTML([\n' +
'        ["Requests", String(d.request_count), "accent"],\n' +
'        ["Uptime", fmtUptime(d.uptime), ""]\n' +
'      ]);\n' +
'    }).catch(function(e) {\n' +
'      document.getElementById("stats-kv").innerHTML = \'<span class="label">Error</span><span class="value error">\' + e.message + \'</span>\';\n' +
'    });\n' +
'  };\n' +
'\n' +
'  // Auto-load on startup\n' +
'  loadStatus();\n' +
'  loadStats();\n' +
'\n' +
'  // Auto-refresh every 30s\n' +
'  setInterval(loadStatus, 30000);\n' +
'})();\n' +
'</script>\n' +
'</body>\n' +
'</html>';
}

module.exports = { controlHTML, createAuthMiddleware, handleControlAPI };
