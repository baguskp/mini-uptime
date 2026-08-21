package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
