---
timestamp: 2026-08-21 16:50 +07:00
commit: 025a0de
topic: Accessibility & polish (P3)
priority: P3
---

# Sprint — Accessibility & Polish

## Goal

Form dan status bisa dipahami semua orang & screen reader; interaksi destruktif jelas; empty state konsisten.

## Evidence

- Semua input di audit terdeteksi `missingAccessibleName` — `<label>` tidak ter-asosiasi dengan input (tidak ada `for`/`id`).
- Class `.detail-list` dan `.small-metric` dipakai `agent-detail.html` tapi tidak ada CSS-nya — `dl` tampil dengan default browser, nilai teks panjang (timestamp) dirender 32px mono hijau seperti metrik angka.
- Kontras: lime `#26734d` untuk angka metrik di atas panel krem `#fffdf8` perlu diverifikasi (WCAG AA teks besar).
- `confirm()` delete generik ("Delete monitor?") tidak menyebut nama objek.
- Empty state tidak konsisten: Groups punya CTA bagus, Monitors cuma baris teks "No monitors found."

## Scope

1. Asosiasi label: `for`/`id` di semua form (monitor-form, monitor-edit, agent-form, settings, setup, login, groups).
2. Definisikan `.detail-list` (grid dt/dd dengan border halus) dan `.small-metric` (ukuran teks, bukan 32px mono) — gotcha #7: mendefinisikan adalah fix yang sah, menghapus pemakaian bukan.
3. Audit kontras lime/red di atas panel; sesuaikan token bila gagal AA.
4. Copy confirm() menyebut nama objek: `Delete monitor "Marketing Site"?`
5. Samakan empty state Monitors/Incidents/Agents dengan pola Groups (judul + penjelasan + CTA).

## Out of scope

- Skip link & restructuring layout (ikut sprint nav bila perlu).
- Dark mode.

## Output / Acceptance

- [ ] Tidak ada input tanpa accessible name (cek snapshot aksesibilitas).
- [ ] Halaman agent-detail tampil rapi tanpa style default browser.
- [ ] Kontras teks lulus WCAG AA (teks besar ≥ 3:1, normal ≥ 4.5:1).
- [ ] Dialog hapus menyebut nama objek.
- [ ] `go test ./...` lulus.

## Verification

Snapshot aksesibilitas tiap form; hitung rasio kontras dari computed styles; uji keyboard-only navigation.
