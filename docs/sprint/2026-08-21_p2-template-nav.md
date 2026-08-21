---
timestamp: 2026-08-21 21:05 +07:00
commit: pending
topic: Template-first navigation — remove normalizeNavbar (P2)
priority: P2
---

# Sprint — Template-first navigation (remove normalizeNavbar)

Executor: DeepSeek Flash.

## Goal

Hapus `normalizeNavbar()` (string surgery pada HTML hasil render) dan inject Agents-card via `strings.Replace` di `render()`. Ganti dengan render template murni: nav di-render oleh template itu sendiri, active state lewat field `Active`, dan Agents card jadi bagian `dashboard.html`.

## Evidence

- `render()` memanggil `normalizeNavbar(name, output.String())` yang mencari `<nav>`/`</nav>` dengan `strings.Index` lalu menulis ulang seluruh nav. Ini fragile dan sumber bug "gotcha #2".
- `render()` juga menyuntik Agents card ke dashboard via `strings.Replace(page, `<div class="grid metrics-grid">`, ...)`.
- Dashboard & beberapa template nav-nya masih **belum** memuat link `Agents` — selama ini ditambal oleh `normalizeNavbar`. Fakta terkini (per 2026-08-21):

| Nav tanpa link Agents (perlu ditambal) | Nav sudah punya Agents |
|---|---|
| `dashboard.html`, `groups.html`, `incidents.html`, `monitor-detail.html`, `monitor-edit.html`, `monitor-form.html`, `monitors.html`, `settings.html` | `agent-detail.html`, `agent-form.html`, `agents.html` |

## Langkah (urutan wajib)

### 1. Samakan nav di semua template

Untuk 8 template di kolom kiri tabel di atas, tambahkan link Agents ke dalam `<nav>` **di posisi setelah Monitors dan sebelum Groups**:

```
<a href="/agents">Agents</a>
```

Urutan final nav harus identik di semua template:
`Dashboard, Monitors, Agents, Groups, Incidents, Settings, Display`.

- Hanya `dashboard.html` yang punya `<form method="post" action="/logout">…</form>` di dalam nav. Biarkan seperti itu; jangan tambahkan logout ke template lain (perilaku lama tidak mengubah ini).

### 2. Tambah active state ke nav tiap template

Setiap link nav diberi kondisi `aria-current="page"` berdasarkan field `Active`:

| Template | Link yang aktif (`/href`) |
|---|---|
| `dashboard.html` | `/dashboard` |
| `monitors.html`, `monitor-form.html`, `monitor-edit.html`, `monitor-detail.html` | `/monitors` |
| `agents.html`, `agent-form.html`, `agent-detail.html` | `/agents` |
| `groups.html` | `/groups` |
| `incidents.html` | `/incidents` |
| `settings.html` | `/settings` |

Pola per link (contoh untuk Monitors di `monitors.html`):

```
<a href="/monitors"{{if eq .Active "/monitors"}} aria-current="page"{{end}}>Monitors</a>
```

`display.html` dan `display-pin.html` tidak punya `<nav>` — lewati.

### 3. `render()`: inject `Active`, hapus string surgery

Ganti isi `render()`:

```go
func render(w http.ResponseWriter, name string, data any) {
	t, err := template.ParseFS(assets, "web/templates/"+name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if values, ok := data.(map[string]any); ok {
		values["Active"] = navActive(name)
	}
	var output bytes.Buffer
	if err := t.Execute(&output, data); err != nil {
		log.Printf("render %s: %v", name, err)
		return
	}
	_, _ = io.WriteString(w, output.String())
}
```

- Hapus `normalizeNavbar` seluruhnya.
- Pindahkan logika pemetaan `name → href` yang ada di `normalizeNavbar` (switch) ke helper baru `navActive(name string) string` di `render.go`. Mapping TIDAK berubah:
  - `dashboard.html` → `/dashboard`
  - `monitors.html`, `monitor-form.html`, `monitor-edit.html`, `monitor-detail.html` → `/monitors`
  - `agents.html`, `agent-form.html`, `agent-detail.html` → `/agents`
  - `groups.html` → `/groups`
  - `incidents.html` → `/incidents`
  - `settings.html` → `/settings`
  - `display.html`, `display-pin.html` → `/display`
  - `login.html`, `setup.html` → `""`

### 4. Pindahkan Agents card ke `dashboard.html`

- Hapus blok `if name == "dashboard.html" { ... strings.Replace(...) }` dari `render()`.
- Di `dashboard.html`, jadikan Agents card anak pertama dari `<div class="grid metrics-grid">`:

```
<div class="card metric metric-agents"><span class="muted">Agents</span><h2>{{.AgentTotal}}</h2><span class="metric-sub muted">{{.AgentUp}} online · {{.AgentDown}} offline</span></div>
```

`dashboard()` sudah mengirim `AgentTotal/AgentUp/AgentDown` — tidak ada perubahan handler.

### 5. `incidentsPage`: data jadi map

`render()` hanya bisa inject `Active` ke `map[string]any`. Ubah `incidentsPage`:

- `render(w, "incidents.html", incidents)` → `render(w, "incidents.html", map[string]any{"Incidents": incidents})`
- Di `incidents.html`: `{{range .}}` → `{{range .Incidents}}`.

### 6. Perbarui test

- Hapus `TestNormalizeNavbarUsesOneMenuOrder` dan `TestNormalizeNavbarMarksActivePage` (fungsi sudah tidak ada).
- Ganti `TestRenderAddsAgentNavigationToLegacyPages`:
  - `render(w, "incidents.html", map[string]any{"Incidents": []map[string]string{}})`.
  - Assert body mengandung `href="/agents">Agents</a>` (nav utuh) **dan** `href="/incidents" aria-current="page"`.
- Tambah `TestRenderInjectsActiveState`: `render(w, "monitors.html", map[string]any{})`, assert mengandung `href="/monitors" aria-current="page"`.
- Jangan ubah test lain.

## Out of scope

- Menambah link logout ke halaman selain dashboard.
- Mengubah route/handler/schema.
- Migrasi ke `html/template` base layout + `{{define}}` partial (boleh nanti; di sini cukup per-template nav inline yang seragam).

## Output / Acceptance

- [ ] `grep -r "normalizeNavbar" .` → kosong.
- [ ] `render()` tidak lagi memanggil `strings.Replace` / `strings.Index` pada HTML.
- [ ] 11 template bernav punya urutan link identik: Dashboard, Monitors, Agents, Groups, Incidents, Settings, Display.
- [ ] Setiap halaman render `aria-current="page"` pada link yang benar (dashboard→Dashboard, monitor-form/edit/detail→Monitors, dst).
- [ ] Dashboard menampilkan Agents card (metric) dengan angka benar.
- [ ] `go test ./... -race` lulus.

## Verification

1. `go test ./... -race`.
2. Jalankan lokal (`DATABASE_PATH=%TEMP%\miniuptime.db PORT=3210`), login, buka `/dashboard`, `/monitors`, `/monitors/1`, `/agents`, `/groups`, `/incidents`, `/settings` — cek `aria-current` di DOM (screenshot/inspect) dan Agents card di dashboard.
3. Pastikan `/display` dan `/login` tidak berubah.
