# DeepFlow 自定义数据接入指南

## 一、架构概览

```
浏览器 → offline_server.js (端口 8888)
              ├── 静态文件 (cloud.deepflow.yunshan.net/)  ← 前端 UI，不需要改
              └── API 路由 (/api/*)
                    ├── 缓存命中 → 返回 api_cache/ 中的 JSON
                    └── 缓存未命中 → 返回空数据 {"OPT_STATUS":"SUCCESS","DATA":[]}
```

**接入自己数据的方式**：将 `offline_server.js` 中的 API 路由指向你自己的后端，或直接替换 `api_cache/` 中的 JSON 文件。

---

## 二、API 分类（共 363 个缓存文件）

### 1. 认证 [AUTH] — 可直接复用缓存

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/fauths/login` | POST | 登录，返回 access_token |
| `/api/fauths/login_list` | GET | 登录方式列表 |

**接入建议**：保持缓存不变，前端会自动"登录成功"。

### 2. 用户与权限 [USER/ORG] — 可直接复用缓存

| 端点 | 方法 | 说明 | 关键字段 |
|------|------|------|----------|
| `/api/fuser/v1/users/current` | GET | 当前用户信息 | ID, USERNAME, EMAIL |
| `/api/fpermit/v1/orgs` | GET | 组织列表 | ID, NAME, LCUUID |
| `/api/fpermit/v1/org/4/role_teams` | GET | 角色团队 | — |
| `/api/fpermit/v1/org/4/page_scopes` | GET | 页面权限 | pages |
| `/api/fpermit/v1/org/4/teams` | GET | 团队列表 | ID, NAME |
| `/api/fuser/v1/user/conf/default/2` | GET | 用户配置 | params[] |

**接入建议**：保持缓存不变。如需改用户名/头像，编辑 `users_current` 缓存文件。

### 3. 仪表盘 [DASHBOARD] — ⭐ 核心接入点

| 端点 | 方法 | 说明 | 关键字段 |
|------|------|------|----------|
| `/api/df-web/v1/dashboards` | GET | 仪表盘列表 | ID, NAME, TYPE, LCUUID |
| `/api/df-web/v1/dashboards?module_type=system_dashboard` | GET | 系统仪表盘 | 同上 |
| `/api/df-web/v1/dashboards?module_type=user_dashboard` | GET | 用户仪表盘 | 同上 |
| `/api/df-web/v1/biz/{uuid}` | GET | 单个仪表盘详情 | PANELS[], VARIABLES[] |
| `/api/df-web/v1/biz/{uuid}/alarms` | GET | 仪表盘告警 | — |
| `/api/df-web/v1/icons` | GET | 图标库(395KB) | ID, NAME, CONTENT |
| `/api/df-web/v1/logo_info` | GET | Logo信息 | — |

**接入建议**：修改仪表盘列表和详情是展示自定义数据的最快路径。

### 4. 资源管理 [RESOURCE] — 接入你的基础设施数据

| 端点 | 方法 | 说明 | 关键字段 |
|------|------|------|----------|
| `/api/deepflow-server/v1/vtaps/` | GET | 采集器列表 | ID, NAME, STATE, TYPE |
| `/api/deepflow-server/v2/domains/` | GET | 域列表 | ID, NAME, TYPE |
| `/api/deepflow-server/v2/vpcs` | GET | VPC列表 | ID, NAME, DOMAIN |
| `/api/deepflow-server/v2/subnets` | GET | 子网列表 | ID, NAME, CIDR |
| `/api/deepflow-server/v2/vms` | GET | 虚拟机列表 | ID, NAME, STATE |
| `/api/deepflow-server/v2/pods` | GET | Pod列表 | ID, NAME, CLUSTER |
| `/api/deepflow-server/v2/pod-services` | GET | Pod Service | ID, NAME |
| `/api/deepflow-server/v2/pod-groups` | GET | Pod Group | ID, NAME |
| `/api/deepflow-server/v2/pod-clusters` | GET | Pod Cluster | ID, NAME |
| `/api/deepflow-server/v1/data-sources/` | GET | 数据源列表 | ID, NAME, INTERVAL |

**接入建议**：替换为你自己的服务器/容器/网络数据。

### 5. 数据查询引擎 [QUERIER] — ⭐⭐ 最核心

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/querier/v1/query/` | POST | SQL查询（拓扑、列表、Top等所有图表数据） |
| `/api/statistics/v1/stats/querier/*` | POST | 统计查询（Topo/ServiceList/Top等） |

