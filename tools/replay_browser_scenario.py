#!/usr/bin/env python3
"""Replay a browser scenario sequentially against the local backend."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

TOOLS_DIR = Path(__file__).resolve().parent
if str(TOOLS_DIR) not in sys.path:
    sys.path.insert(0, str(TOOLS_DIR))

import replay_from_cache


def scenario_entries(capture: dict[str, Any]) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    for item in capture.get("requests", []):
        expected = item.get("response")
        if not isinstance(expected, dict):
            continue
        request = item.get("request")
        request_object = request if isinstance(request, dict) else {}
        request_body = (
            json.dumps(request_object, ensure_ascii=False, separators=(",", ":"))
            if request is not None
            else ""
        )
        entries.append(
            {
                "file": f"scenario-seq-{item.get('sequence', len(entries) + 1)}",
                "method": str(item.get("method", "GET")).upper(),
                "path": str(item.get("path", "")),
                "request": request_object,
                "request_body": request_body,
                "response": expected,
            }
        )
    return entries


def scenario_capture_issues(capture: dict[str, Any]) -> list[str]:
    issues: list[str] = []
    for item in capture.get("requests", []):
        sequence = item.get("sequence", "?")
        path = item.get("path", "")
        if item.get("failure"):
            issues.append(
                f"sequence {sequence} {path}: captured request failure "
                f"{item['failure']}"
            )
        status = item.get("status")
        if not isinstance(status, int) or not 200 <= status < 300:
            issues.append(
                f"sequence {sequence} {path}: captured HTTP status {status!r}"
            )
        if not isinstance(item.get("response"), dict):
            issues.append(
                f"sequence {sequence} {path}: response is missing or not JSON; "
                "request cannot be silently omitted"
            )
    for error in capture.get("page_errors", []):
        issues.append(f"captured page error: {error}")
    for event in capture.get("console", []):
        if event.get("type") == "error":
            issues.append(f"captured console error: {event.get('text', '')}")
    return issues


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scenario")
    parser.add_argument("--host", default="http://localhost:8888")
    parser.add_argument(
        "--source",
        choices=("auto", "zerotrace", "clickhouse", "cache"),
        default="auto",
    )
    parser.add_argument("--strict", action="store_true")
    parser.add_argument("--rewrite-time", action="store_true")
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--json-report")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    capture = json.loads(Path(args.scenario).read_text(encoding="utf-8"))
    entries = scenario_entries(capture)
    capture_issues = scenario_capture_issues(capture)
    if not entries:
        print("Scenario has no replayable JSON API responses.")
        for issue in capture_issues:
            print(f"  {issue}")
        return 1 if capture_issues else 2

    results = replay_from_cache.replay(
        entries,
        args.host,
        source=args.source,
        strict=args.strict,
        rewrite_time=args.rewrite_time,
        timeout=args.timeout,
    )
    replay_from_cache.print_results_summary(
        Path(args.scenario).name,
        results,
        len(entries),
    )
    report = {
        "scenario": args.scenario,
        "scenario_id": capture.get("scenario_id"),
        "source": args.source,
        "results": results,
        "captured_console": capture.get("console", []),
        "captured_page_errors": capture.get("page_errors", []),
        "capture_issues": capture_issues,
    }
    if args.json_report:
        target = Path(args.json_report)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(
            json.dumps(report, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
    if capture_issues:
        print("\nCaptured scenario problems:")
        for issue in capture_issues:
            print(f"  {issue}")
    return 1 if results["fail"] or capture_issues else 0


if __name__ == "__main__":
    raise SystemExit(main())
