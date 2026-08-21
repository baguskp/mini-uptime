package main

import (
	"database/sql"
	"log"
	"net/http"
)

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
		agentTotal, agentUp, agentDown := agentCounts(db)
		render(w, "dashboard.html", map[string]any{"CSRF": csrfData(w, r)["CSRF"], "Total": total, "Up": up, "Down": down, "Monitors": monitors, "Incidents": incidents, "AgentTotal": agentTotal, "AgentUp": agentUp, "AgentDown": agentDown})
	}
}
