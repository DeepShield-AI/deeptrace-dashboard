#!/usr/bin/env python3
"""Replay api_cache contracts against the local backend.

Examples:
    # Inspect observed parameter variants without calling the backend.
    python3 tools/replay_from_cache.py List --coverage-only

    # Compatibility smoke test through the normal source chain.
    python3 tools/replay_from_cache.py List

    # Migration acceptance test: Zerotrace must answer, with no fallback.
    VERIFY_SOURCE_CONTROL=true ./backend/deeptrace-server
    python3 tools/replay_from_cache.py List --source zerotrace --strict

    # Replay old requests against a current live-data window.
    python3 tools/replay_from_cache.py Top \
        --source clickhouse --strict --rewrite-time

    # Validate every discovered endpoint and write a machine-readable report.
    python3 tools/replay_from_cache.py --all --json-report temp/replay.json

Forced-source replay is fail-closed. A request only passes when the backend
returns an X-DeepTrace-Source header matching the requested source.
"""

from __future__ import annotations

import argparse
import base64
import copy
import glob
import hashlib
import json
import os
import re
import socket
import sys
import time
import urllib.error
import urllib.request
from collections import defaultdict
from pathlib import Path
from typing import Any, Iterable
from urllib.parse import urlsplit


DEFAULT_API_CACHE_DIR = Path(__file__).resolve().parent.parent / "api_cache"
SOURCE_HEADER = "X-DeepTrace-Source"
FORCE_SOURCE_HEADER = "X-DeepTrace-Force-Source"
NO_FALLBACK_HEADER = "X-DeepTrace-No-Fallback"

ENDPOINT_PATH_MAP = {
    "ShowDatabases": "/api/statistics/v1/stats/querier/DBDescription/ShowDatabases",
    "ShowTables": "/api/statistics/v1/stats/querier/DBDescription/ShowTables",
    "ShowTags": "/api/statistics/v1/stats/querier/DBDescription/ShowTags",
    "ShowMetrics": "/api/statistics/v1/stats/querier/DBDescription/ShowMetrics",
    "ShowMetricsFunctions": "/api/statistics/v1/stats/querier/DBDescription/ShowMetricsFunctions",
    "ShowTagValues": "/api/statistics/v1/stats/querier/DBDescription/ShowTagValues",
    "List": "/api/statistics/v1/stats/querier/List",
    "Top": "/api/statistics/v1/stats/querier/Top",
    "Profile": "/api/statistics/v1/stats/querier/Profile",
    "FlowLogDetailList": "/api/statistics/v1/stats/querier/FlowLogDetailList",
    "FlowLogDetailInfo": "/api/statistics/v1/stats/querier/FlowLogDetailInfo",
    "TraceMap": "/api/statistics/v1/stats/querier/TraceMap",
    "Topo": "/api/statistics/v1/stats/querier/Topo",
    "Histogram": "/api/statistics/v1/stats/querier/Histogram",
}

PRIORITY_ENDPOINTS = [
    "ShowDatabases",
    "ShowTables",
    "ShowTags",
    "ShowMetrics",
    "ShowTagValues",
    "List",
    "Top",
    "Profile",
    "TraceMap",
    "Histogram",
    "FlowLogDetailList",
    "FlowLogDetailInfo",
]

DSL_FEATURES = [
    "newTag",
    "Enum",
    "icon_id",
    "node_type",
    "PerSecond",
    "Avg",
    "Sum",
    "Count",
    "Percentile",
    "Max",
    "Min",
    "exist",
    "Interval",
]

SCHEMA_FIELDS = ("label_type", "pre_as", "type", "unit", "value_type")
DYNAMIC_TIME_KEYS = {"time_start", "time_end"}


def parse_request_body(raw: Any) -> dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str) and raw.strip():
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            return {}
        return parsed if isinstance(parsed, dict) else {}
    return {}


def parse_response_body(raw: Any, is_b64: bool = False) -> dict[str, Any] | None:
    if not isinstance(raw, str) or not raw:
        return None
    response_text = raw
    if is_b64:
        try:
            response_text = base64.b64decode(response_text).decode("utf-8")
        except (ValueError, UnicodeDecodeError):
            return None
    try:
        parsed = json.loads(response_text)
    except json.JSONDecodeError:
        return None
    return parsed if isinstance(parsed, dict) else None


