package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
)

var db *sql.DB

func initDB() {
	var err error
	if err = os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("[DB] failed to create data directory: %v", err)
	}
	db, err = sql.Open("sqlite", filepath.Join(dataDir, "data.db"))
	if err != nil {
		log.Fatalf("[DB] failed to open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Optimize SQLite performance for better concurrency and I/O speed
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA mmap_size=268435456;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			log.Printf("[DB] PRAGMA error: %v", err)
		}
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			sort INTEGER,
			created_at INTEGER,
			updated_at INTEGER
		);
		CREATE TABLE IF NOT EXISTS profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			group_id INTEGER,
			sort INTEGER,
			proxy INTEGER DEFAULT 0,
			fingerprint TEXT DEFAULT '',
			args TEXT DEFAULT '',
			cookie TEXT DEFAULT '',
			notes TEXT DEFAULT '',
			created_at INTEGER,
			updated_at INTEGER
		);
		CREATE TABLE IF NOT EXISTS proxies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			url TEXT,
			ip TEXT DEFAULT '',
			lang TEXT DEFAULT '',
			timezone TEXT DEFAULT '',
			location TEXT DEFAULT '',
			created_at INTEGER,
			updated_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_profiles_group_id ON profiles(group_id);
		CREATE INDEX IF NOT EXISTS idx_profiles_proxy ON profiles(proxy);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_profiles_group_name ON profiles(group_id, name);
	`)
	if err != nil {
		log.Fatalf("[DB] failed to init schema: %v", err)
	}

	// agent 面只按名称定位 profile，名称需全局唯一。旧库可能遗留跨分组重名，
	// 建索引失败时只告警不阻断启动：重名的名称在 agent 面报 409，改名后重启即可补建
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_profiles_name ON profiles(name)`); err != nil {
		log.Printf("[DB] profile names are not globally unique, agent lookups of duplicates will fail until renamed: %v", err)
	}
}
