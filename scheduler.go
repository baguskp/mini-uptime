package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// durationEnv membaca durasi interval agent dari environment dengan fallback aman.
func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
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
		if alert, err := monitorAlert(db, id, "down", latency, message, now, ""); err == nil {
			go func() {
				if err := telegramAlert(db, alert); err != nil {
					log.Printf("Telegram DOWN alert %d: %v", id, err)
				}
			}()
		} else {
			log.Printf("build down alert %d: %v", id, err)
		}
		if err := execRetry(db, "INSERT OR IGNORE INTO incidents(monitor_id,started_at,error) VALUES(?,?,?)", id, now, message); err != nil {
			log.Printf("open incident %d: %v", id, err)
		}
	}
	if status == "up" && previous == "down" {
		var startedAt string
		if err := db.QueryRow("SELECT started_at FROM incidents WHERE monitor_id=? AND ended_at IS NULL ORDER BY id DESC LIMIT 1", id).Scan(&startedAt); err != nil {
			log.Printf("read incident %d start: %v", id, err)
		}
		if alert, err := monitorAlert(db, id, "recovered", latency, "", now, startedAt); err == nil {
			go func() {
				if err := telegramAlert(db, alert); err != nil {
					log.Printf("Telegram RECOVERED alert %d: %v", id, err)
				}
			}()
		} else {
			log.Printf("build recovered alert %d: %v", id, err)
		}
		if err := execRetry(db, "UPDATE incidents SET ended_at=? WHERE monitor_id=? AND ended_at IS NULL", now, id); err != nil {
			log.Printf("close incident %d: %v", id, err)
		}
	}
	if err := execRetry(db, "UPDATE monitors SET current_status=?,last_latency_ms=?,last_error=?,checked_at=? WHERE id=?", status, latency, message, now, id); err != nil {
		log.Printf("update monitor %d: %v", id, err)
	}
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