def load_all_entries(
    cache_dir: str | os.PathLike[str] = DEFAULT_API_CACHE_DIR,
) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    pattern = str(Path(cache_dir) / "*.json")
    for filename in sorted(glob.glob(pattern)):
        try:
            with open(filename, encoding="utf-8") as handle:
                raw = json.load(handle)
        except (OSError, json.JSONDecodeError):
            continue

        response = parse_response_body(
            raw.get("responseBody"),
            bool(raw.get("responseIsBase64", False)),
        )
        if response is None:
            continue

        request_raw = raw.get("requestBody", "")
        if isinstance(request_raw, dict):
            request_body = json.dumps(
                request_raw,
                ensure_ascii=False,
                separators=(",", ":"),
            )
        elif isinstance(request_raw, str):
            request_body = request_raw
        else:
            request_body = ""

        entries.append(
            {
                "file": os.path.basename(filename),
                "method": str(raw.get("method", "GET")).upper(),
                "path": str(raw.get("path", "")),
                "request_body": request_body,
                "request": parse_request_body(request_raw),
                "response": response,
            }
        )
    return entries


def normalize_endpoint_path(path: str) -> str:
    normalized = urlsplit(path).path.rstrip("/")
    return normalized or "/"


def find_entries(
    endpoint: str,
    all_entries: Iterable[dict[str, Any]] | None = None,
) -> list[dict[str, Any]]:
    entries = list(all_entries) if all_entries is not None else load_all_entries()
    expected = ENDPOINT_PATH_MAP.get(endpoint)
    if expected:
        expected_path = normalize_endpoint_path(expected)
        return [
            entry
            for entry in entries
            if normalize_endpoint_path(str(entry.get("path", ""))) == expected_path
        ]

    endpoint_lower = endpoint.lower()
    if endpoint == "(root)":
        return [
            entry
            for entry in entries
            if normalize_endpoint_path(str(entry.get("path", ""))) == "/"
        ]
    exact_terminal = [
        entry
        for entry in entries
        if normalize_endpoint_path(str(entry.get("path", "")))
        .rsplit("/", 1)[-1]
        .lower()
        == endpoint_lower
    ]
    if exact_terminal:
        return exact_terminal

    pattern = re.compile(rf"(?:^|[/_]){re.escape(endpoint)}(?:$|[?_.])", re.IGNORECASE)
    return [
        entry
        for entry in entries
        if pattern.search(str(entry.get("path", "")))
        or pattern.search(str(entry.get("file", "")))
    ]


def get_endpoint_name(entry: dict[str, Any]) -> str:
    normalized = normalize_endpoint_path(str(entry.get("path", "")))
    for name, path in ENDPOINT_PATH_MAP.items():
        if normalized == normalize_endpoint_path(path):
            return name
    terminal = normalized.rsplit("/", 1)[-1]
    return terminal or "(root)"


def _normalize_dynamic_values(value: Any) -> Any:
    if isinstance(value, dict):
        normalized: dict[str, Any] = {}
        for key, child in sorted(value.items()):
            if str(key).lower() in DYNAMIC_TIME_KEYS:
                normalized[key] = "<timestamp>"
            else:
                normalized[key] = _normalize_dynamic_values(child)
        return normalized
    if isinstance(value, list):
        return [_normalize_dynamic_values(item) for item in value]
    return value


def request_signature(request: dict[str, Any]) -> str:
    canonical = json.dumps(
        _normalize_dynamic_values(request),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    )
    return hashlib.sha256(canonical.encode("utf-8")).hexdigest()[:16]


def _query_features(request: dict[str, Any]) -> set[str]:
    features: set[str] = set()
    for query in request.get("QUERIES", []) or []:
        select = str(query.get("SELECT", ""))
        for feature in DSL_FEATURES:
            if feature.lower() in select.lower():
                features.add(feature)
    for key in ("INCLUDE_HISTORY", "interval", "window_size", "fill", "TOTAL"):
        if key in request:
            features.add(key)
    return features


