package fastlist

import (
	"strings"
	"testing"
)

func TestFlattenAndBranch(t *testing.T) {
	conds := []interface{}{
		map[string]interface{}{"key": "l7_protocol", "op": "=", "val": float64(1)},
		map[string]interface{}{"key": "l7_protocol", "op": "=", "val": float64(2)},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	want := []string{"`l7_protocol` = 1", "`l7_protocol` = 2"}
	if len(got) != len(want) {
		t.Fatalf("got %d conditions %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("condition[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFlattenOrBranch(t *testing.T) {
	conds := []interface{}{
		map[string]interface{}{"key": "signal_source", "op": "=", "val": "a"},
		map[string]interface{}{
			"op": "OR",
			"val": []interface{}{
				map[string]interface{}{"key": "request_type", "op": "=", "val": "x"},
				map[string]interface{}{"key": "request_type", "op": "=", "val": "y"},
			},
		},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	if len(got) != 2 {
		t.Fatalf("got %d conditions %v, want 2 (OR stays one unit)", len(got), got)
	}
	if got[1] != "(`request_type` = 'x' OR `request_type` = 'y')" {
		t.Fatalf("OR branch = %q, want parenthesized OR", got[1])
	}
}

func TestFlattenInArray(t *testing.T) {
	conds := []interface{}{
		map[string]interface{}{
			"key": "policy_type", "op": "IN",
			"val": []interface{}{float64(4), float64(7)},
		},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	if len(got) != 1 {
		t.Fatalf("got %d conditions, want 1", len(got))
	}
	// ClickHouse requires tuple parentheses: `col` IN (4, 7)
	if got[0] != "`policy_type` IN (4, 7)" {
		t.Fatalf("IN condition = %q, want `policy_type` IN (4, 7)", got[0])
	}
}

func TestFlattenStringValueEscapesQuotes(t *testing.T) {
	conds := []interface{}{
		map[string]interface{}{"key": "note", "op": "=", "val": "it's"},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	if len(got) != 1 {
		t.Fatalf("got %d conditions, want 1", len(got))
	}
	if !strings.Contains(got[0], "'it\\'s'") {
		t.Fatalf("quoted value = %q, want escaped single quote", got[0])
	}
}

func TestFlattenVirtualTagUsesIDColumnForNumbers(t *testing.T) {
	// Virtual tag compared to a number → physical ID column with _0 suffix.
	conds := []interface{}{
		map[string]interface{}{"key": "auto_service", "op": "=", "val": float64(5)},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	if len(got) != 1 {
		t.Fatalf("got %d conditions, want 1", len(got))
	}
	if !strings.Contains(got[0], "auto_service_id_0") {
		t.Fatalf("virtual tag = %q, want physical ID column", got[0])
	}
}

func TestFlattenNestedOrAndAnd(t *testing.T) {
	conds := []interface{}{
		map[string]interface{}{
			"op": "OR",
			"val": []interface{}{
				map[string]interface{}{
					"op": "AND",
					"val": []interface{}{
						map[string]interface{}{"key": "a", "op": "=", "val": float64(1)},
						map[string]interface{}{"key": "b", "op": "=", "val": float64(2)},
					},
				},
				map[string]interface{}{"key": "c", "op": "=", "val": float64(3)},
			},
		},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	if len(got) != 1 {
		t.Fatalf("got %d conditions %v, want 1 OR unit", len(got), got)
	}
	// The AND child must stay grouped inside the OR:
	// (a AND b) OR c — not a OR b OR c.
	want := "((`a` = 1 AND `b` = 2) OR `c` = 3)"
	if got[0] != want {
		t.Fatalf("nested = %q, want %q", got[0], want)
	}
}

func TestFlattenVirtualTagWithNumericStringValue(t *testing.T) {
	// Regression: the frontend sends resource filters with numeric-string
	// values (`vpc` = '1', `chost` = '1'). These must map to the physical
	// ID column (epc_id_0 / l3_device_id_0) — the old code only mapped
	// JSON numbers, so ClickHouse failed on Missing columns 'vpc' 'chost'.
	cases := []struct {
		key, op, val string
		want         string
	}{
		{"vpc", "=", "1", "`epc_id_0` = '1'"},
		{"chost", "=", "1", "`l3_device_id_0` = '1'"},
		{"region", "=", "7", "`region_id_0` = '7'"},
	}
	for _, c := range cases {
		conds := []interface{}{
			map[string]interface{}{"key": c.key, "op": c.op, "val": c.val},
		}
		got := FlattenFastListConditions(conds, "flow_log")
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: got %v, want [%s]", c.key, got, c.want)
		}
	}
}

func TestFlattenVirtualTagJSONNumberStillMaps(t *testing.T) {
	conds := []interface{}{
		map[string]interface{}{"key": "vpc", "op": "=", "val": float64(1)},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	if len(got) != 1 || got[0] != "`epc_id_0` = 1" {
		t.Fatalf("got %v, want [`epc_id_0` = 1]", got)
	}
}

func TestFlattenNameStringValueStaysUnmapped(t *testing.T) {
	// A non-numeric string (a display name, rare in fast_list) must NOT be
	// mapped to the ID column.
	conds := []interface{}{
		map[string]interface{}{"key": "vpc", "op": "=", "val": "my-vpc-name"},
	}
	got := FlattenFastListConditions(conds, "flow_log")
	if len(got) != 1 || got[0] != "`vpc` = 'my-vpc-name'" {
		t.Fatalf("got %v, want [`vpc` = 'my-vpc-name']", got)
	}
}
