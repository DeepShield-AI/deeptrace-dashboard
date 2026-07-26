#!/usr/bin/env python3
"""
依赖图谱工具 — 分析 api_cache 重建前端 API 调用链，输出依赖关系图和迁移批次建议。

用法:
    # 输出完整依赖图谱
    python3 tools/dependency_graph.py

    # 输出 Mermaid 格式图谱
    python3 tools/dependency_graph.py --mermaid

    # 只输出指定 endpoint 的上下游
    python3 tools/dependency_graph.py --endpoint List

    # 输出迁移批次建议
    python3 tools/dependency_graph.py --batches

    # 输出验证状态报告
    python3 tools/dependency_graph.py --status

原理:
    前端是编译 JS 黑盒，无法直接读取源码。但 api_cache 的 363 条响应中记录了
    真实请求的参数，通过分析参数流动（输出→输入）和时间窗口重建调用链。
"""

import json
import glob
import os
import re
import sys
from collections import defaultdict, OrderedDict

API_CACHE_DIR = os.path.join(os.path.dirname(__file__), "..", "api_cache")


# ---------------------------------------------------------------------------
# 已知页面加载链（从 workflow.md 和实际 api_cache 验证）
# 前端行为不可读，这个序列通过观察 API 依赖关系反推得到
# ---------------------------------------------------------------------------

# 每个条目的格式: (类别, endpoint简称, 路由正则, 调用时机)
INIT_CHAIN = [
    ("认证", "login", r"login", "页面初始化"),
    ("认证", "login_list", r"login_list", "页面初始化"),
    ("认证", "current_user", r"users/current", "页面初始化"),
    ("认证", "orgs", r"orgs", "页面初始化"),
    ("认证", "select_org", r"org/\d+/select", "页面初始化"),
    ("认证", "page_scopes", r"page_scopes", "页面初始化"),
    ("认证", "role_teams", r"role_teams", "页面初始化"),

    ("配置", "system_config", r"config/system", "页面初始化"),
    ("配置", "logo", r"logo_info", "页面初始化"),
    ("配置", "icons", r"icons", "页面初始化"),

    ("资源", "vtaps", r"vtap", "页面初始化"),
    ("资源", "agents", r"agents", "页面初始化"),
    ("资源", "domains", r"domains", "页面初始化"),
    ("资源", "vpcs", r"vpcs", "页面初始化"),

    ("仪表盘", "dashboards", r"dashboards", "首页加载"),
    ("仪表盘", "biz_list", r"biz$", "首页加载"),
    ("仪表盘", "biz_detail", r"biz/\w+", "仪表盘加载"),

    ("Schema", "ShowDatabases", r"ShowDatabases", "查询面板加载"),
    ("Schema", "ShowTables", r"ShowTables", "查询面板加载"),
    ("Schema", "ShowTags", r"ShowTags", "点击表"),
    ("Schema", "ShowMetrics", r"ShowMetrics", "点击表"),
    ("Schema", "ShowTagValues", r"ShowTagValues", "筛选过滤"),

    ("数据", "List", r"querier/List", "面板数据"),
    ("数据", "Top", r"querier/Top", "面板排序"),
    ("数据", "Profile", r"querier/Profile", "性能剖析"),
    ("数据", "Histogram", r"querier/Histogram", "时间分布"),
    ("数据", "TraceMap", r"querier/TraceMap", "追踪详情"),
    ("数据", "Topo", r"querier/Topo", "拓扑图"),

    ("日志", "FlowLogDetailList", r"FlowLogDetailList", "流日志详情"),
    ("日志", "FlowLogDetailInfo", r"FlowLogDetailInfo", "流日志详情面板"),

    ("其他", "search_histories", r"search-histories", "搜索"),
    ("其他", "alarm", r"alarm", "告警"),
]


# ---------------------------------------------------------------------------
# 参数流动关系（前一个 API 的响应字段 -> 后一个 API 的请求参数）
# 通过比对 api_cache 中的请求/响应结构反推
# ---------------------------------------------------------------------------

