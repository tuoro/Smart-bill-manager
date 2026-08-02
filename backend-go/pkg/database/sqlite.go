package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open 创建数据库连接，并将错误交给调用方决定如何处理。
func Open(dataDir string) (*gorm.DB, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	dbPath := filepath.Join(dataDir, "bills.db")

	dsn := buildSQLiteDSN(dbPath)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接句柄失败: %w", err)
	}

	// SQLite tuning:
	// - apply PRAGMAs for compatibility (DSN sets most options for new conns)
	// - improve read/write concurrency with WAL
	applySQLiteTuning(sqlDB)

	return db, nil
}

func applySQLiteTuning(sqlDB *sql.DB) {
	if sqlDB == nil {
		return
	}

	// Allow concurrent readers under WAL while keeping SQLite safe.
	// Writes are still serialized by SQLite.
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA busy_timeout = 5000;",
		// 20MB page cache (negative value means KiB).
		"PRAGMA cache_size = -20000;",
		// Auto-checkpoint after 1000 pages (~4MB with default page_size=4096).
		"PRAGMA wal_autocheckpoint = 1000;",
	}

	for _, q := range pragmas {
		if _, err := sqlDB.Exec(q); err != nil {
			log.Printf("[DB] sqlite pragma failed: %s err=%v", q, err)
		}
	}
}

func buildSQLiteDSN(dbPath string) string {
	// Apply defaults per-connection via DSN so increased pool sizes remain safe.
	// Most pragmas are also re-applied in applySQLiteTuning as a compatibility fallback.
	p := strings.TrimSpace(dbPath)
	if p == "" {
		return dbPath
	}
	// Avoid duplicating params if caller already passed a DSN.
	if strings.Contains(p, "?") {
		return p
	}
	return p + "?" + strings.Join([]string{
		"_busy_timeout=5000",
		"_foreign_keys=1",
		"_journal_mode=WAL",
		"_synchronous=NORMAL",
		"_temp_store=MEMORY",
		"_cache_size=-20000",
		"_wal_autocheckpoint=1000",
	}, "&")
}

func newGormLogger() logger.Interface {
	// Default to Warn in production, Info during debugging.
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SBM_DB_LOG_SQL")))
	lvl := logger.Warn
	if mode == "1" || mode == "true" || mode == "yes" || mode == "on" {
		lvl = logger.Info
	}

	slowMs := 200
	if v := strings.TrimSpace(os.Getenv("SBM_DB_SLOW_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			slowMs = n
		}
	}

	return logger.New(
		log.New(os.Stdout, "\r\n[GORM] ", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Duration(slowMs) * time.Millisecond,
			LogLevel:                  lvl,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}
