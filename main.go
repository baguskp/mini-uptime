package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/argon2"
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
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS admins(id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS groups(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS monitors(id INTEGER PRIMARY KEY, group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL, name TEXT NOT NULL, type TEXT NOT NULL CHECK(type IN ('http','tcp','ping')), target TEXT NOT NULL, interval_seconds INTEGER NOT NULL DEFAULT 60, enabled INTEGER NOT NULL DEFAULT 1, current_status TEXT NOT NULL DEFAULT 'unknown', last_latency_ms INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', checked_at TEXT, created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY, admin_id INTEGER NOT NULL, expires_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS checks(id INTEGER PRIMARY KEY, monitor_id INTEGER NOT NULL, status TEXT NOT NULL, latency_ms INTEGER NOT NULL, error TEXT, checked_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS incidents(id INTEGER PRIMARY KEY, monitor_id INTEGER NOT NULL, started_at TEXT NOT NULL, ended_at TEXT, error TEXT NOT NULL); CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_checks_monitor_checked ON checks(monitor_id,checked_at); CREATE INDEX IF NOT EXISTS idx_incidents_monitor_ended ON incidents(monitor_id,ended_at); CREATE UNIQUE INDEX IF NOT EXISTS idx_incidents_one_open ON incidents(monitor_id) WHERE ended_at IS NULL; CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at); INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (2, CURRENT_TIMESTAMP);`); err != nil {
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
	return nil
}
func configured(db *sql.DB) bool {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&n); err != nil {
		log.Printf("check configured: %v", err)
		return true
	}
	return n > 0
}
func setupPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if configured(db) {
			http.Redirect(w, r, "/login", 302)
			return
		}
		render(w, "setup.html", csrfData(w, r))
	}
}
func setupSubmit(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if configured(db) {
			http.Error(w, "already configured", 409)
			return
		}
		u, p := strings.TrimSpace(r.FormValue("username")), r.FormValue("password")
		if len(u) < 3 || len(p) < 12 {
			http.Error(w, "username minimum 3 characters; password minimum 12 characters", 400)
			return
		}
		h, _ := hashPassword(p)
		if _, err := db.Exec("INSERT INTO admins(username,password_hash,created_at) VALUES(?,?,?)", u, h, time.Now().UTC().Format(time.RFC3339)); err != nil {
			http.Error(w, "unable to create administrator", 400)
			return
		}
		http.Redirect(w, r, "/login", 303)
	}
}
func loginPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !configured(db) {
			http.Redirect(w, r, "/setup", 302)
			return
		}
		render(w, "login.html", csrfData(w, r))
	}
}
func loginSubmit(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		var h string
		if err := db.QueryRow("SELECT password_hash FROM admins WHERE username=?", username).Scan(&h); err != nil {
			log.Printf("login lookup %q: %v", username, err)
			http.Error(w, "invalid credentials", 401)
			return
		}
		if !checkPassword(password, h) {
			log.Printf("login password mismatch for %q", username)
			http.Error(w, "invalid credentials", 401)
			return
		}
		token := randomToken()
		if _, err := db.Exec("INSERT INTO sessions(token,admin_id,expires_at) SELECT ?,id,? FROM admins WHERE username=?", token, time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339), r.FormValue("username")); err != nil {
			http.Error(w, "unable to create session", 500)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: 86400})
		http.Redirect(w, r, "/dashboard", 303)
	}
}
func requireAuth(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		var expiry string
		if err != nil || db.QueryRow("SELECT expires_at FROM sessions WHERE token=?", cookieValue(c, err)).Scan(&expiry) != nil {
			http.Redirect(w, r, "/login", 302)
			return
		}
		if t, e := time.Parse(time.RFC3339, expiry); e != nil || time.Now().After(t) {
			http.Redirect(w, r, "/login", 302)
			return
		}
		next(w, r)
	}
}
func logout(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err == nil {
			if _, err := db.Exec("DELETE FROM sessions WHERE token=?", c.Value); err != nil {
				log.Printf("logout session: %v", err)
			}
		}
		http.SetCookie(w, &http.Cookie{Name: "session", MaxAge: -1, Path: "/", HttpOnly: true})
		http.Redirect(w, r, "/login", 303)
	}
}
func eventsAccess(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, e := r.Cookie("session"); e == nil {
			var x string
			if db.QueryRow("SELECT token FROM sessions WHERE token=?", c.Value).Scan(&x) == nil {
				events(db)(w, r)
				return
			}
		}
		var mode string
		db.QueryRow("SELECT value FROM settings WHERE key='display_mode'").Scan(&mode)
		if mode == "public" {
			events(db)(w, r)
			return
		}
		if mode == "pin" {
			if c, e := r.Cookie("display_auth"); e == nil && c.Value == "1" {
				events(db)(w, r)
				return
			}
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}
func events(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				rows, err := db.Query("SELECT id,current_status,last_latency_ms FROM monitors ORDER BY id")
				if err != nil {
					continue
				}
				var statuses []map[string]any
				for rows.Next() {
					var id, lat int
					var status string
					if rows.Scan(&id, &status, &lat) == nil {
						statuses = append(statuses, map[string]any{"id": id, "status": status, "latency": lat})
					}
				}
				rows.Close()
				payload, _ := json.Marshal(statuses)
				fmt.Fprintf(w, "event: status\ndata: %s\n\n", payload)
				flusher.Flush()
			}
		}
	}
}
func display(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var mode string
		db.QueryRow("SELECT value FROM settings WHERE key='display_mode'").Scan(&mode)
		if mode == "disabled" || mode == "" {
			http.NotFound(w, r)
			return
		}
		if mode == "pin" {
			c, e := r.Cookie("display_auth")
			if e != nil || c.Value != "1" {
				render(w, "display-pin.html", csrfData(w, r))
				return
			}
		}
		if mode != "public" && mode != "pin" {
			http.NotFound(w, r)
			return
		}
		groupName := strings.TrimSpace(r.URL.Query().Get("group"))
		density := r.URL.Query().Get("density")
		if density != "compact" {
			density = "comfortable"
		}
		rows, _ := db.Query("SELECT m.id,m.name,m.type,m.target,m.current_status,m.last_latency_ms,COALESCE(m.checked_at,''),COALESCE(i.started_at,''),COALESCE((SELECT AVG(c.latency_ms) FROM (SELECT latency_ms FROM checks WHERE monitor_id=m.id ORDER BY id DESC LIMIT 50) c),-1) FROM monitors m LEFT JOIN groups g ON g.id=m.group_id LEFT JOIN incidents i ON i.monitor_id=m.id AND i.ended_at IS NULL WHERE m.enabled=1 AND (?='' OR g.name=?) ORDER BY CASE m.current_status WHEN 'down' THEN 0 WHEN 'unknown' THEN 1 ELSE 2 END,m.name", groupName, groupName)
		if rows == nil {
			http.Error(w, "unable to load monitors", 500)
			return
		}
		defer rows.Close()
		var monitors []map[string]any
		online := 0
		for rows.Next() {
			var id, lat int
			var name, typ, target, status, checkedAt, downSince string
			var average float64
			if rows.Scan(&id, &name, &typ, &target, &status, &lat, &checkedAt, &downSince, &average) == nil {
				averageText := "—"
				if average >= 0 {
					averageText = fmt.Sprintf("%d ms", int(average+0.5))
				}
				if status == "up" {
					online++
				}
				lastChecked := humanTime(checkedAt)
				if checkedAt == "" {
					lastChecked = "Never checked"
				}
				monitors = append(monitors, map[string]any{"ID": id, "Name": name, "Type": typ, "Target": target, "Status": status, "Latency": lat, "Average": averageText, "LastChecked": lastChecked, "DownSince": humanTime(downSince)})
			}
		}
		percentage := 0
		if len(monitors) > 0 {
			percentage = online * 100 / len(monitors)
		}
		comfortableURL := "/display?density=comfortable"
		compactURL := "/display?density=compact"
		if groupName != "" {
			comfortableURL += "&group=" + url.QueryEscape(groupName)
			compactURL += "&group=" + url.QueryEscape(groupName)
		}
		render(w, "display.html", map[string]any{"Monitors": monitors, "Group": groupName, "Online": online, "Total": len(monitors), "Percentage": percentage, "Density": density, "ComfortableURL": comfortableURL, "CompactURL": compactURL})
	}
}
func dashboard(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var total, up, down int
		_ = db.QueryRow("SELECT COUNT(*) FROM monitors").Scan(&total)
		_ = db.QueryRow("SELECT COUNT(*) FROM monitors WHERE current_status='up'").Scan(&up)
		_ = db.QueryRow("SELECT COUNT(*) FROM monitors WHERE current_status='down'").Scan(&down)
		rows, _ := db.Query("SELECT id,name,type,target,current_status,last_latency_ms FROM monitors ORDER BY name")
		if rows == nil {
			http.Error(w, "unable to load monitors", 500)
			return
		}
		defer rows.Close()
		var monitors []map[string]any
		for rows.Next() {
			var id, lat int
			var name, typ, target, status string
			if err := rows.Scan(&id, &name, &typ, &target, &status, &lat); err != nil {
				log.Printf("dashboard monitor scan: %v", err)
				continue
			}
			var checks, success int
			_ = db.QueryRow("SELECT COUNT(*),COALESCE(SUM(status='up'),0) FROM checks WHERE monitor_id=?", id).Scan(&checks, &success)
			uptime := 0
			if checks > 0 {
				uptime = success * 100 / checks
			}
			monitors = append(monitors, map[string]any{"ID": id, "Name": name, "Type": typ, "Target": target, "Status": status, "Latency": lat, "Uptime": uptime})
		}
		ir, _ := db.Query("SELECT m.name,i.started_at,COALESCE(i.ended_at,'') FROM incidents i JOIN monitors m ON m.id=i.monitor_id ORDER BY i.id DESC LIMIT 5")
		defer ir.Close()
		var incidents []map[string]string
		for ir.Next() {
			var name, started, ended string
			if err := ir.Scan(&name, &started, &ended); err != nil {
				log.Printf("dashboard incident scan: %v", err)
				continue
			}
			incidents = append(incidents, map[string]string{"Name": name, "Started": humanTime(started), "Ended": incidentEnded(ended)})
		}
		render(w, "dashboard.html", map[string]any{"CSRF": csrfData(w, r)["CSRF"], "Total": total, "Up": up, "Down": down, "Monitors": monitors, "Incidents": incidents})
	}
}