PARAM_FLOWS = [
    # ShowDatabases → ShowTables: DATABASE 值来自 ShowDatabases 返回的数据库名
    ("ShowDatabases", "ShowTables",
     "ShowDatabases 返回 DATABASE_NAME", "DATABASE"),
    # ShowTables → ShowTags / ShowMetrics: TABLE 来自 ShowTables
    ("ShowTables", "ShowTags",
     "ShowTables 返回 TABLE_NAME", "TABLE"),
    ("ShowTables", "ShowMetrics",
     "ShowTables 返回 TABLE_NAME", "TABLE"),
    # List/Top 使用相同的 (DATABASE, TABLE) 结构
    ("ShowTags", "List",
     "共用 (DATABASE, TABLE)", "(DATABASE, TABLE)"),
    ("ShowTags", "Top",
     "共用 (DATABASE, TABLE)", "(DATABASE, TABLE)"),
]

# ---------------------------------------------------------------------------
# 迁移批次（来自 migration-plan.md 和实际依赖分析）
# ---------------------------------------------------------------------------

MIGRATION_BATCHES = OrderedDict([
    ("Batch 0 — 已完成", {
        "endpoints": ["FlowLogDetailList", "FlowLogDetailInfo",
                       "FlowLogDetailHistory", "FlowLogDetailSearch",
                       "FlowLogAsyncDetail", "FlowLogTimingDetailList",
                       "FlowLogTimingDetailHistory", "TracingDetailList",
                       "FlowMap"],
        "reason": "已迁移到 deepflow-server 直连，无需改动",
        "status": "✅",
    }),
    ("Batch 1 — Schema 层", {
        "endpoints": ["ShowTags", "ShowMetrics", "ShowTagValues"],
        "reason": "互相独立，覆盖所有表，显示 Schema 的 API",
        "status": "⏳",
    }),
    ("Batch 2 — 页面核心数据", {
        "endpoints": ["List", "Top", "Profile"],
        "reason": "所有面板依赖，须同时实现才能完整显示页面",
        "status": "⏳",
    }),
    ("Batch 3 — 高级功能", {
        "endpoints": ["TraceMap", "Histogram", "Topo"],
        "reason": "追踪、时间分布、拓扑等独立功能",
        "status": "⏳",
    }),
    ("Batch 4 — 仪表盘/资源", {
        "endpoints": ["dashboards", "biz_list", "biz_detail",
                       "vtaps", "agents", "domains", "vpcs"],
        "reason": "deepflow-server 透传或实现",
        "status": "⏳",
    }),
    ("Batch ∞ — 无需改", {
        "endpoints": ["login", "login_list", "current_user", "orgs",
                       "select_org", "page_scopes", "role_teams",
                       "system_config", "logo", "icons",
                       "search_histories"],
        "reason": "硬编码或无需迁移",
        "status": "❌",
    }),
])


def categorize_path(path):
    """Return the most specific known endpoint, or the terminal path segment."""
    matches = []
    for _, endpoint, pattern, _ in INIT_CHAIN:
        if re.search(pattern, path, re.IGNORECASE):
            matches.append((len(pattern), endpoint))
    if matches:
        return max(matches)[1]
    terminal = path.split("?", 1)[0].rstrip("/").rsplit("/", 1)[-1]
    return terminal or "(root)"


def load_all_entries():
    """Load all api_cache entries into a dict keyed by endpoint pattern."""
    entries_by_pattern = defaultdict(list)
    all_entries = []

    for f in sorted(glob.glob(os.path.join(API_CACHE_DIR, "*.json"))):
        name = os.path.basename(f)
        with open(f) as fh:
            raw = json.load(fh)

        path = raw.get("path", "")
        method = raw.get("method", "GET")
        req_body_raw = raw.get("requestBody", "")
        resp_body_raw = raw.get("responseBody", "")
        is_b64 = raw.get("responseIsBase64", False)

        # requestBody can be string (JSON) or already-parsed dict
        req = {}
        if isinstance(req_body_raw, str) and req_body_raw.strip():
            try:
                req = json.loads(req_body_raw)
            except json.JSONDecodeError:
                req = {}
        elif isinstance(req_body_raw, dict):
            req = req_body_raw

        # responseBody is always string
        resp_str = ""
        if isinstance(resp_body_raw, str):
            resp_str = resp_body_raw
        else:
            resp_str = json.dumps(resp_body_raw) if resp_body_raw else ""

        if is_b64 and resp_str:
            import base64
            resp_str = base64.b64decode(resp_str).decode("utf-8")

        resp = {}
        if resp_str:
            try:
                resp = json.loads(resp_str)
            except json.JSONDecodeError:
                resp = {}

        entry = {
            "file": name,
            "method": method,
            "path": path,
            "request": req,
            "response": resp,
        }
        all_entries.append(entry)

        # Categorize using the most specific pattern. The old first-match logic
        # classified /login_list as /login and hid many unlisted endpoints.
        endpoint = categorize_path(path)
        entries_by_pattern[endpoint].append(entry)
        known_endpoints = {item[1] for item in INIT_CHAIN}
        if endpoint not in known_endpoints:
            entries_by_pattern["_unmatched"].append(entry)

    return all_entries, entries_by_pattern


