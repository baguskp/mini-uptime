---
timestamp: 2026-08-21 16:50 +07:00
commit: 9485834
topic: Responsive tables (mobile P0)
priority: P0
---

# Sprint — Responsive Tables

## Goal

Semua tabel data bisa dibaca dan semua aksi terlihat di layar ≤ 700px tanpa scroll horizontal di dalam panel.

## Evidence

Audit 21 Aug 2026 (viewport 390×844):

- `/monitors` — tabel terpotong di kolom Group; Interval/State/Actions (Edit/Pause/Delete) tersembunyi di balik horizontal scroll. `docs/screenshots/mobile-monitors-overflow.jpg`
- `/dashboard` — tabel Monitor status terpotong; kolom Latency harus di-scroll. `docs/screenshots/mobile-dashboard-table-overflow.jpg`
- `/incidents` — kolom ERROR terpotong mid-word. `docs/screenshots/mobile-incidents-overflow.jpg`
- `/agents` — tabel 8 kolom, pola sama (belum difoto, struktur identik).

Root cause: tidak ada strategi responsive untuk `<table>`; kolom Actions berisi 3 tombol inline (`td.actions`).

## Scope

1. Pola reflow-to-cards di bawah 700px untuk list dengan row actions:
   - `/monitors` — nama+target full-width, chip status, baris aksi di bawah.
   - `/agents` — sama; kolom CPU/Memory/Latency jadi teks kecil atau disembunyikan.
2. Tabel tanpa row actions (`/incidents`, dashboard Monitor status, Recent checks):
   - bungkus `.table-wrap{overflow-x:auto}` sebagai fallback, plus sembunyikan kolom prioritas rendah di mobile.
3. Form search `/monitors`: input ditumpuk vertikal di mobile (sekarang placeholder terpotong "Search name or t…").

## Out of scope

- Restrukturisasi CSS menyeluruh (sprint terpisah bila diperlukan).
- Perubahan backend/handler.

## Output / Acceptance

- [ ] 390px & 320px: tidak ada horizontal scroll pada 4 halaman tabel.
- [ ] Edit/Pause/Delete (dan Revoke) terlihat & bisa disentuh tanpa scroll dalam panel.
- [ ] Desktop (1440px) tidak berubah visualnya.
- [ ] `go test ./...` lulus.

## Verification

Screenshot before/after per breakpoint (mobile/tablet/desktop) tiap halaman; bandingkan dengan `docs/screenshots/`.
