# MiniUptime

> Lightweight, fast, self-hosted infrastructure monitoring built with Go.

## 1. Project Overview

MiniUptime adalah aplikasi self-hosted monitoring yang ringan untuk memantau availability dan response time dari server, aplikasi, perangkat jaringan, gateway internet, database, API, dan service internal.

MiniUptime bukan clone penuh Uptime Kuma.

Tujuan proyek ini adalah menyediakan fungsi monitoring yang paling penting dengan:

- resource usage rendah,
- dependency minimal,
- startup cepat,
- UI responsif,
- deployment sederhana,
- maintenance mudah,
- satu container,
- satu binary Go,
- satu database SQLite.

Prinsip utama proyek:

> **Lightweight is the primary design mindset.**

Setiap fitur, library, dependency, dan keputusan arsitektur harus dipertanyakan berdasarkan prinsip tersebut.

Jika sebuah fitur dapat dibuat dengan pendekatan yang lebih sederhana tanpa kehilangan fungsi penting, gunakan pendekatan yang lebih sederhana.

---

# 2. Primary Goals

MiniUptime harus:

1. Mudah di-deploy menggunakan Docker.
2. Tidak membutuhkan PostgreSQL.
3. Tidak membutuhkan MySQL.
4. Tidak membutuhkan Redis.
5. Tidak membutuhkan Node.js saat runtime.
6. Tidak membutuhkan frontend container terpisah.
7. Menggunakan satu binary Go.
8. Menggunakan SQLite sebagai database.
9. Memiliki UI modern tetapi ringan.
10. Mendukung realtime status monitoring.
11. Mendukung autentikasi administrator.
12. Memiliki halaman `/display` read-only untuk NOC/TV monitoring.
13. Memiliki notification melalui Telegram.
14. Mendukung HTTP, TCP, dan ICMP Ping sebagai monitor utama.
15. Menyimpan uptime dan latency history dengan efisien.
16. Bisa digunakan untuk puluhan hingga ratusan monitor tanpa resource usage berlebihan.

---

# 3. Non-Goals

Untuk versi awal, MiniUptime TIDAK perlu:

- Kubernetes integration
- Redis
- PostgreSQL
- MySQL sebagai internal database
- React
- Vue
- Angular
- Next.js
- Nuxt
- Node.js runtime
- microservices
- Kafka
- RabbitMQ
- GraphQL
- Elasticsearch
- complicated RBAC
- OAuth
- SSO
- multi-tenant architecture
- mobile application
- plugin system
- 50+ notification providers
- Docker container monitoring
- SNMP monitoring
- agent-based monitoring
- distributed monitoring nodes

Fitur tersebut hanya boleh dipertimbangkan nanti jika benar-benar dibutuhkan.

---

# 4. Technology Stack

## Backend

Gunakan:

```text
Go
```

Preferensi:

```text
net/http
```

Framework besar tidak diperlukan kecuali ada alasan teknis kuat.

Router ringan diperbolehkan jika memberikan manfaat jelas.

Contoh:

```text
chi
```

Tetapi jika `net/http` sudah cukup, gunakan standard library.

---

# 5. Frontend Stack

Frontend harus ringan.

Gunakan:

```text
Go html/template
HTMX
Alpine.js
Tailwind CSS
```

HTMX digunakan untuk:

- forms
- CRUD monitor
- filtering
- pagination
- enable/disable
- maintenance mode
- partial page updates

Alpine.js hanya digunakan untuk:

- modal
- dropdown
- sidebar
- tooltip
- toggle
- UI state kecil

Jangan membuat frontend sebagai SPA.

Jangan menggunakan:

```text
React
Vue
Angular
SvelteKit
Next.js
Nuxt
```

---

# 6. Static Assets

Semua frontend assets harus di-embed ke Go binary menggunakan:

```go
//go:embed
```

Contoh struktur:

```text
web/
├── templates/
├── static/
│   ├── css/
│   ├── js/
│   └── icons/
```

Kemudian di-embed ke binary.

Target deployment:

```text
MiniUptime
└── single executable
```

---

# 7. Database

Gunakan:

```text
SQLite
```

Database berada di:

```text
/app/data/miniuptime.db
```

Database harus mendukung:

- WAL mode
- migrations
- indexes yang sesuai
- retention policy
- aggregate historical data

Jangan mengganti SQLite dengan PostgreSQL kecuali terdapat benchmark yang membuktikan SQLite tidak lagi mencukupi.

---

# 8. Docker Architecture

Target akhir:

```text
Docker
└── MiniUptime
    ├── Go Binary
    ├── Embedded UI
    └── SQLite
```

Satu container saja.

Contoh deployment:

