const http = require('http');
const https = require('https');
const fs = require('fs');
const path = require('path');
const url = require('url');

const PORT = 8888;
const STATIC_DIR = path.join(__dirname, 'cloud.deepflow.yunshan.net');
const UPSTREAM = 'cloud.deepflow.yunshan.net';

const MIME_TYPES = {
  '.html': 'text/html',
  '.js': 'application/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.gif': 'image/gif',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.ttf': 'font/ttf',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.map': 'application/json',
  '.webp': 'image/webp',
};

function proxyRequest(req, res) {
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

  // Handle CORS preflight
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
    const headers = { ...proxyRes.headers };
    delete headers['content-security-policy'];
    delete headers['x-frame-options'];
    headers['access-control-allow-origin'] = req.headers.origin || '*';
    headers['access-control-allow-credentials'] = 'true';
    
    // Rewrite Set-Cookie domain
    if (headers['set-cookie']) {
      const cookies = Array.isArray(headers['set-cookie']) ? headers['set-cookie'] : [headers['set-cookie']];
      headers['set-cookie'] = cookies.map(c => 
        c.replace(/Domain=[^;]+;?/gi, '').replace(/Secure;?/gi, '').replace(/SameSite=[^;]+;?/gi, 'SameSite=Lax;')
      );
    }
    
    res.writeHead(proxyRes.statusCode, headers);
    proxyRes.pipe(res);
  });

  proxyReq.on('error', (err) => {
    console.error('Proxy error:', err.message);
    res.writeHead(502);
    res.end('Bad Gateway');
  });

  req.pipe(proxyReq);
}

function serveStatic(req, res) {
  const parsedUrl = url.parse(req.url);
  let pathname = parsedUrl.pathname;
  
  let filePath = path.join(STATIC_DIR, pathname);
  
  // Check if file exists locally
  if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
    const ext = path.extname(filePath).toLowerCase();
    const contentType = MIME_TYPES[ext] || 'application/octet-stream';
    
    const content = fs.readFileSync(filePath);
    res.writeHead(200, { 
      'Content-Type': contentType,
      'Cache-Control': 'public, max-age=31536000',
    });
    res.end(content);
    return true;
  }
  
  return false;
}

const server = http.createServer((req, res) => {
  const parsedUrl = url.parse(req.url);
  const pathname = parsedUrl.pathname;

  // Non-GET requests always proxy
  if (req.method !== 'GET') {
    proxyRequest(req, res);
    return;
  }

  // Try serving static file first (assets, favicon, monacoeditorwork)
  if (serveStatic(req, res)) {
    return;
  }

  // Known static-only paths -> serve index.html (SPA routes)
  const ext = path.extname(pathname).toLowerCase();
  const isStaticExt = ['.js', '.css', '.png', '.jpg', '.svg', '.ico', '.ttf', '.woff', '.woff2', '.gif', '.webp', '.map'].includes(ext);
  
  if (isStaticExt) {
    // Static file not found locally -> try upstream
    proxyRequest(req, res);
    return;
  }

  // API-like paths -> proxy to upstream
  const spaOnlyPaths = ['/', '/login', '/dashboard', '/tracing', '/network', '/application', '/infrastructure', '/alarm', '/setting', '/ai'];
  const isSpaRoute = spaOnlyPaths.some(p => pathname === p || pathname.startsWith(p + '/'));
  
  if (!isSpaRoute) {
    // Likely an API call, proxy it
    proxyRequest(req, res);
    return;
  }

  // SPA fallback: serve index.html
  const indexPath = path.join(STATIC_DIR, 'index.html');
  if (fs.existsSync(indexPath)) {
    const content = fs.readFileSync(indexPath);
    res.writeHead(200, { 'Content-Type': 'text/html' });
    res.end(content);
  } else {
    res.writeHead(404);
    res.end('Not Found');
  }
});

// WebSocket upgrade support
server.on('upgrade', (req, socket, head) => {
  const options = {
    hostname: UPSTREAM,
    port: 443,
    path: req.url,
    method: 'GET',
    headers: {
      ...req.headers,
      host: UPSTREAM,
    },
  };

  const proxyReq = https.request(options);
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

  proxyReq.on('error', (err) => {
    console.error('WebSocket proxy error:', err.message);
    socket.end();
  });

  proxyReq.end();
});

server.listen(PORT, '0.0.0.0', () => {
  console.log(`DeepFlow clone running at http://localhost:${PORT}`);
  console.log(`Static files: ${STATIC_DIR}`);
  console.log(`API proxy -> https://${UPSTREAM}`);
  console.log(`WebSocket upgrade support enabled`);
});
