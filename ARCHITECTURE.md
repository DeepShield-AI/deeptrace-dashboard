# DeepFlow 后端架构设计

## 目录结构

```
backend/
├── main.go                        # 入口：启动 HTTP 服务、注册路由
├── config/
│   └── config.go                  # 配置管理（端口、数据库、JWT 密钥等）
├── middleware/
│   ├── cors.go                    # CORS + X-Org-Id 注入
│   ├── auth.go                    # JWT 认证中间件
│   ├── logger.go                  # 请求日志
│   └── gzip.go                    # Gzip 压缩
├── router/
│   └── router.go                  # 路由注册（按模块分组）
├── handler/                       # HTTP 处理层（thin layer，只做参数解析+响应序列化）
│   ├── auth.go                    # POST /api/fauths/login, login_list
│   ├── user.go                    # GET /api/fuser/v1/users/current, user conf, user datas
│   ├── org.go                     # GET /api/fpermit/v1/orgs, org select, teams, roles
│   ├── querier.go                 # POST /api/statistics/v1/stats/querier/* (List/Top/Topo/TraceMap/Profile)
│   ├── db_description.go          # POST ShowDatabases/ShowTables/ShowTags/ShowMetrics/ShowTagValues
│   ├── query.go                   # POST /api/querier/v1/query/ (原始 SQL 查询)
│   ├── dashboard.go               # CRUD /api/df-web/v1/dashboards, /dashboard/:id/panels
│   ├── biz.go                     # GET /api/df-web/v1/biz, biz_groups, biz_entry_path
│   ├── service_topo.go            # GET /api/df-web-composer/api/service_topo/biz/:id/*
│   ├── resource.go                # GET /api/deepflow-server/v2/* (pods/vms/vpcs/subnets/domains...)
│   ├── agent.go                   # GET /api/deepflow-server/v1/vtaps, vtap-groups, vtap-group-configuration
│   ├── alarm.go                   # CRUD /api/alarm/v1/deepflow/alarm-policies, silence-policies
│   ├── report.go                  # GET /api/report/v1/deepflow/report-policies, reports
│   ├── config_web.go              # GET /api/df-web/v1/config/system, outerlinks, icons, logo_info
│   ├── search_history.go          # CRUD /api/df-web/v1/search-histories
│   ├── ai.go                      # /api/df-web-composer/api/ai/* (conversations, workflows, inspection)
│   └── static.go                  # 静态文件 + SPA fallback
├── service/                       # 业务逻辑层
│   ├── auth_service.go            # 登录验证、Token 生成/刷新
│   ├── user_service.go            # 用户信息、配置读写
│   ├── org_service.go             # 组织/团队/权限管理
│   ├── querier_service.go         # 核心查询引擎（SQL 构建、指标聚合、时间分组）
│   ├── trace_service.go           # 追踪数据查询（TraceMap、FlowLogDetailList）
│   ├── topo_service.go            # 拓扑计算（实例数据+关系数据→拓扑图）
│   ├── profile_service.go         # 性能剖析数据
│   ├── dashboard_service.go       # 仪表盘 CRUD + panel 配置
│   ├── biz_service.go             # 业务定义、入口路径分析
│   ├── resource_service.go        # 云资源管理（同步+查询）
│   ├── agent_service.go           # 采集器管理
│   ├── alarm_service.go           # 告警策略/静默策略/事件
│   ├── tag_service.go             # 标签枚举值查询 (ShowTagValues, fast_list)
│   └── schema_service.go          # DB/Table/Metric 元数据管理
├── model/                         # 数据模型定义
│   ├── user.go                    # User, Org, Team, Role
│   ├── auth.go                    # LoginRequest, TokenPair, Claims
│   ├── query.go                   # QueryRequest, QueryResult, Column, Row
│   ├── trace.go                   # Span, Trace, TraceMap
│   ├── topo.go                    # TopoNode, TopoPeer, ServiceTopo
│   ├── dashboard.go               # Dashboard, Panel, Widget
│   ├── resource.go                # Pod, VM, VPC, Subnet, Domain, Region...
│   ├── agent.go                   # VTap, VTapGroup, VTapGroupConfig
│   ├── alarm.go                   # AlarmPolicy, SilencePolicy, AlarmEvent
│   ├── schema.go                  # Database, Table, Tag, Metric, MetricFunction
│   └── common.go                  # APIResponse{OPT_STATUS, DATA, DESCRIPTION}
├── store/                         # 数据存储层（可插拔）
│   ├── interface.go               # Store 接口定义
│   ├── clickhouse/                # ClickHouse 实现（生产）
│   │   ├── client.go              # 连接管理
│   │   ├── querier.go             # SQL 查询执行
│   │   ├── trace.go               # 追踪数据查询
│   │   └── metric.go              # 指标数据查询
│   ├── sqlite/                    # SQLite 实现（轻量部署）
│   │   ├── client.go
│   │   ├── querier.go
│   │   └── migration.go           # 建表/迁移
│   └── memory/                    # 内存实现（开发/测试）
│       └── store.go               # 基于 JSON 文件的内存存储
└── pkg/                           # 通用工具包
    ├── jwt/
    │   └── jwt.go                 # JWT 生成/验证
    ├── response/
    │   └── response.go            # 统一响应格式 {OPT_STATUS, DATA, DESCRIPTION}
    ├── sql/
    │   └── builder.go             # DeepFlow SQL 方言构建器
    └── util/
        └── util.go                # 时间解析、分页等工具函数
```

