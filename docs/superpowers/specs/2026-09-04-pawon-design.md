# Pawon — Desain & Spec

Tanggal: 2026-09-04
Status: Draft untuk review

## 1. Tujuan

**Pawon** — panel web UI (localhost) untuk mengelola webserver stack di Windows: **nginx + PHP + MariaDB + Cloudflare Tunnel**. Use case utama: nambah website = isi form (subdomain, folder, opsi Laravel/DB) → site live di `https://<subdomain>.<domain-kamu>` lewat Cloudflare Tunnel dalam hitungan detik, tanpa edit config manual.

Target user: pemilik mesin (homelab/self-host), satu akun Cloudflare. Bukan produk multi-user.

## 2. Non-Goals (v1)

- Multi-user / auth panel (bind `127.0.0.1` saja, no auth).
- Reverse proxy ke app non-PHP (Node/Python) — arsitektur mengakomodasi (ingress bisa point ke port lain), tapi UI/flow-nya v2.
- SSL lokal (Let's Encrypt di sisi nginx) — SSL ditangani Cloudflare di edge; koneksi cloudflared→nginx adalah plaintext HTTP lokal.
- Linux/macOS support.

## 3. Arsitektur

```
pawon.exe (Go, web UI di http://127.0.0.1:7080)
 ├─ supervise: nginx.exe        listen :80, vhost per site (conf/sites.d/*.conf)
 ├─ supervise: php-cgi.exe     1 pool (4 instance) per versi PHP — 127.0.0.1:9100+, 9200+, ...
 ├─ supervise: mariadbd.exe     listen :3306, datadir <stack>/pawon-data/mysql
 └─ supervise: cloudflared.exe  `tunnel run --token <tunnel-token>` (remotely-managed)
```

- **pawon.exe** = satu binary Go. UI statis (HTML + vanilla JS + Tailwind CSS) di-embed via `embed.FS`. Tailwind dikompile jadi satu `app.css` saat build pakai standalone CLI (satu exe, tanpa npm/node) — runtime tetap dependency-free, tanpa CDN. Tanpa framework web JS.
- **Supervisi**: panel spawn semua service sebagai child process di dalam satu *Windows Job Object* dengan `KILL_ON_JOB_CLOSE` — panel mati → semua anak ikut mati (tidak ada proses yatim). Restart otomatis dengan backoff (3× cepat, lalu 30s) per service. Panel sendiri bisa jalan sebagai Windows Service (`pawon.exe service install`) via `golang.org/x/sys/windows/svc`, atau foreground di console.
- **State**: satu file `pawon-data/pawon.json` (lihat §5). Vhost nginx digenerate dari state. Tanpa database untuk panel.
- **Dependensi Go**: stdlib + `golang.org/x/sys` (job object, service) + `go-sql-driver/mysql` untuk MariaDB (§8). CF API dipanggil langsung via `net/http` (tanpa SDK).

## 4. Layout Folder

```
pawon/
├─ pawon.exe
├─ bin/                    # hasil auto-download first-run
│  ├─ nginx/               # conf/nginx.conf, conf/sites.d/*.conf
│  ├─ php/<ver>/           # php-cgi.exe + php.ini per versi; composer.phar di bin/php/
│  ├─ mariadb/
│  └─ cloudflared.exe
├─ sites/                  # default root semua site
│  └─ <site-name>/         # atau folder Laravel yang sudah ada
└─ pawon-data/
   ├─ pawon.json           # state
   ├─ mysql/               # datadir MariaDB
   └─ logs/                # stdout/stderr semua service + access/error log nginx
```

First-run: panel download & extract semua binary dari URL ter-pin (versi & URL di satu file `internal/versions.go`): nginx (nginx.org zip), PHP NTS x64 (windows.php.net), MariaDB zip (archive.mariadb.org), cloudflared (`github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe`), composer.phar (getcomposer.org). Kalau file sudah ada, skip. `ponytail:` tanpa checksum verifikasi — sumber HTTPS resmi; tambah sha256 pin kalau mau strict.

## 5. State: `pawon.json`

```jsonc
{
  "cloudflare": {
    "api_token": "...",            // scope: Account→Tunnel:Edit, Zone→DNS:Edit
    "account_id": "...",           // terisi dari GET /accounts saat setup
    "tunnel_id": "...",            // dibuat panel: name "pawon", config_src=cloudflare
    "tunnel_token": "...",         // dari POST .../cfd_tunnel/{id}/token, dipakai cloudflared
    "zones": [{ "id": "...", "name": "domainkamu.com" }]
  },
  "sites": [
    {
      "id": "site-a1b2",           // id pendek random
      "subdomain": "app",
      "zone_id": "...",
      "hostname": "app.domainkamu.com",
      "root": "C:/pawon/sites/app",        // root fisik
      "docroot": "C:/pawon/sites/app/public", // = root, atau root+"/public" (laravel)
      "php": "8.4",                         // versi pool FastCGI yang dipakai site
      "type": "php" | "laravel",
      "db": { "name": "app", "user": "app", "password": "..." } | null,
      "nginx_conf": "pawon-data/generated/app.domainkamu.com.conf",
      "dns_record_id": "...",      // id CNAME di CF, untuk hapus site
      "created_at": "..."
    }
  ],
  "services": { "php": { "enabled": true }, ... }  // enable/disable manual per service
}
```

## 6. Cloudflare Integration (inti)

Tunnel bersifat **remotely-managed** (`config_src=cloudflare`): ingress rules tersimpan di Cloudflare, cloudflared auto-follow tanpa restart. Semua via REST API `api.cloudflare.com/client/v4`, langsung `net/http`.

**Setup sekali (halaman Tunnel):**
1. User paste API token → panel `GET /accounts` (ambil account pertama) & `GET /zones?per_page=50` (untuk dropdown domain).
2. Panel `POST /accounts/{id}/cfd_tunnel` body `{"name":"pawon","config_src":"cloudflare"}` → `tunnel_id`.
3. Panel `POST /accounts/{id}/cfd_tunnel/{tunnel_id}/token` → `tunnel_token` (disimpan state, dipakai argumen `--token` cloudflared).
4. Ingress awal: hanya catch-all `{"service":"http_status:404"}`.
5. Panel spawn cloudflared. Status konektor dicek via `GET /accounts/{id}/cfd_tunnel/{id}` (field `connections` / `status`).

**Add site (kunci UX):**
1. Tulis vhost → `nginx -t` → `nginx -s reload`.
2. `GET /accounts/{id}/cfd_tunnel/{tid}/configuration` → append ingress `{"hostname":"app.domainkamu.com","service":"http://localhost:80"}` di depan catch-all → `PUT` configuration (endpoint ini *replace* seluruh list — wajib GET-modify-PUT; request bersamaan diserialisasi mutex di panel).
3. `POST /zones/{zone_id}/dns_records` → `{"type":"CNAME","name":"app","content":"{tunnel_id}.cfargotunnel.com","proxied":true}`. Wajib proxied (orange-cloud) — itu mekanisme routing tunnel.

Semua hostname berakhir di `http://localhost:80` (satu port); nginx memutuskan site lewat `server_name`. **Tidak ada 1-port-per-subdomain** — port khusus hanya untuk kasus proxy app non-PHP (v2).

**Remove site:** kebalikan — hapus ingress (GET-modify-PUT), `DELETE /zones/{zone}/dns_records/{dns_record_id}`, hapus vhost + reload. Folder site tidak dihapus (manual).

**Error CF:** response `{success:false, errors:[...]}` ditampilkan mentah-mentah di UI (kode + message) — jangan ditelan.

## 7. nginx & PHP

- `bin/nginx/conf/nginx.conf` statis: `include sites.d/*.conf`. Upstream pool **digenerate per versi PHP**: `upstream php_84_pool { server 127.0.0.1:9100; 9101; 9102; 9103; }` — port_base = 9100 + 100×indeks versi, tercatat di state `php_versions`.
- Vhost per site (`sites.d/<hostname>.conf`), satu template, root = `docroot`:
  ```nginx
  server {
      listen 80;
      server_name app.domainkamu.com app.test;
      root "C:/pawon/sites/app/public";
      index index.php index.html;
      location / { try_files $uri $uri/ /index.php?$query_string; }
      location ~ \.php$ {
          try_files $fastcgi_script_name =404;
          include fastcgi_params;
          fastcgi_pass php_84_pool;   # nama pool dari site.php_version
          fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
      }
  }
  ```
  Template sama untuk php & laravel — bedanya cuma `docroot` dan nama pool. (Laravel perlu `try_files ... /index.php?$query_string` — sudah ada di atas.)
- **Alias lokal**: tiap vhost dapat `server_name <sub>.<domain> <sub>.test` (`.test` = TLD RFC 2606 khusus testing, tak mungkin bentrok domain asli). Panel (jalan sebagai service, LocalSystem) menambah/menghapus baris `127.0.0.1 <sub>.test` di `drivers/etc/hosts` saat add/remove site — test lokal jalan tanpa tunnel. `.test` tidak di-resolve browser sendiri (beda dengan `.localhost`), jadi kalau hosts terkunci AV → warning di UI.
- **Multi-PHP**: per versi = folder `bin/php/<ver>/` (build NTS + php.ini, extension Laravel: pdo_mysql, mysqli, mbstring, openssl, fileinfo, gd, zip, intl, curl, sodium, exif) + 4 instance `php-cgi.exe -b 127.0.0.1:<port>` dengan `PHP_FCGI_MAX_REQUESTS=500` — deterministic, menghindari kelemahan PHP_FCGI_CHILDREN di Windows. Versi ter-pin di `versions.go` (default 8.4; 8.3/8.2/8.1 opsional). Dropdown versi di form site = versi terinstal.
- Reload nginx SELALU lewat `nginx -t` dulu; kalau gagal, config baru dibatalkan (file lama dipulihkan), error ditampilkan di UI.

## 8. MariaDB

- First-run MariaDB: `mariadb-install-db.exe --datadir=...` → password root acak disimpan di state.
- `mariadbd.exe --datadir=... --port=3306 --console` sebagai supervised child.
- "Create DB" per site: satu koneksi `database/sql` + driver `go-sql-driver/mysql` (satu-satunya dep Go non-x/sys) → `CREATE DATABASE` + `CREATE USER ... IDENTIFIED BY` + `GRANT ALL`. Kredensial ditampilkan sekali di UI (untuk `.env` Laravel).
- **phpMyAdmin**: bundel sebagai tool internal — downloader fetch phpMyAdmin zip, panel auto-buat site `pma.test` (vhost + hosts) dengan `config.inc.php` `auth_type=config` memakai kredensial root dari state → buka langsung masuk, tanpa login form.

## 9. Panel API (internal, konsumsi UI)

```
GET    /api/status                    # status semua service (running/stopped/error, pid, uptime)
POST   /api/services/{name}/start|stop|restart
GET    /api/sites                     POST /api/sites        DELETE /api/sites/{id}
GET    /api/zones                     # list zone dari CF (cache)
POST   /api/tunnel/setup              # body: api_token → jalankan flow §6 setup
GET    /api/tunnel/status             # status konektor + list ingress aktif
POST   /api/sites/{id}/composer       # body: {args:[...]} → php composer.phar, stream output
GET/PUT /api/sites/{id}/env     # baca/tulis file .env di root site (editor textarea)
POST   /api/dbs                       # body: {site_id} → create db+user (§8)
GET    /api/logs/{service}?tail=200   # baca file log di pawon-data/logs
```

Bind `127.0.0.1:7080`. UI: 4 halaman (Dashboard, Sites, Tunnel, Settings) — HTML+JS statis, fetch ke API di atas.

## 10. Error Handling

- **Port bentrok** (80, 3306, 7080, port pool PHP 9100+): dicek saat startup & sebelum start service; pesan menyebut proses pemilik (netstat).
- **nginx config invalid**: gate `nginx -t` (§7); config lama dipulihkan otomatis.
- **CF API error**: ditampilkan di UI + log; state site ditandai `dns_ok`/`ingress_ok` per langkah supaya bisa di-retry tanpa duplikat (ingress di-match by hostname; DNS di-query by name sebelum create).
- **Child mati**: restart backoff (§3); status di Dashboard merah + alasan exit terakhir.
- **Download first-run gagal**: retry per-binary, tidak blokir binary yang sudah ada.

## 11. Keamanan

- Panel bind loopback; CF token plaintext di `pawon.json` (`ponytail:` homelab single-user; upgrade path: DPAPI/Credential Manager).
- Token scope minimal: `Account → Cloudflare Tunnel:Edit` + `Zone → DNS:Edit` (Zone Resources: All zones).
- cloudflared→nginx plaintext HTTP lokal — tidak meninggalkan mesin.
- Panel TIDAK mengekspos endpoint eksekusi arbitrary selain composer (argumen dibatasi subcommand whitelist: `create-project`, `require`, `install`, `update`, `dump-autoload`).

## 12. Testing

- **Unit (Go test)**: generator vhost & nginx.conf, builder ingress (GET-modify-PUT payload, idempoten), mutasi state (add/remove site), serialisasi konkuren. Murni fungsi — tanpa I/O Windows.
- **Smoke manual** (mesin user): first-run download → tunnel setup → add site php statis → buka `https://sub.domainkamu.com` → add site Laravel (composer create-project + DB) → buka → remove site → verifikasi DNS & ingress hilang.
- CF API client diabstraksi interface kecil (`cfClient`) supaya unit test bisa fake (tanpa hit API asli).

## 13. Milestone Implementasi

1. **Skeleton**: Go module, embed UI, `pawon.json` state, supervisor + Job Object, service install, halaman Dashboard status.
2. **Bootstrap stack**: downloader (§4), mariadb init, php/nginx siap start, start/stop service dari UI.
3. **Sites lokal**: add/remove site (vhost + reload + nginx -t), site PHP statis jalan via `localhost` header test.
4. **Tunnel**: CF client (zones/accounts/tunnel/config/dns), halaman Tunnel setup, wiring add-site → ingress + CNAME.
5. **Laravel & DB**: composer runner, create DB/user, template docroot public.
6. **Multi-PHP & tools**: pool per versi + dropdown versi, bundel phpMyAdmin, alias *.test + hosts file, .env editor.
7. **Polish**: log viewer, retry/health, error surface.

Setiap milestone harus berakhir di keadaan jalan (panel bisa di-start), commit per milestone.