type group struct {
	ID           int
	Name         string
	Monitors     []groupMonitor
	MonitorCount int
	CSRF         string
}

type groupMonitor struct {
	ID       int
	Name     string
	Target   string
	Selected bool
}

func groupsPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT id,name FROM groups ORDER BY name")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var list []group
		for rows.Next() {
			var g group
			if err := rows.Scan(&g.ID, &g.Name); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			list = append(list, g)
		}
		monitorRows, err := db.Query("SELECT id,name,target,COALESCE(group_id,0) FROM monitors ORDER BY name")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer monitorRows.Close()
		for monitorRows.Next() {
			var m groupMonitor
			var groupID int
			if err := monitorRows.Scan(&m.ID, &m.Name, &m.Target, &groupID); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			for i := range list {
				candidate := m
				candidate.Selected = list[i].ID == groupID
				if candidate.Selected {
					list[i].MonitorCount++
				}
				list[i].Monitors = append(list[i].Monitors, candidate)
			}
		}
		token := csrfData(w, r)["CSRF"]
		for i := range list {
			list[i].CSRF = token
		}
		render(w, "groups.html", map[string]any{"Groups": list, "CSRF": token})
	}
}

func groupAssignMonitors(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || groupID < 1 {
			http.Error(w, "invalid group", 400)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", 400)
			return
		}
		var exists bool
		if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM groups WHERE id=?)", groupID).Scan(&exists); err != nil || !exists {
			http.Error(w, "group not found", 404)
			return
		}
		tx, err := db.Begin()
		if err != nil {
			http.Error(w, "unable to save group monitors", 500)
			return
		}
		defer tx.Rollback()
		if _, err = tx.Exec("UPDATE monitors SET group_id=NULL WHERE group_id=?", groupID); err != nil {
			http.Error(w, "unable to save group monitors", 500)
			return
		}
		for _, rawID := range r.Form["monitor_id"] {
			monitorID, parseErr := strconv.Atoi(rawID)
			if parseErr != nil || monitorID < 1 {
				http.Error(w, "invalid monitor", 400)
				return
			}
			if _, err = tx.Exec("UPDATE monitors SET group_id=? WHERE id=?", groupID, monitorID); err != nil {
				http.Error(w, "unable to save group monitors", 500)
				return
			}
		}
		if err = tx.Commit(); err != nil {
			http.Error(w, "unable to save group monitors", 500)
			return
		}
		http.Redirect(w, r, "/groups", 303)
	}
}
func groupCreate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			http.Error(w, "name required", 400)
			return
		}
		if _, err := db.Exec("INSERT INTO groups(name,created_at) VALUES(?,?)", name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			http.Error(w, "group already exists", 409)
			return
		}
		http.Redirect(w, r, "/groups", 303)
	}
}
func groupDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := db.Exec("DELETE FROM groups WHERE id=?", r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/groups", 303)
	}
}