## 模块职责

### 1. 认证模块 (`handler/auth.go` + `service/auth_service.go`)

| 接口 | 说明 |
|------|------|
| `POST /api/fauths/login` | 用户登录，返回 access_token + refresh_token |
| `GET /api/fauths/login_list` | 支持的登录方式（密码/LDAP/OAuth） |
| `GET /api/fuser/v1/users/current` | 当前用户信息 |
| `GET /api/warrant/check/license` | License 校验 + 功能授权列表 |

### 2. 组织权限模块 (`handler/org.go` + `service/org_service.go`)

| 接口 | 说明 |
|------|------|
| `GET /api/fpermit/v1/orgs` | 组织列表 |
| `GET /api/fpermit/v1/org/:id/select` | 切换组织（设置 X-Org-Id） |
| `GET /api/fpermit/v1/org/:id/teams` | 团队列表 |
| `GET /api/fpermit/v1/org/:id/page_scopes` | 页面权限范围 |
| `GET /api/fpermit/v1/org/:id/role_teams` | 角色-团队关系 |

### 3. 查询引擎模块 (`handler/querier.go` + `service/querier_service.go`)

核心模块，处理所有数据查询：

| 接口 | 说明 |
|------|------|
| `POST .../querier/List` | 列表查询（流日志、Span 列表） |
| `POST .../querier/Top` | TopN 排序查询 |
| `POST .../querier/Topo` | 拓扑数据查询（instance_data + peers_data） |
| `POST .../querier/TraceMap` | 追踪火焰图/瀑布图数据 |
| `POST .../querier/Profile` | 性能剖析（CPU/Mem） |
| `POST .../querier/FlowLogDetailList` | 流日志详情 |
| `POST /api/querier/v1/query/` | 原始 SQL 查询 |

**请求格式**：
```json
{
  "DATABASE": "flow_log",
  "TABLE": "l7_flow_log",
  "selects": [...],
  "conditions": [...],
  "groupBy": [...],
  "orderBy": [...],
  "limit": 100
}
```

### 4. Schema 元数据模块 (`handler/db_description.go` + `service/schema_service.go`)

| 接口 | 说明 |
|------|------|
| `POST .../ShowDatabases` | 数据库列表（flow_metrics/flow_log/event/profile...） |
| `POST .../ShowTables` | 指定 DB 的表列表 + datasources |
| `POST .../ShowTags` | 指定 DB+Table 的标签列表（含 type/display_name） |
| `POST .../ShowMetrics` | 指定 DB+Table 的指标列表（含 unit/is_agg） |
| `POST .../ShowMetricsFunctions` | 可用的聚合函数 |
| `POST .../ShowTagValues` | 标签枚举值（带 LIKE 过滤和分页） |

### 5. 仪表盘模块 (`handler/dashboard.go` + `service/dashboard_service.go`)

| 接口 | 说明 |
|------|------|
| `GET /api/df-web/v1/dashboards` | 仪表盘列表 |
| `GET /api/df-web/v1/dashboard/:id` | 仪表盘详情 |
| `GET /api/df-web/v1/dashboard/:id/panels` | 面板列表 |
| `POST /api/df-web/v1/dashboards` | 创建仪表盘 |
| `PUT /api/df-web/v1/dashboard/:id` | 更新仪表盘 |

### 6. 业务拓扑模块 (`handler/biz.go` + `handler/service_topo.go`)

| 接口 | 说明 |
|------|------|
| `GET /api/df-web/v1/biz` | 业务列表 |
| `GET /api/df-web/v1/biz/:uuid/biz_entry_path` | 业务入口路径 |
| `GET .../service_topo/biz/:uuid/entry_path_overview` | 入口路径概览（趋势+列表） |
| `GET .../service_topo/biz/:uuid/entry_path_alert_events_statistic` | 入口路径告警统计 |

### 7. 云资源模块 (`handler/resource.go` + `service/resource_service.go`)

| 接口 | 说明 |
|------|------|
| `GET /api/deepflow-server/v2/pods` | Pod 列表 |
| `GET /api/deepflow-server/v2/vms` | 虚拟机列表 |
| `GET /api/deepflow-server/v2/epcs` (vpcs) | VPC 列表 |
| `GET /api/deepflow-server/v2/subnets` | 子网列表 |
| `GET /api/deepflow-server/v2/domains/` | 域/云平台列表 |
| `GET /api/deepflow-server/v2/regions` | 区域列表 |
| `GET /api/deepflow-server/v2/processes` | 进程列表 |
| `GET /api/deepflow-server/v2/pod-clusters` | K8s 集群列表 |
| ... 共 15+ 种资源 | |

