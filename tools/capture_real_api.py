#!/usr/bin/env python3
"""
请求链捕捉器 — 模拟 SpanList 页面真实请求链

用法:
  export DEEPFLOW_TOKEN="你的token"
  python3 tools/capture_real_api.py

原理: 按前端 SpanList 页面实际触发的请求顺序，
      依次发到 cloud.deepflow.yunshan.net，记录每个请求和响应。
"""

import os
import sys
import json
import re
import time
import requests
from pathlib import Path
from urllib.parse import urljoin, quote
from datetime import datetime

BASE = "https://cloud.deepflow.yunshan.net"
SENSITIVE_KEYS = {
    "access_token",
    "authorization",
    "cookie",
    "email",
    "mobile",
    "nickname",
    "password",
    "phone",
    "phone_num",
    "phone_number",
    "real_name",
    "refresh_token",
    "secret",
    "token",
    "token_key",
    "username",
}

# ─── 配置 ───────────────────────────────────────────────────────
TOKEN = os.environ.get("DEEPFLOW_TOKEN", "")
if not TOKEN:
    print("❌ 请设置 DEEPFLOW_TOKEN 环境变量或用参数传入 token")
    print("   用法: DEEPFLOW_TOKEN='xxx' python3 tools/capture_real_api.py")
    sys.exit(1)

HEADERS = {
    "Authorization": f"Bearer {TOKEN}",
    "X-Org-Id": "4",
    "Content-Type": "application/json",
    "Accept": "application/json, text/plain, */*",
    "User-Agent": "Mozilla/5.0",
}

session = requests.Session()
session.headers.update(HEADERS)

# ─── 工具 ───────────────────────────────────────────────────────
def redact_sensitive(value):
    """Recursively redact credentials and user identifiers from captures."""
    if isinstance(value, dict):
        return {
            key: (
                "<redacted>"
                if str(key).lower() in SENSITIVE_KEYS
                else redact_sensitive(child)
            )
            for key, child in value.items()
        }
    if isinstance(value, list):
        return [redact_sensitive(child) for child in value]
    return value


def redact_text(value):
    value = re.sub(
        r"(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*",
        "Bearer <redacted>",
        value,
    )
    value = re.sub(
        r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b",
        "<redacted-jwt>",
        value,
    )
    value = re.sub(
        r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b",
        "<redacted-email>",
        value,
    )
    return value


def api(method, path, **kwargs):
    """发请求并记录"""
    url = urljoin(BASE, path)
    label = kwargs.pop("label", path)
    data = kwargs.get("json")

    ts = datetime.now().strftime("%H:%M:%S.%f")[:-3]

    try:
        if method == "GET":
            resp = session.get(url, **kwargs, timeout=30)
        else:
            resp = session.post(url, **kwargs, timeout=30)
    except Exception as e:
        print(f"\n  ⛔ {ts} ❌ {label} — {e}")
        return None

    try:
        body = resp.json()
    except ValueError:
        body = redact_text(resp.text[:500])
    body = redact_sensitive(body)
    data = redact_sensitive(data)

    status = resp.status_code
    size = len(resp.content)

    opt = body.get("OPT_STATUS", "?") if isinstance(body, dict) else "?"
    icon = "✅" if status == 200 and opt == "SUCCESS" else "⚠️"

    print(f"\n  {icon} [{status}] {ts} {redact_text(label)}")
    print(f"     Size: {size:,} bytes  |  STATUS: {opt}")

    # 打印请求体摘要
    if data:
        data_str = json.dumps(data, ensure_ascii=False)
        data_short = data_str[:300] + "…" if len(data_str) > 300 else data_str
        print(f"     Req:  {data_short}")

    # 打印响应摘要
    body_str = json.dumps(body, ensure_ascii=False, indent=None)
    body_short = body_str[:500] + "…" if len(body_str) > 500 else body_str
    print(f"     Resp: {body_short}")

    return body

def print_separator(title):
    print(f"\n{'='*70}")
    print(f"  {title}")
    print(f"{'='*70}")

