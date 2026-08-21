package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type agentHealthPayload struct {
	Hostname     string           `json:"hostname"`
	PrimaryIP    string           `json:"primary_ip"`
	Interfaces   []agentInterface `json:"interfaces"`
	OS           string           `json:"os"`
	Architecture string           `json:"architecture"`
	CPUs         int              `json:"cpus"`
	Memory       agentMemory      `json:"memory"`
	Disk         agentDisk        `json:"disk"`
	InternetPing *float64         `json:"internet_ping_ms"`
	GatewayIP    string           `json:"gateway_ip"`
	GatewayPing  *float64         `json:"gateway_ping_ms"`
	CollectedAt  string           `json:"collected_at"`
}

type agentInterface struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	IP     string `json:"ip"`
	Status string `json:"status"`
}

type agentMemory struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type agentDisk struct {
	Path           string `json:"path"`
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

// agentHealth memvalidasi heartbeat agent dan menyimpan snapshot health terakhirnya.
func agentHealth(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		providedHash := sha256.Sum256([]byte(token))
		var registeredID int
		tokenHash := fmt.Sprintf("%x", providedHash)
		registered := token != "" && db.QueryRow("SELECT id FROM agents WHERE token_hash=?", tokenHash).Scan(&registeredID) == nil
		expected := strings.TrimSpace(os.Getenv("AGENT_INGEST_TOKEN"))
		globalHash := sha256.Sum256([]byte(expected))
		global := expected != "" && token != "" && subtle.ConstantTimeCompare(globalHash[:], providedHash[:]) == 1
		if !registered && !global {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var payload agentHealthPayload
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil || !validAgentHealth(payload) {
			http.Error(w, "invalid health payload", http.StatusUnprocessableEntity)
			return
		}

		now := time.Now().UTC()
		values := []any{payload.Hostname, tokenHash, "online", now.Format(time.RFC3339), payload.CollectedAt, payload.PrimaryIP, payload.OS, payload.Architecture, payload.CPUs, payload.Memory.TotalBytes, payload.Memory.AvailableBytes, payload.Disk.Path, payload.Disk.TotalBytes, payload.Disk.AvailableBytes, payload.InternetPing, payload.GatewayIP, payload.GatewayPing, now.Format(time.RFC3339), now.Format(time.RFC3339)}
		var err error
		if registered {
			// Token registration owns the row; hostname dari payload boleh berbeda dari label awal.
			_, _ = db.Exec("DELETE FROM agents WHERE hostname=? AND id<>? AND token_hash=?", payload.Hostname, registeredID, tokenHash)
			updateValues := append([]any{}, values[:17]...)
			updateValues = append(updateValues, values[18], registeredID)
			_, err = db.Exec(`UPDATE agents SET hostname=?,token_hash=?,status=?,heartbeat_at=?,collected_at=?,primary_ip=?,os=?,architecture=?,cpus=?,memory_total_bytes=?,memory_available_bytes=?,disk_path=?,disk_total_bytes=?,disk_available_bytes=?,internet_ping_ms=?,gateway_ip=?,gateway_ping_ms=?,updated_at=? WHERE id=?`, updateValues...)
		} else {
			_, err = db.Exec(`INSERT INTO agents(hostname,token_hash,status,heartbeat_at,collected_at,primary_ip,os,architecture,cpus,memory_total_bytes,memory_available_bytes,disk_path,disk_total_bytes,disk_available_bytes,internet_ping_ms,gateway_ip,gateway_ping_ms,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(hostname) DO UPDATE SET token_hash=excluded.token_hash,status=excluded.status,heartbeat_at=excluded.heartbeat_at,collected_at=excluded.collected_at,primary_ip=excluded.primary_ip,os=excluded.os,architecture=excluded.architecture,cpus=excluded.cpus,memory_total_bytes=excluded.memory_total_bytes,memory_available_bytes=excluded.memory_available_bytes,disk_path=excluded.disk_path,disk_total_bytes=excluded.disk_total_bytes,disk_available_bytes=excluded.disk_available_bytes,internet_ping_ms=excluded.internet_ping_ms,gateway_ip=excluded.gateway_ip,gateway_ping_ms=excluded.gateway_ping_ms,updated_at=excluded.updated_at`, values...)
		}
		if err != nil {
			http.Error(w, "unable to store health", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}
}

type agentView struct {
	ID              int
	DisplayName     string
	Hostname        string
	Status          string
	Online          bool
	Heartbeat       string
	Collected       string
	PrimaryIP       string
	OS              string
	Architecture    string
	CPUs            int
	MemoryTotal     string
	MemoryAvailable string
	DiskPath        string
	DiskTotal       string
	DiskAvailable   string
	InternetPing    string
	GatewayIP       string
	GatewayPing     string
}

// agentsPage menampilkan ringkasan status seluruh PC yang terdaftar.
func agentsPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT id,display_name,hostname,status,heartbeat_at,collected_at,primary_ip,os,architecture,cpus,memory_total_bytes,memory_available_bytes,disk_path,disk_total_bytes,disk_available_bytes,internet_ping_ms,gateway_ip,gateway_ping_ms FROM agents ORDER BY display_name,hostname")
		if err != nil {
			http.Error(w, "unable to load agents", 500)
			return
		}
		defer rows.Close()
		list := []agentView{}
		for rows.Next() {
			view, err := scanAgent(rows, time.Now().UTC())
			if err != nil {
				http.Error(w, "unable to read agents", 500)
				return
			}
			list = append(list, view)
		}
		total, online, offline := agentCounts(db)
		render(w, "agents.html", map[string]any{"Agents": list, "CSRF": csrfData(w, r)["CSRF"], "Total": total, "Online": online, "Offline": offline})
	}
}

// agentDetail menampilkan metrik terakhir yang dikirim oleh satu agent.
func agentDetail(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id < 1 {
			http.Error(w, "invalid agent", 400)
			return
		}
		row := db.QueryRow("SELECT id,display_name,hostname,status,heartbeat_at,collected_at,primary_ip,os,architecture,cpus,memory_total_bytes,memory_available_bytes,disk_path,disk_total_bytes,disk_available_bytes,internet_ping_ms,gateway_ip,gateway_ping_ms FROM agents WHERE id=?", id)
		view, err := scanAgent(row, time.Now().UTC())
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "unable to read agent", 500)
			return
		}
		render(w, "agent-detail.html", map[string]any{"Agent": view, "CSRF": csrfData(w, r)["CSRF"]})
	}
}

