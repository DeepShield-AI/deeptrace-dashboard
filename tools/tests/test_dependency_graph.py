import importlib.util
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).resolve().parents[1] / "dependency_graph.py"
SPEC = importlib.util.spec_from_file_location("dependency_graph", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
dependency_graph = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(dependency_graph)


class DependencyGraphClassificationTests(unittest.TestCase):
    def test_specific_login_list_pattern_beats_login_pattern(self) -> None:
        endpoint = dependency_graph.categorize_path("/api/fauths/login_list")

        self.assertEqual(endpoint, "login_list")

    def test_unclassified_path_uses_terminal_endpoint_name(self) -> None:
        endpoint = dependency_graph.categorize_path(
            "/api/statistics/v1/stats/querier/MergedMultiList"
        )

        self.assertEqual(endpoint, "MergedMultiList")


if __name__ == "__main__":
    unittest.main()