```yaml
services:
  miniuptime:
    image: ghcr.io/OWNER/miniuptime:latest
    container_name: miniuptime
    restart: unless-stopped

    ports:
      - "3001:3000"

    volumes:
      - ./data:/app/data

    environment:
      TZ: Asia/Jakarta
```

User cukup menjalankan:

```bash
docker compose up -d
```

Kemudian membuka:

```text
http://SERVER-IP:3001
```

---

# 9. First Run Setup

Jika database belum memiliki administrator, aplikasi harus otomatis membuka Setup Wizard.

Flow:

```text
Welcome
↓
Create Administrator
↓
Timezone
↓
Telegram Configuration (optional)
↓
Create First Monitor (optional)
↓
Finish
```

Setup wizard hanya muncul apabila instance belum dikonfigurasi.

---

# 10. Authentication

Dashboard administrator harus membutuhkan login.

Gunakan:

```text
Server-side session
Secure cookie
Password hashing
CSRF protection
```

Tidak perlu JWT untuk browser authentication.

Password harus disimpan menggunakan:

```text
Argon2id
```

atau alternatif aman yang tersedia secara baik di Go.

Session cookie:

```text
HttpOnly
SameSite
Secure when HTTPS
```

---

# 11. User Roles

Untuk versi pertama cukup:

```text
ADMIN
```

Administrator dapat:

- login
- logout
- add monitor
- edit monitor
- delete monitor
- enable monitor
- disable monitor
- maintenance mode
- configure Telegram
- manage groups
- view incidents
- view history
- configure settings

Tidak perlu RBAC kompleks pada versi pertama.

---

# 12. Admin Routes

Contoh:

```text
/login

/dashboard

/monitors
/monitors/new
/monitors/:id
/monitors/:id/edit

/groups

/incidents

/alerts

/settings

/logout
```

Semua route administratif selain `/login` wajib authenticated.

---

# 13. Display Mode

MiniUptime harus memiliki:

```text
/display
```

Tujuan `/display` adalah untuk TV, wall monitor, atau layar NOC tim IT.

Display mode:

- read-only
- realtime
- fullscreen friendly
- tidak memiliki tombol edit
- tidak memiliki tombol delete
- tidak menampilkan secrets
- tidak menampilkan configuration credentials
- automatic SSE reconnect

---

# 14. Display Access Modes

Settings harus menyediakan:

```text
Display Access

Disabled
Public Read Only
PIN Protected
```

Default:

```text
Disabled
```

## Disabled

`/display` tidak dapat diakses.

## Public Read Only

`/display` dapat diakses tanpa login.

Cocok untuk internal LAN.

## PIN Protected

User diminta memasukkan PIN sebelum membuka display.

Browser boleh menyimpan authorization menggunakan secure session cookie.

---

# 15. Display Filtering

Display nantinya dapat mendukung:

```text
/display
/display?group=network
/display?group=application
/display?group=infrastructure
```

Contoh penggunaan:

TV utama:

```text
/display
```

TV network:

```text
/display?group=network
```

---

# 16. Dashboard Design

Gunakan dark dashboard yang modern dan bersih.

Layout desktop:

```text
Sidebar
+
Topbar
+
Summary Cards
+
Uptime Graph
+
Monitor Table
+
Recent Incidents
```

Contoh summary:

```text
Total Monitors
30

UP
27

DOWN
1

DEGRADED
2

Average Response
28 ms
```

---

# 17. Sidebar

Menu:

```text
Overview
Monitors
Groups
Incidents
Alerts
Status Page
Settings
```

Status Page dapat disiapkan sebagai placeholder untuk fase berikutnya jika belum diimplementasikan.

---

# 18. Monitor Groups

Monitor dapat dikelompokkan.

Contoh:

```text
Infrastructure
Application
Network
Database
External
```

Contoh monitor:

```text
Infrastructure
├── NAS
├── CCTV Server
└── Hypervisor

Application
├── OpenERP
├── TMS
├── PORTYS
└── GOWA

Network
├── MikroTik POS1
├── MikroTik POS3
├── ISP 1
└── ISP 2

Database
├── PostgreSQL
└── MySQL
```

---

# 19. Supported Monitor Types

MVP harus mendukung:

```text
HTTP / HTTPS
TCP
ICMP Ping
```

Future:

```text
DNS
```

DNS jangan menjadi blocker untuk MVP.

---

# 20. HTTP Monitor

HTTP monitor configuration:

```text
Name
URL
Method
Interval
Timeout
Retries
Expected HTTP Status
Group
Enabled
```

Default:

```text
Method: GET
Expected status: 200-399
Interval: 30 seconds
Timeout: 5 seconds
Retries: 2
```

Response body checking dapat menjadi fitur fase berikutnya.

---

# 21. HTTP Monitoring Philosophy

