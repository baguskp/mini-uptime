package main

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"testing"
)

func BenchmarkValidMonitor(b *testing.B) {
	for i := 0; i < b.N; i++ {
		validMonitor("http", "site", "https://example.com", 60)
	}
}

func TestCleanupRetention(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE TABLE checks(checked_at TEXT); CREATE TABLE sessions(expires_at TEXT)"); e != nil {
		t.Fatal(e)
	}
	_, _ = db.Exec("INSERT INTO checks VALUES('2000-01-01T00:00:00Z'),('2999-01-01T00:00:00Z'); INSERT INTO sessions VALUES('2000-01-01T00:00:00Z'),('2999-01-01T00:00:00Z')")
	cleanupRetention(db)
	var n int
	db.QueryRow("SELECT COUNT(*) FROM checks").Scan(&n)
	if n != 1 {
		t.Fatalf("checks retained: %d", n)
	}
	db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 1 {
		t.Fatalf("sessions retained: %d", n)
	}
}

func TestValidMonitor(t *testing.T) {
	if !validMonitor("http", "site", "https://example.com", 10) {
		t.Fatal("valid monitor rejected")
	}
	if validMonitor("bad", "site", "target", 10) {
		t.Fatal("invalid type accepted")
	}
	if validMonitor("http", "", "target", 10) {
		t.Fatal("empty name accepted")
	}
	if validMonitor("http", "site", "target", 9) {
		t.Fatal("short interval accepted")
	}
}
