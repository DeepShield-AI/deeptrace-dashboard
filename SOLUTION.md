# 方案：Go 后端 + DeepFlow 前端 + 数据库（以 Trace 为例）

## 一、总体架构

```
┌─────────────────────────────────────────────────────────────┐
│  浏览器（DeepFlow 前端，纯静态文件，不改任何代码）              │
└─────────────┬───────────────────────────────────────────────┘
              │ HTTP
┌─────────────▼───────────────────────────────────────────────┐
│  Go Web Server（端口 8888）                                   │
│                                                              │
│  ├── GET /*         → 本地 static 文件（前端 SPA）            │
│  ├── GET /api/fauths/*   → 硬编码登录/权限（免认证）          │
│  ├── GET /api/deepflow-server/* → 资源查询 handler           │
│  ├── POST /api/statistics/v1/stats/querier/Topo → Topo handler│
│  ├── POST /api/querier/v1/query/ → SQL 查询 handler          │
│  └── POST /api/df-web-composer/* → Composer handler          │
└─────────────┬───────────────────────────────────────────────┘
              │ SQL
┌─────────────▼──────────────────────────────────────┐
│  ClickHouse / PostgreSQL / MySQL                    │
│                                                     │
│  tables:                                            │
│    - services          （服务注册表）                │
│    - spans             （Trace/Span 数据）          │
│    - flow_metrics      （网络指标时序数据）          │
│    - alert_events      （告警事件）                  │
└─────────────────────────────────────────────────────┘
```

---

## 二、以 Trace 链路追踪为例的具体实现

### 数据库表设计（ClickHouse）

```sql
-- 服务注册表
CREATE TABLE services (
    id          UInt32,
    name        String,          -- 如 "frontend", "payment-service"
    type        UInt8,           -- 11=pod_service, 1=vm, etc
    icon_id     Int32,           -- 图标ID，前端用于显示
    protocol    String           -- HTTP, gRPC, MySQL, Redis...
) ENGINE = MergeTree() ORDER BY id;

-- Span 表（分布式链路追踪）
CREATE TABLE spans (
    trace_id        String,
    span_id         String,
    parent_span_id  String,
    service_name    String,
    operation_name  String,
    start_time      DateTime64(6),
    end_time        DateTime64(6),
    duration_us     UInt64,          -- 微秒
    status_code     UInt16,          -- HTTP status / gRPC code
    response_status UInt8,           -- 0=正常, 1=客户端异常, 2=服务端异常
    attributes      Map(String, String),
    -- 网络层字段
    client_ip       String,
    server_ip       String,
    client_port     UInt16,
    server_port     UInt16
) ENGINE = MergeTree()
PARTITION BY toDate(start_time)
ORDER BY (service_name, start_time);

-- 流量指标（按分钟聚合）
CREATE TABLE flow_metrics (
    time            DateTime,
    service_id      UInt32,
    peer_service_id UInt32,         -- 0=自身指标, >0=边指标
    request_rate    Float64,        -- 请求/秒
    error_ratio     Float64,        -- 异常比例 0~1
    response_delay  Float64,        -- 响应时延(微秒)
    byte_tx         UInt64,
    byte_rx         UInt64
) ENGINE = MergeTree()
PARTITION BY toDate(time)
ORDER BY (service_id, peer_service_id, time);
```

### Go Handler 实现要点

#### 1. 全景拓扑（服务地图）
前端请求 `POST /api/statistics/v1/stats/querier/Topo`，期望返回：

```json
{
  "OPT_STATUS": "SUCCESS",
  "DATA": {
    "instance_data": [
      {
        "node_type": "pod_service",
        "icon_id": -16,
        "auto_service_id": 143,
        "auto_service": "frontend",
        "auto_service_type": 11,
        "is_internet": 0,
        "请求速率": 26.6,
        "服务端异常比例": 0.0,
        "响应时延": 1052.3,
        "uid": "auto_service=frontend,auto_service_id=143,...",
        "_querier_region": "default"
      }
    ],
    "peers_data": [
      {
        "client_node_type": "pod_service",
        "auto_service_0": "frontend",
        "auto_service_id_0": 143,
        "server_node_type": "pod_service",
        "auto_service_1": "productcatalogservice",
        "auto_service_id_1": 176,
        "请求速率": 12.1,
        "服务端异常比例": 0.0,
        "响应时延": 791.5
      }
    ]
  }
}
```