Jangan melakukan request yang berat jika tidak dibutuhkan.

Ideal:

```text
GET /health
```

Response:

```json
{
  "status": "ok"
}
```

Health endpoint sebaiknya tidak melakukan operasi berat.

---

# 22. TCP Monitor

Configuration:

```text
Name
Host
Port
Interval
Timeout
Retries
Group
Enabled
```

Contoh:

```text
PostgreSQL
10.10.1.20
5432
```

TCP monitor hanya perlu membuktikan port dapat menerima connection.

---

# 23. ICMP Ping Monitor

Configuration:

```text
Name
Host
Interval
Timeout
Retries
Group
Enabled
```

Default:

```text
Interval: 30 seconds
Timeout: 3 seconds
Retries: 2
```

Ping tidak boleh dijalankan terlalu agresif.

---

# 24. Monitor Interval Presets

Sediakan preset:

## Critical

```text
Interval: 10s
Timeout: 3s
Retries: 2
```

## Normal

```text
Interval: 30s
Timeout: 5s
Retries: 2
```

## Light

```text
Interval: 60s
Timeout: 5s
Retries: 2
```

Default:

```text
Normal
```

---

# 25. Scheduler Architecture

Jangan membuat polling loop yang tidak efisien.

Gunakan:

```text
Scheduler
↓
Due Monitor Queue
↓
Worker Pool
↓
Check Engine
↓
Result Queue
↓
Result Writer
↓
SQLite
```

Monitor yang belum waktunya check tidak perlu melakukan pekerjaan.

---

# 26. Worker Pool

Worker count harus configurable tetapi memiliki default rasional.

Contoh:

```text
10 workers
```

Jangan membuat satu OS thread khusus untuk setiap monitor.

Goroutine boleh digunakan, tetapi concurrency harus dibatasi.

Gunakan:

```text
bounded worker pool
```

---

# 27. Check Scheduling

Setiap monitor memiliki:

```text
next_check_at
```

Scheduler mengambil monitor yang:

```text
next_check_at <= now
```

Setelah check:

```text
next_check_at = now + interval + jitter
```

---

# 28. Jitter

Tambahkan random jitter ringan.

Contoh:

```text
±1-3 detik
```

Tujuan:

Jika terdapat 200 monitor, jangan semuanya melakukan request tepat pada:

```text
15:00:00
```

Distribusikan check ke beberapa detik.

---

# 29. Status Model

Gunakan status:

```text
UP
PENDING
DOWN
RECOVERING
DEGRADED
MAINTENANCE
```

---

# 30. Failure Threshold

Jangan langsung DOWN karena satu timeout.

Default:

```text
Failure Threshold = 3
Recovery Threshold = 2
```

Flow:

```text
UP

failure #1
↓
PENDING

failure #2
↓
PENDING

failure #3
↓
DOWN
```

Recovery:

```text
DOWN

success #1
↓
RECOVERING

success #2
↓
UP
```

---

# 31. Degraded Status

Monitor dapat dianggap degraded jika service masih tersedia tetapi latency melewati threshold.

Optional configuration:

```text
Latency Warning Threshold
```

Contoh:

```text
MikroTik

Normal:
10 ms

Threshold:
100 ms

Response:
140 ms

Status:
DEGRADED
```

---

# 32. Maintenance Mode

Monitor dapat dimasukkan maintenance mode.

Saat maintenance:

```text
status = MAINTENANCE
```

Behavior:

- monitoring boleh tetap berjalan
- alert tidak dikirim
- downtime tidak dihitung sebagai incident normal
- UI menunjukkan maintenance

---

# 33. Realtime Updates

Gunakan:

```text
Server-Sent Events
```

Jangan menggunakan WebSocket kecuali dibutuhkan secara nyata.

Endpoint:

```text
/events
```

Contoh event:

```text
monitor.updated
monitor.down
monitor.recovered
incident.created
incident.resolved
```

---

# 34. SSE Payload

Payload harus kecil.

Contoh:

```json
{
  "id": 12,
  "status": "up",
  "latency_ms": 21,
  "checked_at": "2026-08-18T15:30:00+07:00"
}
```

Browser hanya meng-update komponen yang berubah.

Jangan reload seluruh halaman.

---

# 35. SSE Reconnect

Browser harus otomatis reconnect ketika connection SSE terputus.

Gunakan mekanisme native:

```javascript
EventSource
```

Jika memungkinkan, hindari library tambahan.

---

# 36. Monitor History

Setiap monitor memiliki halaman detail.

Contoh:

```text
OpenERP

Status:
UP

Response:
21 ms

Uptime 24h:
100%

Uptime 7d:
99.98%

Uptime 30d:
99.95%
```

---

