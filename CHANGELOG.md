# Changelog

Format mengikuti [Keep a Changelog](https://keepachangelog.com/id/1.1.0/); versi mengikuti [SemVer](https://semver.org/lang/id/).

## [0.2.0] - 2026-09-20

Enam bug dari issue #1–#6, enam bug setup tunnel dari issue #8, multi-PHP 8.1–8.4, dan mode lokal-saja.

**Catatan upgrade:** state lama otomatis termigrasi (PHP 8.4 dipetakan ke port kanonik 9400). Kalau panel dipakai sebagai Windows service, restart panel sekali setelah update supaya adapter Cloudflare terpasang untuk site berikutnya.

### Diubah
- `docs/` dan `pawon.exe~` tidak lagi dilacak git (masing-masing 12 MB dan dokumen kerja lokal); keduanya kini di `.gitignore`. File tetap ada di disk.
- `GET /api/zones` kini membalas `{zones, last_zone_id}` (sebelumnya array polos), dan aset UI dikirim dengan `Cache-Control: no-cache, must-revalidate` supaya `app.js` lama tidak tertinggal di browser.

### Ditambahkan
- Deteksi folder `sites/` + mode **lokal saja** (PR #10): `GET /api/sites/scan` melaporkan folder yang belum terdaftar (read-only, tidak auto-register — folder berisi `.env` tidak pernah terbit sendiri). Site lokal-saja cukup vhost + hosts, tanpa ingress/CNAME; zone jadi opsional dan yang terakhir dipakai diingat sebagai default.
- **Terbitkan** site lokal-saja (`POST /api/sites/{id}/publish`) tanpa hapus+daftar ulang, sehingga kredensial DB yang tersimpan di state tidak hilang. Ada tombolnya di tabel Sites.
- Multi-PHP: pin PHP 8.1.34, 8.2.33, 8.3.33, 8.4.25 (NTS x64) dengan port pool kanonik 9100/9200/9300/9400. Tiap seri punya folder + php.ini sendiri.
- Tombol **Buat DB** / **Reset DB** per site di halaman Sites (sebelumnya hanya saat create site).
- Panel menulis `pawon-data/logs/pawon.log` — sebelumnya menu "Panel (pawon.log)" di log viewer selalu 404.
- Validasi header `Host` (hanya `127.0.0.1:7080` / `localhost:7080`) + wajib `Content-Type: application/json` untuk endpoint mutasi.
- Validasi input `php` (harus versi terpasang & aktif), `type` (php|laravel), dan `root` (absolut, tanpa karakter yang bisa memutus quoting config nginx).
- Unit test untuk `versions` (port & URL pin), `seed` kanonik, `ensurePhpIni`, `panelLog`, guard Host/Content-Type, rollback `Add`, retry `writeAtomic`, dan idempotensi statement DB.

### Diperbaiki
- **#1** Add site yang gagal setelah validate meninggalkan vhost orphan yang sudah aktif di nginx (nginx ter-reload sebelum langkah hosts). Urutan diubah: vhost → validate → hosts → CF → reload → save, dengan rollback berurutan terbalik.
- **#2** `hosts.Add` gagal non-deterministik sebagai LocalSystem — rename tepat setelah write kalah race dengan filter driver/AV. Kini retry dengan backoff + fallback tulis langsung.
- **#3** "Buat database" dua kali menyimpan password baru di state sementara MariaDB masih memakai yang lama. Ditambah `ALTER USER ... IDENTIFIED BY`.
- **#4** Subdomain duplikat pada zone sama membuat vhost saling menimpa dan site kedua kehilangan vhost saat yang pertama dihapus. Hostname duplikat kini ditolak.
- **#5** Panel tanpa validasi `Host` rentan DNS rebinding (bind loopback tidak menutupnya).
- **#6** `php`/`root` dari request masuk mentah ke config nginx; injeksi terbukti lolos `nginx -t` di level renderer.
- **#8** Setup tunnel Cloudflare dari UI tidak pernah bisa berhasil — enam bug sekaligus: `Setup()` mengabaikan parameter token (memakai klien yang di-wire saat boot dengan token kosong), `cf.Tunnel.Connections` bertipe `int` padahal API mengembalikan array, endpoint config memakai `/configuration` (singular, 404) alih-alih `/configurations`, `TunnelToken` memakai POST padahal Cloudflare hanya menerima GET, `server_names_hash_bucket_size` tidak diset sehingga hostname panjang ditolak `nginx -t`, dan UI membaca `Connections` sebagai angka.
- php.ini per versi kini hanya memuat extension yang DLL-nya ada — `zip` di PHP 8.1 built-in, dan menulis `extension=zip` memunculkan warning tiap request.
- Adapter Cloudflare selalu terpasang, jadi setup tunnel dari UI langsung berlaku untuk site berikutnya tanpa restart panel.

## [0.1.1] - 2026-09-04

Fix dari smoke end-to-end pertama (semua komponen lokal terverifikasi: first-run download, add/remove site, PHP via FastCGI, Laravel + composer, phpMyAdmin, supervisor).

### Diperbaiki
- `pawon-data/` dibuat sebelum `mariadb-install-db` (fresh init sebelumnya FATAL).
- `fastcgi_params` + `mime.types` dikopi ke conf dir panel — nginx gagal start tanpa itu.
- Nama worker php-cgi dibuat unik (`php-8.4-1..4`) — sebelumnya saling menimpa, hanya 1 pool yang hidup.
- php.ini generated kini menyertakan `openssl`, `pdo_sqlite`, `sqlite3` — composer & Laravel fresh jalan out-of-the-box.
- Guard `/api/tunnel/status` saat tunnel belum di-setup (tidak lagi menembak CF dengan kredensial kosong) + pesan error API tampil benar di UI.

### Diubah
- README: status smoke diperbarui.

## [0.1.0] - 2026-09-04

Rilis awal — panel webserver Windows (Go, satu binary, UI embedded).

### Ditambahkan
- State store `pawon.json` (load/save atomik, mutasi site).
- Supervisor proses child + Windows Job Object (`KILL_ON_JOB_CLOSE`) + auto-restart backoff.
- Downloader first-run + pin versi: nginx 1.30.4, PHP 8.4 NTS (resolusi 8.4.25), MariaDB 11.8.9, cloudflared 2026.8.3, composer stable, phpMyAdmin 5.2.3.
- Generator config nginx: main.conf + upstream pool per versi PHP + vhost per site; gate `nginx -t` + rollback.
- Hosts file manager (idempoten, marker `# pawon`).
- MariaDB: init datadir, create database/user/grant, set root password otomatis di fresh init.
- Cloudflare API client (accounts, zones, tunnel, ingress config GET-modify-PUT, DNS CNAME) + helper ingress idempoten.
- Orkestrasi add/remove site: vhost → validate/reload → hosts → ingress+CNAME.
- Tunnel remotely-managed: setup dari API token, wire/unwire site tanpa restart cloudflared.
- HTTP API panel: status, services start/stop/restart, sites CRUD, zones, tunnel setup/status, composer stream (whitelist), .env editor, create DB, log tail.
- UI 4 halaman (Dashboard/Sites/Tunnel/Settings) — Tailwind v4 standalone CLI, tanpa npm/node.
- phpMyAdmin bundel sebagai `pma.test` (auto-config, login-less).
- Alias lokal `*.test` + hosts otomatis.
- Windows service mode (`service install|remove`) + graceful shutdown.
