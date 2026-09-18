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

// PHPVersion adalah satu seri PHP yang didukung panel.
type PHPVersion struct {
	Series string // "8.1"
	Patch  string // "8.1.34"
	Toolset string // "vs16" | "vs17" — penanda build, bukan syarat runtime
}

// PHPSeries: seri PHP yang diinstal first-run, urut naik. Sumber tunggal
// kebenaran untuk pin download, seed state, dan urutan port pool.
//
// Patch = rilis final tiap seri saat pin. 8.1/8.2/8.3 sudah EOL sehingga
// angkanya tidak akan bergerak lagi; 8.4 masih menerima patch, jadi naikkan
// manual di sini saat mau ikut rilis terbaru.
var PHPSeries = []PHPVersion{
	{Series: "8.1", Patch: "8.1.34", Toolset: "vs16"},
	{Series: "8.2", Patch: "8.2.33", Toolset: "vs16"},
	{Series: "8.3", Patch: "8.3.33", Toolset: "vs16"},
	{Series: "8.4", Patch: "8.4.25", Toolset: "vs17"},
}

// PortBaseFor: port dasar pool php-cgi untuk seri ke-i = 9100 + 100×i
// (design §7). Determinstik dari urutan PHPSeries, jadi state dan upstream
// nginx tidak mungkin desync.
func PortBaseFor(series string) (int, bool) {
	for i, v := range PHPSeries {
		if v.Series == series {
			return 9100 + 100*i, true
		}
	}
	return 0, false
}

// PHPPins membangun entri download PHP dari PHPSeries.
func PHPPins() []Bin {
	out := make([]Bin, 0, len(PHPSeries))
	for _, v := range PHPSeries {
		out = append(out, Bin{
			Name: "php-" + v.Series,
			URL: "https://downloads.php.net/~windows/releases/archives/php-" + v.Patch +
				"-nts-Win32-" + v.Toolset + "-x64.zip",
			Kind: "zip",
			Dir:  "bin/php/" + v.Series,
		})
	}
	return out
}

var Pinned = buildPinned()

func buildPinned() []Bin {
	bins := []Bin{
		// Pin 2026-09-04: nginx stable 1.30.4 (https://nginx.org/en/download.html).
		{Name: "nginx", URL: "https://nginx.org/download/nginx-1.30.4.zip", Kind: "zip", Dir: "bin/nginx"},
	}
	// PHP multi-versi (8.1–8.4), NTS x64, URL arsip ber-versi persis.
	//
	// Kenapa arsip, bukan "latest": 8.1/8.2/8.3 sudah EOL, dan windows.php.net
	// hanya menyediakan URL latest untuk seri yang masih didukung — permintaan
	// php-8.1-...-latest.zip sekarang 404. Arsip di downloads.php.net menyimpan
	// rilis final tiap seri (8.1.34, 8.2.33, 8.3.33) secara permanen.
	//
	// Toolchain: 8.1–8.3 dibangun dengan VS16 (VC++ 2019), 8.4+ dengan VS17
	// (VC++ 2022). Keduanya jalan di VC++ 2015-2022 x64 redistributable yang
	// sudah satu paket di Windows 10/11 modern.
	bins = append(bins, PHPPins()...)
	return append(bins,
		// Pin 2026-09-04: MariaDB seri stabil 11.8.9 winx64 (12.3.3 = GA terbaru;
		// 11.8 dipilih: seri matang, kompatibilitas Laravel/MySQL terluas).
		Bin{Name: "mariadb", URL: "https://archive.mariadb.org/mariadb-11.8.9/winx64-packages/mariadb-11.8.9-winx64.zip", Kind: "zip", Dir: "bin/mariadb"},
		// Pin 2026-09-04: URL "latest" resmi Cloudflare (resolusi saat pin: 2026.8.3).
		Bin{Name: "cloudflared", URL: "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe", Kind: "exe", Dir: "bin/cloudflared.exe"},
		// Pin 2026-09-04: composer-stable.phar = alias resmi rilis stabil terkini.
		Bin{Name: "composer", URL: "https://getcomposer.org/composer-stable.phar", Kind: "phar", Dir: "bin/php/composer.phar"},
		// Pin 2026-09-04: phpMyAdmin 5.2.3 (6.0 masih development).
		Bin{Name: "pma", URL: "https://files.phpmyadmin.net/phpMyAdmin/5.2.3/phpMyAdmin-5.2.3-all-languages.zip", Kind: "zip", Dir: "bin/pma"},
	)
}
