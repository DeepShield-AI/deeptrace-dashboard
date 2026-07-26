import importlib.util
import unittest
from pathlib import Path


def load_tool(name: str):
    path = Path(__file__).resolve().parents[1] / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


capture_browser_scenario = load_tool("capture_browser_scenario")
replay_browser_scenario = load_tool("replay_browser_scenario")


class BrowserScenarioTests(unittest.TestCase):
    def test_sensitive_fields_are_redacted_recursively(self) -> None:
        value = {
            "username": "user",
            "password": "secret",
            "nested": [{"access_token": "token"}],
        }

        redacted = capture_browser_scenario.redact_sensitive(value)

        self.assertEqual(redacted["username"], "<redacted>")
        self.assertEqual(redacted["password"], "<redacted>")
        self.assertEqual(redacted["nested"][0]["access_token"], "<redacted>")

    def test_url_and_diagnostic_text_are_redacted(self) -> None:
        url = (
            "http://localhost/api/test?access_token=secret"
            "&email=user%40example.com"
        )

        redacted_url = capture_browser_scenario.redact_url(url)
        redacted_text = capture_browser_scenario.redact_text(
            "failed for user@example.com token=secret"
        )

        self.assertNotIn("secret", redacted_url)
        self.assertNotIn("user%40example.com", redacted_url)
        self.assertNotIn("user@example.com", redacted_text)
        self.assertNotIn("token=secret", redacted_text)

    def test_scenario_conversion_keeps_request_order(self) -> None:
        capture = {
            "requests": [
                {
                    "sequence": 1,
                    "method": "GET",
                    "path": "/api/one",
                    "request": None,
                    "response": {"OPT_STATUS": "SUCCESS", "DATA": []},
                },
                {
                    "sequence": 2,
                    "method": "POST",
                    "path": "/api/two",
                    "request": {"TABLE": "l7_flow_log"},
                    "response": {"OPT_STATUS": "SUCCESS", "DATA": []},
                },
            ]
        }

        entries = replay_browser_scenario.scenario_entries(capture)

        self.assertEqual([entry["path"] for entry in entries], ["/api/one", "/api/two"])
        self.assertEqual(entries[1]["method"], "POST")

    def test_scenario_capture_issues_include_skipped_and_page_failures(self) -> None:
        capture = {
            "requests": [
                {
                    "sequence": 1,
                    "path": "/api/fail",
                    "status": 500,
                    "response": None,
                    "failure": "connection reset",
                }
            ],
            "console": [{"type": "error", "text": "render failed"}],
            "page_errors": ["TypeError"],
        }

        issues = replay_browser_scenario.scenario_capture_issues(capture)

        self.assertTrue(any("request failure" in issue for issue in issues))
        self.assertTrue(any("HTTP status 500" in issue for issue in issues))
        self.assertTrue(any("not JSON" in issue for issue in issues))
        self.assertTrue(any("page error" in issue for issue in issues))
        self.assertTrue(any("console error" in issue for issue in issues))


if __name__ == "__main__":
    unittest.main()
