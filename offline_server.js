const http = require('http');
const fs = require('fs');
const path = require('path');
const url = require('url');
const crypto = require('crypto');

const PORT = 8888;
const STATIC_DIR = path.join(__dirname, 'cloud.deepflow.yunshan.net');
const CACHE_DIR = path.join(__dirname, 'api_cache');

const MIME_TYPES = {
  '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css',
  '.json': 'application/json', '.png': 'image/png', '.jpg': 'image/jpeg',
  '.gif': 'image/gif', '.svg': 'image/svg+xml', '.ico': 'image/x-icon',
  '.ttf': 'font/ttf', '.woff': 'font/woff', '.woff2': 'font/woff2',
  '.map': 'application/json', '.webp': 'image/webp',
};

// Load all cached API responses into memory
const apiCache = new Map();
let cacheCount = 0;

if (fs.existsSync(CACHE_DIR)) {
  const files = fs.readdirSync(CACHE_DIR).filter(f => f.endsWith('.json') && f !== '_index.json');
  for (const file of files) {
    try {
      const entry = JSON.parse(fs.readFileSync(path.join(CACHE_DIR, file), 'utf8'));
      const key = getCacheKey(entry.method, entry.path, entry.requestBody);
      apiCache.set(key, entry);
      cacheCount++;
    } catch (e) {}
  }
}

function getCacheKey(method, urlPath, body) {
  const hash = body ? crypto.createHash('md5').update(body).digest('hex').slice(0, 12) : 'nobody';
  return `${method}_${urlPath.replace(/[^a-zA-Z0-9_.-]/g, '_').slice(0, 150)}_${hash}`;
}

function findCachedResponse(method, urlPath, body) {
  // Exact match
  const exactKey = getCacheKey(method, urlPath, body);
  if (apiCache.has(exactKey)) return apiCache.get(exactKey);

  // Try without body hash for GET
  if (method === 'GET') {
    for (const [key, entry] of apiCache) {
      if (entry.method === method && entry.path === urlPath) return entry;
    }
  }

  // Try path-only match (ignore query string differences)
  const basePath = urlPath.split('?')[0];
  for (const [key, entry] of apiCache) {
    if (entry.method === method && entry.path.split('?')[0] === basePath) return entry;
  }

  return null;
}

function serveStatic(req, res) {
  const parsedUrl = url.parse(req.url);
  let filePath = path.join(STATIC_DIR, parsedUrl.pathname);

  if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
    const ext = path.extname(filePath).toLowerCase();
    const contentType = MIME_TYPES[ext] || 'application/octet-stream';
    res.writeHead(200, { 'Content-Type': contentType, 'Cache-Control': 'public, max-age=31536000' });
    res.end(fs.readFileSync(filePath));
    return true;
  }
  return false;
}

function serveCached(req, res) {
  let bodyChunks = [];
  req.on('data', chunk => bodyChunks.push(chunk));
  req.on('end', () => {
    const body = Buffer.concat(bodyChunks).toString() || null;
    const cached = findCachedResponse(req.method, req.url, body);

    if (cached) {
      const respBody = cached.responseIsBase64
        ? Buffer.from(cached.responseBody, 'base64')
        : Buffer.from(cached.responseBody);

      const headers = cached.headers || { 'content-type': 'application/json' };
      headers['access-control-allow-origin'] = '*';
      headers['access-control-allow-credentials'] = 'true';
      delete headers['transfer-encoding'];

      console.log(`✅ CACHE HIT [${cached.status}] ${req.method} ${req.url} (${respBody.length}b)`);
      res.writeHead(cached.status, headers);
      res.end(respBody);
    } else {
      console.log(`❌ CACHE MISS ${req.method} ${req.url}`);
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ OPT_STATUS: 'SUCCESS', DATA: [], DESCRIPTION: '' }));
    }
  });
}

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

  // Non-GET always try cache
  if (req.method !== 'GET') {
    serveCached(req, res);
    return;
  }

  // Try local static file
  if (serveStatic(req, res)) return;

  // Static extension not found locally -> cache
  const ext = path.extname(pathname).toLowerCase();
  if (['.js', '.css', '.png', '.jpg', '.svg', '.ico', '.ttf', '.woff', '.woff2', '.gif', '.webp', '.map'].includes(ext)) {
    serveCached(req, res);
    return;
  }

  // SPA routes -> index.html
  const spaOnlyPaths = ['/', '/login', '/dashboard', '/tracing', '/network', '/application', '/infrastructure', '/alarm', '/setting', '/ai', '/reset'];
  const isSpaRoute = spaOnlyPaths.some(p => pathname === p || pathname.startsWith(p + '/'));

  if (isSpaRoute) {
    const indexPath = path.join(STATIC_DIR, 'index.html');
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(fs.readFileSync(indexPath));
    return;
  }

  // Everything else -> try API cache
  serveCached(req, res);
});

server.listen(PORT, '0.0.0.0', () => {
  console.log(`\n🟢 OFFLINE MODE - DeepFlow clone at http://localhost:${PORT}`);
  console.log(`Static files: ${STATIC_DIR}`);
  console.log(`API cache: ${CACHE_DIR} (${cacheCount} responses loaded)`);
  console.log(`\n⚡ Fully standalone - no internet needed.\n`);
});