**请求格式**：
```json
{
  "db": "flow_metrics",
  "sql": "SELECT auto_service, Byte FROM vtap_app_port WHERE time>=now()-3600 GROUP BY auto_service ORDER BY Byte DESC LIMIT 100"
}
```

**响应格式**：
```json
{
  "OPT_STATUS": "SUCCESS",
  "DATA": {
    "columns": ["auto_service", "Byte"],
    "values": [["service-a", 123456], ["service-b", 78901]]
  }
}
```

**接入建议**：这是所有图表/拓扑/列表的数据来源。实现一个 SQL 解析层，将查询转换为你自己的数据查询。

### 6. 查询元数据 [QUERIER_META] — 定义可查询的指标和标签

| 端点 | 方法 | 说明 | 关键字段 |
|------|------|------|----------|
| `ShowDatabases` | POST | 可用数据库 | name, datasources |
| `ShowTables` | POST | 可用表 | name, datasources |
| `ShowTags` | POST | 可用标签/维度 | name, display_name, type |
| `ShowMetrics` | POST | 可用指标 | name, display_name, is_agg, unit |
| `ShowTagValues` | POST | 标签值枚举 | value, display_name |
| `/api/df-web/v1/indicator_template` | GET | 指标模板 | — |

**接入建议**：定义你自己的数据库/表/指标/标签体系。

### 7. 告警 [ALARM]

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/alarm/v1/deepflow/alarm-policies/` | GET | 告警策略列表 |
| `/api/alarm/v1/deepflow/alarm-endpoints/` | GET | 告警通知端点 |

### 8. 系统配置 [CONFIG]

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/df-web/v1/config/system` | GET | 系统配置 |
| `/api/df-web/v1/config/outerlinks` | GET | 外部链接 |

---

## 三、接入方式

### 方式一：替换缓存文件（最简单）

每个缓存文件格式：
```json
{
  "url": "/api/deepflow-server/v1/vtaps/",
  "method": "GET",
  "status": 200,
  "responseHeaders": {"Content-Type": "application/json"},
  "responseBody": "<base64 编码的响应体>"
}
```

用脚本生成替换文件：
```bash
node tools/gen_cache.js --url "/api/deepflow-server/v1/vtaps/" \
  --data '[{"ID":1,"NAME":"my-agent","STATE":1}]'
```

### 方式二：自定义后端（推荐）

修改 `offline_server.js`，将 API 请求转发到你的后端：

```javascript
// 在 offline_server.js 中修改 API 路由
if (pathname.startsWith('/api/')) {
  // 优先转发到自定义后端
  const customBackend = 'http://localhost:9090';
  proxyToBackend(customBackend, req, res, () => {
    // 自定义后端无响应时回退到缓存
    serveCached(req, res);
  });
  return;
}
```

### 方式三：实现自定义 API 后端（完全控制）

见下方 `custom_backend.js`。

---

## 四、所有 API 统一响应格式

```json
{
  "OPT_STATUS": "SUCCESS",
  "DATA": <array | object>,
  "DESCRIPTION": ""
}
```

所有 API 必须返回这个包装格式，前端通过 `OPT_STATUS` 判断成功/失败，通过 `DATA` 获取数据。

---

## 五、快速开始：5 分钟接入自己的数据

1. **启动自定义后端**：`node custom_backend.js`（端口 9090）
2. **修改 offline_server.js**：添加后端代理（见方式二）
3. **编辑 custom_backend.js**：在对应路由里返回你的数据
4. **刷新浏览器**：数据立即更新

重点关注这 3 个接口就能让大部分页面有数据：
- **资源列表**：`/api/deepflow-server/v2/*` → 你的服务器/容器列表
- **查询引擎**：`/api/querier/v1/query/` → 你的指标数据
- **仪表盘**：`/api/df-web/v1/biz/{uuid}` → 你的仪表盘配置
