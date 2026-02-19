'use strict';

const fs = require('fs');
const { streamChat } = require('./llm');

/**
 * Handle admin API requests (flattened paths under /api/).
 * @param {http.IncomingMessage} req
 * @param {http.ServerResponse} res
 * @param {string} pathname - parsed URL pathname
 * @param {object} config - full gateway config
 * @param {object} stats - { requestCount, startedAt }
 */
function handleAdminAPI(req, res, pathname, config, stats) {
  var pkg = require('../package.json');

  if (req.method === 'GET' && pathname === '/api/status') {
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

  if (req.method === 'GET' && pathname === '/api/stats') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      request_count: stats.requestCount,
      uptime: Math.floor((Date.now() - stats.startedAt) / 1000)
    }));
    return;
  }

  if (req.method === 'POST' && pathname === '/api/llm-test') {
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

  if (req.method === 'GET' && pathname === '/api/logs') {
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

  if (req.method === 'POST' && pathname === '/api/restart') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ message: 'Restarting process...' }));
    setTimeout(function () { process.exit(1); }, 500);
    return;
  }

  // Unknown admin API endpoint — return false so caller can try other routes
  return false;
}

module.exports = { handleAdminAPI };
