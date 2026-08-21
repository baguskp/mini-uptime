---
timestamp: 2026-08-21 16:50 +07:00
commit: 95e54f7
topic: Navigation state & dashboard hierarchy (P1)
priority: P1
---

# Sprint — Navigation State & Dashboard Hierarchy

## Goal

User selalu tahu halaman aktif, nav mobile tidak makan ruang, dan dashboard membedakan metrik dari tautan.

## Evidence

- Nav tidak punya state aktif di semua halaman (desktop & mobile) — link Monitors tampil identik saat berada di `/monitors`.
- Nav mobile wrap 2 baris + tombol Logout besar ≈ 150px vertikal.
- Dashboard: kartu angka (Total/Operational/Down) dan kartu tautan ("Manage checks", "Organize services", "Track downtime") tampil identik dalam satu grid 3 kolom; 7 kartu menyisakan kartu yatim di baris terakhir.

## Scope

1. `aria-current="page"` + styling aktif (underline/warna) pada nav. Ingat gotcha #2: nav dipatch `normalizeNavbar()` di `main.go` — state aktif harus di-set di sana atau via JS kecil, bukan hanya edit template.
2. Nav mobile: ringkas jadi satu baris scroll-horizontal atau wrap lebih rapat; Logout ikut serta.
3. Dashboard:
   - Baris metrik di atas (Total/Operational/Down) dengan aksen visual status.
   - Quick links jadi baris tautan biasa di bawah metrik, bukan kartu palsu.

## Out of scope

- Hamburger menu / JS navigation (overkill untuk 8 link).
- Perubahan route/handler.

## Output / Acceptance

- [ ] Halaman aktif terlihat jelas di nav (visual + `aria-current`).
- [ ] Nav mobile ≤ 1 baris logis, tidak mendominasi viewport.
- [ ] Dashboard: metrik dan tautan tidak lagi tampak sebagai kartu identik.
- [ ] `go test ./...` lulus (perhatikan test urutan nav di `main_test.go`).

## Verification

Screenshot dashboard + /monitors di mobile & desktop; cek atribut `aria-current` di DOM.