# 37. Response Time Graph

Tampilkan graph:

```text
24 Hours
7 Days
30 Days
```

Gunakan chart library yang ringan.

Preferensi:

```text
uPlot
```

Hindari library graph berat jika tidak diperlukan.

---

# 38. Recent Checks

Monitor detail menampilkan:

```text
Time
Status
Latency
HTTP Status
Error
```

Contoh:

```text
15:30:00 UP 21ms 200
15:29:30 UP 19ms 200
15:29:00 UP 23ms 200
```

---

# 39. Incident Management

Incident dibuat otomatis ketika monitor berubah:

```text
PENDING → DOWN
```

Incident resolved ketika:

```text
RECOVERING → UP
```

Incident harus mencatat:

```text
monitor
started_at
ended_at
duration
reason
last_error
```

---

# 40. Incident Example

```text
CCTV Server Down

Started:
15:02:30

Recovered:
15:08:00

Duration:
5m 30s

Cause:
connection timeout
```

---

# 41. Telegram Notifications

MVP notification provider:

```text
Telegram
```

Settings:

```text
Bot Token
Chat ID
Enabled
```

Bot token harus diperlakukan sebagai secret.

Jangan pernah menampilkan token penuh setelah tersimpan.

---

# 42. Telegram DOWN Message

Contoh:

```text
🔴 MiniUptime

CCTV Server is DOWN

Type: HTTP
Target: 10.10.3.10
Error: connection timeout

Started: 15:02:30 WIB
```

---

# 43. Telegram Recovery Message

Contoh:

```text
🟢 MiniUptime

CCTV Server has RECOVERED

Downtime: 5m 30s
Response: 18 ms
```

---

# 44. Notification Deduplication

Jangan mengirim notifikasi berulang setiap check.

Flow:

```text
UP
↓
DOWN
↓
SEND DOWN ALERT

DOWN
DOWN
DOWN

NO ADDITIONAL ALERT

↓
UP

SEND RECOVERY
```

---

# 45. Alert Cooldown

Future-ready support untuk:

```text
alert cooldown
```

Tetapi tidak perlu kompleks untuk MVP.

---

# 46. SQLite Tables

Minimal schema:

```text
users
monitors
groups
checks
incidents
notification_channels
settings
sessions
```

---

# 47. Users Table

Contoh:

```text
users

id
username
password_hash
created_at
updated_at
```

---

# 48. Groups Table

```text
groups

id
name
sort_order
created_at
updated_at
```

---

# 49. Monitors Table

```text
monitors

id
name
type
target
port
method
expected_status
interval_seconds
timeout_seconds
retries
failure_threshold
recovery_threshold
latency_warning_ms
group_id
enabled
maintenance
status
consecutive_failures
consecutive_successes
last_latency_ms
last_error
last_checked_at
next_check_at
created_at
updated_at
```

Fields dapat disesuaikan berdasarkan monitor type.

Jangan membuat schema terlalu abstrak jika membuat implementasi lebih rumit.

---

# 50. Checks Table

```text
checks

id
monitor_id
status
latency_ms
http_status
error
checked_at
```

Index:

```text
monitor_id
checked_at
```

---

# 51. Incidents Table

```text
incidents

id
monitor_id
started_at
ended_at
reason
created_at
```

---

# 52. Notification Channels Table

```text
notification_channels

id
name
type
config
enabled
created_at
updated_at
```

Secrets dalam `config` harus dilindungi sebisa mungkin.

---

# 53. Settings Table

```text
settings

key
value
```

Contoh:

```text
timezone
display_mode
display_pin_hash
retention_days
```

---

# 54. Database Retention

Raw checks jangan disimpan selamanya.

Default recommendation:

```text
Raw checks:
7 days

5-minute aggregates:
30 days

Hourly aggregates:
365 days

Incidents:
Permanent
```

Retention harus configurable.

---

# 55. Aggregation

Future/phase 2 dapat membuat:

```text
check_aggregates_5m
check_aggregates_1h
```

Data:

```text
monitor_id
bucket_time
avg_latency
min_latency
max_latency
success_count
failure_count
uptime_percent
```

---

# 56. Cleanup Job

Background cleanup dijalankan berkala.

Misalnya:

```text
once per hour
```

atau:

```text
once per day
```

Cleanup bukan loop agresif.

---

# 57. Uptime Calculation

Uptime dihitung berdasarkan check result pada periode tertentu.

Tampilkan:

```text
24 hours
7 days
30 days
```

Maintenance period sebaiknya dikecualikan dari uptime calculation.

---

# 58. Health Endpoint

MiniUptime sendiri harus memiliki:

```text
/health
```

Response:

```json
{
  "status": "ok"
}
```

HTTP:

```text
200
```

