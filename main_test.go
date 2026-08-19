package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"net/http"
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

func TestMonitorAlertEscapesAndRedacts(t *testing.T) {
	alert := formatMonitorAlert(monitorAlertData{Name: "API <prod>", Type: "http", Target: "https://user:pass@example.com/health?token=secret", Group: "Production & Core"}, "down", 0, "<timeout>", "2026-08-18T14:42:10Z", "")
	for _, forbidden := range []string{"secret", "user:pass", "<prod>", "<timeout>"} {
		if strings.Contains(alert, forbidden) {
			t.Fatalf("alert leaked %q: %s", forbidden, alert)
		}
	}
	for _, expected := range []string{"MONITOR DOWN", "API &lt;prod&gt;", "example.com/health?redacted", "Production &amp; Core", "&lt;timeout&gt;"} {
		if !strings.Contains(alert, expected) {
			t.Fatalf("alert missing %q: %s", expected, alert)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	if got := humanDuration(25*time.Hour + 2*time.Minute + 3*time.Second); got != "1d 1h 2m 3s" {
		t.Fatalf("duration=%q", got)
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

func TestAgentHealthValidTokenStoresHeartbeat(t *testing.T) {
	db := newAgentTestDB(t)
	t.Setenv("AGENT_INGEST_TOKEN", "agent-secret")
	r := httptest.NewRequest("POST", "/api/agent/health", strings.NewReader(`{"hostname":"PC-001","os":"windows","architecture":"amd64","cpus":8,"memory":{"total_bytes":100,"available_bytes":40},"disk":{"path":"C:\\\\","total_bytes":200,"available_bytes":80},"internet_ping_ms":25.4,"collected_at":"2026-08-14T10:00:00Z"}`))
	r.Header.Set("Authorization", "Bearer agent-secret")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	agentHealth(db)(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var hostname, status string
	if err := db.QueryRow("SELECT hostname,status FROM agents").Scan(&hostname, &status); err != nil {
		t.Fatal(err)
	}
	if hostname != "PC-001" || status != "online" {
		t.Fatalf("agent=%q status=%q", hostname, status)
	}
}

func TestAgentHealthTokenRegistrationRenamesExistingRow(t *testing.T) {
	db := newAgentTestDB(t)
	t.Setenv("AGENT_INGEST_TOKEN", "")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("registered-token")))
	_, err := db.Exec("INSERT INTO agents(hostname,token_hash,status,heartbeat_at,collected_at,primary_ip,os,architecture,cpus,memory_total_bytes,memory_available_bytes,disk_path,disk_total_bytes,disk_available_bytes,gateway_ip,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", "PC-LABEL", hash, "offline", "", "", "", "", "", 0, 0, 0, "", 0, 0, "", "now", "now")
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"hostname":"PC-ACTUAL","os":"windows","architecture":"amd64","cpus":4,"memory":{"total_bytes":100,"available_bytes":50},"disk":{"path":"C:\\\\","total_bytes":200,"available_bytes":100},"collected_at":"2026-08-14T10:00:00Z"}`
	r := httptest.NewRequest("POST", "/api/agent/health", strings.NewReader(payload))
	r.Header.Set("Authorization", "Bearer registered-token")
	w := httptest.NewRecorder()
	agentHealth(db)(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	var hostname string
	if err := db.QueryRow("SELECT COUNT(*) FROM agents").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT hostname FROM agents").Scan(&hostname); err != nil {
		t.Fatal(err)
	}
	if count != 1 || hostname != "PC-ACTUAL" {
		t.Fatalf("count=%d hostname=%q", count, hostname)
	}
}

func TestAgentHealthRejectsInvalidToken(t *testing.T) {
	db := newAgentTestDB(t)
	t.Setenv("AGENT_INGEST_TOKEN", "agent-secret")
	r := httptest.NewRequest("POST", "/api/agent/health", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	agentHealth(db)(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestAgentHealthRejectsInvalidPayload(t *testing.T) {
	db := newAgentTestDB(t)
	t.Setenv("AGENT_INGEST_TOKEN", "agent-secret")
	r := httptest.NewRequest("POST", "/api/agent/health", strings.NewReader(`{"hostname":"PC-001"}`))
	r.Header.Set("Authorization", "Bearer agent-secret")
	w := httptest.NewRecorder()
	agentHealth(db)(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestAgentOnlineUsesThreeHeartbeatIntervals(t *testing.T) {
	t.Setenv("AGENT_HEARTBEAT_INTERVAL", "10s")
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	if !agentOnline(now.Add(-30*time.Second).Format(time.RFC3339), now) {
		t.Fatal("agent should be online at the threshold")
	}
	if agentOnline(now.Add(-31*time.Second).Format(time.RFC3339), now) {
		t.Fatal("agent should be offline after the threshold")
	}
}

func TestAgentCreateRegistersHostname(t *testing.T) {
	db := newAgentTestDB(t)
	r := httptest.NewRequest("POST", "/agents", strings.NewReader("display_name=PC-002"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	agentCreate(db)(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var status, tokenHash string
	if err := db.QueryRow("SELECT status,token_hash FROM agents WHERE hostname=?", "PC-002").Scan(&status, &tokenHash); err != nil {
		t.Fatal(err)
	}
	if status != "offline" || tokenHash == "" {
		t.Fatalf("status=%q token hash empty=%v", status, tokenHash == "")
	}
}

func TestAgentPagesRender(t *testing.T) {
	db := newAgentTestDB(t)
	_, err := db.Exec("INSERT INTO agents(hostname,token_hash,status,heartbeat_at,collected_at,primary_ip,os,architecture,cpus,memory_total_bytes,memory_available_bytes,disk_path,disk_total_bytes,disk_available_bytes,gateway_ip,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", "PC-003", "hash", "online", time.Now().UTC().Format(time.RFC3339), "2026-08-19T12:00:00Z", "192.168.1.3", "windows", "amd64", 4, 1000, 500, "C:\\", 2000, 1000, "192.168.1.1", "2026-08-19T12:00:00Z", "2026-08-19T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	agentsPage(db)(w, httptest.NewRequest("GET", "/agents", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "PC-003") {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/agents/1", nil)
	r.SetPathValue("id", "1")
	agentDetail(db)(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "PC-003") {
		t.Fatalf("detail status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRenderAddsAgentNavigationToLegacyPages(t *testing.T) {
	w := httptest.NewRecorder()
	render(w, "incidents.html", []map[string]string{})
	if !strings.Contains(w.Body.String(), `href="/agents">Agents</a>`) {
		t.Fatal("legacy page is missing Agents navigation")
	}
}

func TestNormalizeNavbarUsesOneMenuOrder(t *testing.T) {
	page := normalizeNavbar(`<main><nav><strong>MiniUptime</strong><a href="/settings">Settings</a><a href="/dashboard">Dashboard</a><form method="post" action="/logout"><input name="csrf"></form></nav></main>`)
	order := []string{`href="/dashboard"`, `href="/monitors"`, `href="/agents"`, `href="/groups"`, `href="/incidents"`, `href="/settings"`, `href="/display"`}
	previous := -1
	for _, item := range order {
		current := strings.Index(page, item)
		if current <= previous {
			t.Fatalf("menu item %q is out of order: %s", item, page)
		}
		previous = current
	}
	if !strings.Contains(page, `action="/logout"`) {
		t.Fatal("logout form was dropped")
	}
}

func newAgentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE agents(id INTEGER PRIMARY KEY, display_name TEXT NOT NULL DEFAULT '', hostname TEXT UNIQUE NOT NULL, token_hash TEXT NOT NULL, status TEXT NOT NULL, heartbeat_at TEXT NOT NULL, collected_at TEXT NOT NULL, primary_ip TEXT NOT NULL, os TEXT NOT NULL, architecture TEXT NOT NULL, cpus INTEGER NOT NULL, memory_total_bytes INTEGER NOT NULL, memory_available_bytes INTEGER NOT NULL, disk_path TEXT NOT NULL, disk_total_bytes INTEGER NOT NULL, disk_available_bytes INTEGER NOT NULL, internet_ping_ms REAL, gateway_ip TEXT NOT NULL, gateway_ping_ms REAL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
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