# ─── 主执行 ─────────────────────────────────────────────────────
def main():
    print(f"🔍 开始模拟 SpanList 页面的请求链")
    print(f"   后端: {BASE}")
    print(f"   时间: {datetime.now().isoformat()}")
    print("   Token: loaded from DEEPFLOW_TOKEN")

    output = {}

    # ==================== 第一波：页面初始化 ====================
    print_separator("Phase 1 — 页面基础元数据")

    # 1. 获取用户信息
    print("\n[1/14] 获取当前用户")
    r = api("GET", "/api/fuser/v1/users/current", label="users/current")
    output["users_current"] = r

    # 2. 获取可登录类型
    print("\n[2/14] 获取登录列表")
    r = api("GET", "/api/fauths/login_list", label="login_list")
    output["login_list"] = r

    # 3. 获取 Org 列表
    print("\n[3/14] 获取组织列表")
    r = api("GET", "/api/fpermit/v1/orgs", label="orgs")
    output["orgs"] = r

    # 4. 系统配置
    print("\n[4/14] 获取系统配置")
    r = api("GET", "/api/df-web/v1/config/", label="config")
    output["config"] = r

    # 5. License / Warrant
    print("\n[5/14] 获取 License 鉴权")
    r = api("GET", "/api/warrant/", label="warrant")
    output["warrant"] = r

    # 6. 搜索历史（app_link_trace）
    print("\n[6/14] 获取搜索历史")
    r = api("GET", "/api/df-web/v1/search-histories?uri=app_link_trace", label="search-history")
    output["search_history"] = r

    # 7. Fast filter black lists
    print("\n[7/14] 获取快速过滤黑名单")
    r = api("GET", "/api/df-web/v1/fast_filter_black_lists?db=flow_log&table=l7_flow_log&page_key=flow_log.l7_flow_log.app_link_trace", label="fast-filter-blacklist")
    output["fast_filter_blacklist"] = r

    # ==================== 第二波：Schema 元数据 ====================
    print_separator("Phase 2 — Schema 元数据（ShowTags / ShowMetrics）")

    # 8. ShowTags
    print("\n[8/14] 获取 Tags 元数据")
    r = api("POST", "/api/statistics/v1/stats/querier/DBDescription/ShowTags",
            json={"TABLE": "l7_flow_log", "DATABASE": "flow_log"},
            label="ShowTags")
    output["show_tags"] = r

    # 9. ShowMetrics
    print("\n[9/14] 获取 Metrics 元数据")
    r = api("POST", "/api/statistics/v1/stats/querier/DBDescription/ShowMetrics",
            json={"TABLE": "l7_flow_log", "DATABASE": "flow_log"},
            label="ShowMetrics")
    output["show_metrics"] = r

    # ==================== 第三波：数据查询 ====================
    print_separator("Phase 3 — 核心数据查询（折线图 + 表格）")

    timestamp_15min = {
        "shortcut": "15min", "interval": 1, "isAutoInterval": False,
        "startTime": int(time.time()) - 900,
        "endTime": int(time.time()),
    }

    # 10. Top 查询 — 折线图1: 请求总量趋势
    print("\n[10/14] 折线图1 — 请求总量趋势 (Sum request)")
    r = api("POST", "/api/statistics/v1/stats/querier/Top",
            json={
                "QUERIES": [{
                    "QUERY_ID": "R1",
                    "ROLES": ["R"],
                    "SELECT": "newTag('R1') as query_id, time, Count(row) as count_row, Sum(request) as 请求总量",
                    "TAGS": ["time", "newTag('R1') as query_id"],
                    "METRICS": ["Count(row) as count_row", "Sum(request) as 请求总量"],
                    "WHERE": "response_status=2",
                }],
                "DATABASE": "flow_log",
                "TABLE": "l7_flow_log",
                "TIME_START": int(timestamp_15min["startTime"]),
                "TIME_END": int(timestamp_15min["endTime"]),
                "ORDER_BY": "time",
                "ORDER": "ASC",
            },
            label="Top - 请求总量趋势")
    output["top_sum_request"] = r

    # 11. Top 查询 — 折线图2: 响应延迟趋势
    print("\n[11/14] 折线图2 — 响应延迟趋势 (Avg response_duration)")
    r = api("POST", "/api/statistics/v1/stats/querier/Top",
            json={
                "QUERIES": [{
                    "QUERY_ID": "R1",
                    "ROLES": ["R"],
                    "SELECT": "newTag('R1') as query_id, time, Avg(response_duration) as 平均延迟, Percentile(response_duration, 0.75) as P75, Percentile(response_duration, 0.9) as P90, Percentile(response_duration, 0.99) as P99",
                    "TAGS": ["time", "newTag('R1') as query_id"],
                    "METRICS": ["Avg(response_duration) as 平均延迟"],
                    "WHERE": "response_status=2",
                }],
                "DATABASE": "flow_log",
                "TABLE": "l7_flow_log",
                "TIME_START": int(timestamp_15min["startTime"]),
                "TIME_END": int(timestamp_15min["endTime"]),
                "ORDER_BY": "time",
                "ORDER": "ASC",
            },
            label="Top - 延迟趋势")
    output["top_avg_latency"] = r

    # 12. Top 查询 — 折线图3: 错误数趋势
    print("\n[12/14] 折线图3 — 错误数趋势 (Sum request where response_status!=0)")
    r = api("POST", "/api/statistics/v1/stats/querier/Top",
            json={
                "QUERIES": [{
                    "QUERY_ID": "R1",
                    "ROLES": ["R"],
                    "SELECT": "newTag('R1') as query_id, time, Sum(request) as 错误数, Count(row) as count_row",
                    "TAGS": ["time", "newTag('R1') as query_id"],
                    "METRICS": ["Sum(request) as 错误数", "Count(row) as count_row"],
                    "WHERE": "response_status!=0 AND response_status=2",
                }],
                "DATABASE": "flow_log",
                "TABLE": "l7_flow_log",
                "TIME_START": int(timestamp_15min["startTime"]),
                "TIME_END": int(timestamp_15min["endTime"]),
                "ORDER_BY": "time",
                "ORDER": "ASC",
            },
            label="Top - 错误趋势")
    output["top_errors"] = r

    # 13. List 查询 — 服务间调用统计表格
    print("\n[13/14] 表格 — 服务间调用统计 (List)")
    r = api("POST", "/api/statistics/v1/stats/querier/List",
            json={
                "PAGE_INDEX": 1,
                "PAGE_SIZE": 999,
                "SORT": {"ORDER_BY": "count_row", "SORTED_BY": "DESC"},
                "QUERIES": [{
                    "QUERY_ID": "R1-R1",
                    "ROLES": ["R", "R"],
                    "SELECT": "newTag('R1-R1') as query_id, Count(row) as count_row, Sum(request) as 请求总量, Enum(response_status) as 响应状态, response_status",
                    "TAGS": ["newTag('R1-R1') as query_id"],
                    "CTAGS": [],
                    "STAGS": [],
                    "METRICS": ["Count(row) as count_row", "Sum(request) as 请求总量"],
                    "WHERE": "response_status=2",
                }],
                "DATABASE": "flow_log",
                "TABLE": "l7_flow_log",
                "TIME_START": int(timestamp_15min["startTime"]),
                "TIME_END": int(timestamp_15min["endTime"]),
            },
            label="List - 服务统计")
    output["list"] = r

    # ==================== 第四波：Tracing 相关 ====================
    print_separator("Phase 4 — 调用链追踪（Tracing）")

    # 14. TracingAlgoParams
    print("\n[14/14] 获取调用链算法参数")
    r = api("GET", "/api/statistics/v1/stats/querier/TracingAlgoParams",
            label="TracingAlgoParams")
    output["tracing_params"] = r

    # ==================== 保存结果 ====================
    ts = datetime.now().strftime("%Y%m%d_%H%M%S")
    out_path = Path(__file__).resolve().parent / f"real_api_capture_{ts}.json"
    with open(out_path, "w") as f:
        json.dump(output, f, ensure_ascii=False, indent=2, default=str)

    print(f"\n{'='*70}")
    print(f"  ✅ 请求链模拟完成！结果已保存到: {out_path}")
    print(f"{'='*70}")

if __name__ == "__main__":
    main()