def print_dependency_graph(all_entries, entries_by_pattern):
    """Print the complete dependency graph with counts."""
    print("=" * 70)
    print("API 依赖图谱 — 页面加载调用链")
    print("=" * 70)

    # Phase 1: 认证（硬编码）
    print("\n┌─ Phase 0: 认证 & 配置 (硬编码，无需迁移)")
    print("│")
    for cat, ep, pat, ctx in INIT_CHAIN:
        if cat not in ("认证", "配置"):
            continue
        entries = entries_by_pattern.get(ep, [])
        count = len(entries)
        marker = "✅" if count > 0 else "⚠️"
        print(f"│  {marker}  {cat} → {ep:25s} ({count} cache)  ← {ctx}")

    # Phase 2: 资源
    print("\n├─ Phase 1: 基础设施资源 (待迁移)")
    print("│")
    for cat, ep, pat, ctx in INIT_CHAIN:
        if cat not in ("资源",):
            continue
        entries = entries_by_pattern.get(ep, [])
        count = len(entries)
        marker = "⏳"
        print(f"│  {marker}  {ep:25s} ({count} cache)  ← {ctx}")

    # Phase 3: Schema
    print("\n├─ Phase 2: Schema 元数据 ← Batch 1")
    print("│")
    for cat, ep, pat, ctx in INIT_CHAIN:
        if cat not in ("Schema",):
            continue
        entries = entries_by_pattern.get(ep, [])
        count = len(entries)
        # ShowDatabases/ShowTables might be done
        if ep in ("ShowDatabases", "ShowTables"):
            marker = "✅"
        else:
            marker = "⏳"
        print(f"│  {marker}  {ep:25s} ({count} cache)  ← {ctx}")
        show_param_flow(ep, entries)

    # Phase 4: 数据查询
    print("\n├─ Phase 3: 数据查询引擎 ← Batch 2 & 3")
    print("│")
    for cat, ep, pat, ctx in INIT_CHAIN:
        if cat not in ("数据",):
            continue
        entries = entries_by_pattern.get(ep, [])
        count = len(entries)
        if ep == "FlowLogDetailList":
            marker = "✅"
        else:
            marker = "⏳"
        print(f"│  {marker}  {ep:25s} ({count} cache)  ← {ctx}")
        show_param_flow(ep, entries)

    # Phase 5: 日志
    print("\n├─ Phase 4: 流日志详情 (已迁移)")
    print("│")
    for cat, ep, pat, ctx in INIT_CHAIN:
        if cat not in ("日志",):
            continue
        entries = entries_by_pattern.get(ep, [])
        count = len(entries)
        marker = "✅"
        print(f"│  {marker}  {ep:25s} ({count} cache)  ← {ctx}")

    # Phase 6: 仪表盘
    print("\n└─ Phase 5: 仪表盘 & 其他 (待迁移)")
    print("   ")
    for cat, ep, pat, ctx in INIT_CHAIN:
        if cat not in ("仪表盘", "其他"):
            continue
        entries = entries_by_pattern.get(ep, [])
        count = len(entries)
        marker = "⏳"
        print(f"   {marker}  {ep:25s} ({count} cache)  ← {ctx}")

    # Summary
    total_cached = len(all_entries) - len(entries_by_pattern.get("_unmatched", []))
    unmatched = len(entries_by_pattern.get("_unmatched", []))
    print(f"\n{'─' * 70}")
    print(f"总计: {total_cached} 已归类 + {unmatched} 未匹配 = {total_cached + unmatched} cache 条目")
    if unmatched > 0:
        print(f"未匹配条目:")
        for e in entries_by_pattern["_unmatched"][:10]:
            print(f"   {e['method']} {e['path']}")


