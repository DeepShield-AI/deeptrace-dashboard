# DeepTrace Dashboard 后端开发指南

## 项目概览

可观测性平台前端（编译 JS，不可读源码）+ Go 后端。后端 API 行为通过**观察前端请求链 + api_cache 响应结构**反推。

> **核心原则**：不读前端 JS，只看 API 调用链和 api_cache。

## 后端架构

```
backend/
├── main.go                        # 入口：组装数据源、启动服务
├── config/config.go               # 环境变量配置
├── transport/                     # HTTP 路由 + Handler
│   ├── router.go                  # 路由注册总入口
│   ├── flowlog.go                 # FlowLogDetailHandler
│   ├── querier.go                 # List/Top 等查询 Handler
│   ├── auth.go                    # 认证（硬编码）
│   ├── resource.go                # 基础设施资源
│   ├── dashboard.go               # 仪表盘
│   ├── dbdesc.go                  # Schema 元数据
│   ├── fallback.go                # 兜底路由
│   └── ...
├── query/                         # 业务查询逻辑（核心）
│   ├── flowlog.go                 # FlowLogDetail：deepflow-server 直连
│   ├── list.go / top.go           # List/Top：DataSourceChain
│   ├── tracemap.go                # TraceMap
│   ├── chain.go                   # DataSourceChain 定义
│   └── types.go                   # Result / Envelope
├── client/                        # 外部服务客户端
│   └── zerotrace.go               # deepflow-server HTTP 客户端
├── clickhouse/                    # ClickHouse 直连引擎
│   ├── builder.go                 # SQL 构建
│   ├── querier.go                 # 查询方法
│   └── scanner.go                 # 结果扫描
├── engine/                        # 通用工具
│   └── helpers.go                 # IconIDDefault, FormatTimestamp
├── source/                        # DataSourceChain 各数据源适配器
├── cache/cache.go                 # api_cache 加载 + 查找
├── aggregator/aggregator.go       # 从 traces.json 聚合数据
└── data/                          # 模拟数据 JSON
```

## 数据来源

```
请求 → 硬编码（认证/组织/配置）
     → DataSourceChain（api_cache → backend/data → deepflow-server → ClickHouse → []）
     → deepflow-server 直连（FlowLogDetail / 正在迁移中）
```

### 数据源迁移状态

各 API 正在从 **api_cache + 文件模拟** 向 **deepflow-server 直连** 迁移。
迁移计划和进度见 [docs/migration-plan.md](docs/migration-plan.md)，**实现前必须查看该文档确认进度**。

| 状态 | 含义 |
|------|------|
| ✅ | 已完成（deepflow-server 直连，经用户确认） |
| ⏳ | 待实现 |
| ❌ | 无需改 |

