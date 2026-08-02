package biz

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"time"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/logging"
)

// QueryEntryPaths generates the business entry paths for a biz from real
// ClickHouse data: observed client→server service pairs with their endpoints.
// The cloud stores these as configuration; locally they are derived from
// flow_metrics (auto_service pairs + endpoint aggregation), keeping the exact
// response shape of the cloud contract.
//
// The request carries no payload — the biz_id is the only input, so a fixed
// recent window is used to observe the service pairs.
func QueryEntryPaths(ch *clickhouse.CHService, ctx context.Context, bizID string) []map[string]interface{} {
	if ch == nil || !ch.Enabled() {
		return []map[string]interface{}{}
	}

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	now := time.Now().Unix()
	window := int64(24 * 3600)
	table := ch.ResolveTable("flow_metrics", "application_map", window, "1h")
	if table == "" {
		table = "application_map.1h"
	}

	// Per-side service pairs with their endpoints, name-resolved through the
	// device_map dictionary. groupArray(DISTINCT endpoint) aggregates the
	// endpoints each pair serves.
	sql := fmt.Sprintf(`SELECT auto_service_id_0, auto_service_type_0, auto_service_id_1, auto_service_type_1,
		dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_0), toUInt64(auto_service_id_0)), toString(auto_service_id_0)) AS name_0,
		dictGetOrDefault('flow_tag.device_map', 'name', (toUInt64(auto_service_type_1), toUInt64(auto_service_id_1)), toString(auto_service_id_1)) AS name_1,
		groupArray(DISTINCT endpoint) AS endpoints,
		count() AS cnt
		FROM `+"`flow_metrics`.`%s`"+` WHERE time >= %d AND time <= %d
		GROUP BY auto_service_id_0, auto_service_type_0, auto_service_id_1, auto_service_type_1, name_0, name_1
		ORDER BY cnt DESC LIMIT 50`, table, now-window, now)
	logging.Debugf("biz_entry_path SQL: %s", sql)

	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		logging.Warnf("biz_entry_path query failed: %v", err)
		return []map[string]interface{}{}
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil {
		logging.Warnf("biz_entry_path scan failed: %v", err)
		return []map[string]interface{}{}
	}

	result := make([]map[string]interface{}, 0, len(data))
	for _, row := range data {
		id0 := int(clickhouse.Get[float64](row, "auto_service_id_0"))
		typ0 := int(clickhouse.Get[float64](row, "auto_service_type_0"))
		id1 := int(clickhouse.Get[float64](row, "auto_service_id_1"))
		typ1 := int(clickhouse.Get[float64](row, "auto_service_type_1"))
		name0 := clickhouse.GetStr(row, "name_0")
		name1 := clickhouse.GetStr(row, "name_1")
		endpoints := strList(clickhouse.Get[[]interface{}](row, "endpoints"))

		pairUID := fmt.Sprintf("auto_service_id=%d,auto_service_type=%d->auto_service_id=%d,auto_service_type=%d", id0, typ0, id1, typ1)
		clientUID := fmt.Sprintf("auto_service_id=%d,auto_service_type=%d", id0, typ0)
		serverUID := fmt.Sprintf("auto_service_id=%d,auto_service_type=%d", id1, typ1)

		result = append(result, map[string]interface{}{
			"ID":            int64Hash(pairUID) % 1000000,
			"LCUUID":        UUID5("biz-entry-path:" + pairUID),
			"APP_BIZ_LCUID": bizID,
			"NAME":          name0 + " -> " + name1,
			"CLIENT_SELECT": map[string]interface{}{
				"type": "svc", "lcuid": UUID5("biz-svc:" + clientUID),
			},
			"SERVER_SELECT": map[string]interface{}{
				"type": "svc", "lcuid": UUID5("biz-svc:" + serverUID),
			},
			"SUB_PATHS": []map[string]interface{}{{
				"endpoints":           endpoints,
				"auto_service_0":      name0,
				"auto_service_1":      name1,
				"auto_service_id_0":   id0,
				"auto_service_id_1":   id1,
				"auto_service_type_0": typ0,
				"auto_service_type_1": typ1,
			}},
			"IS_ENTRY":        1,
			"EXTRA_CONDITION": nil,
			"CREATED_AT":      "",
			"UPDATED_AT":      "",
			"CLIENT_DETAIL":   serviceDetail(id0, typ0, name0, bizID, clientUID, "0"),
			"SERVER_DETAIL":   serviceDetail(id1, typ1, name1, bizID, serverUID, "1"),
		})
	}
	return result
}

