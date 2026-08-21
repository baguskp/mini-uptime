package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

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
		graphCaption := "No checks yet"
		if len(checks) == 1 {
			graphCaption = checks[0]["Time"].(string) + " · last 1 check"
		} else if len(checks) > 1 {
			oldest := checks[len(checks)-1]["Time"].(string)
			newest := checks[0]["Time"].(string)
			graphCaption = oldest + " – " + newest + " · last 50 checks"
		}
		render(w, "monitor-detail.html", map[string]any{"Monitor": m, "Checks": checks, "Avg": avg, "P95": p95, "Min": minLatency, "Max": maxLatency, "GraphCaption": graphCaption})
	}
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
