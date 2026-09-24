package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// useTestDB 在临时目录建库并替换全局 db，测试结束后还原。
func useTestDB(t *testing.T) {
	t.Helper()
	oldDB, oldDataDir := db, dataDir
	dataDir = t.TempDir()
	initDB()
	t.Cleanup(func() {
		db.Close()
		db, dataDir = oldDB, oldDataDir
	})
}

// insertTestProfile 写入一条最简 profile，返回运行表所用的内部 ID。
func insertTestProfile(t *testing.T, name string, groupID int64) string {
	t.Helper()
	res, err := db.Exec("INSERT INTO profiles (name, group_id, sort) VALUES (?, ?, 0)", name, groupID)
	if err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	rawID, _ := res.LastInsertId()
	return encodeID(rawID)
}

func TestProfileNamesAreGloballyUnique(t *testing.T) {
	useTestDB(t)
	insertTestProfile(t, "HK-01", 1)

	_, err := db.Exec("INSERT INTO profiles (name, group_id, sort) VALUES ('HK-01', 2, 0)")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("cross-group duplicate insert: err = %v, want UNIQUE violation", err)
	}
}

// 升级前的库可能已有跨分组重名：建全局唯一索引失败不能阻断启动，
// 重名的名称在 agent 面报 409 并列出所在分组。
func TestInitDBToleratesLegacyDuplicateNames(t *testing.T) {
	oldDB, oldDataDir := db, dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { db, dataDir = oldDB, oldDataDir })

	legacy, err := sql.Open("sqlite", filepath.Join(dataDir, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			sort INTEGER,
			created_at INTEGER,
			updated_at INTEGER
		);
		CREATE TABLE profiles (
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
		CREATE UNIQUE INDEX idx_profiles_group_name ON profiles(group_id, name);
		INSERT INTO groups (name) VALUES ('电商');
		INSERT INTO profiles (name, group_id, sort) VALUES ('dup', 0, 0), ('dup', 1, 0);
	`)
	legacy.Close()
	if err != nil {
		t.Fatal(err)
	}

	initDB()
	t.Cleanup(func() { db.Close() })

	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_profiles_name'`).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("global name index count = %d (err %v), want 0 while duplicates remain", n, err)
	}
	_, code, err := resolveProfileName("dup")
	if code != 409 || err == nil || !strings.Contains(err.Error(), "电商") {
		t.Fatalf("resolveProfileName(dup) = %d, %v; want 409 listing the groups", code, err)
	}
}