type monitor struct {
	ID                            int
	GroupID                       int
	GroupName, Name, Type, Target string
	Interval                      int
	Enabled                       bool
	CSRF                          string
}

func monitorsPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		groupName := strings.TrimSpace(r.URL.Query().Get("group"))
		rows, err := db.Query("SELECT m.id,COALESCE(m.group_id,0),COALESCE(g.name,''),m.name,m.type,m.target,m.interval_seconds,m.enabled FROM monitors m LEFT JOIN groups g ON g.id=m.group_id WHERE (m.name LIKE ? OR m.target LIKE ?) AND (?='' OR g.name=?) ORDER BY m.id DESC", "%"+q+"%", "%"+q+"%", groupName, groupName)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var list []monitor
		for rows.Next() {
			var m monitor
			var enabled int
			if err := rows.Scan(&m.ID, &m.GroupID, &m.GroupName, &m.Name, &m.Type, &m.Target, &m.Interval, &enabled); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			m.Enabled = enabled == 1
			list = append(list, m)
		}
		token := csrfData(w, r)["CSRF"]
		groups := []string{}
		gr, _ := db.Query("SELECT name FROM groups ORDER BY name")
		if gr != nil {
			defer gr.Close()
			for gr.Next() {
				var n string
				if gr.Scan(&n) == nil {
					groups = append(groups, n)
				}
			}
		}
		for i := range list {
			list[i].CSRF = token
		}
		render(w, "monitors.html", map[string]any{"Monitors": list, "Query": q, "Group": groupName, "Groups": groups, "CSRF": token})
	}
}
func monitorForm(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups := []group{}
		rows, err := db.Query("SELECT id,name FROM groups ORDER BY name")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var g group
			if err := rows.Scan(&g.ID, &g.Name); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			groups = append(groups, g)
		}
		render(w, "monitor-form.html", map[string]any{"Groups": groups, "CSRF": csrfData(w, r)["CSRF"]})
	}
}
func monitorEditPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var m monitor
		var enabled int
		if err := db.QueryRow("SELECT id,COALESCE(group_id,0),name,type,target,interval_seconds,enabled FROM monitors WHERE id=?", r.PathValue("id")).Scan(&m.ID, &m.GroupID, &m.Name, &m.Type, &m.Target, &m.Interval, &enabled); err != nil {
			http.NotFound(w, r)
			return
		}
		m.Enabled = enabled == 1
		groups := []group{}
		rows, _ := db.Query("SELECT id,name FROM groups ORDER BY name")
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var g group
				if err := rows.Scan(&g.ID, &g.Name); err != nil {
					log.Printf("monitor group scan: %v", err)
					continue
				}
				groups = append(groups, g)
			}
		}
		data := map[string]any{"Monitor": m, "Groups": groups, "CSRF": csrfData(w, r)["CSRF"]}
		render(w, "monitor-edit.html", data)
	}
}
func monitorAssignGroup(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.FormValue("group_id"))
		var err error
		if groupID == "" {
			_, err = db.Exec("UPDATE monitors SET group_id=NULL WHERE id=?", r.PathValue("id"))
		} else {
			_, err = db.Exec("UPDATE monitors SET group_id=? WHERE id=?", groupID, r.PathValue("id"))
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		http.Redirect(w, r, "/monitors", 303)
	}
}
func monitorUpdate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typ := r.FormValue("type")
		name, target := strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("target"))
		interval := 60
		if _, scanErr := fmt.Sscanf(r.FormValue("interval"), "%d", &interval); (typ != "http" && typ != "tcp" && typ != "ping") || name == "" || target == "" || scanErr != nil || interval < 10 {
			http.Error(w, "invalid monitor data", 400)
			return
		}
		groupID := strings.TrimSpace(r.FormValue("group_id"))
		var err error
		if groupID == "" {
			_, err = db.Exec("UPDATE monitors SET name=?,type=?,target=?,interval_seconds=?,group_id=NULL WHERE id=?", name, typ, target, interval, r.PathValue("id"))
		} else {
			_, err = db.Exec("UPDATE monitors SET name=?,type=?,target=?,interval_seconds=?,group_id=? WHERE id=?", name, typ, target, interval, groupID, r.PathValue("id"))
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/monitors", 303)
	}
}
func csrfData(w http.ResponseWriter, r *http.Request) map[string]string {
	token := ""
	if c, err := r.Cookie("csrf"); err == nil {
		token = c.Value
	}
	if token == "" {
		token = randomToken()
		http.SetCookie(w, &http.Cookie{Name: "csrf", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: 3600})
	}
	return map[string]string{"CSRF": token}
}
func checkCSRF(r *http.Request) bool {
	c, err := r.Cookie("csrf")
	return err == nil && c.Value != "" && c.Value == r.FormValue("csrf")
}
func csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !checkCSRF(r) {
			http.Error(w, "invalid csrf token", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
func retentionLoop(db *sql.DB) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		cleanupRetention(db)
		<-ticker.C
	}
}
func execRetry(db *sql.DB, q string, args ...any) error {
	var err error
	for i := 0; i < 4; i++ {
		if _, err = db.Exec(q, args...); err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "locked") {
			return err
		}
		time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
	}
	return err
}
func cleanupRetention(db *sql.DB) {
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec("DELETE FROM checks WHERE checked_at < ?", cutoff); err != nil {
		log.Printf("retention checks: %v", err)
	}
	if _, err := db.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now().UTC().Format(time.RFC3339)); err != nil {
		log.Printf("retention sessions: %v", err)
	}
}
func monitorLoop(db *sql.DB) {
	jobs := make(chan monitorJob, 32)
	for i := 0; i < 4; i++ {
		go func() {
			for job := range jobs {
				runCheck(db, job.id, job.typ, job.target)
			}
		}()
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		rows, err := db.Query("SELECT m.id,m.type,m.target,m.interval_seconds,COALESCE(MAX(c.checked_at),'') FROM monitors m LEFT JOIN checks c ON c.monitor_id=m.id WHERE m.enabled=1 GROUP BY m.id")
		if err != nil {
			continue
		}
		now := currentLocationTime()
		for rows.Next() {
			var id, interval int
			var typ, target, last string
			if rows.Scan(&id, &typ, &target, &interval, &last) != nil {
				continue
			}
			if last == "" {
				jobs <- monitorJob{id, typ, target}
				continue
			}
			checked, e := time.Parse(time.RFC3339, last)
			if e == nil && now.Sub(checked) >= time.Duration(interval)*time.Second {
				jobs <- monitorJob{id, typ, target}
			}
		}
		rows.Close()
	}
}