// agentForm menampilkan formulir pendaftaran dan instruksi pemasangan agent.
func agentForm(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, "agent-form.html", map[string]any{"CSRF": csrfData(w, r)["CSRF"]})
	}
}

// agentCreate mendaftarkan nama tampilan dan menghasilkan token yang hanya ditampilkan sekali.
func agentCreate(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		displayName := strings.TrimSpace(r.FormValue("display_name"))
		if displayName == "" || len(displayName) > 255 {
			http.Error(w, "agent name required and must be 255 characters or fewer", 400)
			return
		}
		token := randomToken()
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := db.Exec("INSERT INTO agents(display_name,hostname,token_hash,status,heartbeat_at,collected_at,primary_ip,os,architecture,cpus,memory_total_bytes,memory_available_bytes,disk_path,disk_total_bytes,disk_available_bytes,gateway_ip,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", displayName, displayName, fmt.Sprintf("%x", sha256.Sum256([]byte(token))), "offline", "", "", "", "", "", 0, 0, 0, "", 0, 0, "", now, now)
		if err != nil {
			http.Error(w, "agent hostname already exists", 409)
			return
		}
		render(w, "agent-form.html", map[string]any{"CSRF": csrfData(w, r)["CSRF"], "Token": token, "DisplayName": displayName})
	}
}

