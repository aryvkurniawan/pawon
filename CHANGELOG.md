# Changelog

Format mengikuti [Keep a Changelog](https://keepachangelog.com/id/1.1.0/); versi mengikuti [SemVer](https://semver.org/lang/id/).

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
