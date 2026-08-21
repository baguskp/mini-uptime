package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed web/templates/* web/static/*
var assets embed.FS

var sessions = struct {
	sync.Mutex
	items map[string]time.Time
}{items: make(map[string]time.Time)}

var displayAttempts = struct {
	sync.Mutex
	items map[string][]time.Time
}{items: make(map[string][]time.Time)}

var appLocation = struct {
	sync.RWMutex
	value *time.Location
}{value: time.UTC}

func main() {
	dbPath := getenv("DATABASE_PATH", "/app/data/miniuptime.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	defer db.Close()
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}
	configureLocation(db)
	go monitorLoop(db)
	go retentionLoop(db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", eventsAccess(db))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /api/agent/health", agentHealth(db))
	staticFS, _ := fs.Sub(assets, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if configured(db) {
			http.Redirect(w, r, "/login", 302)
		} else {
			http.Redirect(w, r, "/setup", 302)
		}
	})
	mux.HandleFunc("GET /setup", setupPage(db))
	mux.Handle("POST /setup", csrf(setupSubmit(db)))
	mux.HandleFunc("GET /login", loginPage(db))
	mux.Handle("POST /login", csrf(loginSubmit(db)))
	mux.Handle("POST /logout", csrf(requireAuth(db, logout(db))))
	mux.HandleFunc("GET /dashboard", requireAuth(db, dashboard(db)))
	mux.HandleFunc("GET /display", display(db))
	mux.Handle("POST /display/unlock", csrf(displayUnlock(db)))
	mux.HandleFunc("GET /groups", requireAuth(db, groupsPage(db)))
	mux.Handle("POST /groups", csrf(requireAuth(db, groupCreate(db))))
	mux.Handle("POST /groups/{id}/monitors", csrf(requireAuth(db, groupAssignMonitors(db))))
	mux.Handle("POST /groups/{id}/delete", csrf(requireAuth(db, groupDelete(db))))
	mux.HandleFunc("GET /monitors", requireAuth(db, monitorsPage(db)))
	mux.HandleFunc("GET /monitors/{id}", requireAuth(db, monitorDetail(db)))
	mux.HandleFunc("GET /agents", requireAuth(db, agentsPage(db)))
	mux.HandleFunc("GET /agents/new", requireAuth(db, agentForm(db)))
	mux.Handle("POST /agents", csrf(requireAuth(db, agentCreate(db))))
	mux.Handle("POST /agents/{id}/delete", csrf(requireAuth(db, agentDelete(db))))
	mux.HandleFunc("GET /agents/{id}", requireAuth(db, agentDetail(db)))
	mux.HandleFunc("GET /incidents", requireAuth(db, incidentsPage(db)))
	mux.HandleFunc("GET /settings", requireAuth(db, settingsPage(db)))
	mux.Handle("POST /settings", csrf(requireAuth(db, settingsSave(db))))
	mux.Handle("POST /settings/test", csrf(requireAuth(db, settingsTest(db))))
	mux.HandleFunc("GET /monitors/new", requireAuth(db, monitorForm(db)))
	mux.HandleFunc("GET /monitors/{id}/edit", requireAuth(db, monitorEditPage(db)))
	mux.Handle("POST /monitors", csrf(requireAuth(db, monitorCreate(db))))
	mux.Handle("POST /monitors/{id}", csrf(requireAuth(db, monitorUpdate(db))))
	mux.Handle("POST /monitors/{id}/group", csrf(requireAuth(db, monitorAssignGroup(db))))
	mux.Handle("POST /monitors/{id}/toggle", csrf(requireAuth(db, monitorToggle(db))))
	mux.Handle("POST /monitors/{id}/delete", csrf(requireAuth(db, monitorDelete(db))))

	server := &http.Server{Addr: ":" + getenv("PORT", "3000"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("MiniUptime listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	ctx, stop := signalContext()
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS admins(id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS groups(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS monitors(id INTEGER PRIMARY KEY, group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL, name TEXT NOT NULL, type TEXT NOT NULL CHECK(type IN ('http','tcp','ping')), target TEXT NOT NULL, interval_seconds INTEGER NOT NULL DEFAULT 60, enabled INTEGER NOT NULL DEFAULT 1, current_status TEXT NOT NULL DEFAULT 'unknown', last_latency_ms INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', checked_at TEXT, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY, admin_id INTEGER NOT NULL, expires_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS checks(id INTEGER PRIMARY KEY, monitor_id INTEGER NOT NULL, status TEXT NOT NULL, latency_ms INTEGER NOT NULL, error TEXT, checked_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS incidents(id INTEGER PRIMARY KEY, monitor_id INTEGER NOT NULL, started_at TEXT NOT NULL, ended_at TEXT, error TEXT NOT NULL); CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS agents(id INTEGER PRIMARY KEY, display_name TEXT NOT NULL DEFAULT '', hostname TEXT UNIQUE NOT NULL, token_hash TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'offline', heartbeat_at TEXT NOT NULL, collected_at TEXT NOT NULL, primary_ip TEXT NOT NULL DEFAULT '', os TEXT NOT NULL, architecture TEXT NOT NULL, cpus INTEGER NOT NULL, memory_total_bytes INTEGER NOT NULL, memory_available_bytes INTEGER NOT NULL, disk_path TEXT NOT NULL DEFAULT '', disk_total_bytes INTEGER NOT NULL, disk_available_bytes INTEGER NOT NULL, internet_ping_ms REAL, gateway_ip TEXT NOT NULL DEFAULT '', gateway_ping_ms REAL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_agents_heartbeat ON agents(heartbeat_at); CREATE INDEX IF NOT EXISTS idx_checks_monitor_checked ON checks(monitor_id,checked_at); CREATE INDEX IF NOT EXISTS idx_incidents_monitor_ended ON incidents(monitor_id,ended_at); CREATE UNIQUE INDEX IF NOT EXISTS idx_incidents_one_open ON incidents(monitor_id) WHERE ended_at IS NULL; CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at); INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (2, CURRENT_TIMESTAMP);`); err != nil {
		return err
	}
	var hasGroup bool
	rows, err := db.Query("PRAGMA table_info(monitors)")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var def any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			return err
		}
		if name == "group_id" {
			hasGroup = true
		}
	}
	if !hasGroup {
		if _, err = db.Exec("ALTER TABLE monitors ADD COLUMN group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL"); err != nil {
			return err
		}
	}
	for _, column := range []string{"current_status TEXT NOT NULL DEFAULT 'unknown'", "last_latency_ms INTEGER NOT NULL DEFAULT 0", "last_error TEXT NOT NULL DEFAULT ''", "checked_at TEXT"} {
		name := strings.Split(column, " ")[0]
		var exists bool
		rows2, _ := db.Query("PRAGMA table_info(monitors)")
		for rows2.Next() {
			var cid int
			var n, t string
			var nn, pk int
			var d any
			_ = rows2.Scan(&cid, &n, &t, &nn, &d, &pk)
			if n == name {
				exists = true
			}
		}
		rows2.Close()
		if !exists {
			if _, err = db.Exec("ALTER TABLE monitors ADD COLUMN " + column); err != nil {
				return err
			}
		}
	}
	var hasAgentDisplayName bool
	agentColumns, err := db.Query("PRAGMA table_info(agents)")
	if err != nil {
		return err
	}
	for agentColumns.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err := agentColumns.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			agentColumns.Close()
			return err
		}
		if name == "display_name" {
			hasAgentDisplayName = true
		}
	}
	agentColumns.Close()
	if !hasAgentDisplayName {
		if _, err := db.Exec("ALTER TABLE agents ADD COLUMN display_name TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if _, err := db.Exec("UPDATE agents SET display_name=hostname WHERE display_name='' AND hostname<>''"); err != nil {
		return err
	}
	return nil
}