### 8. 采集器模块 (`handler/agent.go` + `service/agent_service.go`)

| 接口 | 说明 |
|------|------|
| `GET /api/deepflow-server/v1/vtaps/` | 采集器列表 |
| `GET /api/deepflow-server/v1/vtap-groups/` | 采集器组列表 |
| `GET /api/deepflow-server/v1/vtap-group-configuration/` | 组配置 |
| `GET /api/deepflow-server/v1/data-sources/` | 数据源定义 |

### 9. 告警模块 (`handler/alarm.go` + `service/alarm_service.go`)

| 接口 | 说明 |
|------|------|
| `GET /api/alarm/v1/deepflow/alarm-policies/` | 告警策略列表 |
| `GET /api/alarm/v1/deepflow/alarm-endpoints/` | 告警通知终端 |
| `GET /api/alarm/v1/deepflow/silence-policies/` | 静默策略 |

### 10. AI 模块 (`handler/ai.go`)

| 接口 | 说明 |
|------|------|
| `GET .../ai/conversations` | AI 对话列表 |
| `GET .../ai/conversations/v3/models` | 可用模型列表 |
| `GET .../ai/workflows` | AI 工作流 |
| `GET .../ai/inspection-tasks` | AI 巡检任务 |

## 数据存储方案

### 方案 A：ClickHouse（生产级）

```
┌─────────────────────────────────────────────────┐
│                 ClickHouse                       │
├─────────────────────────────────────────────────┤
│ flow_metrics.network        网络指标（1s/1m）    │
│ flow_metrics.application    应用指标（1s/1m）    │
│ flow_log.l4_flow_log        四层流日志           │
│ flow_log.l7_flow_log        七层流日志（Span）   │
│ event.event                 系统事件             │
│ event.alert_event           告警事件             │
│ profile.in_process          性能剖析             │
│ application_log.log         应用日志             │
└─────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────┐
│            MySQL / PostgreSQL                     │
├─────────────────────────────────────────────────┤
│ users, orgs, teams, roles    用户权限            │
│ dashboards, panels           仪表盘配置          │
│ alarm_policies               告警策略            │
│ biz, biz_entry_paths         业务定义            │
│ vtaps, vtap_groups           采集器管理          │
│ search_histories             搜索历史            │
└─────────────────────────────────────────────────┘
```

### 方案 B：SQLite（轻量部署，推荐起步）

```
backend/
├── data/deepflow.db           # 配置数据（用户/仪表盘/告警等）
└── data/metrics.db            # 时序数据（指标/流日志/追踪）
```

### 方案 C：内存 + JSON（当前方案的演进）

保留现有 `data/*.json` 文件作为数据源，适合 demo/展示场景。

## 查询引擎核心流程

```
前端请求（JSON DSL）
    ↓
handler/querier.go          解析请求参数
    ↓
service/querier_service.go  构建查询
    ↓
pkg/sql/builder.go          生成 DeepFlow SQL
    ↓
store/clickhouse/querier.go 执行查询
    ↓
model/query.go              格式化结果 {columns, values}
    ↓
响应返回
```

## 实现优先级

| 优先级 | 模块 | 原因 |
|--------|------|------|
| P0 | 认证 + 组织权限 | 页面加载必需 |
| P0 | Schema 元数据 | 页面加载必需（ShowDatabases/Tables/Tags/Metrics） |
| P0 | 配置接口 | system config, icons, logo_info |
| P1 | 查询引擎 (List) | 追踪/日志页面核心数据 |
| P1 | 拓扑查询 (Topo) | 网络拓扑/应用拓扑 |
| P1 | TraceMap | 追踪详情瀑布图 |
| P2 | 仪表盘 CRUD | 自定义面板 |
| P2 | 云资源 | 基础设施管理页 |
| P2 | 采集器管理 | Agent 管理页 |
| P3 | 告警系统 | 告警策略管理 |
| P3 | AI 模块 | AI 对话/巡检 |
| P3 | 报表模块 | 定时报表 |

## 技术选型建议

| 组件 | 选择 | 理由 |
|------|------|------|
| Web 框架 | 标准库 `net/http` + chi router | 轻量、无依赖 |
| 配置 | `viper` | YAML/ENV 支持 |
| 数据库驱动 | `clickhouse-go` + `modernc.org/sqlite` | 纯 Go，无 CGO |
| JWT | `golang-jwt/jwt/v5` | 标准 JWT 库 |
| 日志 | `slog`（Go 1.21+） | 标准库结构化日志 |
| 迁移 | `golang-migrate` | 数据库版本管理 |
| 测试 | `testing` + `testify` | 标准 + 断言库 |