// agentDelete mencabut token dan menghapus agent dari daftar MiniUptime.
func agentDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id < 1 {
			http.Error(w, "invalid agent", 400)
			return
		}
		if _, err := db.Exec("DELETE FROM agents WHERE id=?", id); err != nil {
			http.Error(w, "unable to remove agent", 500)
			return
		}
		http.Redirect(w, r, "/agents", http.StatusSeeOther)
	}
}

type agentScanner interface {
	Scan(dest ...any) error
}

func scanAgent(scanner agentScanner, now time.Time) (agentView, error) {
	var view agentView
	var heartbeat, collected string
	var memoryTotal, memoryAvailable, diskTotal, diskAvailable uint64
	var internetPing, gatewayPing sql.NullFloat64
	if err := scanner.Scan(&view.ID, &view.DisplayName, &view.Hostname, &view.Status, &heartbeat, &collected, &view.PrimaryIP, &view.OS, &view.Architecture, &view.CPUs, &memoryTotal, &memoryAvailable, &view.DiskPath, &diskTotal, &diskAvailable, &internetPing, &view.GatewayIP, &gatewayPing); err != nil {
		return view, err
	}
	view.Heartbeat, view.Collected = humanAgentTime(heartbeat), humanAgentTime(collected)
	if view.DisplayName == "" {
		view.DisplayName = view.Hostname
	}
	view.Online = heartbeat != "" && agentOnline(heartbeat, now)
	if !view.Online {
		view.Status = "offline"
	}
	view.MemoryTotal, view.MemoryAvailable = formatBytes(memoryTotal), formatBytes(memoryAvailable)
	view.DiskTotal, view.DiskAvailable = formatBytes(diskTotal), formatBytes(diskAvailable)
	view.InternetPing, view.GatewayPing = formatAgentPing(internetPing), formatAgentPing(gatewayPing)
	return view, nil
}

func agentCounts(db *sql.DB) (int, int, int) {
	rows, err := db.Query("SELECT heartbeat_at FROM agents")
	if err != nil {
		return 0, 0, 0
	}
	defer rows.Close()
	total, online := 0, 0
	now := time.Now().UTC()
	for rows.Next() {
		var heartbeat string
		if rows.Scan(&heartbeat) != nil {
			continue
		}
		total++
		if heartbeat != "" && agentOnline(heartbeat, now) {
			online++
		}
	}
	return total, online, total - online
}

func humanAgentTime(value string) string {
	if value == "" {
		return "Never"
	}
	return humanTime(value)
}

func formatAgentPing(value sql.NullFloat64) string {
	if !value.Valid {
		return "ÔÇö"
	}
	return strconv.FormatFloat(value.Float64, 'f', 1, 64) + " ms"
}

func formatBytes(value uint64) string {
	if value == 0 {
		return "ÔÇö"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	amount := float64(value)
	unit := 0
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", amount, units[unit])
	}
	return fmt.Sprintf("%.1f %s", amount, units[unit])
}

// validAgentHealth memastikan data minimum tersedia dan timestamp dapat dipakai sebagai waktu UTC.
func validAgentHealth(payload agentHealthPayload) bool {
	if strings.TrimSpace(payload.Hostname) == "" || len(payload.Hostname) > 255 || strings.TrimSpace(payload.OS) == "" || strings.TrimSpace(payload.Architecture) == "" || payload.CPUs < 1 || payload.Memory.TotalBytes == 0 || payload.Disk.TotalBytes == 0 {
		return false
	}
	collected, err := time.Parse(time.RFC3339, payload.CollectedAt)
	if err != nil || collected.IsZero() {
		return false
	}
	return payload.Memory.AvailableBytes <= payload.Memory.TotalBytes && payload.Disk.AvailableBytes <= payload.Disk.TotalBytes
}

// agentOnline menentukan status berdasarkan tiga kali interval heartbeat yang dikonfigurasi.
func agentOnline(heartbeatAt string, now time.Time) bool {
	interval := durationEnv("AGENT_HEARTBEAT_INTERVAL", time.Minute)
	heartbeat, err := time.Parse(time.RFC3339, heartbeatAt)
	return err == nil && now.Sub(heartbeat) <= 3*interval
}