def print_coverage_matrix(entries: list[dict[str, Any]]) -> None:
    combos: dict[tuple[str, str], list[dict[str, Any]]] = defaultdict(list)
    signatures: set[str] = set()
    for entry in entries:
        request = entry.get("request", {})
        queries = request.get("QUERIES") or [{}]
        query = queries[0] if isinstance(queries[0], dict) else {}
        database = str(request.get("DATABASE", "") or "")
        table = str(request.get("TABLE", "") or "")
        signatures.add(request_signature(request))
        combos[(database, table)].append(
            {
                "select": str(query.get("SELECT", "")),
                "where": str(query.get("WHERE", "")),
                "page_size": request.get("PAGE_SIZE", request.get("LIMIT", "")),
                "query_count": len(queries),
                "features": _query_features(request),
                "signature": request_signature(request),
            }
        )

    print("=" * 78)
    print(
        f"Parameter coverage: {len(entries)} cache entries, "
        f"{len(signatures)} canonical variants"
    )
    print("=" * 78)
    for (database, table), samples in sorted(combos.items()):
        features: set[str] = set()
        for sample in samples:
            features.update(sample["features"])
        feature_text = ", ".join(sorted(features)) if features else "(none)"
        print(
            f"\n  {database or '(empty)':20s} / {table or '(empty)':25s}"
            f" [{len(samples)} requests] features: {feature_text}"
        )
        for sample in samples[:3]:
            print(
                f"    variant={sample['signature']} "
                f"queries={sample['query_count']} limit={sample['page_size']}"
            )
            if sample["select"]:
                print(f"    SELECT: {sample['select'][:140]}")
            if sample["where"]:
                print(f"    WHERE:  {sample['where'][:140]}")


def rewrite_request_times(
    request: dict[str, Any],
    now: int | None = None,
) -> dict[str, Any]:
    """Move every sibling time_start/time_end pair to a current time window."""
    rewritten = copy.deepcopy(request)
    target_end = int(time.time()) if now is None else int(now)

    def visit(value: Any) -> None:
        if isinstance(value, dict):
            start_keys = [
                key for key in value if str(key).lower() == "time_start"
            ]
            end_keys = [key for key in value if str(key).lower() == "time_end"]
            if start_keys and end_keys:
                start_key = start_keys[0]
                end_key = end_keys[0]
                start_value = value.get(start_key)
                end_value = value.get(end_key)
                if isinstance(start_value, (int, float)) and isinstance(
                    end_value,
                    (int, float),
                ):
                    duration = max(0, int(end_value) - int(start_value))
                    value[start_key] = target_end - duration
                    value[end_key] = target_end
            for child in value.values():
                visit(child)
        elif isinstance(value, list):
            for child in value:
                visit(child)

    visit(rewritten)
    return rewritten


def _is_numeric_type(type_name: str) -> bool:
    return type_name in {"int", "float"}


def _compatible_types(actual: str, expected: str) -> bool:
    if actual == expected:
        return True
    return _is_numeric_type(actual) and _is_numeric_type(expected)


def _non_null_types(rows: list[Any], key: str) -> set[str]:
    types: set[str] = set()
    for row in rows:
        if not isinstance(row, dict):
            continue
        value = row.get(key)
        if value is not None:
            types.add(type(value).__name__)
    return types


def _compare_dict_shape(
    actual: dict[str, Any],
    expected: dict[str, Any],
    prefix: str,
    strict: bool,
) -> list[str]:
    issues: list[str] = []
    missing = set(expected) - set(actual)
    for key in sorted(missing):
        issues.append(f"{prefix} missing key: '{key}'")
    if strict:
        for key in sorted(set(actual) - set(expected)):
            issues.append(f"{prefix} extra key: '{key}'")
    for key in sorted(set(actual) & set(expected)):
        actual_value = actual[key]
        expected_value = expected[key]
        if isinstance(expected_value, dict) and isinstance(actual_value, dict):
            issues.extend(
                _compare_dict_shape(
                    actual_value,
                    expected_value,
                    f"{prefix}.{key}",
                    strict,
                )
            )
        elif expected_value is not None and actual_value is not None:
            actual_type = type(actual_value).__name__
            expected_type = type(expected_value).__name__
            if not _compatible_types(actual_type, expected_type):
                issues.append(
                    f"{prefix}.{key} type: expected {expected_type}, "
                    f"got {actual_type}"
                )
    return issues


