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

- `main.go` — everything: routes, handlers, check scheduler, SSE hub, migrations (~1700 lines, single file by design)
- `web/templates/*.html` — one file per page; each template is written as a single minified line. Keep that style.
- `web/static/app.css` — the only stylesheet
- `main_test.go` — unit tests
- `CONTEXT.md` — domain glossary (display scope, online rate, etc.). Use these terms exactly.

## Gotchas (read before editing)

1. **CSS is layered, not merged.** `app.css` is a handful of physical lines; each sprint appended a new line that overrides earlier ones instead of editing them. To change styles, append a new line at the end — do not rewrite existing lines. Expect duplicate selectors and conflicting rules (e.g. two `nav a:hover` colors).
2. **Nav is patched at render time.** `normalizeNavbar()` in `main.go` injects the "Agents" nav link into every page, and `render()` injects the Agents summary card into `dashboard.html` via string replace. Editing templates alone is not enough for nav/dashboard-card changes.
3. **`/display` returns 404** unless Settings → display mode is set to `public` or `pin` (`display_mode` setting). Not a bug.
4. **All POST forms require a `csrf` hidden field** (see `csrf()` middleware).
5. **Monitor interval minimum is 10 seconds** (`validMonitor`).
6. **Dashboard live updates use SSE** (`/events`, event `status`). `/display` still uses `<meta refresh>` — known gap.
7. Some classes used by templates have no CSS yet (e.g. `.detail-list`, `.small-metric` in `agent-detail.html`) — defining them is a legitimate fix, removing them is not.

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
