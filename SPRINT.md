# MiniUptime Sprint Plan

## Goal

Stabilkan app, tutup gap SPEC, rapikan UI, harden security, tambah test. Minimal diff. No speculative refactor.

## Rules

- Go + SQLite tetap.
- No React/Vue/Node runtime.
- No PostgreSQL/Redis.
- Stdlib/native HTML/CSS/SVG first.
- No dependency baru tanpa alasan kuat.
- Fix root cause.
- Test setiap logic non-trivial.
- Jangan hapus `data/miniuptime.db`.
- Commit per sprint kecil.

## Sprint 1 — Acceptance Audit

### Scope

- Setup, login, logout.
- CSRF semua POST.
- Monitor CRUD.
- Group CRUD/filter.
- HTTP/TCP/Ping validation.
- Incident open/recovery/dedup.
- Telegram config/test/alert.
- Display disabled/public/PIN/group filter.
- SSE dashboard/display.
- Responsive UI.

### Output

- Pass/fail matrix.
- Repro step tiap failure.
- Severity P0/P1/P2.
- Fix order.

### Prompt

```text
Audit current MiniUptime app. Do not add features yet.

Test setup/login/logout, CSRF, monitor CRUD, group CRUD/filter,
HTTP/TCP/Ping validation, incidents, Telegram, display modes,
SSE dashboard/display, and responsive UI.

For each failure report route/page, exact repro steps, expected,
actual, root-cause guess, and severity P0/P1/P2.
Do not broadly refactor. Output pass/fail matrix, bug list, and fix order.
```

## Sprint 2 — SQLite Stability

### Scope

- Reproduce `database is locked`.
- Audit scheduler/check writes.
- Audit login/session/display/dashboard reads.
- Review WAL, busy timeout, connection pool.
- Review retention and indexes.
- Remove blocking or silent failure on critical paths.

### Done when

- Reproduction recorded or ruled out.
- Minimal fix applied.
- `go test ./... -race` passes.
- `/health` and `/login` respond during checks.

### Prompt

```text
Audit SQLite stability in current MiniUptime code.
Focus on scheduler writes, login/session queries, retention,
dashboard/display reads, WAL, busy_timeout, and connection pool.

Fix only measured or clearly reproducible problems. No new dependency.
No architecture rewrite. Log critical DB errors with context.
Run tests and report before/after behavior and remaining risks.
```

## Sprint 3 — Security Hardening

### Scope

- Display PIN hash, legacy migration behavior, rate limit.
- Telegram token storage risk.
- Session cookie flags and expiry.
- CSRF coverage.
- Secret exposure in UI/log/display.
- HTTPS behavior.

### Done when

- Findings ranked.
- Quick fixes applied.
- Secret handling decision documented.
- `go test ./... -race` passes.

### Prompt

```text
Perform small-scope security audit on MiniUptime.
Review display PIN, Telegram token, sessions/cookies, CSRF,
rate limiting, and secret exposure.

Prefer minimum safe fixes. No enterprise auth rewrite or secret manager.
Report already-mitigated risks, fixes applied, deferred risks, and why.
```

## Sprint 4 — UI Consistency

### Scope

Review dashboard, monitors, new/edit monitor, detail, groups,
incidents, settings, login, setup, display, and display PIN.

Check:

- Navigation consistency.
- Input/button alignment.
- Spacing and table rhythm.
- Status badges.
- Empty/error states.
- Focus/hover/active states.
- Typography hierarchy.
- Mobile layout.
- Display down-state visibility.

### Done when

- Shared admin pages use one visual language.
- Forms and buttons align.
- Display remains intentionally read-only.
- No stack rewrite.

### Prompt

```text
Audit and improve UI consistency across all MiniUptime pages.
Use current Go templates and CSS. No React/Vue rewrite.
Prefer CSS/HTML and smallest server-side changes.

Report before/after changes by page. Check alignment, spacing,
forms, tables, badges, focus states, mobile layout, empty states,
and display down-state clarity.
```

## Sprint 5 — Latency and Incident Visualization

### Scope

- Latency summary: average, P95, best, worst.
- Meaningful latency trend.
- Down/outlier markers.
- Humanized timestamps.
- Incident readability.
- Native SVG/CSS/HTML only unless proven insufficient.

### Done when

- Operator can see trend and outlier quickly.
- Graph labels do not collide.
- Absolute time appears where needed.
- Relative time used only where useful.

### Prompt

```text
Improve MiniUptime latency and incident visualization.
Current output must communicate trend, outliers, status, and time.
Prefer native HTML/CSS/SVG. No chart library by default.

Review average/P95/best/worst, graph scale, down markers,
humanized timestamps, and incident rows. Apply the smallest useful diff.
Explain why each visual change improves operator understanding.
```

## Sprint 6 — Test Expansion

### Scope

Add focused tests for:

- Password hash round-trip.
- Login failure/success logic.
- Display PIN success/failure/rate limit.
- Settings save.
- Monitor validation matrix.
- Retention cleanup.
- Humanized time.
- Display access modes.
- Telegram test success/failure where practical.
- Incident dedup/recovery.

### Done when

```bash
podman run --rm -v "$PWD:/src:Z" -w /src docker.io/library/golang:1.22 go test ./... -race
```

passes.

### Prompt

```text
Add focused tests for highest-risk MiniUptime logic.
Do not add a framework. Keep tests direct and small.
Cover auth/password, display PIN/rate limit, settings, validation,
retention, time formatting, display access, Telegram result,
and incident dedup/recovery.
Explain why each test matters and list remaining gaps.
```

## Sprint 7 — Safe Code Health

### Scope

- Identify safe seams in large `main.go`.
- Remove clear duplication.
- Improve naming/error context.
- Avoid abstraction bloat.
- Preserve behavior.

### Do not

- Interface with one implementation.
- Factory/config layer for static values.
- Broad rewrite.
- Split files without measurable maintenance gain.

### Prompt

```text
Review MiniUptime code health. main.go is large but intentionally compact.
Find only safe, high-value refactors. Preserve behavior.

Rank opportunities by value/risk. State what should not be refactored.
Apply only minimal changes. No speculative abstractions.
```

## Execution Order

1. Sprint 1 — Acceptance Audit.
2. Sprint 2 — SQLite Stability.
3. Sprint 3 — Security.
4. Sprint 4 — UI Consistency.
5. Sprint 5 — Visualization.
6. Sprint 6 — Tests.
7. Sprint 7 — Code Health.

## Per-Sprint Closeout

- Run tests.
- Run `/health` check.
- Check `git diff`.
- Update `SESSION.md`.
- Commit focused changes.
- Record deferred items.