def validate_response_structure(
    actual: dict[str, Any],
    expected: dict[str, Any],
    strict: bool = False,
) -> list[str]:
    issues: list[str] = []

    if actual.get("OPT_STATUS") != expected.get("OPT_STATUS"):
        issues.append(
            f"OPT_STATUS: expected {expected.get('OPT_STATUS')}, "
            f"got {actual.get('OPT_STATUS')}"
        )
    if "DESCRIPTION" in expected and "DESCRIPTION" not in actual:
        issues.append("DESCRIPTION missing")
    if expected.get("TYPE") and actual.get("TYPE") != expected.get("TYPE"):
        issues.append(
            f"TYPE: expected '{expected.get('TYPE')}', got '{actual.get('TYPE')}'"
        )
    if "COUNT" in expected:
        if "COUNT" not in actual:
            issues.append("COUNT missing")
        elif not isinstance(actual["COUNT"], (int, float)):
            issues.append(
                f"COUNT type: expected numeric, got {type(actual['COUNT']).__name__}"
            )

    actual_data = actual.get("DATA")
    expected_data = expected.get("DATA")
    if expected_data is None:
        if actual_data is not None:
            issues.append("DATA: expected null")
        return issues
    if actual_data is None:
        issues.append("DATA missing")
        return issues
    if type(actual_data) is not type(expected_data):
        issues.append(
            f"DATA type: expected {type(expected_data).__name__}, "
            f"got {type(actual_data).__name__}"
        )
        return issues

    if isinstance(expected_data, dict):
        issues.extend(_compare_dict_shape(actual_data, expected_data, "DATA", strict))
    elif isinstance(expected_data, list):
        if expected_data and not actual_data:
            issues.append("DATA expected rows but live source returned none")
        elif strict and not expected_data and actual_data:
            issues.append("DATA expected no rows but live source returned rows")
        elif expected_data and actual_data:
            expected_rows = [row for row in expected_data if isinstance(row, dict)]
            actual_rows = [row for row in actual_data if isinstance(row, dict)]
            if len(expected_rows) != len(expected_data):
                expected_item_type = type(expected_data[0]).__name__
                for index, item in enumerate(actual_data):
                    if not _compatible_types(
                        type(item).__name__,
                        expected_item_type,
                    ):
                        issues.append(
                            f"DATA[{index}] type: expected {expected_item_type}, "
                            f"got {type(item).__name__}"
                        )
            elif len(actual_rows) != len(actual_data):
                issues.append("DATA contains non-object rows")
            else:
                expected_shapes = {frozenset(row) for row in expected_rows}
                missing_counts: dict[str, int] = defaultdict(int)
                extra_counts: dict[str, int] = defaultdict(int)
                for row in actual_rows:
                    actual_shape = frozenset(row)
                    if actual_shape in expected_shapes:
                        continue
                    closest_shape = min(
                        expected_shapes,
                        key=lambda shape: len(shape ^ actual_shape),
                    )
                    for key in sorted(closest_shape - actual_shape):
                        missing_counts[key] += 1
                    if strict:
                        for key in sorted(actual_shape - closest_shape):
                            extra_counts[key] += 1
                checked_rows = len(actual_rows)
                for key, count in sorted(missing_counts.items()):
                    issues.append(
                        f"DATA missing key '{key}' in {count}/{checked_rows} "
                        "checked rows"
                    )
                for key, count in sorted(extra_counts.items()):
                    issues.append(
                        f"DATA extra key '{key}' in {count}/{checked_rows} "
                        "checked rows"
                    )
                expected_keys = set.union(*(set(row) for row in expected_rows))
                actual_keys = set.union(*(set(row) for row in actual_rows))
                for key in sorted(expected_keys & actual_keys):
                    expected_types = _non_null_types(expected_rows, key)
                    actual_types = _non_null_types(actual_rows, key)
                    if not expected_types or not actual_types:
                        continue
                    compatible = any(
                        _compatible_types(actual_type, expected_type)
                        for actual_type in actual_types
                        for expected_type in expected_types
                    )
                    if not compatible:
                        issues.append(
                            f"DATA field '{key}' type: expected "
                            f"{sorted(expected_types)}, got {sorted(actual_types)}"
                        )

    expected_schemas = expected.get("SCHEMAS")
    actual_schemas = actual.get("SCHEMAS")
    if isinstance(expected_schemas, dict) and expected_schemas:
        if not isinstance(actual_schemas, dict):
            issues.append("SCHEMAS missing or not an object")
        else:
            for key in sorted(set(expected_schemas) - set(actual_schemas)):
                issues.append(f"SCHEMAS missing key: '{key}'")
            if strict:
                for key in sorted(set(actual_schemas) - set(expected_schemas)):
                    issues.append(f"SCHEMAS extra key: '{key}'")
            for key in sorted(set(expected_schemas) & set(actual_schemas)):
                expected_schema = expected_schemas[key]
                actual_schema = actual_schemas[key]
                if not isinstance(expected_schema, dict) or not isinstance(
                    actual_schema,
                    dict,
                ):
                    continue
                for field in SCHEMA_FIELDS:
                    if field not in expected_schema:
                        continue
                    if field not in actual_schema:
                        issues.append(f"SCHEMAS.{key}.{field} missing")
                    elif actual_schema[field] != expected_schema[field]:
                        issues.append(
                            f"SCHEMAS.{key}.{field}: expected "
                            f"{expected_schema[field]!r}, got "
                            f"{actual_schema[field]!r}"
                        )
    return issues


