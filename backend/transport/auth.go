package transport

import (
	"fmt"
	"net/http"
	"strings"
)

// RegisterAuth adds auth-related API routes.
func RegisterAuth(mux *http.ServeMux) {
	mux.HandleFunc("/api/fauths/login", handleLogin)
	mux.HandleFunc("/api/fauths/login_list", handleLoginList)
	mux.HandleFunc("/api/fuser/v1/users/current", handleCurrentUser)
	mux.HandleFunc("/api/fpermit/v1/orgs", handleOrgs)
	mux.HandleFunc("/api/fpermit/v1/org/", handleOrgRoutes)
}

// --------------------------------------------------------------------------
// Hardcoded auth handlers (matching real DeepFlow API shapes)
// --------------------------------------------------------------------------

const (
	fakeAccessToken  = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTksImlhdCI6MTcyMTUwMDAwMCwiaXNzIjoiYXV0aDp0b2tlbiIsImRhdGEiOnsiaWQiOjEsInVzZXJuYW1lIjoiYWRtaW4iLCJlbWFpbCI6ImFkbWluQGRlZXB0cmFjZS5sb2NhbCIsImxvZ2luX3RpbWUiOjE3MjE1MDAwMDAsInJlZnJlc2hfdGltZSI6MTcyMTUwMDAwMCwidG9rZW5fa2V5IjoiZmFrZS1rZXkiLCJvcmdfaWQiOjQsInRlYW1faWQiOjF9fQ.ZmFrZS1zaWduYXR1cmU"
	fakeRefreshToken = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTksImlhdCI6MTcyMTUwMDAwMH0.ZmFrZS1zaWduYXR1cmU"
)

func handleLogin(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"OPT_STATUS": "SUCCESS", "DESCRIPTION": "",
		"DATA": map[string]interface{}{
			"access_token":  fakeAccessToken,
			"refresh_token": fakeRefreshToken,
		},
	})
}

func handleLoginList(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, map[string]interface{}{
		"1": map[string]interface{}{
			"name": "DeepFlow", "platform": "deepflow", "type": "deepflow",
			"format": "email", "login_account_mode": []string{"email", "phone"}, "raw_input": true,
		},
	})
}

// userStore is set by main when MySQL metadb access is configured.
var userStore *UserStore

// buildUserResponse assembles the /users/current DATA contract (verified
// against api_cache GET__api_fuser_v1_users_current_nobody.json: 17 keys).
// Identity fields come from the real user when available; the account rule
// objects are stable defaults (the local metadb has no such columns).
func buildUserResponse(u *UserInfo) map[string]interface{} {
	id, username, email, orgID := 1, "admin", "admin@deeptrace.local", 4
	company, uuid := "DeepTrace", "admin-uuid-001"
	if u != nil {
		id, username, email, orgID = u.ID, u.Username, u.Email, u.OrgID
		uuid = fmt.Sprintf("user-uuid-%d", u.ID)
		company = ""
	}
	return map[string]interface{}{
		"ID": id, "USERNAME": username, "EMAIL": email,
		"PHONE_NUM": "", "USER_TYPE": 5, "REAL_USER_TYPE": 5,
		"USERUUID": uuid, "ORG_ID": orgID, "COMPANY": company,
		"ACCESS_TOKEN": fakeAccessToken,
		"ACCOUNT_RULE": map[string]interface{}{
			"account_allowed_login_time_period": false,
			"account_allowed_login_min_time":    0, "account_allowed_login_max_time": 0,
			"account_not_login_lock_time": 0, "account_not_change_pwd_lock_time": 0,
			"account_first_login_change_pwd": false, "account_second_check": false,
			"account_login_failed_count": 0, "account_login_failed_locked_time": 60,
			"account_allow_login_white_list_ip": "*", "verifycode_use": false,
			"user_limit": 100, "user_tenant_limit": 200,
			"use_ungrouped_type": false, "read_only_admin": false,
			"radius_account_switch": false,
		},
		"PWD_RULE": map[string]interface{}{
			"pwd_min_len": 8, "pwd_max_len": 16,
			"pwd_include_number": false, "pwd_include_string": false,
			"pwd_include_case": false, "pwd_include_special_chars": false,
		},
		"SESSION_RULE": map[string]interface{}{
			"session_inactive_close": false, "session_inactive_close_interval": 0,
			"session_single": false, "session_max_online": 0, "session_one_client": false,
		},
		"SSO_RULE":          map[string]interface{}{"sso_open": false, "sso_link": []interface{}{}},
		"FILE_STORAGE_RULE": map[string]interface{}{"file_storage_extension": "rpm,iso,gz,zip,tar", "file_storage_size": 3072},
		"SEARCH_RULE":       nil,
		"TENANT_ORG_CONFIG": map[string]interface{}{"org_create_enable": false, "org_create_max_num": 2},
	}
}

func handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, buildUserResponse(userStore.Get()))
}

func handleOrgs(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, []map[string]interface{}{
		{
			"ID": 4, "LCUUID": "org-uuid-001", "ORG_ID": 4, "NAME": "DeepTrace",
			"DESC": "", "STATUS": 0, "OWNER_USER_ID": 1,
			"DISABLED_DELETE": false, "USER_NUM": 1,
			"OWNER_USER_INFO": map[string]interface{}{"ID": 1, "EMAIL": "admin@deeptrace.local", "USER_TYPE": 5},
		},
	})
}

func handleOrgRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.Contains(path, "select"):
		w.Header().Set("Set-Cookie", "X-Org-Id=4; Path=/; Max-Age=31536000")
		writeSuccess(w, nil)
	case strings.Contains(path, "page_scopes") && strings.Contains(path, "team"):
		writeSuccess(w, []map[string]interface{}{
			{"ID": 1, "LCUUID": "scope-001", "SCOPE": "[]", "TEAM_ID": 1},
		})
	case strings.Contains(path, "page_scopes"):
		writeSuccess(w, map[string]interface{}{"pages": []interface{}{}})
	case strings.Contains(path, "role_teams"):
		writeSuccess(w, []map[string]interface{}{
			{"ID": 1, "NAME": "默认团队", "ROLE": "owner", "SHORT_LCUUID": "team-001", "ORG_ID": 4},
		})
	case strings.Contains(path, "teams"):
		writeSuccess(w, []map[string]interface{}{
			{"ID": 1, "NAME": "默认团队", "SHORT_LCUUID": "team-001", "ORG_ID": 4},
		})
	default:
		writeSuccess(w, []interface{}{})
	}
}
