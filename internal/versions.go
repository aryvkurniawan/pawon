// Package versions memusatkan pin versi + URL download seluruh binary stack
// (spec §4: "versi & URL di satu file"). Update pin = edit file ini saja.
package versions

// Bin adalah satu entri download first-run.
type Bin struct {
	Name string // "nginx", "php-8.4", "mariadb", "cloudflared", "composer", "pma"
	URL  string
	Kind string // "zip" | "exe" | "phar"
	Dir  string // target relatif stack root; "" = file tunggal
}

// Pinned: semua binary di-download first-run oleh dl.Ensure; file/dir target
// yang sudah ada di-skip (idempoten).
var Pinned = []Bin{
	// Pin 2026-09-04: nginx stable 1.30.4 (https://nginx.org/en/download.html).
	{Name: "nginx", URL: "https://nginx.org/download/nginx-1.30.4.zip", Kind: "zip", Dir: "bin/nginx"},
	// Pin 2026-09-04: PHP 8.4 NTS x64, URL "latest" resmi windows.php.net
	// (resolusi saat pin: 8.4.25, build vs17). ponytail: URL latest selalu
	// mengikuti patch terbaru 8.4 — pin persis php-8.4.25-nts-Win32-vs17-x64.zip
	// bila butuh reproducibility ketat; cek ulang saat smoke.
	{Name: "php-8.4", URL: "https://windows.php.net/downloads/releases/latest/php-8.4-nts-Win32-vs17-x64-latest.zip", Kind: "zip", Dir: "bin/php/8.4"},
	// Pin 2026-09-04: MariaDB seri stabil 11.8.9 winx64 (12.3.3 = GA terbaru;
	// 11.8 dipilih: seri matang, kompatibilitas Laravel/MySQL terluas).
	{Name: "mariadb", URL: "https://archive.mariadb.org/mariadb-11.8.9/winx64-packages/mariadb-11.8.9-winx64.zip", Kind: "zip", Dir: "bin/mariadb"},
	// Pin 2026-09-04: URL "latest" resmi Cloudflare (resolusi saat pin: 2026.8.3).
	{Name: "cloudflared", URL: "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe", Kind: "exe", Dir: "bin/cloudflared.exe"},
	// Pin 2026-09-04: composer-stable.phar = alias resmi rilis stabil terkini.
	{Name: "composer", URL: "https://getcomposer.org/composer-stable.phar", Kind: "phar", Dir: "bin/php/composer.phar"},
	// Pin 2026-09-04: phpMyAdmin 5.2.3 (6.0 masih development).
	{Name: "pma", URL: "https://files.phpmyadmin.net/phpMyAdmin/5.2.3/phpMyAdmin-5.2.3-all-languages.zip", Kind: "zip", Dir: "bin/pma"},
}