**Go 伪代码**：
```go
func TopoHandler(w http.ResponseWriter, r *http.Request) {
    // 1. 从 flow_metrics 聚合每个 service 的指标 → instance_data
    rows := db.Query(`
        SELECT service_id, sum(request_rate), avg(response_delay), 
               sumIf(request_rate, response_status=2)/sum(request_rate)
        FROM flow_metrics
        WHERE time >= now() - INTERVAL 1 HOUR AND peer_service_id = 0
        GROUP BY service_id
    `)
    
    // 2. 从 flow_metrics 聚合边指标 → peers_data
    edges := db.Query(`
        SELECT service_id, peer_service_id, sum(request_rate), avg(response_delay)
        FROM flow_metrics
        WHERE time >= now() - INTERVAL 1 HOUR AND peer_service_id > 0
        GROUP BY service_id, peer_service_id
    `)
    
    // 3. Join services 表拿名称，组装响应
    json.NewEncoder(w).Encode(response)
}
```

#### 2. Trace 调用链查询
前端请求 `POST /api/querier/v1/query/`，body 含 SQL：
```json
{"db": "flow_log", "sql": "SELECT * FROM l7_flow_log WHERE trace_id='abc123' ORDER BY start_time"}
```

**Go 实现**：
```go
func QueryHandler(w http.ResponseWriter, r *http.Request) {
    var req struct { DB string `json:"db"`; SQL string `json:"sql"` }
    json.NewDecoder(r.Body).Decode(&req)
    
    // 解析 SQL，提取 trace_id 条件
    // 查 spans 表，返回该 trace 下所有 span
    spans := db.Query(`SELECT * FROM spans WHERE trace_id = ?`, traceID)
    
    // 转为前端期望格式
    json.NewEncoder(w).Encode(map[string]any{
        "OPT_STATUS": "SUCCESS",
        "DATA": spans,
    })
}
```

#### 3. 服务列表（资源页）
前端请求 `GET /api/deepflow-server/v2/pod-services?field=ID&field=NAME`

```go
func PodServicesHandler(w http.ResponseWriter, r *http.Request) {
    services := db.Query(`SELECT id, name, type FROM services`)
    json.NewEncoder(w).Encode(map[string]any{
        "OPT_STATUS": "SUCCESS",
        "DATA": services,
    })
}
```

---

## 三、需要实现的 API 端点（按优先级）

### P0 — 页面能渲染
| 端点 | 方法 | 作用 | 实现方式 |
|------|------|------|----------|
| `/api/fauths/login` | POST | 登录 | 硬编码返回 token |
| `/api/fauths/login_list` | GET | 登录方式 | 硬编码 |
| `/api/fuser/v1/users/current` | GET | 当前用户 | 硬编码 |
| `/api/fpermit/v1/orgs` | GET | 组织 | 硬编码 |
| `/api/fpermit/v1/org/4/page_scopes` | GET | 页面权限 | 硬编码（全开） |
| `/api/fpermit/v1/org/4/teams` | GET | 团队 | 硬编码 |
| `/api/df-web/v1/icons` | GET | 图标 | 直接返回缓存文件 |
| `/api/df-web/v1/config/system` | GET | 系统配置 | 硬编码 |

### P1 — 拓扑图和概览
| 端点 | 方法 | 作用 | 数据来源 |
|------|------|------|----------|
| `/api/statistics/v1/stats/querier/Topo` | POST | 全景拓扑 | flow_metrics 表聚合 |
| `/api/df-web-composer/api/service_topo/...` | GET/POST | 服务拓扑详情 | flow_metrics + services |
| `/api/df-web/v1/dashboards` | GET | 仪表盘列表 | dashboards 表 |
| `/api/df-web/v1/biz/{uuid}` | GET | 仪表盘详情 | dashboards 表 |

