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

func TestDisplaySummaryAndAverage(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE groups(id INTEGER PRIMARY KEY,name TEXT); CREATE TABLE monitors(id INTEGER PRIMARY KEY,group_id INTEGER,name TEXT,type TEXT,target TEXT,current_status TEXT,last_latency_ms INTEGER,checked_at TEXT,enabled INTEGER); CREATE TABLE incidents(monitor_id INTEGER,started_at TEXT,ended_at TEXT); CREATE TABLE checks(id INTEGER PRIMARY KEY,monitor_id INTEGER,latency_ms INTEGER)"); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec("INSERT INTO settings VALUES('display_mode','public'); INSERT INTO monitors VALUES(1,NULL,'API','http','https://api.example.com','up',18,'2026-08-18T14:42:10Z',1),(2,NULL,'Router','ping','10.0.0.1','down',0,'',1); INSERT INTO checks VALUES(1,1,10),(2,1,14); INSERT INTO incidents VALUES(2,'2026-08-18T14:42:10Z',NULL)")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/display", nil)
	display(db)(w, r)
	body := w.Body.String()
	for _, expected := range []string{"1/2", "50%", "12 ms", "API", "Router"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("display missing %q", expected)
		}
	}
}

func TestHumanTime(t *testing.T) {
	if got := humanTime("2026-08-18T14:42:10Z"); got != time.Date(2026, 8, 18, 14, 42, 10, 0, time.UTC).Format("02 Jan, 15:04") {
		t.Fatalf("humanTime=%q", got)
	}
	if got := humanTime("bad"); got != "bad" {
		t.Fatalf("invalid time=%q", got)
	}
	if got := incidentEnded(""); got != "Ongoing" {
		t.Fatalf("open incident time=%q", got)
	}
	if got := incidentEnded("2026-08-18T14:42:10Z"); got != humanTime("2026-08-18T14:42:10Z") {
		t.Fatalf("ended incident time=%q", got)
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

func TestGroupAssignMonitors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE groups(id INTEGER PRIMARY KEY, name TEXT); CREATE TABLE monitors(id INTEGER PRIMARY KEY, group_id INTEGER, name TEXT, target TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO groups(id,name) VALUES(1,'Production'),(2,'Staging'); INSERT INTO monitors(id,group_id,name,target) VALUES(1,1,'API','https://api.example.com'),(2,2,'Web','https://example.com')"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/groups/1/monitors", strings.NewReader("monitor_id=2"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	groupAssignMonitors(db)(w, r)
	if w.Code != 303 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var groupID sql.NullInt64
	if err := db.QueryRow("SELECT group_id FROM monitors WHERE id=1").Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if groupID.Valid {
		t.Fatalf("old monitor group_id=%d", groupID.Int64)
	}
	if err := db.QueryRow("SELECT group_id FROM monitors WHERE id=2").Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if !groupID.Valid || groupID.Int64 != 1 {
		t.Fatalf("selected monitor group_id=%v", groupID)
	}
}
