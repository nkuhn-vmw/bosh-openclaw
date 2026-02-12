#!/usr/bin/env node
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

// Parse --config argument
const configIdx = process.argv.indexOf('--config');
const configPath = configIdx > -1 ? process.argv[configIdx + 1] : null;

let config = { port: 8083 };
if (configPath && fs.existsSync(configPath)) {
  config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
}

const port = config.port || 8083;

const server = http.createServer((req, res) => {
  // Basic auth check
  if (config.auth && config.auth.username) {
    const authHeader = req.headers.authorization;
    if (!authHeader || !authHeader.startsWith('Basic ')) {
      res.writeHead(401, { 'WWW-Authenticate': 'Basic realm="OpenClaw Admin"' });
      res.end('Unauthorized');
      return;
    }
    const decoded = Buffer.from(authHeader.slice(6), 'base64').toString();
    const [user, pass] = decoded.split(':');
    if (user !== config.auth.username || pass !== config.auth.password) {
      res.writeHead(401, { 'WWW-Authenticate': 'Basic realm="OpenClaw Admin"' });
      res.end('Unauthorized');
      return;
    }
  }

  if (req.url === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ status: 'ok' }));
    return;
  }

  if (req.url === '/api/instances') {
    // Proxy to broker endpoint
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ instances: [] }));
    return;
  }

  // Serve admin dashboard UI
  res.writeHead(200, { 'Content-Type': 'text/html' });
  const hostname = (config.route && config.route.hostname) || 'openclaw-admin';
  const domain = (config.route && config.route.domain) || '';
  res.end(`<!DOCTYPE html>
<html>
<head><title>OpenClaw Admin Dashboard</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; margin: 0; background: #f5f5f5; }
  .header { background: #1a1a2e; color: white; padding: 16px 24px; }
  .header h1 { margin: 0; font-size: 20px; }
  .content { padding: 24px; max-width: 960px; margin: 0 auto; }
  .card { background: white; border-radius: 8px; padding: 24px; margin-bottom: 16px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  .status { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #4caf50; margin-right: 8px; }
</style>
</head>
<body>
  <div class="header"><h1>OpenClaw Admin Dashboard</h1></div>
  <div class="content">
    <div class="card">
      <h2><span class="status"></span>Service Broker</h2>
      <p>Endpoint: ${config.broker_endpoint || 'not configured'}</p>
      <p>Dashboard: ${hostname}.${domain}</p>
    </div>
    <div class="card">
      <h2>Agent Instances</h2>
      <p>Loading...</p>
    </div>
  </div>
</body>
</html>`);
});

server.listen(port, '0.0.0.0', () => {
  console.log(`OpenClaw Admin Dashboard listening on port ${port}`);
});
