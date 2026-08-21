---
timestamp: 2026-08-21 16:50 +07:00
commit: 03e611b
topic: Display live update & chart labels (P2)
priority: P2
---

# Sprint — Display Live Update & Chart Labels

## Goal

Wallboard `/display` tidak flicker saat refresh, dan grafik latensi di monitor detail terbaca.

## Evidence

- `/display` masih `<meta http-equiv="refresh" content="300">` — reload seluruh halaman tiap 5 menit (flicker di TV). Dashboard sudah punya SSE `/events` event `status` yang bisa dipakai ulang.
- Grafik latensi `monitor-detail.html`: label waktu rotate 90° ukuran mungil per bar, tumpang tindih dan tidak terbaca.

## Scope

1. Ganti meta refresh di `display.html` dengan SSE pola yang sama dengan dashboard: update hanya bagian DOM yang berubah (status chip, latency, last checked), fallback reload bila koneksi putus.
2. Grafik latensi:
   - hapus label per-bar;
   - satu caption rentang waktu ("09:00 – 09:50 · last 50 checks");
   - tooltip via atribut `title` sudah ada — pertahankan.

## Out of scope

- Chart library / canvas / SVG interaktif.
- Perubahan skema data checks.

## Output / Acceptance

- [ ] `/display` tidak melakukan full reload saat status berubah.
- [ ] Status down muncul di display ≤ 1 interval check setelah terjadi.
- [ ] Label waktu grafik terbaca dan tidak tumpang tindih.
- [ ] `go test ./...` lulus.

## Verification

Buka `/display`, matikan satu monitor target, amati update tanpa reload; screenshot grafik before/after.