def validate_source_provenance(
    requested_source: str,
    actual_source: str | None,
) -> list[str]:
    requested = requested_source.strip().lower()
    if requested in {"", "auto"}:
        return []
    if not actual_source:
        return [
            f"{SOURCE_HEADER} missing for forced source '{requested}'; "
            "the endpoint may have bypassed verification controls"
        ]
    actual = actual_source.strip().lower()
    if actual != requested:
        return [
            f"{SOURCE_HEADER}: expected '{requested}', got '{actual}' "
            "(fallback or wrong source)"
        ]
    return []


def _row_count(data: Any) -> int | None:
    return len(data) if isinstance(data, list) else None


def replay(
    entries: list[dict[str, Any]],
    host: str,
    *,
    source: str = "auto",
    strict: bool = False,
    rewrite_time: bool = False,
    timeout: float = 30.0,
) -> dict[str, Any]:
    results: dict[str, Any] = {
        "pass": 0,
        "fail": 0,
        "issues": [],
        "details": [],
    }
    host = host.rstrip("/")

    for entry in entries:
        request_object = entry.get("request", {})
        if rewrite_time and request_object:
            request_object = rewrite_request_times(request_object)
            request_body = json.dumps(
                request_object,
                ensure_ascii=False,
                separators=(",", ":"),
            )
        else:
            request_body = str(entry.get("request_body", ""))

        data = request_body.encode("utf-8") if request_body else None
        headers = {"Content-Type": "application/json"} if data is not None else {}
        if source != "auto":
            headers[FORCE_SOURCE_HEADER] = source
            headers[NO_FALLBACK_HEADER] = "true"

        url = f"{host}{entry['path']}"
        actual_source: str | None = None
        try:
            request = urllib.request.Request(
                url,
                data=data,
                headers=headers,
                method=str(entry.get("method", "GET")).upper(),
            )
            with urllib.request.urlopen(request, timeout=timeout) as response:
                actual_source = response.headers.get(SOURCE_HEADER)
                actual = json.loads(response.read().decode("utf-8"))
            if not isinstance(actual, dict):
                raise json.JSONDecodeError("top-level response is not an object", "", 0)
            issues = validate_source_provenance(source, actual_source)
            issues.extend(
                validate_response_structure(
                    actual,
                    entry["response"],
                    strict=strict,
                )
            )
        except urllib.error.HTTPError as error:
            actual_source = error.headers.get(SOURCE_HEADER)
            description = error.read().decode("utf-8", errors="replace")[:300]
            issues = [f"HTTP {error.code}: {description}"]
        except (
            urllib.error.URLError,
            ConnectionRefusedError,
            TimeoutError,
            socket.timeout,
        ) as error:
            issues = [f"HTTP connection error: {error}"]
        except (json.JSONDecodeError, UnicodeDecodeError) as error:
            issues = [f"Response is not valid JSON: {error}"]

        status = "fail" if issues else "pass"
        results[status] += 1
        if issues:
            results["issues"].append(
                f"[FAIL] {entry.get('file', entry['path'])} ({entry['path']})"
            )
            results["issues"].extend(f"       {issue}" for issue in issues)
        results["details"].append(
            {
                "file": entry.get("file", ""),
                "method": entry.get("method", ""),
                "path": entry.get("path", ""),
                "status": status,
                "requested_source": source,
                "actual_source": actual_source,
                "signature": request_signature(entry.get("request", {})),
                "issues": issues,
                "expected_rows": _row_count(entry["response"].get("DATA")),
            }
        )
    return results


