const http = require('http');
const https = require('https');
const fs = require('fs');
const path = require('path');
const url = require('url');
const crypto = require('crypto');

const PORT = 8888;
const STATIC_DIR = path.join(__dirname, 'cloud.deepflow.yunshan.net');
const UPSTREAM = 'cloud.deepflow.yunshan.net';
const CACHE_DIR = path.join(__dirname, 'api_cache');
const INDEX_FILE = path.join(CACHE_DIR, '_index.json');

if (!fs.existsSync(CACHE_DIR)) fs.mkdirSync(CACHE_DIR, { recursive: true });

// Load existing index
let cacheIndex = {};
if (fs.existsSync(INDEX_FILE)) {
  try { cacheIndex = JSON.parse(fs.readFileSync(INDEX_FILE, 'utf8')); } catch(e) {}
}

function saveCacheIndex() {
  fs.writeFileSync(INDEX_FILE, JSON.stringify(cacheIndex, null, 2));
}

const MIME_TYPES = {
  '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css',
  '.json': 'application/json', '.png': 'image/png', '.jpg': 'image/jpeg',
  '.gif': 'image/gif', '.svg': 'image/svg+xml', '.ico': 'image/x-icon',
  '.ttf': 'font/ttf', '.woff': 'font/woff', '.woff2': 'font/woff2',
  '.map': 'application/json', '.webp': 'image/webp',
};

function getCacheKey(method, urlPath, body) {
  const hash = body ? crypto.createHash('md5').update(body).digest('hex').slice(0, 12) : 'nobody';
  return `${method}_${urlPath.replace(/[^a-zA-Z0-9_.-]/g, '_').slice(0, 150)}_${hash}`;
}

function proxyAndRecord(req, res) {
  let bodyChunks = [];
  req.on('data', chunk => bodyChunks.push(chunk));
  req.on('end', () => {
    const body = Buffer.concat(bodyChunks);
    const bodyStr = body.length > 0 ? body.toString() : '';
    const cacheKey = getCacheKey(req.method, req.url, bodyStr);

    const options = {
      hostname: UPSTREAM,
      port: 443,
      path: req.url,
      method: req.method,
      headers: {
        ...req.headers,
        host: UPSTREAM,
        origin: `https://${UPSTREAM}`,
        referer: `https://${UPSTREAM}/`,
      },
    };
    delete options.headers['accept-encoding'];

    if (req.method === 'OPTIONS') {
      res.writeHead(204, {
        'access-control-allow-origin': req.headers.origin || '*',
        'access-control-allow-methods': 'GET, POST, PUT, DELETE, PATCH, OPTIONS',
        'access-control-allow-headers': req.headers['access-control-request-headers'] || '*',
        'access-control-allow-credentials': 'true',
        'access-control-max-age': '86400',
      });
      res.end();
      return;
    }

    const proxyReq = https.request(options, (proxyRes) => {
      let respChunks = [];
      proxyRes.on('data', chunk => respChunks.push(chunk));
      proxyRes.on('end', () => {
        const respBody = Buffer.concat(respChunks);
        const headers = { ...proxyRes.headers };
        delete headers['content-security-policy'];
        delete headers['x-frame-options'];
        headers['access-control-allow-origin'] = req.headers.origin || '*';
        headers['access-control-allow-credentials'] = 'true';

        if (headers['set-cookie']) {
          const cookies = Array.isArray(headers['set-cookie']) ? headers['set-cookie'] : [headers['set-cookie']];
          headers['set-cookie'] = cookies.map(c =>
            c.replace(/Domain=[^;]+;?/gi, '').replace(/Secure;?/gi, '').replace(/SameSite=[^;]+;?/gi, 'SameSite=Lax;')
          );
        }

        // Record API response
        const isAPI = !req.url.startsWith('/assets/') && !req.url.startsWith('/monacoeditorwork/') && req.url !== '/favicon.ico';
        if (isAPI && proxyRes.statusCode >= 200 && proxyRes.statusCode < 400) {
          const cacheFile = cacheKey + '.json';
          const entry = {
            method: req.method,
            path: req.url,
            requestBody: bodyStr || null,
            status: proxyRes.statusCode,
            headers: Object.fromEntries(
              Object.entries(headers).filter(([k]) => !['transfer-encoding', 'connection'].includes(k))
            ),
            responseBody: respBody.toString('base64'),
            responseIsBase64: true,
            timestamp: new Date().toISOString(),
          };
          fs.writeFileSync(path.join(CACHE_DIR, cacheFile), JSON.stringify(entry));
          cacheIndex[cacheKey] = { method: req.method, path: req.url, file: cacheFile, status: proxyRes.statusCode, size: respBody.length };
          saveCacheIndex();
          console.log(`📝 RECORDED [${proxyRes.statusCode}] ${req.method} ${req.url} (${respBody.length}b) -> ${cacheFile}`);
        }

        res.writeHead(proxyRes.statusCode, headers);
        res.end(respBody);
      });
    });

    proxyReq.on('error', (err) => {
      console.error('Proxy error:', err.message);
      res.writeHead(502);
      res.end('Bad Gateway');
    });

    proxyReq.setTimeout(60000);
    if (body.length > 0) proxyReq.write(body);
    proxyReq.end();
  });
}

