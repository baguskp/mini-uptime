# AGENTS.md

Instructions for coding agents working in this repository.

## Project

MiniUptime — lightweight self-hosted uptime monitoring. Single Go binary (stdlib `net/http`, no web framework), SQLite (WAL mode), server-rendered HTML templates embedded via `embed.FS`. No Node.js or frontend build step.

## Commands

```sh
go build -o tmp/uptime.exe .   # build
go test ./...                  # run tests
```

Run locally (never touch `data/` — that is the user's real database):

```sh
DATABASE_PATH=/tmp/miniuptime.db PORT=3210 ./uptime.exe
```

- `PORT` defaults to `3000`
- `DATABASE_PATH` defaults to `/app/data/miniuptime.db` (Docker path); on Windows dev use a temp path
- Docker Compose maps `3001:3000` and mounts `./data`

First run redirects to `/setup` (create admin: username ≥ 3 chars, password ≥ 12 chars).

## Structure

- `main.go` — entry point only: `embed.FS` assets, global state, `main()`, `migrate()`, route wiring (~200 lines).
- Handlers and helpers live in focused files, all `package main`: `auth.go`, `agents.go`, `monitors.go`, `groups.go`, `scheduler.go`, `display.go`, `dashboard.go`, `settings.go`, `incidents.go`, `telegram.go`, `render.go`, `util.go`.
- `web/templates/*.html` — one file per page; each template is written as a single minified line. Keep that style.
- `web/static/app.css` — the only stylesheet
- `main_test.go` — unit tests
- `CONTEXT.md` — domain glossary (display scope, online rate, etc.). Use these terms exactly.

## Gotchas (read before editing)

1. **CSS is layered, not merged.** `app.css` is a handful of physical lines; each sprint appended a new line that overrides earlier ones instead of editing them. To change styles, append a new line at the end — do not rewrite existing lines. Expect duplicate selectors and conflicting rules (e.g. two `nav a:hover` colors).
2. **Nav is template-first.** Every nav-bearing template renders its own `<nav>`; `render()` injects an `Active` field (via `navActive()` in `render.go`) that drives `aria-current="page"` conditionals. The Agents link and the dashboard Agents card live directly in the templates — there is no runtime HTML patching anymore. Editing templates is sufficient.
3. **`/display` returns 404** unless Settings → display mode is set to `public` or `pin` (`display_mode` setting). Not a bug.
4. **All POST forms require a `csrf` hidden field** (see `csrf()` middleware).
5. **Monitor interval minimum is 10 seconds** (`validMonitor`).
6. **Dashboard and `/display` live-update via SSE** (`/events`, event `status`). `/display` has no meta refresh; its script updates chips/latency/checked/summary and falls back to `location.reload()` after 30s if the stream errors.
7. `.detail-list` and `.small-metric` (used by `agent-detail.html`) are defined in `app.css` — keep them; they are not placeholders.
8. **Windows encoding hazard.** All repo files are UTF-8. Never pipe git or command output through PowerShell `Out-File` / `Set-Content` — PowerShell 5.1 decodes external output as cp437/cp1252 and silently corrupts non-ASCII bytes (`–`, `—`, `·` become mojibake like `ÔÇô`, `┬À`). Read/write via the file tools, or Python with `encoding='utf-8'`, or `git cat-file blob` written as raw bytes.

## Conventions

- UI copy language: English (one Indonesian string exists in `agent-form.html`; prefer fixing it over adding more Indonesian).
- Status values: `up`, `down`, `unknown` — map to CSS classes `.status.up/.down/.unknown`.
- Destructive actions are plain forms with inline `onsubmit="return confirm(...)"`.
- Time formatting: `21 Aug, 15:52` style via template helpers.

## Workflow (user preference — follow strictly)

1. **Do not execute fixes directly.** Write the plan as a sprint doc in `docs/sprint/` first; implementation happens only after the user approves the sprint.
2. Sprint file naming: `YYYY-MM-DD_<priority-slug>.md` with front matter containing `timestamp`, `commit` (set `pending`; fill with the real commit hash when implemented), and `topic`. Follow the format of existing sprint docs there.
3. Use the `grill-me` / `grill-with-docs` skills to sharpen a sprint plan before executing it, especially for large or ambiguous sprints.
4. Small commits per sprint (matches rules in root `SPRINT.md`).
