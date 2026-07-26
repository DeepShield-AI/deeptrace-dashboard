#!/usr/bin/env python3
"""Capture a black-box browser API scenario with Playwright.

Example:
    python3 tools/capture_browser_scenario.py \
      --url http://localhost:8888 \
      --output temp/scenarios/span-list-default.json \
      --wait-ms 8000

The capture contains API request order, JSON request/response bodies, response
source headers, console errors, and uncaught page errors. Sensitive keys and
authentication headers are never written.
"""

from __future__ import annotations

import argparse
import json
import re
import time
import uuid
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit


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
SAFE_HEADERS = {
    "content-type",
    "x-deeptrace-requested-source",
    "x-deeptrace-source",
    "x-org-id",
}


def redact_sensitive(value: Any) -> Any:
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


def redact_text(value: str) -> str:
    redacted = re.sub(
        r"(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*",
        "Bearer <redacted>",
        value,
    )
    redacted = re.sub(
        r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b",
        "<redacted-jwt>",
        redacted,
    )
    redacted = re.sub(
        r"(?i)\b(access_token|refresh_token|password|secret|token)=([^&\s]+)",
        r"\1=<redacted>",
        redacted,
    )
    redacted = re.sub(
        r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b",
        "<redacted-email>",
        redacted,
    )
    return redacted


def redact_url(value: str) -> str:
    parsed = urlsplit(value)
    query = []
    for key, child in parse_qsl(parsed.query, keep_blank_values=True):
        query.append(
            (
                key,
                "<redacted>"
                if key.lower() in SENSITIVE_KEYS
                else redact_text(child),
            )
        )
    return urlunsplit(
        (
            parsed.scheme,
            parsed.netloc,
            parsed.path,
            urlencode(query),
            parsed.fragment,
        )
    )


def parse_json_body(text: str | None) -> Any:
    if not text:
        return None
    try:
        return redact_sensitive(json.loads(text))
    except json.JSONDecodeError:
        return None


def safe_headers(headers: dict[str, str]) -> dict[str, str]:
    return {
        key.lower(): value
        for key, value in headers.items()
        if key.lower() in SAFE_HEADERS
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://localhost:8888")
    parser.add_argument(
        "--output",
        default="temp/scenarios/browser-default.json",
    )
    parser.add_argument("--api-prefix", default="/api/")
    parser.add_argument("--wait-ms", type=int, default=5000)
    parser.add_argument(
        "--click",
        action="append",
        default=[],
        help="Playwright selector to click after initial load; repeatable",
    )
    parser.add_argument("--headed", action="store_true")
    parser.add_argument("--storage-state", help="Playwright storage-state JSON")
    parser.add_argument("--screenshot", help="Optional final screenshot path")
    parser.add_argument("--scenario-id", default=str(uuid.uuid4()))
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        from playwright.sync_api import Error as PlaywrightError
        from playwright.sync_api import sync_playwright
    except ImportError:
        print(
            "Playwright is not installed. Install the Python playwright package "
            "and Chromium before running this recorder."
        )
        return 2

    started_at = time.time()
    requests: list[dict[str, Any]] = []
    request_indexes: dict[int, int] = {}
    console_events: list[dict[str, Any]] = []
    page_errors: list[str] = []

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=not args.headed)
        context_options: dict[str, Any] = {
            "extra_http_headers": {
                "X-DeepTrace-Scenario-ID": args.scenario_id,
            }
        }
        if args.storage_state:
            context_options["storage_state"] = args.storage_state
        context = browser.new_context(**context_options)
        page = context.new_page()

        def on_request(request: Any) -> None:
            sanitized_url = redact_url(request.url)
            parsed_url = urlsplit(sanitized_url)
            path = parsed_url.path
            if not path.startswith(args.api_prefix):
                return
            entry = {
                "sequence": len(requests) + 1,
                "timestamp_ms": round((time.time() - started_at) * 1000),
                "method": request.method,
                "url": sanitized_url,
                "path": path
                + (f"?{parsed_url.query}" if parsed_url.query else ""),
                "request": parse_json_body(request.post_data),
                "request_headers": safe_headers(request.headers),
                "status": None,
                "response": None,
                "response_headers": {},
                "failure": None,
            }
            request_indexes[id(request)] = len(requests)
            requests.append(entry)

        def on_response(response: Any) -> None:
            index = request_indexes.get(id(response.request))
            if index is None:
                return
            entry = requests[index]
            entry["status"] = response.status
            entry["response_headers"] = safe_headers(response.headers)
            content_type = response.headers.get("content-type", "")
            if "json" in content_type.lower():
                try:
                    entry["response"] = redact_sensitive(response.json())
                except PlaywrightError:
                    entry["response"] = None

        def on_request_failed(request: Any) -> None:
            index = request_indexes.get(id(request))
            if index is not None:
                requests[index]["failure"] = request.failure

        page.on("request", on_request)
        page.on("response", on_response)
        page.on("requestfailed", on_request_failed)
        page.on(
            "console",
            lambda message: console_events.append(
                {
                    "timestamp_ms": round((time.time() - started_at) * 1000),
                    "type": message.type,
                    "text": redact_text(message.text),
                }
            )
            if message.type in {"error", "warning"}
            else None,
        )
        page.on("pageerror", lambda error: page_errors.append(redact_text(str(error))))

        page.goto(args.url, wait_until="domcontentloaded")
        try:
            page.wait_for_load_state("networkidle", timeout=max(args.wait_ms, 1000))
        except PlaywrightError:
            page.wait_for_timeout(args.wait_ms)

        for selector in args.click:
            page.locator(selector).click()
            try:
                page.wait_for_load_state("networkidle", timeout=max(args.wait_ms, 1000))
            except PlaywrightError:
                page.wait_for_timeout(args.wait_ms)

        if args.screenshot:
            screenshot = Path(args.screenshot)
            screenshot.parent.mkdir(parents=True, exist_ok=True)
            page.screenshot(path=str(screenshot), full_page=True)

        capture = {
            "format": "deeptrace-browser-scenario-v1",
            "scenario_id": args.scenario_id,
            "url": args.url,
            "captured_at_unix": int(started_at),
            "duration_ms": round((time.time() - started_at) * 1000),
            "requests": requests,
            "console": console_events,
            "page_errors": page_errors,
        }
        output = Path(args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(
            json.dumps(capture, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        context.close()
        browser.close()

    print(f"Captured {len(requests)} API requests to {args.output}")
    print(f"Console warnings/errors: {len(console_events)}")
    print(f"Page errors: {len(page_errors)}")
    return 1 if page_errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