### P2 — Trace 链路追踪
| 端点 | 方法 | 作用 | 数据来源 |
|------|------|------|----------|
| `/api/querier/v1/query/` | POST | SQL 查询 | 解析 SQL → spans/flow_metrics |
| `/api/statistics/v1/stats/querier/DBDescription/ShowDatabases` | POST | 数据库列表 | 硬编码 |
| `/api/statistics/v1/stats/querier/DBDescription/ShowTables` | POST | 表列表 | 硬编码 |
| `/api/statistics/v1/stats/querier/DBDescription/ShowTags` | POST | 可用标签 | 硬编码元数据 |
| `/api/statistics/v1/stats/querier/DBDescription/ShowMetrics` | POST | 可用指标 | 硬编码元数据 |

### P3 — 告警
| 端点 | 方法 | 作用 | 数据来源 |
|------|------|------|----------|
| `/api/alarm/v1/deepflow/alarm-policies/` | GET | 告警策略 | alert_policies 表 |
| `/api/alarm/v1/deepflow/alarm-endpoints/` | GET | 通知渠道 | 硬编码 |

---

## 四、项目结构

```
backend/
├── main.go                    # 入口，路由注册
├── config.yaml                # 数据库连接、端口配置
├── go.mod
│
├── handler/
│   ├── auth.go                # P0: 登录/用户/权限（全硬编码）
│   ├── resource.go            # P1: 资源列表（services→DB）
│   ├── topo.go                # P1: 全景拓扑（flow_metrics→DB）
│   ├── querier.go             # P2: SQL查询引擎（spans/metrics→DB）
│   ├── dashboard.go           # P1: 仪表盘CRUD
│   └── meta.go                # P2: ShowDatabases/ShowTables/ShowTags
│
├── db/
│   ├── clickhouse.go          # ClickHouse 连接池
│   └── query.go               # 通用查询封装
│
├── model/
│   ├── topo.go                # Topo 响应结构体
│   ├── span.go                # Span 结构体
│   └── service.go             # Service 结构体
│
├── static/                    # → symlink 到 cloud.deepflow.yunshan.net/
└── migrations/
    └── 001_init.sql           # 建表 SQL
```

---

## 五、接入 Trace 数据的具体步骤

### 1. 数据写入
你的 Trace 数据通过以下任一方式写入 ClickHouse：
- **OpenTelemetry Collector** → ClickHouse Exporter
- **Jaeger** → ClickHouse Storage
- **自定义 Agent** → 直接 INSERT

### 2. Go 后端查询
```go
// 从 ClickHouse 查 trace
func getTrace(traceID string) []Span {
    rows, _ := db.Query(`
        SELECT trace_id, span_id, parent_span_id, service_name,
               operation_name, start_time, end_time, duration_us,
               status_code, response_status
        FROM spans 
        WHERE trace_id = $1
        ORDER BY start_time
    `, traceID)
    // ...
}
```

### 3. 前端显示
前端 Trace 页面会发 SQL 查询到 `/api/querier/v1/query/`，你的 Go 后端解析后查 DB 返回，前端自动渲染火焰图/瀑布图。

---

## 六、关键点总结

1. **前端零修改** — 所有 Vue 代码原封不动，只实现后端 API
2. **Go 后端 ≈ API 适配器** — 把你的数据库数据转换为 DeepFlow API 格式
3. **渐进式实现** — P0 硬编码让页面能打开 → P1 拓扑图有数据 → P2 Trace 链路可查
4. **核心工作量**：
   - 解析前端 SQL 查询 → ClickHouse SQL（约 200 行）
   - 拓扑聚合逻辑（约 150 行）
   - Auth/Meta 硬编码（约 100 行）
   - 总计 Go 代码 ~1000 行可跑起基本功能
5. **数据格式约定**：所有 API 必须包装为 `{"OPT_STATUS":"SUCCESS","DATA":...}`
