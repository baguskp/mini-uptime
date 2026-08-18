package main

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func BenchmarkValidMonitor(b *testing.B) {
	for i := 0; i < b.N; i++ {
		validMonitor("http", "site", "https://example.com", 60)
	}
}

func TestExecRetry(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE TABLE items(value TEXT)"); e != nil {
		t.Fatal(e)
	}
	if e := execRetry(db, "INSERT INTO items(value) VALUES(?)", "ok"); e != nil {
		t.Fatal(e)
	}
	var v string
	db.QueryRow("SELECT value FROM items").Scan(&v)
	if v != "ok" {
		t.Fatalf("value=%q", v)
	}
	if e := execRetry(db, "INSERT INTO missing(value) VALUES(?)", "bad"); e == nil {
		t.Fatal("expected SQL error")
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

func TestSettingsDoesNotRenderTelegramToken(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.Exec("CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)")
	db.Exec("INSERT INTO settings VALUES('telegram_token','secret-token')")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/settings", nil)
	settingsPage(db)(w, r)
	if strings.Contains(w.Body.String(), "secret-token") {
		t.Fatal("telegram token rendered")
	}
}

func TestDisplayPINHash(t *testing.T) {
	h, e := hashPassword("1234")
	if e != nil || !checkPassword("1234", h) {
		t.Fatal("pin hash failed")
	}
	if checkPassword("4321", h) {
		t.Fatal("wrong pin accepted")
	}
}

func TestHumanTime(t *testing.T) {
	if got := humanTime("2026-08-18T14:42:10Z"); got != time.Date(2026, 8, 18, 14, 42, 10, 0, time.UTC).Local().Format("02 Jan, 15:04") {
		t.Fatalf("humanTime=%q", got)
	}
	if got := humanTime("bad"); got != "bad" {
		t.Fatalf("invalid time=%q", got)
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	h, e := hashPassword("correct horse battery staple")
	if e != nil || !checkPassword("correct horse battery staple", h) {
		t.Fatal("password rejected")
	}
	if checkPassword("wrong", h) {
		t.Fatal("wrong password accepted")
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
	if !validMonitor("tcp", "db", "localhost:5432", 10) {
		t.Fatal("valid tcp rejected")
	}
	if !validMonitor("ping", "router", "192.168.1.1", 10) {
		t.Fatal("valid ping rejected")
	}
	if validMonitor("ping", "router", "https://example.com", 10) {
		t.Fatal("url accepted as ping")
	}
}
