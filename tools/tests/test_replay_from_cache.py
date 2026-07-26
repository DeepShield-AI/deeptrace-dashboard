import importlib.util
import json
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).resolve().parents[1] / "replay_from_cache.py"
SPEC = importlib.util.spec_from_file_location("replay_from_cache", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
replay_from_cache = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(replay_from_cache)


class ReplayValidationTests(unittest.TestCase):
    def test_strict_validation_checks_all_rows(self) -> None:
        expected = {
            "OPT_STATUS": "SUCCESS",
            "DESCRIPTION": "",
            "DATA": [{"name": "a", "count": 1}, {"name": "b", "count": 2}],
        }
        actual = {
            "OPT_STATUS": "SUCCESS",
            "DESCRIPTION": "",
            "DATA": [{"name": "a", "count": 1}, {"name": "b"}],
        }

        issues = replay_from_cache.validate_response_structure(
            actual,
            expected,
            strict=True,
        )

        self.assertTrue(any("count" in issue for issue in issues))

    def test_strict_validation_checks_rows_after_fifty(self) -> None:
        expected = {
            "OPT_STATUS": "SUCCESS",
            "DATA": [{"name": "expected", "count": 1}],
        }
        actual_rows = [
            {"name": f"row-{index}", "count": index}
            for index in range(51)
        ]
        actual_rows.append({"name": "row-51"})
        actual = {"OPT_STATUS": "SUCCESS", "DATA": actual_rows}

        issues = replay_from_cache.validate_response_structure(
            actual,
            expected,
            strict=True,
        )

        self.assertTrue(any("1/52" in issue and "count" in issue for issue in issues))

    def test_strict_validation_checks_scalar_items_after_fifty(self) -> None:
        expected = {"OPT_STATUS": "SUCCESS", "DATA": ["value"]}
        actual = {
            "OPT_STATUS": "SUCCESS",
            "DATA": ["value"] * 50 + [{"wrong": "type"}],
        }

        issues = replay_from_cache.validate_response_structure(
            actual,
            expected,
            strict=True,
        )

        self.assertTrue(any("DATA[50] type" in issue for issue in issues))

    def test_strict_validation_accepts_observed_heterogeneous_shapes(self) -> None:
        expected = {
            "OPT_STATUS": "SUCCESS",
            "DATA": [{"name": "a", "count": 1}, {"name": "b", "state": "ok"}],
        }
        actual = {
            "OPT_STATUS": "SUCCESS",
            "DATA": [{"name": "x", "state": "ok"}, {"name": "y", "count": 2}],
        }

        issues = replay_from_cache.validate_response_structure(
            actual,
            expected,
            strict=True,
        )

        self.assertEqual(issues, [])

    def test_strict_validation_checks_schema_metadata(self) -> None:
        expected = {
            "OPT_STATUS": "SUCCESS",
            "DESCRIPTION": "",
            "DATA": [{"latency": 1.5}],
            "SCHEMAS": {
                "latency": {
                    "type": 1,
                    "unit": "us",
                    "value_type": "Float64",
                    "pre_as": "Avg(latency)",
                    "label_type": "",
                }
            },
        }
        actual = json.loads(json.dumps(expected))
        actual["SCHEMAS"]["latency"]["unit"] = "ms"

        issues = replay_from_cache.validate_response_structure(
            actual,
            expected,
            strict=True,
        )

        self.assertTrue(any("SCHEMAS.latency.unit" in issue for issue in issues))

    def test_rewrite_request_times_preserves_duration_and_case(self) -> None:
        request = {
            "time_start": 100,
            "time_end": 160,
            "nested": {"TIME_START": 200, "TIME_END": 260},
        }

        rewritten = replay_from_cache.rewrite_request_times(request, now=1_000)

        self.assertEqual(rewritten["time_start"], 940)
        self.assertEqual(rewritten["time_end"], 1_000)
        self.assertEqual(rewritten["nested"]["TIME_START"], 940)
        self.assertEqual(rewritten["nested"]["TIME_END"], 1_000)
        self.assertEqual(request["time_end"], 160)

    def test_known_endpoint_matching_is_exact(self) -> None:
        entries = [
            {
                "path": "/api/statistics/v1/stats/querier/List",
                "method": "POST",
            },
            {
                "path": "/api/statistics/v1/stats/querier/FlowLogDetailList",
                "method": "POST",
            },
        ]

        matched = replay_from_cache.find_entries("List", entries)

        self.assertEqual(len(matched), 1)
        self.assertTrue(matched[0]["path"].endswith("/List"))

    def test_root_endpoint_can_be_replayed(self) -> None:
        entries = [{"path": "/", "method": "GET"}]

        matched = replay_from_cache.find_entries("(root)", entries)

        self.assertEqual(matched, entries)

    def test_source_provenance_is_required_for_forced_replay(self) -> None:
        issues = replay_from_cache.validate_source_provenance(
            requested_source="zerotrace",
            actual_source=None,
        )

        self.assertEqual(len(issues), 1)
        self.assertIn("X-DeepTrace-Source", issues[0])


if __name__ == "__main__":
    unittest.main()