function serveStatic(req, res) {
  const parsedUrl = url.parse(req.url);
  let pathname = parsedUrl.pathname;
  let filePath = path.join(STATIC_DIR, pathname);

  if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
    const ext = path.extname(filePath).toLowerCase();
    const contentType = MIME_TYPES[ext] || 'application/octet-stream';
    const content = fs.readFileSync(filePath);
    res.writeHead(200, { 'Content-Type': contentType, 'Cache-Control': 'public, max-age=31536000' });
    res.end(content);
    return true;
  }
  return false;
}

const server = http.createServer((req, res) => {
  const parsedUrl = url.parse(req.url);
  const pathname = parsedUrl.pathname;

  if (req.method !== 'GET') {
    proxyAndRecord(req, res);
    return;
  }

  if (serveStatic(req, res)) return;

  const ext = path.extname(pathname).toLowerCase();
  const isStaticExt = ['.js', '.css', '.png', '.jpg', '.svg', '.ico', '.ttf', '.woff', '.woff2', '.gif', '.webp', '.map'].includes(ext);
  if (isStaticExt) {
    proxyAndRecord(req, res);
    return;
  }

  const spaOnlyPaths = ['/', '/login', '/dashboard', '/tracing', '/network', '/application', '/infrastructure', '/alarm', '/setting', '/ai', '/reset'];
  const isSpaRoute = spaOnlyPaths.some(p => pathname === p || pathname.startsWith(p + '/'));

  if (!isSpaRoute) {
    proxyAndRecord(req, res);
    return;
  }

  const indexPath = path.join(STATIC_DIR, 'index.html');
  if (fs.existsSync(indexPath)) {
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(fs.readFileSync(indexPath));
  } else {
    res.writeHead(404);
    res.end('Not Found');
  }
});

server.on('upgrade', (req, socket, head) => {
  const options = { hostname: UPSTREAM, port: 443, path: req.url, method: 'GET', headers: { ...req.headers, host: UPSTREAM } };
  const proxyReq = https.request(options);
  proxyReq.on('upgrade', (proxyRes, proxySocket, proxyHead) => {
    socket.write(`HTTP/1.1 101 Switching Protocols\r\n` + Object.entries(proxyRes.headers).map(([k, v]) => `${k}: ${v}`).join('\r\n') + '\r\n\r\n');
    if (proxyHead.length) socket.write(proxyHead);
    proxySocket.pipe(socket);
    socket.pipe(proxySocket);
  });
  proxyReq.on('error', () => socket.end());
  proxyReq.end();
});

server.listen(PORT, '0.0.0.0', () => {
  console.log(`\n🔴 RECORDING MODE - DeepFlow clone at http://localhost:${PORT}`);
  console.log(`Static: ${STATIC_DIR}`);
  console.log(`API cache: ${CACHE_DIR}`);
  console.log(`Upstream: https://${UPSTREAM}`);
  console.log(`\n👉 Browse the site to record all API responses.`);
  console.log(`   Then run: node offline_server.js\n`);
});
