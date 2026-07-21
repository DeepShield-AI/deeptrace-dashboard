# DeepTrace Dashboard

Network observability dashboard — fully offline website clone.

## Quick Start

```bash
node offline_server.js
```

Access at http://localhost:8888

No internet connection needed.

## Architecture

- **676 static files** (JS/CSS/fonts/images) served locally
- **352 cached API responses** for fully offline operation
- SPA route fallback to index.html

## Structure

```
├── offline_server.js                  # Standalone offline server
├── api_cache/                         # Cached API responses (352 files)
├── cloud.deepflow.yunshan.net/        # Static assets
│   ├── index.html                     # SPA entry point
│   ├── favicon.ico
│   ├── monacoeditorwork/             # Monaco editor web worker
│   └── assets/                       # JS/CSS/fonts (669 files)
└── README.md
```

## Configuration

Edit `offline_server.js` to change:
- `PORT` — Local server port (default: 8888)