// serviceDetail builds the CLIENT_DETAIL / SERVER_DETAIL object of an entry
// path (cloud contract shape).
func serviceDetail(id, typ int, name, bizID, serviceUID, side string) map[string]interface{} {
	return map[string]interface{}{
		"ID":                    int64Hash(serviceUID) % 1000000,
		"LCUUID":                UUID5("biz-svc:" + serviceUID),
		"APP_BIZ_LCUID":         bizID,
		"APP_BIZ_SVC_GRP_LCUID": "",
		"ICON_ID":               clickhouse.IconFor(typ),
		"NAME":                  name,
		"SEARCH_PARAMS": map[string]interface{}{
			"endpoints_" + side: []interface{}{},
			"service_uid":       serviceUID,
			"auto_service":      name,
			"auto_service_id":   id,
			"auto_service_type": typ,
		},
		"METRICS_THRESHOLD":      map[string]interface{}{},
		"OBSERVATION_POINT":      "s",
		"OBSERVATION_POINT_ROLE": "",
		"RESOURCE_TYPE":          "",
		"CREATED_AT":             "",
		"UPDATED_AT":             "",
	}
}

// int64Hash returns a stable 63-bit hash of s.
func int64Hash(s string) int64 {
	sum := md5.Sum([]byte(s))
	var v int64
	for i := 0; i < 8; i++ {
		v = v<<8 | int64(sum[i])
	}
	if v < 0 {
		v = -v
	}
	return v
}

// UUID5 returns a deterministic UUID (md5-based, version 5 style) for a name.
func UUID5(name string) string {
	sum := md5.Sum([]byte("deeptrace-biz:" + name))
	sum[6] = (sum[6] & 0x0f) | 0x50 // version 5
	sum[8] = (sum[8] & 0x3f) | 0x80 // variant
	h := hex.EncodeToString(sum[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// strList converts an interface{} slice to string slice.
func strList(v []interface{}) []string {
	out := make([]string, 0, len(v))
	for _, e := range v {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// QueryBizList returns the business list. Cloud deployments store user
// business definitions; locally there is no config table, so a default
// business is derived from real ClickHouse data — the observed service and
// path counts come from an actual flow_metrics query.
func QueryBizList(ch *clickhouse.CHService, ctx context.Context) []map[string]interface{} {
	if ch == nil || !ch.Enabled() {
		return []map[string]interface{}{}
	}

	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	now := time.Now().Unix()
	window := int64(24 * 3600)
	table := ch.ResolveTable("flow_metrics", "application_map", window, "1h")
	if table == "" {
		table = "application_map.1h"
	}

	// Real statistics: distinct services on each side + observed service
	// pairs (paths) in the window.
	sql := fmt.Sprintf(`SELECT uniqExact(auto_service_id_0, auto_service_type_0) AS svc0,
		uniqExact(auto_service_id_1, auto_service_type_1) AS svc1,
		count() AS paths
		FROM `+"`flow_metrics`.`%s`"+` WHERE time >= %d AND time <= %d`, table, now-window, now)
	logging.Debugf("biz_list SQL: %s", sql)

	rows, err := ch.Query(qCtx, sql)
	if err != nil {
		logging.Warnf("biz_list query failed: %v", err)
		return []map[string]interface{}{}
	}
	defer rows.Close()

	data, err := clickhouse.ScanRows(rows)
	if err != nil || len(data) == 0 {
		logging.Warnf("biz_list scan failed: %v", err)
		return []map[string]interface{}{}
	}

	svcNum := int(clickhouse.Get[float64](data[0], "svc0")) + int(clickhouse.Get[float64](data[0], "svc1"))
	pathNum := int(clickhouse.Get[float64](data[0], "paths"))

	return []map[string]interface{}{
		{
			"ID":                1,
			"LCUUID":            "default-biz-00000000-0000-0000-0000-000000000001",
			"USER_ID":           1,
			"TYPE":              2,
			"NAME":              "默认业务",
			"TABLE_NAME":        "flow_log.l7_flow_log",
			"METRICS_THRESHOLD": "[]",
			"POSITION":          1,
			"STAR":              0,
			"SHARE_STAR":        0,
			"DISABLED":          false,
			"DISABLED_REASON":   "",
			"CREATED_AT":        "",
			"UPDATED_AT":        "",
			"SVC_NUM":           svcNum,
			"SVC_GRP_NUM":       0,
			"PATH_NUM":          pathNum,
			"TEAM_INFO":         []interface{}{},
			"TEAM_ID":           1,
			"OWNER_USER_INFO":   map[string]interface{}{"ID": 1, "EMAIL": "admin@deeptrace.local", "USER_TYPE": 5},
			"ACCESS_ACTIONS":    []interface{}{},
		},
	}
}

// QueryBizGroups returns the business groups (groupings of businesses for the
// business-overview scenario view). Cloud stores these as config; locally a
// default group is derived, containing the default business LCUUID.
func QueryBizGroups() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"ID":         1,
			"LCUUID":     "default-biz-group-00000000-0000-0000-0000-000000000001",
			"NAME":       "默认分组",
			"TYPE":       2,
			"USER_ID":    1,
			"SORT_ORDER": 0,
			"ORG_ORDER":  4,
			"CREATED_AT": "",
			"UPDATED_AT": "",
			"BIZ_COUNT":  1,
			"BIZ_LIST":   []string{"default-biz-00000000-0000-0000-0000-000000000001"},
		},
	}
}
