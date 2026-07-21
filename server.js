/**
 * DeepFlow Dashboard Server
 * 
 * 用法:
 *   BACKEND=http://your-deepflow-server:20416  node server.js
 *   BACKEND=https://cloud.deepflow.yunshan.net  node server.js
 * 
 * 前端文件本地服务，所有 /api/* 请求实时代理到你的 DeepFlow 后端。
 * 不是离线缓存，是动态代理——后端数据变，页面实时变。
 */

const http = require('http');
const https = require('https');
const fs = require('fs');
const path = require('path');
const url = require('url');

const PORT = process.env.PORT || 8888;
const BACKEND = process.env.BACKEND || 'https://cloud.deepflow.yunshan.net';
const STATIC_DIR = path.join(__dirname, 'cloud.deepflow.yunshan.net');

const backendUrl = new URL(BACKEND);
const isHttps = backendUrl.protocol === 'https:';
const backendHost = backendUrl.hostname;
const backendPort = backendUrl.port || (isHttps ? 443 : 80);
const transport = isHttps ? https : http;

const MIME_TYPES = {
  '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css',
  '.json': 'application/json', '.png': 'image/png', '.jpg': 'image/jpeg',
  '.gif': 'image/gif', '.svg': 'image/svg+xml', '.ico': 'image/x-icon',
  '.ttf': 'font/ttf', '.woff': 'font/woff', '.woff2': 'font/woff2',
  '.map': 'application/json', '.webp': 'image/webp',
};

// ============================================================
// 反向代理：/api/* → 后端
// ============================================================
function proxyRequest(req, res) {
  const opts = {
    hostname: backendHost,
    port: backendPort,
    path: req.url,
    method: req.method,
    headers: {
      ...req.headers,
      host: backendHost,
    },
    rejectUnauthorized: false,
  };
  delete opts.headers['accept-encoding']; // 避免压缩，简化处理

  const proxyReq = transport.request(opts, (proxyRes) => {
    // 注入 CORS
    const headers = { ...proxyRes.headers };
    headers['access-control-allow-origin'] = req.headers.origin || '*';
    headers['access-control-allow-credentials'] = 'true';
    // 替换 cookie 域名
    if (headers['set-cookie']) {
      headers['set-cookie'] = [].concat(headers['set-cookie']).map(c =>
        c.replace(/domain=[^;]+/gi, 'domain=localhost')
      );
    }
    delete headers['content-security-policy'];
    delete headers['x-frame-options'];

    res.writeHead(proxyRes.statusCode, headers);
    proxyRes.pipe(res);

    const size = proxyRes.headers['content-length'] || '?';
    console.log(`🔄 [${proxyRes.statusCode}] ${req.method} ${req.url.substring(0, 80)} (${size}b)`);
  });

  proxyReq.on('error', (err) => {
    console.error(`❌ PROXY ERROR ${req.method} ${req.url}: ${err.message}`);
    res.writeHead(502, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ OPT_STATUS: 'SERVER_ERROR', DESCRIPTION: err.message }));
  });

  req.pipe(proxyReq);
}

// ============================================================
// 静态文件服务
// ============================================================
function serveStatic(req, res) {
  const parsedUrl = url.parse(req.url);
  const filePath = path.join(STATIC_DIR, parsedUrl.pathname);

  if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
    const ext = path.extname(filePath).toLowerCase();
    const ct = MIME_TYPES[ext] || 'application/octet-stream';
    res.writeHead(200, { 'Content-Type': ct, 'Cache-Control': 'public, max-age=31536000' });
    fs.createReadStream(filePath).pipe(res);
    return true;
  }
  return false;
}

// ============================================================
// 主服务器
// ============================================================
const server = http.createServer((req, res) => {
  const parsedUrl = url.parse(req.url);
  const pathname = parsedUrl.pathname;

  // CORS preflight
  if (req.method === 'OPTIONS') {
    res.writeHead(204, {
      'access-control-allow-origin': req.headers.origin || '*',
      'access-control-allow-methods': 'GET, POST, PUT, DELETE, PATCH, OPTIONS',
      'access-control-allow-headers': req.headers['access-control-request-headers'] || '*',
      'access-control-allow-credentials': 'true',
    });
    res.end();
    return;
  }

  // /api/* → 代理到后端（动态数据）
  if (pathname.startsWith('/api/')) {
    proxyRequest(req, res);
    return;
  }

  // 非 GET → 代理
  if (req.method !== 'GET') {
    proxyRequest(req, res);
    return;
  }

  // 本地静态文件
  if (serveStatic(req, res)) return;

  // 静态文件扩展名但本地没有 → 404
  const ext = path.extname(pathname).toLowerCase();
  if (Object.keys(MIME_TYPES).includes(ext)) {
    res.writeHead(404);
    res.end('Not Found');
    return;
  }

  // SPA fallback → index.html
  const indexPath = path.join(STATIC_DIR, 'index.html');
  if (fs.existsSync(indexPath)) {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    fs.createReadStream(indexPath).pipe(res);
  } else {
    res.writeHead(404);
    res.end('Not Found');
  }
});

// WebSocket 代理
server.on('upgrade', (req, socket, head) => {
  const opts = {
    hostname: backendHost,
    port: backendPort,
    path: req.url,
    method: 'GET',
    headers: { ...req.headers, host: backendHost },
  };

  const proxyReq = transport.request(opts);
  proxyReq.on('upgrade', (proxyRes, proxySocket, proxyHead) => {
    socket.write(
      `HTTP/1.1 101 Switching Protocols\r\n` +
      Object.entries(proxyRes.headers).map(([k, v]) => `${k}: ${v}`).join('\r\n') +
      '\r\n\r\n'
    );
    if (proxyHead.length) socket.write(proxyHead);
    proxySocket.pipe(socket);
    socket.pipe(proxySocket);
  });
  proxyReq.on('error', () => socket.destroy());
  proxyReq.end();
});

server.listen(PORT, '0.0.0.0', () => {
  console.log(`
╔══════════════════════════════════════════════════════╗
║  DeepFlow Dashboard                                  ║
║                                                      ║
║  前端:  http://localhost:${PORT}                       ║
║  后端:  ${BACKEND.padEnd(43)}║
║                                                      ║
║  所有 /api/* 请求实时代理到后端                       ║
║  前端静态文件本地服务                                ║
╚══════════════════════════════════════════════════════╝
`);
});
