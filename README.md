# DeepTrace Dashboard

Network observability dashboard with full API proxy support.

## Quick Start

```bash
node server.js
```

Access at http://localhost:8888

## Architecture

- Static frontend assets served locally (fast load)
- API requests proxied to upstream backend
- WebSocket upgrade support for real-time data
- Cookie domain rewriting for seamless auth
- SPA route fallback to index.html

## Structure

```
├── server.js                          # Node.js reverse proxy server
├── cloud.deepflow.yunshan.net/        # Static assets
│   ├── index.html                     # SPA entry point
│   ├── favicon.ico
│   ├── monacoeditorwork/             # Monaco editor web worker
│   └── assets/                       # JS/CSS/fonts (669 files)
└── README.md
```

## Configuration

Edit `server.js` to change:
- `PORT` — Local server port (default: 8888)
- `UPSTREAM` — Backend API server hostname
