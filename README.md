# DeepTrace Dashboard

Network observability dashboard — full website clone with two operating modes.

## Quick Start

### Offline Mode (standalone, no internet needed)

```bash
node offline_server.js
```

All API responses served from local cache (352 recorded responses, 31MB).

### Online Mode (reverse proxy)

```bash
node server.js
```

Static files served locally, API calls proxied to upstream.

### Recording Mode (capture new API data)

```bash
node record_server.js
```

Browse the site normally — all API responses are recorded to `api_cache/` for offline use.

Access at http://localhost:8888

## Architecture

- **676 static files** (JS/CSS/fonts/images) served locally
- **352 cached API responses** for fully offline operation
- Reverse proxy mode for live data
- Recording proxy to capture new API interactions
- WebSocket upgrade support
- Cookie domain rewriting for seamless auth
- SPA route fallback to index.html

## Structure

```
├── offline_server.js                  # Standalone offline server
├── server.js                          # Online reverse proxy server
├── record_server.js                   # Recording proxy server
├── api_cache/                         # Cached API responses (352 files)
├── cloud.deepflow.yunshan.net/        # Static assets
│   ├── index.html                     # SPA entry point
│   ├── favicon.ico
│   ├── monacoeditorwork/             # Monaco editor web worker
│   └── assets/                       # JS/CSS/fonts (669 files)
└── README.md
```

## Configuration

Edit any server file to change:
- `PORT` — Local server port (default: 8888)
- `UPSTREAM` — Backend API server hostname (proxy modes only)