各文件作用：
- **api_cache/** — 363 个真实 API 响应缓存，**参照标准，不主动修改**；迁移完成后仅作参考
- **backend/data/** — 模拟 JSON（可修改），用于无真实数据时 Demo；**正在逐步淘汰**
- **deepflow-server**:20416 — ClickHouse query 代理（**核心数据源**）
- **ClickHouse** (zt-clickhouse:9000) — 终极数据源

### 已完成迁移

| 端点 | 数据源 | 参考文件 |
|------|-------|---------|
| `POST .../FlowLogDetailList` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../FlowLogDetailInfo` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../FlowLogDetailHistory` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../FlowLogDetailSearch` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../FlowLogAsyncDetail` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../FlowLogTimingDetailList` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../FlowLogTimingDetailHistory` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../TracingDetailList` | deepflow-server 直连 | `query/flowlog.go` |
| `POST .../FlowMap` | deepflow-server 直连 | `query/flowlog.go` |

> **重要**: FlowLogDetail 系列已经完成，**不要再加 cache/chain**。不走 DataSourceChain，直接调 deepflow-server。

## 详细文档

| 文档 | 内容 |
|------|------|
| [docs/deepflow-server.md](docs/deepflow-server.md) | query 接口、原生函数支持、列名映射、后处理要点 |
| [docs/schema.md](docs/schema.md) | l7_flow_log 按类别列参考、其他表、快速查询命令 |
| [docs/workflow.md](docs/workflow.md) | 开发模板、SQL 构建规则、API 链、实现状态、验证命令 |

## 快速参考

### 构建 & 启动

```bash
cd backend && go build -o deeptrace-server . && ./deeptrace-server
```

配置见 `backend/.env`，可被 `backend/.env.local` 覆盖（已 gitignore）。

### 核心配置

| 变量 | 默认值 | 说明 |
|------|-------|------|
| `PORT` | `8888` | 服务端口 |
| `STATIC_DIR` | `../cloud.deepflow.yunshan.net` | 前端静态文件 |
| `DATA_DIR` | `./data` | 模拟数据路径 |
| `CACHE_DIR` | `../api_cache` | API 缓存路径 |
| `ZEROTRACE_ADDR` | `localhost:20416` | deepflow-server 地址 |
| `CLICKHOUSE_HOST` | 空（不启用） | ClickHouse 直连 |
| `ALGORITHMS_ADDR` | 空（不启用） | 算法服务 |

### 关键规则

1. **不修改 api_cache/** — 参照标准，验证后端返回格式
2. **响应包裹** — `{"OPT_STATUS":"SUCCESS","DATA":...,"DESCRIPTION":""}`
3. **deepflow-server 优先** — Enum/icon_id/node_type/newTag/auto_service 透传到 SQL，Go 端不做预处理
4. **时间戳用 Unix 整数** — WHERE 子句中 `time >= %d` 避免 timezone 偏移
5. **新增 Handler** — transport/ 下加文件 → 实现 Register* → router.go 中注册
6. **_querier_region** — 每条数据加 `"本地"`
7. **UID** — Top 查询结果需要，通常由 tag 值拼接
8. **cache 匹配** — POST 用 `cache.FindWithBody()`, GET 用 `cache.Find()`
9. **实现前先看 [docs/migration-plan.md](docs/migration-plan.md)** — 检查迁移进度，已完成的不要重复做
10. **FlowLogDetail 系列已迁移完成** — 不要加 cache，不要走 Chain，直接调 deepflow-server

### 迁移方法论（重要）

所有 API 迁移必须遵循以下流程，确保逐步、可验证地替换 mock 数据。

#### M1. 迁移流程（五步）

```
Step 1 — 参数覆盖矩阵
   提取 api_cache 中该 endpoint 所有请求的 (DATABASE, TABLE, SELECT 特征, WHERE 模式, 分页参数)，
   列出你的实现必须覆盖的全部组合。

Step 2 — 查询 query/ 下参考模板
   flowlog/list.go 是 deepflow-server 直连的标准模板：
   • 解析请求 body
   • 构建 SQL（DeepFlow DSL 原语透传）
   • zt.QueryRaw(db, sql)
   • buildData/buildSchemas 后处理
   • 返回 query.Result

Step 3 — 安全插入 DataSourceChain
   新实现在 main.go 中通过 chain.AddListSource() / chain.AddTopSource() 
   **插在 cache 之后、mock 之前**：
       cache → NEW_ZT_SOURCE → mock → CH → zt → agg
   • 出错时间 nil, nil（chain 自动 fallback 到 cache）
   • 不要跳过 cache 直接替换（除非已对全部 cache 条目验证通过）

Step 4 — 回放验证（必须）
   用 api_cache 中该 endpoint 的所有条目做回归校验：
   • 对每条 cache 记录，提取 requestBody → POST 到后端
   • 比响应**结构**不比数值：DATA 的 key 集合、值类型、SCHEMAS 结构、TYPE 值
   • 至少覆盖表中列出的参数组合 80% 才算完成

Step 5 — 提升优先级
   所有 cache 验证通过后 → 移除该 endpoint 的 cache/mock source → 标记 migration-plan.md 完成
```

#### M2. 批量实现策略

**不要按端点类型单点实现**，按前端页面依赖集群批量实现：

```
Batch 1（Schema 层，互相独立）
  ShowTags + ShowMetrics + ShowTagValues
Batch 2（页面加载核心，必须一起）
  List + Top + Profile
Batch 3（高级功能）
  TraceMap + Histogram + Composer
```

这么做是因为前端页面点击会依次调用 A→B→C，只实现 A 但 B 出错，C 就不会被调用且你无法调试。

#### M3. api_cache 是验收标准

- api_cache 的 363 条记录是**真实请求-响应对**，不是 mock 数据
- 每个端点迁移前先跑参数覆盖矩阵分析（提取 DATABASE/TABLE/SELECT 特征/WHERE）
- 每个端点迁移后用 cache replay 做**结构回归验证**
- 验证通过前不要移除 DataSourceChain 中的 cache/mock source

#### M4. DataSourceChain 安全模式

```
Schema 类（ShowTags/ShowMetrics）：cache → ZT → data file
    ↑ cache 精确匹配可靠，ZT 做后盾

数据类（List/Top）: ZT → cache → mock → CH → agg
    ↑ ZT 优先！因为 cache 的模糊匹配对参数变化大的查询不可靠
      （DB/TABLE 相同但 SELECT/WHERE 不同时返回错误缓存）
```

• Schema 类（ShowTags/ShowMetrics）：新实现在 cache 之后
• 数据类（List/Top）：新实现在 cache 之前（ZT 返回精确数据）
• 返回格式：出错回 `nil, nil`（chain 跳过当前 source）；正常返回 `Result{Data, Count, Type, Fields}`
• 不要返回 error 打断 chain；不要直接删 cache/source 层
• FlowLogDetail 系列例外 — 已经完成，不要加 cache，不要走 Chain

**chain 源注册顺序**（main.go）：
```go
// List/Top: ZT → cache → ...
chain.AddListSource(ztListDS)
chain.AddListSource(cacheDS)

// Schema: cache → ZT → ...
// (通过 transport handler 直接调，不走 chain)
```

#### M5. 回放验证命令

```bash
# 验证后端结果结构与 api_cache 一致
tools/replay_from_cache.py <EndpointName>

# 只显示参数覆盖矩阵（实现前必做）
tools/replay_from_cache.py <EndpointName> --coverage-only

# 批量验证所有 endpoint
tools/replay_from_cache.py --all
```

#### M6. 依赖图谱与自动化验证工具

项目自带两个工具，放在 `tools/` 目录下：

| 工具 | 功能 | 运行时机 |
|------|------|---------|
| `tools/dependency_graph.py` | 输出完整依赖图谱、迁移批次建议、验证状态、参数流动 | 规划迁移批次时 |
| `tools/replay_from_cache.py` | 对 endpoint 全部 cache 条目做结构回归验证 | 每次实现后必须跑 |

**依赖图谱用法：**
```bash
# 查看完整依赖图谱（用 --endpoint 查具体端点）
python3 tools/dependency_graph.py

# 输出 Mermaid 格式流程图（可粘贴到 Markdown 渲染）
python3 tools/dependency_graph.py --mermaid

# 查看迁移批次建议
python3 tools/dependency_graph.py --batches

# 查看整体验证状态
python3 tools/dependency_graph.py --status

# 查看参数流动关系（输出→输入）
python3 tools/dependency_graph.py --param-flow
```

<｜｜DSML｜｜parameter name="replace_all" string="false">false