Endpoint harus sangat ringan.

---

# 59. Docker Healthcheck

Docker image dapat menggunakan:

```dockerfile
HEALTHCHECK
```

untuk request:

```text
http://localhost:3000/health
```

---

# 60. Metrics

Jangan langsung menambahkan Prometheus dependency.

Jika nanti diperlukan dapat dipertimbangkan.

Untuk MVP:

```text
No Prometheus exporter required.
```

---

# 61. Logging

Logging harus sederhana.

Gunakan structured logging.

Contoh:

```text
timestamp
level
component
message
```

Gunakan Go standard `slog` jika sesuai.

Contoh:

```text
INFO monitor.check monitor_id=12 latency=21 status=up
```

Jangan menghasilkan log setiap detik tanpa kebutuhan.

---

# 62. Sensitive Logging

Jangan log:

```text
password
Telegram bot token
session cookie
display PIN
authorization header
```

---

# 63. Error Handling

Error monitor harus dicatat secara ringkas.

Contoh:

```text
connection refused
timeout
DNS resolution failed
HTTP 500
```

Batasi ukuran error text yang masuk database.

Jangan menyimpan response body besar.

---

# 64. Security Principles

Implementasikan:

- password hashing
- CSRF
- session expiration
- HttpOnly cookies
- secure cookie ketika HTTPS
- input validation
- output escaping
- SQL parameterization
- rate limit login
- no plaintext passwords
- no plaintext display PIN

---

# 65. Login Rate Limiting

Tambahkan rate limiter ringan.

Contoh:

```text
5 failed login attempts
↓
temporary delay/block
```

Tidak perlu Redis.

Rate limiting dapat dilakukan in-memory.

---

# 66. HTTP Client Safety

HTTP monitor harus memiliki:

```text
timeout
connection limit
idle connection reuse
```

Gunakan shared:

```go
http.Client
```

atau shared transport.

Jangan membuat transport baru secara tidak perlu setiap request.

---

# 67. Connection Reuse

HTTP checker harus memanfaatkan keep-alive.

Tujuan:

- mengurangi socket overhead
- mengurangi allocations
- mengurangi CPU
- mengurangi latency

---

# 68. Resource Philosophy

Setiap background task harus memiliki alasan.

Tidak boleh ada polling loop dengan interval sangat kecil tanpa kebutuhan.

Contoh buruk:

```text
while true:
    query all monitors
    sleep 100ms
```

Gunakan scheduler yang efisien.

---

# 69. Resource Targets

Target awal, bukan klaim benchmark:

```text
Idle RAM:
< 30-50 MB preferred

Normal workload:
< 100 MB preferred

Idle CPU:
near 0%

Startup:
< 1 second preferred

Docker image:
< 50 MB preferred
```

Jika target tidak tercapai, lakukan benchmark sebelum melakukan optimasi besar.

---

# 70. Performance Test Scenario

Benchmark minimal:

```text
50 monitors
100 monitors
250 monitors
500 monitors
```

Interval:

```text
30 seconds
```

Monitor mix:

```text
40% Ping
40% HTTP
20% TCP
```

Measure:

```text
RAM
CPU
goroutines
check throughput
SQLite size
dashboard latency
```

---

# 71. UI Performance Goal

Dashboard harus terasa instant pada LAN normal.

Hindari:

- full page reload
- massive JavaScript bundles
- excessive animations
- huge icon libraries
- loading thousands of history points

---

# 72. Chart Downsampling

Jangan mengirim 50.000 raw points ke browser.

Untuk graph:

```text
24h → raw / 5-minute
7d  → 5-minute
30d → hourly
```

sesuai kebutuhan.

---

# 73. Icons

Gunakan icon set yang ringan.

Ideal:

```text
SVG icons
```

Jangan load font icon besar hanya untuk beberapa icon.

---

# 74. UI Theme

Default:

```text
Dark Mode
```

Style:

```text
clean
modern
compact
professional
infrastructure dashboard
```

Status colors:

```text
UP          green
DOWN        red
DEGRADED    orange
MAINTENANCE blue/gray
PENDING     yellow
```

---

# 75. Responsive Design

Prioritas:

```text
Desktop
TV
Tablet
```

Mobile harus usable tetapi bukan fokus utama.

---

# 76. NOC Display UI

`/display` harus lebih sederhana daripada admin dashboard.

Contoh:

```text
MiniUptime                     15:42 WIB    ● LIVE

30 MONITORS
27 UP
1 DOWN
2 DEGRADED

APPLICATION

● OpenERP       21ms
● TMS           34ms
● PORTYS        18ms
● GOWA          47ms

NETWORK

● MikroTik POS1     4ms
● MikroTik POS3    94ms
● ISP 1             8ms
● ISP 2            22ms

INFRASTRUCTURE

● NAS              18ms
● CCTV Server      DOWN

Recent Incident

15:02 CCTV Server DOWN
```

