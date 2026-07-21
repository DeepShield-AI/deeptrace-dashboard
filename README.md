# DeepFlow Clone

基于 DeepFlow 前端的可观测性平台，使用 Go 后端提供 API 服务，支持完全离线运行。

## 快速启动

```bash
cd backend
go build -o deeptrace-server .
./deeptrace-server
```

访问 http://localhost:8888

## 环境要求

- Go 1.21+
- 无需数据库、无需网络连接

## 架构

```
deepflow-clone/
├── backend/                           # Go 后端服务
│   ├── main.go                        # 主程序（API路由、缓存加载、静态文件服务）
│   ├── go.mod                         # Go module
│   └── data/                          # 自定义数据文件（可编辑）
│       ├── topo.json                  # 服务拓扑数据
│       ├── traces.json                # 追踪/Span 数据
│       ├── services.json              # 服务列表
│       ├── agents.json                # 采集器列表
│       ├── dashboards.json            # 仪表盘列表
│       ├── pods.json                  # Pod 列表
│       ├── domains.json               # 域列表
│       ├── vpcs.json                  # VPC 列表
│       ├── metrics.json               # 指标定义
│       └── service_overview.json      # 服务概览
├── api_cache/                         # 363 条真实 API 缓存响应
├── cloud.deepflow.yunshan.net/        # 前端静态资源（884 文件）
│   ├── index.html                     # SPA 入口
│   └── assets/                        # JS/CSS/字体/图片
├── offline_server.js                  # Node.js 备用离线服务器
└── README.md
```

## 数据加载说明

### 1. 缓存层（api_cache/）

启动时自动加载 `api_cache/` 目录中 363 个 JSON 文件，包含从真实 DeepFlow 集群捕获的完整 API 响应：

| 数据类别 | 示例接口 | 说明 |
|---------|---------|------|
| DB Schema | `ShowDatabases`, `ShowTables`, `ShowTags`, `ShowMetrics` | 数据库/表/标签/指标元数据 |
| 数据源 | `/api/deepflow-server/v1/data-sources/` | 网络指标、流日志等数据源定义 |
| 图标 | `/api/df-web/v1/icons` | 395KB 图标库 |
| 仪表盘 | `/api/df-web/v1/dashboards` | 76KB 仪表盘列表 |
| 服务拓扑 | `/api/df-web-composer/api/service_topo/` | 业务入口路径及告警统计 |
| 用户配置 | `/api/fuser/v1/user/conf/` | 搜索偏好设置 |

缓存支持 **请求体匹配**：同一接口（如 `ShowTables`）根据 `{"DATABASE":"flow_log"}` 等请求体返回不同响应。

### 2. 自定义数据层（backend/data/）

可直接编辑 JSON 文件来自定义显示数据：

- **`topo.json`** — 服务拓扑图的节点（`instance_data`）和连线（`peers_data`）
- **`traces.json`** — 追踪列表和 Span 详情
- **`services.json`** — 服务列表数据
- **`service_overview.json`** — 服务概览趋势和列表

### 3. 硬编码响应

认证和组织相关接口使用硬编码响应：

- `/api/fauths/login` — 返回含 `org_id=4, team_id=1` 的 JWT Token
- `/api/fuser/v1/users/current` — 当前用户信息
- `/api/fpermit/v1/orgs` — 组织列表
- `/api/warrant/check/license` — 全功能授权（追踪/日志/性能剖析等）

## 请求处理优先级

```
请求到达 → 硬编码处理器（认证/组织/配置）
         → api_cache 缓存匹配（按 method + path + body 匹配）
         → backend/data/ 文件
         → 空数组 fallback []
```

## 配置

| 环境变量 | 默认值 | 说明 |
|---------|-------|------|
| `PORT` | `8888` | 服务端口 |

## 功能特性

- **Gzip 压缩** — JS/CSS 自动 gzip 压缩传输（~70-80% 压缩率）
- **长缓存** — 静态资源设置 1 年浏览器缓存
- **SPA 路由** — 所有非静态路径 fallback 到 index.html
- **CORS** — 完整 CORS 支持
- **X-Org-Id** — 所有 API 响应自动携带组织 ID header

## 备用启动方式（Node.js）

```bash
node offline_server.js
```

使用纯 Node.js 启动，无需 Go 编译，功能相同。
