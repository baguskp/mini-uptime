package main

import (
	"database/sql"
	"log"
	"net/http"
)

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
		render(w, "incidents.html", map[string]any{"Incidents": incidents})
	}
}
