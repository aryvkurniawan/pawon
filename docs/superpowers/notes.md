# Notes

## Task 11 — UI (4 halaman + Tailwind build)
- `web/static/` slot di `internal/server/server.go` masih `http.NotFound` — **swap ke `embed.FS` dikerjakan Task 12** (func `static()`, ganti dengan `http.FileServer(http.FS(web))` atas `//go:embed web` di main/server).
- Semua class Tailwind di-generate dari `web/` saja (`@source "./"` di `web/tw.css`); build: `tools/tailwindcss.exe -i web/tw.css -o web/static/app.css --minify` (v4.3.3).
- PHP dropdown (form site + log viewer + settings) didapat dari nama service `php-*` di `/api/status` — tidak ada endpoint versi terpisah.
- **Gap API dari spec §9** (tidak ada di handlers.go, UI menampilkan path/info saja):
  - Password root MariaDB tidak diekspos endpoint mana pun → Settings menunjukkan lokasinya (`pawon-data/pawon.json` → `db.root_password`).
  - "Buka folder log" tidak punya endpoint (spec menyebut `+ buka folder log` di `/api/logs`) → diganti tombol Salin Path + link Log Viewer. Kalau mau tombol buka explorer sungguhan, tambahkan endpoint kecil di Task 12/13.
- `proc.Status` & `cf.Tunnel` tidak punya json tag → key JSON kapital (`Name`, `Running`, `ID`, `Status`, `Connections`) — app.js mengikuti itu.
