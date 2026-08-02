package transport

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"deeptrace-backend/config"
	"deeptrace-backend/logging"
)

// UserInfo is the real identity read from the deepflow metadb (MySQL).
// The local metadb t_user table only carries id/username — the cloud
// contract fields (USER_TYPE/ACCOUNT_RULE/...) are synthesized defaults.
type UserInfo struct {
	ID       int
	Username string
	Email    string
	OrgID    int
}

// UserStore loads the real user/org from MySQL when configured.
// Get returns nil when MySQL is not configured or the query fails —
// callers then fall back to the hardcoded identity.
type UserStore struct {
	info *UserInfo
}

// NewUserStore connects to the deepflow metadb (when configured) and reads
// the first user + default org. A failed connection is logged and leaves the
// store empty (hardcoded fallback stays active).
func NewUserStore(cfg *config.MysqlConfig) *UserStore {
	store := &UserStore{}
	if cfg == nil || cfg.Host == "" {
		return store
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=5s&readTimeout=5s&charset=utf8mb4",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		logging.Warnf("MySQL user store: open failed: %v", err)
		return store
	}
	defer db.Close()
	db.SetConnMaxLifetime(10 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// t_user carries id/username (metadb model). Prefer the last-registered
	// user (ORDER BY id DESC) — the cloud sample user has a large id.
	var info UserInfo
	err = db.QueryRowContext(ctx,
		"SELECT id, username FROM t_user ORDER BY id DESC LIMIT 1").Scan(&info.ID, &info.Username)
	if err != nil {
		logging.Warnf("MySQL user store: t_user query failed: %v", err)
		return store
	}
	// t_org default org (deepflow standard is org id 4 for the first org).
	orgID := 4
	err = db.QueryRowContext(ctx, "SELECT id FROM t_org ORDER BY id LIMIT 1").Scan(&orgID)
	if err != nil {
		logging.Debugf("MySQL user store: t_org query failed (keep default 4): %v", err)
	}
	info.OrgID = orgID
	info.Email = info.Username
	store.info = &info
	logging.Infof("MySQL user store: loaded real user id=%d username=%q org=%d", info.ID, info.Username, info.OrgID)
	return store
}

// Get returns the loaded real user, or nil when unavailable.
func (s *UserStore) Get() *UserInfo {
	if s == nil {
		return nil
	}
	return s.info
}

// SetUserStore installs the MySQL-backed user store used by auth handlers.
func SetUserStore(s *UserStore) {
	userStore = s
}