type monitorJob struct {
	id          int
	typ, target string
}

func runCheck(db *sql.DB, id int, typ, target string) {
	started := currentLocationTime()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = checkTarget(typ, target)
		if err == nil || attempt == 2 {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	status, message := "up", ""
	if err != nil {
		status = "down"
		message = err.Error()
	}
	latency := time.Since(started).Milliseconds()
	now := time.Now().UTC().Format(time.RFC3339)
	var previous string
	if err := db.QueryRow("SELECT current_status FROM monitors WHERE id=?", id).Scan(&previous); err != nil {
		log.Printf("read monitor %d status: %v", id, err)
	}
	if err := execRetry(db, "INSERT INTO checks(monitor_id,status,latency_ms,error,checked_at) VALUES(?,?,?,?,?)", id, status, latency, message, now); err != nil {
		log.Printf("insert check %d: %v", id, err)
	}
	if status == "down" && previous != "down" {
		var monitorName string
		if err := db.QueryRow("SELECT name FROM monitors WHERE id=?", id).Scan(&monitorName); err != nil {
			log.Printf("read monitor %d name: %v", id, err)
		}
		go func() { _ = telegramAlert(db, fmt.Sprintf("MiniUptime DOWN: %s: %s", monitorName, message)) }()
		if err := execRetry(db, "INSERT OR IGNORE INTO incidents(monitor_id,started_at,error) VALUES(?,?,?)", id, now, message); err != nil {
			log.Printf("open incident %d: %v", id, err)
		}
	}
	if status == "up" && previous == "down" {
		var monitorName string
		if err := db.QueryRow("SELECT name FROM monitors WHERE id=?", id).Scan(&monitorName); err != nil {
			log.Printf("read monitor %d name: %v", id, err)
		}
		go func() { _ = telegramAlert(db, fmt.Sprintf("MiniUptime RECOVERED: %s", monitorName)) }()
		if err := execRetry(db, "UPDATE incidents SET ended_at=? WHERE monitor_id=? AND ended_at IS NULL", now, id); err != nil {
			log.Printf("close incident %d: %v", id, err)
		}
	}
	if err := execRetry(db, "UPDATE monitors SET current_status=?,last_latency_ms=?,last_error=?,checked_at=? WHERE id=?", status, latency, message, now, id); err != nil {
		log.Printf("update monitor %d: %v", id, err)
	}
}
func settingsPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var token, chat, mode, timezone string
		db.QueryRow("SELECT value FROM settings WHERE key='telegram_token'").Scan(&token)
		db.QueryRow("SELECT value FROM settings WHERE key='telegram_chat_id'").Scan(&chat)
		db.QueryRow("SELECT value FROM settings WHERE key='display_mode'").Scan(&mode)
		db.QueryRow("SELECT value FROM settings WHERE key='timezone'").Scan(&timezone)
		if mode == "" {
			mode = "disabled"
		}
		if timezone == "" {
			timezone = "UTC"
		}
		render(w, "settings.html", map[string]any{"CSRF": csrfData(w, r)["CSRF"], "TokenSet": token != "", "ChatID": chat, "DisplayMode": mode, "Timezone": timezone, "Timezones": timezoneOptions(), "TelegramSent": r.URL.Query().Get("telegram_sent") == "1", "TelegramError": r.URL.Query().Get("telegram_error")})
	}
}
func settingsSave(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := r.FormValue("display_mode")
		if mode != "" {
			if mode != "disabled" && mode != "public" && mode != "pin" {
				http.Error(w, "invalid display mode", 400)
				return
			}
			db.Exec("INSERT INTO settings(key,value) VALUES('display_mode',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", mode)
			if mode == "pin" && len(r.FormValue("display_pin")) < 4 {
				http.Error(w, "display PIN minimum 4 characters", 400)
				return
			}
		}
		if timezone := strings.TrimSpace(r.FormValue("timezone")); timezone != "" {
			if _, err := time.LoadLocation(timezone); err != nil {
				http.Error(w, "invalid timezone", 400)
				return
			}
			if _, err := db.Exec("INSERT INTO settings(key,value) VALUES('timezone',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", timezone); err != nil {
				http.Error(w, "unable to save timezone", 500)
				return
			}
			setLocation(timezone)
		}
		if pin := strings.TrimSpace(r.FormValue("display_pin")); pin != "" {
			hash, e := hashPassword(pin)
			if e != nil {
				http.Error(w, "unable to save display PIN", 500)
				return
			}
			if _, e = db.Exec("INSERT INTO settings(key,value) VALUES('display_pin',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", hash); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if token := strings.TrimSpace(r.FormValue("token")); token != "" {
			if _, e := db.Exec("INSERT INTO settings(key,value) VALUES('telegram_token',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", token); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		if chat := strings.TrimSpace(r.FormValue("chat_id")); chat != "" {
			if _, e := db.Exec("INSERT INTO settings(key,value) VALUES('telegram_chat_id',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", chat); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		http.Redirect(w, r, "/settings", 303)
	}
}
func displayUnlock(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pin string
		db.QueryRow("SELECT value FROM settings WHERE key='display_pin'").Scan(&pin)
		ip := strings.Split(r.RemoteAddr, ":")[0]
		displayAttempts.Lock()
		now := currentLocationTime()
		recent := displayAttempts.items[ip][:0]
		for _, at := range displayAttempts.items[ip] {
			if now.Sub(at) < time.Minute {
				recent = append(recent, at)
			}
		}
		displayAttempts.items[ip] = recent
		if len(recent) >= 5 {
			displayAttempts.Unlock()
			http.Error(w, "too many display PIN attempts", 429)
			return
		}
		displayAttempts.items[ip] = append(recent, now)
		displayAttempts.Unlock()
		valid := false
		if strings.HasPrefix(pin, "$argon2id$") {
			valid = checkPassword(r.FormValue("pin"), pin)
		} else {
			valid = pin != "" && r.FormValue("pin") == pin
		}
		if !valid {
			http.Error(w, "invalid display PIN", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "display_auth", Value: "1", Path: "/display", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: 86400})
		http.Redirect(w, r, "/display", 303)
	}
}
func settingsTest(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := telegramAlert(db, "MiniUptime test alert"); err != nil {
			http.Redirect(w, r, "/settings?telegram_error="+url.QueryEscape(err.Error()), 303)
			return
		}
		http.Redirect(w, r, "/settings?telegram_sent=1", 303)
	}
}
func telegramAlert(db *sql.DB, message string) error {
	var token, chatID string
	db.QueryRow("SELECT value FROM settings WHERE key='telegram_token'").Scan(&token)
	db.QueryRow("SELECT value FROM settings WHERE key='telegram_chat_id'").Scan(&chatID)
	if token == "" {
		token = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if chatID == "" {
		chatID = os.Getenv("TELEGRAM_CHAT_ID")
	}
	if token == "" || chatID == "" {
		return errors.New("Telegram is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := strings.NewReader(url.Values{"chat_id": {chatID}, "text": {message}}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Telegram returned %s", resp.Status)
	}
	return nil
}
func checkTarget(typ, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch typ {
	case "http":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("HTTP %s", resp.Status)
			}
		}
		return err
	case "tcp":
		conn, err := net.DialTimeout("tcp", target, 10*time.Second)
		if err == nil {
			conn.Close()
		}
		return err
	case "ping":
		return exec.CommandContext(ctx, "ping", "-c", "1", "-W", "5", target).Run()
	}
	return fmt.Errorf("unknown monitor type")
}
func incidentsPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT i.started_at,COALESCE(i.ended_at,''),m.name,i.error FROM incidents i JOIN monitors m ON m.id=i.monitor_id ORDER BY i.id DESC LIMIT 100")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var incidents []map[string]string
		for rows.Next() {
			var started, ended, name, e string
			if err := rows.Scan(&started, &ended, &name, &e); err != nil {
				log.Printf("incident scan: %v", err)
				continue
			}
			incidents = append(incidents, map[string]string{"Started": humanTime(started), "Ended": incidentEnded(ended), "Monitor": name, "Error": e})
		}
		render(w, "incidents.html", incidents)
	}
}
func monitorDetail(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var m monitor
		var enabled int
		if err := db.QueryRow("SELECT id,COALESCE(group_id,0),name,type,target,interval_seconds,enabled FROM monitors WHERE id=?", r.PathValue("id")).Scan(&m.ID, &m.GroupID, &m.Name, &m.Type, &m.Target, &m.Interval, &enabled); err != nil {
			http.NotFound(w, r)
			return
		}
		rows, err := db.Query("SELECT status,latency_ms,COALESCE(error,''),checked_at FROM checks WHERE monitor_id=? ORDER BY id DESC LIMIT 50", m.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var checks []map[string]any
		maxLatency := 1
		for rows.Next() {
			var s, e, at string
			var l int
			if err := rows.Scan(&s, &l, &e, &at); err != nil {
				log.Printf("check scan: %v", err)
				continue
			}
			if l > maxLatency {
				maxLatency = l
			}
			checks = append(checks, map[string]any{"Status": s, "Latency": l, "Error": e, "At": at})
		}
		sum := 0
		minLatency, maxLatency := int(^uint(0)>>1), 0
		for _, c := range checks {
			l := c["Latency"].(int)
			sum += l
			if l < minLatency {
				minLatency = l
			}
			if l > maxLatency {
				maxLatency = l
			}
		}
		if len(checks) == 0 {
			minLatency = 0
		}
		avg := 0
		if len(checks) > 0 {
			avg = sum / len(checks)
		}
		p95 := 0
		if len(checks) > 0 {
			vals := make([]int, 0, len(checks))
			for _, c := range checks {
				vals = append(vals, c["Latency"].(int))
			}
			sort.Ints(vals)
			p95 = vals[(len(vals)*95+99)/100-1]
		}
		for _, c := range checks {
			l := c["Latency"].(int)
			h := 8
			if maxLatency > 0 {
				h = max(8, l*100/maxLatency)
			}
			c["Height"] = h
			c["Time"] = humanTime(c["At"].(string))
		}
		render(w, "monitor-detail.html", map[string]any{"Monitor": m, "Checks": checks, "Avg": avg, "P95": p95, "Min": minLatency, "Max": maxLatency})
	}
}
func humanTime(value string) string {
	t, e := time.Parse(time.RFC3339, value)
	if e != nil {
		return value
	}
	return t.In(currentLocation()).Format("02 Jan, 15:04")
}
func timezoneOptions() []string {
	return []string{"UTC", "Asia/Jakarta", "Asia/Singapore", "Asia/Tokyo", "Australia/Sydney", "Europe/London", "Europe/Amsterdam", "America/New_York", "America/Los_Angeles"}
}
func configureLocation(db *sql.DB) {
	var timezone string
	if db.QueryRow("SELECT value FROM settings WHERE key='timezone'").Scan(&timezone) == nil && timezone != "" {
		setLocation(timezone)
	}
}
func setLocation(timezone string) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return
	}
	appLocation.Lock()
	appLocation.value = location
	appLocation.Unlock()
}
func currentLocation() *time.Location {
	appLocation.RLock()
	defer appLocation.RUnlock()
	return appLocation.value
}
func currentLocationTime() time.Time {
	return time.Now().In(currentLocation())
}
func incidentEnded(value string) string {
	if value == "" {
		return "Ongoing"
	}
	return humanTime(value)
}
func validMonitor(typ, name, target string, interval int) bool {
	target = strings.TrimSpace(target)
	if typ == "http" {
		u, e := url.ParseRequestURI(target)
		if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
			return false
		}
	} else if typ == "tcp" {
		if _, _, e := net.SplitHostPort(target); e != nil {
			return false
		}
	} else if typ == "ping" {
		if strings.Contains(target, "://") || target == "" {
			return false
		}
	} else {
		return false
	}
	return strings.TrimSpace(name) != "" && interval >= 10
}
func monitorCreate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typ := r.FormValue("type")
		if typ != "http" && typ != "tcp" && typ != "ping" {
			http.Error(w, "invalid monitor type", 400)
			return
		}
		name, target := strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("target"))
		if name == "" || target == "" {
			http.Error(w, "name and target required", 400)
			return
		}
		interval := 60
		if _, err := fmt.Sscanf(r.FormValue("interval"), "%d", &interval); err != nil || !validMonitor(typ, name, target, interval) {
			http.Error(w, "interval must be at least 10 seconds", 400)
			return
		}
		var groupID any
		if rawGroupID := strings.TrimSpace(r.FormValue("group_id")); rawGroupID != "" {
			parsedGroupID, parseErr := strconv.Atoi(rawGroupID)
			if parseErr != nil || parsedGroupID < 1 {
				http.Error(w, "invalid group", 400)
				return
			}
			var exists bool
			if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM groups WHERE id=?)", parsedGroupID).Scan(&exists); err != nil || !exists {
				http.Error(w, "group not found", 404)
				return
			}
			groupID = parsedGroupID
		}
		_, err := db.Exec("INSERT INTO monitors(name,type,target,interval_seconds,group_id,created_at) VALUES(?,?,?,?,?,?)", name, typ, target, interval, groupID, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/monitors", 303)
	}
}
func monitorToggle(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := db.Exec("UPDATE monitors SET enabled=CASE enabled WHEN 1 THEN 0 ELSE 1 END WHERE id=?", r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/monitors", 303)
	}
}
func monitorDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := db.Exec("DELETE FROM monitors WHERE id=?", r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		http.Redirect(w, r, "/monitors", 303)
	}
}
func render(w http.ResponseWriter, name string, data any) {
	t, err := template.ParseFS(assets, "web/templates/"+name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := t.Execute(w, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}
func hashPassword(p string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%x$%x", salt, argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)), nil
}
func checkPassword(p, h string) bool {
	var salt, expected []byte
	if _, err := fmt.Sscanf(h, "$argon2id$v=19$m=65536,t=3,p=2$%x$%x", &salt, &expected); err != nil {
		return false
	}
	got := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return string(got) == string(expected)
}
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("random token: %v", err)
	}
	return fmt.Sprintf("%x", b)
}
func cookieValue(c *http.Cookie, err error) string {
	if err != nil {
		return ""
	}
	return c.Value
}
func getenv(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