def print_results_summary(
    endpoint: str,
    results: dict[str, Any],
    total_entries: int,
) -> None:
    pass_rate = (
        results["pass"] / total_entries * 100 if total_entries > 0 else 0.0
    )
    print("\n" + "=" * 78)
    print(f"Replay result: {endpoint}")
    print("=" * 78)
    print(f"  Passed: {results['pass']} / {total_entries}")
    print(f"  Failed: {results['fail']} / {total_entries}")
    print(f"  Pass rate: {pass_rate:.0f}%")
    if results["issues"]:
        print("\n  Differences:")
        for issue in results["issues"]:
            print(f"    {issue}")


def _ordered_endpoint_names(entries: list[dict[str, Any]]) -> list[str]:
    discovered = {get_endpoint_name(entry) for entry in entries}
    priority = [name for name in PRIORITY_ENDPOINTS if name in discovered]
    return priority + sorted(discovered - set(priority), key=str.lower)


def _write_json_report(path: str, report: dict[str, Any]) -> None:
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(
        json.dumps(report, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def build_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("endpoint", nargs="?", help="Endpoint terminal name")
    parser.add_argument("--all", action="store_true", help="Replay all endpoints")
    parser.add_argument(
        "--host",
        default="http://localhost:8888",
        help="Backend base URL",
    )
    parser.add_argument(
        "--cache-dir",
        default=str(DEFAULT_API_CACHE_DIR),
        help="Directory containing captured cache JSON files",
    )
    parser.add_argument(
        "--coverage-only",
        action="store_true",
        help="Print observed variants without sending requests",
    )
    parser.add_argument(
        "--source",
        choices=("auto", "zerotrace", "clickhouse", "cache"),
        default="auto",
        help="Force one backend source; non-auto replay is fail-closed",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Compare every observed row shape and SCHEMAS metadata",
    )
    parser.add_argument(
        "--rewrite-time",
        action="store_true",
        help="Move captured time windows to the current time",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=30.0,
        help="Per-request timeout in seconds",
    )
    parser.add_argument(
        "--json-report",
        help="Write a machine-readable replay report",
    )
    parser.add_argument(
        "--list-endpoints",
        action="store_true",
        help="List all endpoints discovered from cache paths",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_argument_parser().parse_args(argv)
    entries = load_all_entries(args.cache_dir)
    endpoint_names = _ordered_endpoint_names(entries)

    if args.list_endpoints:
        for name in endpoint_names:
            print(f"{name:40s} {len(find_entries(name, entries)):4d}")
        print(f"\nDiscovered endpoints: {len(endpoint_names)}")
        print(f"Usable cache entries: {len(entries)}")
        return 0

    if not args.all and not args.endpoint:
        build_argument_parser().error("provide an endpoint or --all")

    selected_names = endpoint_names if args.all else [args.endpoint]
    report: dict[str, Any] = {
        "host": args.host,
        "source": args.source,
        "strict": args.strict,
        "rewrite_time": args.rewrite_time,
        "cache_entries": len(entries),
        "endpoints": {},
    }
    total_failures = 0

    for endpoint in selected_names:
        endpoint_entries = find_entries(str(endpoint), entries)
        if not endpoint_entries:
            print(f"No cache entries found for '{endpoint}'", file=sys.stderr)
            total_failures += 1
            continue

        print(f"\nEndpoint: {endpoint} ({len(endpoint_entries)} entries)")
        print_coverage_matrix(endpoint_entries)
        if args.coverage_only:
            report["endpoints"][endpoint] = {
                "entries": len(endpoint_entries),
                "variants": len(
                    {
                        request_signature(entry.get("request", {}))
                        for entry in endpoint_entries
                    }
                ),
            }
            continue

        results = replay(
            endpoint_entries,
            args.host,
            source=args.source,
            strict=args.strict,
            rewrite_time=args.rewrite_time,
            timeout=args.timeout,
        )
        print_results_summary(str(endpoint), results, len(endpoint_entries))
        report["endpoints"][endpoint] = results
        total_failures += results["fail"]

    if args.json_report:
        _write_json_report(args.json_report, report)
        print(f"\nJSON report: {args.json_report}")
    return 1 if total_failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
