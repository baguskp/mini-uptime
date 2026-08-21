package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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
				rows, err := db.Query("SELECT id,current_status,last_latency_ms,COALESCE(checked_at,'') FROM monitors ORDER BY id")
				if err != nil {
					continue
				}
				var statuses []map[string]any
				for rows.Next() {
					var id, lat int
					var status, checkedAt string
					if rows.Scan(&id, &status, &lat, &checkedAt) == nil {
						checked := "Never checked"
						if checkedAt != "" {
							checked = humanTime(checkedAt)
						}
						statuses = append(statuses, map[string]any{"id": id, "status": status, "latency": lat, "checked": checked})
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