def show_param_flow(ep, entries):
    """Show parameter details for an endpoint."""
    if not entries:
        return
    # Show distinct (DATABASE, TABLE) combos
    dbs_tbls = defaultdict(set)
    for e in entries:
        req = e["request"]
        db = req.get("DATABASE", "") or ""
        tbl = req.get("TABLE", "") or ""
        if db or tbl:
            dbs_tbls[db].add(tbl)
    if dbs_tbls:
        for db, tbls in sorted(dbs_tbls.items()):
            tbl_list = ", ".join(sorted(tbls)[:5])
            if len(tbls) > 5:
                tbl_list += f"... (+{len(tbls)-5})"
            print(f"│    DATABASE={db or '(空)':15s} TABLES={tbl_list}")


def print_mermaid(all_entries, entries_by_pattern):
    """Output dependency graph as Mermaid flowchart."""
    print("```mermaid")
    print("flowchart TD")
    print("  title[API 依赖图谱 — 页面加载流程]")
    print("  style title fill:#f0f0f0,stroke:#333")

    # Group by phase
    phase_nodes = {
        "Auth": ["login", "login_list", "current_user", "orgs", "select_org"],
        "Config": ["system_config", "logo", "icons"],
        "Resource": ["vtaps", "agents", "domains", "vpcs"],
        "Schema": ["ShowDatabases", "ShowTables", "ShowTags", "ShowMetrics", "ShowTagValues"],
        "Query": ["List", "Top", "Profile", "Histogram", "TraceMap", "Topo"],
        "Dashboard": ["dashboards", "biz_list", "biz_detail"],
        "FlowLog": ["FlowLogDetailList", "FlowLogDetailInfo"],
    }

    for phase, nodes in phase_nodes.items():
        for i, n in enumerate(nodes):
            entries = entries_by_pattern.get(n, [])
            count = len(entries)
            label = f"{n}\\n({count} cache)"
            print(f"    {n}(\"{label}\")")

        # Phase ordering
        if phase == "Auth":
            for n in nodes:
                print(f"    {n} --> Config")
        elif phase == "Config":
            print("    Config --> Resource")
            print("    Config --> Dashboard")
        elif phase == "Resource":
            print("    Resource --> Schema")
        elif phase == "Dashboard":
            print("    Dashboard --> Schema")
        elif phase == "Schema":
            # Schema splits into data queries
            for q in ["List", "Top", "Profile", "Histogram", "TraceMap", "Topo"]:
                if entries_by_pattern.get(q):
                    print(f"    Schema --> {q}")
            # And flow log
            if entries_by_pattern.get("FlowLogDetailList"):
                print("    Schema --> FlowLogDetailList")
        elif phase == "Query":
            pass  # Terminals

    print("```")


def print_batches(all_entries, entries_by_pattern):
    """Print migration batch recommendations."""
    print("=" * 70)
    print("迁移批次建议（按依赖链分批实现，避免链中断）")
    print("=" * 70)

    for batch_name, info in MIGRATION_BATCHES.items():
        endpoints = info["endpoints"]
        reason = info["reason"]
        status = info["status"]

        total_count = 0
        ep_details = []
        for ep in endpoints:
            entries = entries_by_pattern.get(ep, [])
            count = len(entries)
            total_count += count
            ep_details.append((ep, count))

        print(f"\n{status}  {batch_name}")
        print(f"   原因: {reason}")
        print(f"   包含: {', '.join(endpoints)}")
        print(f"   覆盖: {total_count} cache 条目")

        # Per-endpoint detail
        for ep, count in ep_details:
            if count == 0:
                print(f"     ⚠️  {ep:25s} — 0 cache 条目，可能路径不匹配")
            else:
                # Show DB/TABLE coverage
                entries = entries_by_pattern.get(ep, [])
                dbs = set()
                for e in entries:
                    db = e["request"].get("DATABASE", "") or ""
                    if db:
                        dbs.add(db)
                db_str = ", ".join(sorted(dbs)) if dbs else "(无参数)"
                print(f"     {ep:25s} ({count:2d} cache, DB: {db_str})")


