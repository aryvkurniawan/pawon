# Pawon 🔥

Panel webserver untuk Windows: **nginx + PHP (multi versi) + MariaDB + Cloudflare Tunnel** dalam satu binary Go. Nambah website = isi form di panel → subdomain live lewat Cloudflare Tunnel dalam hitungan detik.

> *Pawon* (bhs. Jawa) = dapur — tempat semua "dimasak".

## Fitur

- **Panel web** (http://127.0.0.1:7080) — Dashboard status service, Sites, Tunnel, Settings.
- **Multi-PHP per site** — 1 pool php-cgi per versi (default 8.4), dropdown versi saat nambah site.
- **Cloudflare Tunnel** — remotely-managed: ingress & DNS diatur via API, tanpa restart cloudflared. Nambah site = tulis vhost + tambah ingress + CNAME, otomatis.
- **Alias lokal `*.test`** — tiap site bisa dibuka via `<sub>.<domain>` (tunnel) dan `<sub>.test` (lokal, tanpa tunnel).
- **phpMyAdmin bundel** — auto-site `pma.test`, login sekali-klik (kredensial root dari state).
- **Composer runner** — jalankan `create-project`/`require`/`install` dari panel (whitelist subcommand).
- **Log viewer** — php/nginx per-site/mariadb/cloudflared/laravel.log, tail + auto-refresh.
- **First-run otomatis** — panel download & extract semua binary ter-pin (nginx, PHP NTS, MariaDB, cloudflared, composer, phpMyAdmin).
- **Windows service** — `pawon.exe service install`; supervisor dengan Job Object (panel mati → semua anak mati) + auto-restart.

## Persyaratan

- Windows 10/11
- [Go 1.22+](https://go.dev/dl/) untuk build (atau pakai `pawon.exe` yang sudah ada)
- Koneksi internet untuk first-run download + Cloudflare API
- API token Cloudflare (scope: **Account → Cloudflare Tunnel:Edit**, **Zone → DNS:Edit**) — dipaste di halaman Tunnel

## Mulai Cepat

```powershell
# build
go build -o pawon.exe .

# jalankan (foreground)
.\pawon.exe

# atau sebagai Windows service
.\pawon.exe service install
```

1. Buka http://127.0.0.1:7080
2. Halaman **Tunnel** → paste API token Cloudflare → panel membuat tunnel `pawon` + menyiapkan connector.
3. Halaman **Sites** → Add Site: isi subdomain, pilih domain (zone), folder, tipe (php/laravel), versi PHP, centang "Create DB" kalau perlu.
4. Site live di `https://<sub>.<domain-kamu>` (via tunnel) dan `http://<sub>.test` (lokal).

### Laravel

- Tipe **laravel** otomatis memakai docroot `<folder>/public` + `try_files` sesuai kebutuhan Laravel.
- Tombol Composer di panel: `create-project laravel/laravel <folder>` atau `require` paket.
- **Create DB** membuat database + user dan menampilkan kredensial untuk `.env`.

## Struktur

```
pawon/
├─ pawon.exe              # binary panel (UI embedded)
├─ bin/                   # hasil auto-download first-run
│  ├─ nginx/  php/<ver>/  mariadb/  pma/  cloudflared.exe
├─ sites/                 # default root semua site
├─ nginx/conf/            # main.conf + sites.d/<hostname>.conf (digenerate panel)
└─ pawon-data/
   ├─ pawon.json          # state panel (jangan edit saat panel jalan)
   ├─ mysql/              # datadir MariaDB
   └─ logs/               # semua log: pawon, nginx-*, php-<ver>, mariadb, cloudflared
```

## Keamanan

- Panel bind `127.0.0.1` saja — tidak terekspos jaringan. Akses lan perlu tunnel/reverse-proxy sendiri.
- Token CF & password DB tersimpan plaintext di `pawon-data/pawon.json` (single-user homelab); upgrade path: DPAPI/Credential Manager.
- phpMyAdmin `pma.test` auto-login memakai kredensial root — hanya reachable lokal.

## Development

```powershell
go test ./...          # semua unit test
go vet ./...
# rebuild CSS (tailwind v4 standalone, tanpa npm):
tools\tailwindcss.exe -i web\tw.css -o web\static\app.css --minify
```

Dokumen desain: [`docs/superpowers/specs/2026-09-04-pawon-design.md`](docs/superpowers/specs/2026-09-04-pawon-design.md) · Plan implementasi: [`docs/superpowers/plans/2026-09-04-pawon.md`](docs/superpowers/plans/2026-09-04-pawon.md)

## Status / Catatan

- ⚠️ Smoke end-to-end (first-run download nyata + add site + pma + laravel) belum dijalankan di mesin fresh — unit test semua package hijau; verifikasi runtime menyusul.
- Fitur tunnel butuh paste token Cloudflare milikmu (tidak ada kredensial di repo).
- Lihat [CHANGELOG.md](CHANGELOG.md).

## License

MIT
