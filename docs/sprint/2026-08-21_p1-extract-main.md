---
timestamp: 2026-08-21 21:00 +07:00
commit: pending
topic: Extract main.go into focused files (P1)
priority: P1
---

# Sprint — Extract main.go into focused files

Executor: DeepSeek Flash (mechanical refactor, no behavior change).

## Goal

Pecah `main.go` (1784 baris) menjadi beberapa file dalam package yang sama (`package main`) tanpa mengubah satu pun perilaku. Ini mengurangi bug-surface dan membuat fitur berikutnya (lihat sprint template-nav) lebih mudah di-review.

## Evidence

- `main.go` berisi semua handler, scheduler, migrasi, helpers, dan renderer dalam satu file.
- `render()` + `normalizeNavbar()` melakukan string surgery (inject Agents card, patch nav) yang rawan — akan dihapus di sprint terpisah `2026-08-21_p2-template-nav.md`. Splitting file memudahkan review sprint itu.

## Rules (wajib)

1. Semua file tetap `package main`, satu direktori root. Tidak ada sub-package.
2. **Pindahkan fungsi/tipe/var apa adanya** — jangan rename, jangan reorder logika, jangan refactor, jangan ubah komentar yang tidak perlu. Diff harus "code moved", bukan "code changed".
3. Imports ikut pindah ke file tujuan. Jangan menambahkan dependensi baru.
4. `//go:embed web/templates/* web/static/*`, `var assets embed.FS`, dan semua global var (`sessions`, `displayAttempts`, `appLocation`) **tetap di `main.go`**.
5. `func main()`, `migrate()`, dan seluruh wiring route (blok `mux.HandleFunc(...)`) **tetap di `main.go`**.
6. `main_test.go` (package main) tidak boleh diubah dan harus tetap lulus.

## File mapping (target)

| File | Isi (fungsi/tipe, pindah utuh) |
|---|---|
| `main.go` | embed, global vars, `main()`, `migrate()` |
| `auth.go` | `configured`, `setupPage`, `setupSubmit`, `loginPage`, `loginSubmit`, `requireAuth`, `logout`, `csrfData`, `checkCSRF`, `csrf`, `hashPassword`, `checkPassword`, `randomToken`, `cookieValue` |
| `agents.go` | `agentHealthPayload`, `agentInterface`, `agentMemory`, `agentDisk`, `agentHealth`, `agentView`, `agentsPage`, `agentDetail`, `agentForm`, `agentCreate`, `agentDelete`, `agentScanner`, `scanAgent`, `agentCounts`, `humanAgentTime`, `formatAgentPing`, `formatBytes`, `validAgentHealth`, `agentOnline` |
| `monitors.go` | `monitor`, `monitorsPage`, `monitorForm`, `monitorEditPage`, `monitorAssignGroup`, `monitorUpdate`, `monitorCreate`, `monitorToggle`, `monitorDelete`, `monitorDetail`, `validMonitor` |
| `groups.go` | `group`, `groupMonitor`, `groupsPage`, `groupAssignMonitors`, `groupCreate`, `groupDelete` |
| `scheduler.go` | `monitorLoop`, `monitorJob`, `runCheck`, `checkTarget`, `retentionLoop`, `execRetry`, `cleanupRetention`, `durationEnv` |
| `display.go` | `eventsAccess`, `events`, `display`, `displayUnlock` |
| `dashboard.go` | `dashboard` |
| `settings.go` | `settingsPage`, `settingsSave`, `settingsTest`, `timezoneOptions`, `configureLocation`, `setLocation`, `currentLocation`, `currentLocationTime`, `incidentEnded` |
| `incidents.go` | `incidentsPage` |
| `telegram.go` | `monitorAlertData`, `monitorAlert`, `formatMonitorAlert`, `sanitizeAlertTarget`, `humanDuration`, `telegramAlert` |
| `render.go` | `render`, `normalizeNavbar`, `humanTime` |
| `util.go` | `getenv`, `signalContext` |

> Jika satu file hanya berisi 1 fungsi (mis. `dashboard.go`, `incidents.go`), boleh digabung ke file terdekat yang logis (mis. `dashboard.go` ke `monitors.go`, `incidents.go` ke `monitors.go`). Yang penting tidak ada satu file raksasa tersisa. Catat keputusan penggabungan di commit message.

## Out of scope

- Menghapus `normalizeNavbar` / string surgery (sprint terpisah P2).
- Mengubah skema DB, route, template, CSS, atau logika apa pun.
- Menambahkan interface/abstraction baru.

## Output / Acceptance

- [ ] `main.go` tersisa ≤ ~200 baris (hanya embed + main + migrate + routes).
- [ ] `go build -o tmp/uptime.exe .` sukses.
- [ ] `go vet ./...` sukses tanpa warning baru.
- [ ] `go test ./... -race` lulus (tanpa perubahan `main_test.go`).
- [ ] `git diff --stat` menunjukkan dominasi rename/move, bukan perubahan konten.

## Verification

```sh
go build -o tmp/uptime.exe .
go vet ./...
go test ./... -race
```

Perbandingan perilaku: sebelum & sesudah, `DATABASE_PATH=%TEMP%\miniuptime.db PORT=3210` — buka `/health` (200), `/login` (200), setup admin, buat 1 monitor, pastikan `/monitors` & `/display` render identik secara visual.