---

# 77. NOC Display Behavior

Display page harus:

- auto reconnect SSE
- update tanpa reload
- menampilkan current time
- menampilkan connection status
- fullscreen friendly
- tidak menampilkan sidebar admin
- tidak menampilkan buttons untuk edit

Status connection:

```text
● LIVE
```

Jika SSE disconnect:

```text
● RECONNECTING
```

---

# 78. Optional Display Audio

Jangan implementasikan pada MVP kecuali sangat mudah.

Jika nanti dibuat:

```text
Optional sound on DOWN
```

Default:

```text
disabled
```

---

# 79. CRUD Monitor UX

Add Monitor form:

```text
Name

Monitor Type
HTTP / TCP / Ping

Target

Group

Preset
Critical / Normal / Light / Custom

Advanced Settings
```

Advanced settings default collapsed.

Tujuan:

User dapat membuat monitor tanpa memahami semua parameter teknis.

---

# 80. Monitor List

Columns:

```text
Status
Name
Type
Group
Response
Uptime 24h
Last Check
Actions
```

Support:

```text
search
group filter
status filter
```

---

# 81. Monitor Actions

Actions:

```text
View
Edit
Enable/Disable
Maintenance
Delete
```

Delete harus meminta confirmation.

---

# 82. Dashboard Summary

Dashboard overview:

```text
Total Monitors
UP
DOWN
DEGRADED
Average Response Time
```

Jika semua sehat:

```text
All Systems Operational
```

---

# 83. Recent Incidents

Dashboard menampilkan incident terbaru.

Contoh:

```text
CCTV Server is down
12 minutes ago

MikroTik POS3 high latency
26 minutes ago

GOWA recovered
1 hour ago
```

---

# 84. API Design

MiniUptime bukan API-first application.

Gunakan server rendered pages + HTMX.

Tetapi endpoint JSON diperbolehkan untuk kebutuhan internal yang jelas.

Jangan membuat REST API besar tanpa kebutuhan.

---

# 85. Application Structure

Contoh struktur repository:

