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
     → DataSourceChain（deepflow-server → ClickHouse → api_cache → []）
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
| `VERIFY_SOURCE_CONTROL` | `false` | 本地回放时允许强制指定数据源；禁止在公开环境开启 |

### 关键规则

1. **不修改 api_cache/** — 参照标准，验证后端返回格式
2. **响应包裹** — `{"OPT_STATUS":"SUCCESS","DATA":...,"DESCRIPTION":""}`
3. **deepflow-server 优先** — Enum/icon_id/node_type/newTag/auto_service 透传到 SQL，Go 端不做预处理
4. **时间戳用 Unix 整数** — WHERE 子句中 `time >= %d` 避免 timezone 偏移
5. **新增 Handler** — transport/ 下加文件 → 实现 Register* → router.go 中注册
6. **_querier_region** — 每条数据加 `"本地"`
7. **UID** — Top 查询结果需要，通常由 tag 值拼接
8. **cache 匹配** — POST 优先精确/规范化 body 匹配；禁止把仅 DB+TABLE 的模糊命中作为迁移验收
9. **实现前先看 [docs/migration-plan.md](docs/migration-plan.md)** — 检查迁移进度，已完成的不要重复做
10. **FlowLogDetail 系列已迁移完成** — 不要加 cache，不要走 Chain，直接调 deepflow-server

### 迁移方法论（重要）

所有 API 迁移必须遵循以下流程，确保逐步、可验证地替换 mock 数据。

#### M1. 迁移流程（六步）

```
Step 1 — 端点与参数变体盘点
   用 replay_from_cache.py 提取所有真实请求；迁移单位不是 endpoint 名，
   而是 endpoint + DB/TABLE + SELECT 函数 + GROUP_BY + history/interval + 分页。

Step 2 — 保存原始请求语义
   Handler 必须保留 raw JSON；新增字段不能在 Unmarshal → Marshal 中被静默丢弃。
   对未知字段输出诊断，确认是否影响 SQL 与返回结构。

Step 3 — 建立引擎能力判断
   明确该参数签名由 zerotrace、ClickHouse 或 cache 处理。
   不支持必须返回 nil；支持且查询结果为空必须返回非 nil 的 DATA=[]。

Step 4 — 强制数据源回放（必须）
   启动 VERIFY_SOURCE_CONTROL=true 的本地后端，使用 --source + --strict：
   • 回放请求必须收到 X-DeepTrace-Source 且与指定源一致
   • 禁止回退到 cache 后把 cache 与自身比较
   • 比较全部 DATA 行的字段/类型，以及 SCHEMAS 的 unit/value_type/pre_as/type

Step 5 — 页面依赖链验证
   按实际页面调用链验证上游响应正确后，下游 API 仍会继续发出。
   记录浏览器 console error、请求顺序和关键 UI 状态。

Step 6 — 按参数签名提升
   只有已通过强制源回放和页面验证的参数签名才切到真实数据源。
   未支持签名显式保留 cache；不要因为某个 endpoint 部分通过就整端点移除 cache。
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

#### M3. api_cache 是契约样本，不是真实数据源验收本身

- api_cache 的 363 条记录是**真实请求-响应对**，不是 mock 数据
- 它用于提取参数覆盖和响应契约；数值可因时间与环境不同而变化
- `--source auto` 只用于兼容性冒烟测试，不能作为迁移完成证据
- 迁移验收必须使用 `--source zerotrace|clickhouse --strict`
- 强制源回放缺少 `X-DeepTrace-Source` 时直接失败
- 回放真实数据可加 `--rewrite-time`，保持原窗口长度并移动到当前时间
- 验证通过前保留 cache，但验收请求必须禁用 fallback

#### M4. DataSourceChain 正确语义

```
正常请求:
  ZT → ClickHouse → exact cache → empty

强制验收:
  指定源且禁止 fallback；源不可用/不支持时返回 HTTP 502
```

- `result != nil, DATA=[]`：该源已处理且结果确实为空，必须停止 chain
- `result == nil, err == nil`：该源不支持此参数签名，可以尝试下一源
- `err != nil`：执行失败；正常模式记录后按策略处理，强制验收模式立即失败
- 成功响应由 `X-DeepTrace-Source` 标记实际来源
- FlowLogDetail 系列例外：保持 deepflow-server 直连，不重新加入 cache/chain
- 真实请求禁止依赖仅 DB+TABLE 的 cache 模糊匹配

**chain 源注册顺序**（main.go）：
```go
chain.AddListSource(ztDS)
chain.AddListSource(chDS)
chain.AddListSource(cacheDS)
```

#### M5. 回放验证命令

```bash
# 查看真实参数变体，不请求后端
python3 tools/replay_from_cache.py List --coverage-only

# 查看工具识别到的全部 endpoint，防止遗漏未分类 API
python3 tools/replay_from_cache.py --list-endpoints

# 普通链路冒烟测试（不能作为迁移验收）
python3 tools/replay_from_cache.py List --source auto

# 迁移验收：先用 VERIFY_SOURCE_CONTROL=true 启动后端
VERIFY_SOURCE_CONTROL=true ./backend/deeptrace-server
python3 tools/replay_from_cache.py List --source zerotrace --strict
python3 tools/replay_from_cache.py Top --source clickhouse --strict --rewrite-time

# 输出机器可读报告
python3 tools/replay_from_cache.py --all \
  --json-report temp/replay-all.json

# 黑盒页面链采集（记录 API 顺序、console error、page error）
python3 tools/capture_browser_scenario.py \
  --url http://localhost:8888 \
  --output temp/scenarios/span-list-default.json

# 按采集顺序回放完整页面链
python3 tools/replay_browser_scenario.py \
  temp/scenarios/span-list-default.json \
  --source zerotrace --strict --rewrite-time
```

> `VERIFY_SOURCE_CONTROL` 允许客户端选择内部数据源，只能用于本地验证，
> 不得在公开或生产环境启用。

#### M6. 依赖图谱与自动化验证工具

项目自带两个工具，放在 `tools/` 目录下：

| 工具 | 功能 | 运行时机 |
|------|------|---------|
| `tools/replay_from_cache.py` | 参数变体、强制源回放、严格结构校验、时间改写、JSON 报告 | 实现前盘点；实现后验收 |
| `tools/capture_browser_scenario.py` | Playwright 黑盒采集 API 顺序、响应来源和浏览器错误；自动脱敏 | 建立页面基线时 |
| `tools/replay_browser_scenario.py` | 顺序回放页面场景并复用严格契约校验 | 检查上游错误是否阻断下游 API 时 |
| `tools/dependency_graph.py` | 已知依赖图、迁移批次、已归类/未归类 API 统计 | 规划迁移批次时 |
| `tools/capture_real_api.py` | 使用环境变量 token 采集远端 API 样本 | 补充缺失契约时 |

> 采集工具只保存脱敏后的新结果。历史 `real_api_capture_*.json` 可能含旧 token
> 与用户信息；发现后必须立即轮换 token，并在确认无需保留后手工删除历史文件。

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

**工具自身测试：**
```bash
python3 -m unittest discover -s tools/tests -v
cd backend && go test ./...
```
