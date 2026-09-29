# Pawon 🔥

Panel webserver untuk Windows: **nginx + PHP (multi versi) + MariaDB + Cloudflare Tunnel** dalam satu binary Go. Nambah website = isi form di panel → subdomain live lewat Cloudflare Tunnel dalam hitungan detik.

> *Pawon* (bhs. Jawa) = dapur — tempat semua "dimasak".

## Fitur

- **Panel web** (http://127.0.0.1:7080) — Dashboard status service, Sites, Tunnel, Settings.
- **Multi-PHP per site** — 4 pool php-cgi (PHP 8.1 / 8.2 / 8.3 / 8.4), dropdown versi saat nambah site. Tiap seri punya folder `bin/php/<ver>/` + php.ini sendiri, jadi site lama bisa tetap di 8.1 sementara site baru pakai 8.4.
- **Cloudflare Tunnel** — remotely-managed: ingress & DNS diatur via API, tanpa restart cloudflared. Nambah site = tulis vhost + tambah ingress + CNAME, otomatis.
- **Alias lokal `*.test`** — tiap site bisa dibuka via `<sub>.<domain>` (tunnel) dan `<sub>.test` (lokal, tanpa tunnel).
- **phpMyAdmin bundel** — auto-site `pma.test`, login sekali-klik (kredensial root dari state).
- **Composer runner** — jalankan `create-project`/`require`/`install` dari panel (whitelist subcommand).
- **Log viewer** — php/nginx per-site/mariadb/cloudflared/laravel.log, tail + auto-refresh.
- **First-run otomatis** — panel download & extract semua binary ter-pin (nginx, PHP NTS, MariaDB, cloudflared, composer, phpMyAdmin).
- **Windows service** — `pawon.exe service install`; supervisor dengan Job Object (panel mati → semua anak mati) + auto-restart.
- **Ikon baki sistem** — `pawon.exe tray`; warna ikon di pojok kanan bawah menunjukkan kesehatan stack, plus kontrol service dari menu klik-kanan.

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

### CLI dari terminal mana pun

Panel menempatkan alias shim di `bin/shim` dan mendaftarkannya ke PATH user,
jadi binary stack bisa dipanggil langsung:

```powershell
php -v              # PHP seri tertinggi yang terpasang
php81 -v            # seri tertentu (php81 / php82 / php83)
composer install    # composer.phar lewat PHP tertinggi
mysql -uroot -p     # klien MariaDB
nginx -s reload     # nginx milik panel (prefix-nya sudah benar)
```

Alias adalah salinan `pawon.exe` yang mendeteksi namanya sendiri lalu
meneruskan argumen ke binary yang sesuai, termasuk exit code-nya. PATH
diperbarui saat `service install` dan tiap boot panel — buka terminal baru
bila terminal lama belum melihatnya.


### Ikon baki sistem

```powershell
.\pawon.exe tray
```

Ikon muncul di pojok kanan bawah dan warnanya menunjukkan keadaan stack:

| Warna | Arti |
| --- | --- |
| 🟢 Hijau | semua service jalan |
| 🟡 Kuning | sebagian service jalan |
| 🔴 Merah | panel tidak merespons, atau belum ada service |

Klik-kanan ikon untuk membuka panel, menjalankan **Mulai / Berhenti /
Ulangi** per service, atau menyalakan/mematikan **Mulai saat login**.
Dobel-klik ikon = buka panel.

`service install` mendaftarkan tray ke Run key HKCU supaya ikon muncul
otomatis setelah login. Tray **tidak** dijalankan dari dalam service:
service hidup di Session 0 tanpa desktop, jadi ikonnya tidak akan pernah
terlihat — tray adalah proses terpisah di session pengguna yang memantau
panel lewat HTTP.

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
- Panel juga memvalidasi header `Host` (hanya `127.0.0.1:7080` / `localhost:7080`) dan mewajibkan `Content-Type: application/json` untuk endpoint mutasi. Tanpa keduanya, halaman web yang dikunjungi pengguna bisa memanggil API panel lewat DNS rebinding — bind loopback saja tidak menutup itu.
- Input `php`, `type`, dan `root` divalidasi sebelum masuk ke config nginx.
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

- ✅ Smoke end-to-end terverifikasi: first-run download, 6 service hijau, add/remove site (`*.test` 200 via FastCGI), Laravel via composer (`demo.test` 200), phpMyAdmin auto-login. Belum diverifikasi: setup tunnel (butuh paste token Cloudflare milik pengguna).
- ✅ Multi-PHP 8.1/8.2/8.3/8.4 terverifikasi: 16 worker php-cgi, tiap versi melayani `PHP_VERSION`-nya sendiri via `<sub>.test`.
- Fitur tunnel butuh paste token Cloudflare milikmu (tidak ada kredensial di repo).
- Lihat [CHANGELOG.md](CHANGELOG.md).

## License

MIT