```text
miniuptime/
├── cmd/
│   └── miniuptime/
│       └── main.go
│
├── internal/
│   ├── auth/
│   ├── config/
│   ├── database/
│   ├── monitor/
│   │   ├── checker.go
│   │   ├── http.go
│   │   ├── tcp.go
│   │   └── ping.go
│   │
│   ├── scheduler/
│   ├── incident/
│   ├── notification/
│   │   └── telegram.go
│   │
│   ├── realtime/
│   ├── retention/
│   └── web/
│
├── web/
│   ├── templates/
│   └── static/
│
├── migrations/
│
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

Struktur dapat disesuaikan jika ada alasan yang baik.

---

# 86. Package Philosophy

Setiap package harus memiliki satu tanggung jawab jelas.

Hindari:

```text
utils/
helpers/
common/
```

yang menjadi tempat berbagai fungsi tanpa boundary jelas.

---

# 87. Interfaces

Gunakan interface hanya jika benar-benar memberikan boundary berguna.

Contoh cocok:

```go
type Checker interface {
    Check(ctx context.Context, monitor Monitor) Result
}
```

Jangan membuat interface untuk semua struct hanya demi pattern.

---

# 88. Context Cancellation

Semua network check harus mendukung:

```go
context.Context
```

Timeout harus menghentikan operation.

---

# 89. Graceful Shutdown

Saat menerima:

```text
SIGTERM
SIGINT
```

aplikasi harus:

1. stop menerima request baru
2. stop scheduler
3. tunggu worker aktif secara terbatas
4. flush database operation
5. close SQLite
6. exit

Ini penting untuk Docker.

---

# 90. Database Migration

Migration harus otomatis saat startup.

Contoh:

```text
container start
↓
open database
↓
run pending migrations
↓
start service
```

User tidak perlu menjalankan migration manual.

---

# 91. Upgrade Experience

Deployment production sebaiknya mendukung version tags:

```text
ghcr.io/owner/miniuptime:v1.0.0
```

Update:

```bash
docker compose pull
docker compose up -d
```

Database migration otomatis.

---

# 92. Docker Image

Gunakan multi-stage build.

Contoh konsep:

```text
Builder
↓
compile static/small Go binary
↓
minimal runtime image
```

Runtime image dapat menggunakan:

```text
distroless
```

atau image minimal lainnya.

Pastikan timezone dan certificate support tetap bekerja.

---

# 93. Docker Volume

Persistent data:

```text
/app/data
```

Semua data penting harus berada di volume.

Container harus disposable.

Delete container tidak boleh menghapus database jika volume masih tersedia.

---

# 94. Backup

Backup awal cukup sederhana.

User dapat backup:

```text
data/miniuptime.db
```

Tetapi SQLite WAL harus ditangani dengan benar.

Future UI dapat menyediakan:

```text
Download Backup
```

Tidak wajib MVP.

---

# 95. Configuration

Environment hanya untuk deployment-level configuration.

Contoh:

```text
PORT
DATA_DIR
TZ
LOG_LEVEL
```

Application configuration normal dilakukan melalui UI.

Jangan memaksa user mengedit `.env` untuk:

- Telegram
- monitor
- group
- alert
- display mode

---

# 96. Default Environment

Contoh:

```text
PORT=3000
DATA_DIR=/app/data
TZ=Asia/Jakarta
LOG_LEVEL=info
```

---

# 97. Testing Strategy

Testing wajib.

Gunakan:

```text
go test
```

Minimal:

```text
unit tests
integration tests
HTTP handler tests
monitor checker tests
scheduler tests
incident tests
authentication tests
```

---

# 98. Checker Tests

HTTP checker test:

```text
200 → UP
500 → DOWN/failure
timeout → failure
connection refused → failure
```

TCP checker:

```text
open port → UP
closed port → failure
timeout → failure
```

---

# 99. State Transition Tests

Wajib test:

```text
UP
failure
failure
failure
DOWN
```

dan:

```text
DOWN
success
success
UP
```

Pastikan notification hanya dikirim saat transition.

---

# 100. Incident Tests

Pastikan:

```text
DOWN transition
→ incident created
```

dan:

```text
recovery
→ same incident resolved
```

Tidak boleh membuat incident baru setiap failed check.

---

# 101. Authentication Tests

Test:

```text
valid login
invalid login
logout
protected route
CSRF
expired session
```

---

# 102. SSE Tests

Test:

```text
subscriber connect
event broadcast
subscriber disconnect
slow subscriber
```

Slow SSE client tidak boleh memblokir monitoring engine.

---

# 103. SQLite Concurrency Tests

Test concurrent:

```text
monitor result writes
dashboard reads
incident writes
```

Pastikan tidak sering mendapatkan:

```text
database is locked
```

Gunakan WAL dan write pattern yang sesuai.

---

# 104. Acceptance Criteria MVP

MVP dianggap berhasil jika:

- Docker container dapat start.
- Setup wizard muncul pertama kali.
- Admin dapat dibuat.
- Login bekerja.
- Admin dapat logout.
- HTTP monitor dapat dibuat.
- TCP monitor dapat dibuat.
- Ping monitor dapat dibuat.
- Monitor berjalan sesuai interval.
- Failure threshold bekerja.
- Recovery threshold bekerja.
- Monitor history tersimpan.
- Incident dibuat otomatis.
- Incident resolved otomatis.
- Telegram DOWN alert bekerja.
- Telegram recovery alert bekerja.
- Dashboard realtime.
- `/display` realtime.
- `/display` read-only.
- Display access mode bekerja.
- Docker restart tidak kehilangan data.
- Migration otomatis.
- `/health` bekerja.
- UI responsive.
- Tidak membutuhkan external database.
- Tidak membutuhkan Redis.
- Tidak membutuhkan Node runtime.

---

# 105. Suggested Development Phases

## Phase 1 — Foundation

Implementasikan:

- project structure
- configuration
- SQLite
- migrations
- web server
- embedded assets
- health endpoint
- graceful shutdown

Acceptance:

```text
docker compose up
↓
MiniUptime running
↓
/health = 200
```

---

## Phase 2 — Authentication

Implementasikan:

- first-run wizard
- admin creation
- login
- logout
- session
- password hash
- CSRF

---

## Phase 3 — Monitor CRUD

Implementasikan:

- groups
- monitor form
- HTTP monitor
- TCP monitor
- Ping monitor
- enable/disable

---

## Phase 4 — Monitoring Engine

Implementasikan:

- scheduler
- worker pool
- jitter
- timeout
- retries
- status transitions
- latency

---

## Phase 5 — History & Incident

Implementasikan:

- checks
- uptime calculation
- incidents
- recovery
- monitor details

---

## Phase 6 — Dashboard

Implementasikan:

- overview
- summary cards
- monitor table
- filtering
- recent incidents
- response graph

---

## Phase 7 — Realtime

Implementasikan:

- SSE
- monitor updates
- dashboard updates
- auto reconnect

---

## Phase 8 — Telegram

Implementasikan:

- Telegram configuration
- connection test
- DOWN notification
- recovery notification
- deduplication

---

## Phase 9 — Display Mode

Implementasikan:

- `/display`
- public read-only
- disabled
- PIN protected
- group filtering
- fullscreen layout
- SSE

---

## Phase 10 — Optimization

Lakukan:

- benchmark
- profiling
- SQLite tuning
- memory profiling
- goroutine profiling
- frontend bundle review
- database retention

Jangan melakukan premature optimization sebelum mempunyai benchmark.

---

# 106. AI Development Rules

Saat AI coding agent mengimplementasikan proyek ini:

## Rule 1

Jangan menambahkan dependency tanpa menjelaskan alasannya.

## Rule 2

Prioritaskan Go standard library.

## Rule 3

Jangan menggunakan React/Vue.

## Rule 4

Jangan menggunakan Redis.

## Rule 5

Jangan menggunakan external database.

## Rule 6

Jangan membuat microservices.

## Rule 7

Jangan membuat abstraction yang belum dibutuhkan.

## Rule 8

Setiap fitur baru harus mempunyai test.

## Rule 9

Jalankan test sebelum menyatakan task selesai.

## Rule 10

Jangan mengubah banyak subsystem sekaligus tanpa alasan.

## Rule 11

Pertahankan compatibility dengan Docker single-container deployment.

## Rule 12

Selalu pertimbangkan memory, CPU, network, dan database impact.

---

# 107. AI Workflow

Untuk setiap feature:

```text
1. Read existing architecture
2. Understand requirement
3. Propose implementation
4. Write/update tests
5. Implement minimum required code
6. Run tests
7. Run formatter
8. Run static analysis
9. Verify application
10. Report changes
```

Gunakan:

```bash
go test ./...
```

dan:

```bash
go vet ./...
```

Jika project menggunakan linter tambahan, jalankan juga.

---

# 108. No Premature Complexity

Sebelum membuat sesuatu seperti:

```text
repository abstraction
event bus
message broker
service locator
CQRS
distributed scheduler
generic plugin framework
```

AI harus menjawab:

> Apakah masalah nyata saat ini membutuhkan ini?

Jika tidak:

```text
DO NOT IMPLEMENT.
```

---

# 109. Lightweight Decision Test

Untuk setiap dependency atau fitur baru, tanyakan:

```text
1. Apakah benar-benar dibutuhkan?
2. Apakah standard library dapat melakukannya?
3. Berapa memory impact?
4. Berapa runtime impact?
5. Berapa deployment complexity?
6. Berapa maintenance burden?
7. Apakah user mendapatkan manfaat nyata?
```

Jika manfaat kecil tetapi complexity tinggi:

```text
reject.
```

---

# 110. Definition of Done

Sebuah task baru dianggap selesai hanya ketika:

- code selesai
- tests tersedia
- tests passing
- build passing
- tidak ada known regression
- tidak ada unnecessary dependency
- error handling tersedia
- security implications diperiksa
- Docker compatibility tetap terjaga

---

# 111. Product Philosophy

MiniUptime harus terasa seperti:

```text
install
↓
open browser
↓
configure
↓
forget about it
```

Bukan:

```text
install database
install cache
configure broker
configure frontend
configure backend
build JS
run migrations manually
debug dependencies
```

---

# 112. Core Product Statement

> MiniUptime is a lightweight self-hosted monitoring appliance built around one Go binary, one SQLite database, one Docker container, and a fast server-rendered web interface.

---

# 113. Final Architecture

```text
                       Browser
                          │
                 HTTP / HTMX / SSE
                          │
                          ▼
                ┌──────────────────┐
                │    MiniUptime    │
                │                  │
                │ Go HTTP Server   │
                │ Authentication   │
                │ Scheduler        │
                │ Worker Pool      │
                │ Monitor Engine   │
                │ Incident Engine  │
                │ Telegram Alert   │
                │ SSE Broker       │
                └────────┬─────────┘
                         │
                         ▼
                      SQLite
                         │
                         ▼
                  /app/data
```

Monitoring:

```text
Scheduler
   │
   ▼
Due Queue
   │
   ▼
Worker Pool
   │
   ├── HTTP Checker
   ├── TCP Checker
   └── Ping Checker
          │
          ▼
        Result
          │
          ├── Status State Machine
          ├── Incident Engine
          ├── Notification Engine
          ├── SQLite
          └── SSE
```

---

# 114. Final Requirement

Prioritas pengembangan harus selalu:

```text
Correctness
    ↓
Reliability
    ↓
Simplicity
    ↓
Low Resource Usage
    ↓
Good UX
    ↓
Additional Features
```

MiniUptime tidak perlu menjadi aplikasi monitoring dengan fitur paling banyak.

MiniUptime harus menjadi:

> **monitoring tool yang kecil, cepat, stabil, mudah dijalankan, dan menyenangkan digunakan.**