def print_status_report(all_entries, entries_by_pattern):
    """Print verification status report with cache counts."""
    print("=" * 70)
    print("验证状态报告 — 每个 endpoint 的 cache 条目数和参数覆盖")
    print("=" * 70)
    print(f"{'Endpoint':25s} {'Cache':>6s} {'DB/TABLE 组合':>20s} {'迁移状态':>10s}")
    print("-" * 70)

    # Map endpoints to migration status
    status_map = {}
    for batch_name, info in MIGRATION_BATCHES.items():
        status = info["status"]
        for ep in info["endpoints"]:
            status_map[ep] = status

    for cat, ep, pat, ctx in INIT_CHAIN:
        entries = entries_by_pattern.get(ep, [])
        count = len(entries)
        status = status_map.get(ep, "❓")

        dbs_tbls = set()
        for e in entries:
            req = e["request"]
            db = req.get("DATABASE", "") or "-"
            tbl = req.get("TABLE", "") or "-"
            if db != "-" or tbl != "-":
                dbs_tbls.add((db, tbl))

        combo_str = f"{len(dbs_tbls)} 种" if dbs_tbls else "-"
        print(f"{ep:25s} {count:6d} {combo_str:>20s} {status:>10s}")

    unmatched_entries = entries_by_pattern.get("_unmatched", [])
    unmatched_by_endpoint = defaultdict(int)
    for entry in unmatched_entries:
        unmatched_by_endpoint[categorize_path(entry["path"])] += 1

    if unmatched_by_endpoint:
        print("-" * 70)
        print("Unclassified endpoints (must not be omitted from migration coverage):")
        for endpoint, count in sorted(
            unmatched_by_endpoint.items(),
            key=lambda item: (-item[1], item[0].lower()),
        ):
            print(f"{endpoint:25s} {count:6d} {'-':>20s} {'❓':>10s}")

    total = len(all_entries)
    classified = total - len(unmatched_entries)
    print("-" * 70)
    print(f"{'总计':25s} {total:6d}")
    print(f"{'已归类':25s} {classified:6d}")
    print(f"{'未归类':25s} {len(unmatched_entries):6d}")
    print()
    print("状态图例: ✅=已完成  ⏳=待迁移  ❌=无需改  ❓=未分类")


def print_param_flow(all_entries, entries_by_pattern):
    """Print parameter flow between endpoints."""
    print("=" * 70)
    print("参数流动关系 — 前一个 API 的输出是后一个 API 的输入")
    print("=" * 70)
    print()

    for upstream, downstream, up_field, down_field in PARAM_FLOWS:
        up_entries = entries_by_pattern.get(upstream, [])
        down_entries = entries_by_pattern.get(downstream, [])
        print(f"  {upstream:20s} ──[{up_field}]──▶ {downstream:20s}  ──[{down_field}]──▶")
        print(f"  {'':20s}    ({len(up_entries)} cache)         ({len(down_entries)} cache)")
        print()


def main():
    all_entries, entries_by_pattern = load_all_entries()

    args = sys.argv[1:] if len(sys.argv) > 1 else []

    if "--mermaid" in args:
        print_mermaid(all_entries, entries_by_pattern)
    elif "--batches" in args:
        print_batches(all_entries, entries_by_pattern)
    elif "--status" in args:
        print_status_report(all_entries, entries_by_pattern)
    elif "--param-flow" in args:
        print_param_flow(all_entries, entries_by_pattern)
    elif any(a.startswith("--endpoint=") for a in args):
        ep = [a.split("=", 1)[1] for a in args if a.startswith("--endpoint=")][0]
        print(f"Endpoint: {ep}")
        entries = entries_by_pattern.get(ep, [])
        print(f"  Cache 条目: {len(entries)}")
        if entries:
            dbs = defaultdict(lambda: defaultdict(int))
            for e in entries:
                req = e["request"]
                db = req.get("DATABASE", "") or "(none)"
                tbl = req.get("TABLE", "") or "(none)"
                dbs[db][tbl] += 1
            for db, tbls in sorted(dbs.items()):
                print(f"  DATABASE={db}")
                for tbl, cnt in sorted(tbls.items()):
                    print(f"    TABLE={tbl} ({cnt} requests)")

            # Show parameter details in data queries
            for e in entries[:3]:
                req = e["request"]
                q = req.get("QUERIES", [{}])[0]
                sel = q.get("SELECT", "")
                whr = q.get("WHERE", "")
                print(f"  示例 SELECT: {sel[:120]}...")
                if whr:
                    print(f"  示例 WHERE:  {whr[:120]}...")
                break
    else:
        # Default: print full report
        print_dependency_graph(all_entries, entries_by_pattern)
        print()
        print_batches(all_entries, entries_by_pattern)
        print()
        print_status_report(all_entries, entries_by_pattern)
        print()
        print_param_flow(all_entries, entries_by_pattern)


if __name__ == "__main__":
    main()
