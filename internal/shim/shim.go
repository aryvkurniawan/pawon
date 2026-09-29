// Package shim: mode "berpura-pura jadi binary stack" untuk pawon.exe.
//
// Alias CLI (php.exe, composer.exe, mysql.exe, nginx.exe) dibuat dengan
// menyalin pawon.exe ke nama itu. Saat dijalankan dengan nama alias, binary
// ini tidak menjalankan panel — ia meneruskan argumen ke binary stack yang
// sesuai lalu keluar dengan exit code yang sama.
//
// Kenapa menyalin pawon.exe, bukan meng-embed shim terpisah: tidak ada binary
// kedua yang bisa usang terhadap layout bin/, tidak ada 3 MB ekstra di repo,
// dan panel + shim selalu satu versi.
//
// Kenapa .exe, bukan .bat: memanggil .bat dari .bat lain tanpa `call`
// mentransfer kontrol secara permanen — batch pemanggil berhenti setelah
// shim selesai. Shim .exe tidak punya semantik itu.
package shim

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Known melaporkan apakah nama (tanpa ekstensi) adalah alias shim yang
// dikenal. Dipakai main untuk memutuskan "jalankan panel atau jadi shim".
func Known(name string) bool {
	alias := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	if alias == "" {
		return false
	}
	switch alias {
	case "php", "composer", "mysql", "nginx":
		return true
	}
	// php<digit> → seri PHP spesifik (php81, php82, ...).
	return strings.HasPrefix(alias, "php") && isSeriesDigits(alias[3:])
}

// Run menjalankan alias dari dir shim. root adalah stack root (induk dua
// level di atas dir shim: <root>/bin/shim/). Mengembalikan false bila nama
// bukan alias yang dikenal — pemanggil lalu menjalankan panel seperti biasa.
//
// Tidak pernah kembali dalam keadaan normal: proses berakhir dengan exit
// code target, persis seperti memanggil binary itu langsung.
func Run(exePath string, args []string) bool {
	name := filepath.Base(exePath)
	if !Known(name) {
		return false
	}
	alias := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	root := filepath.Dir(filepath.Dir(filepath.Dir(exePath)))

	target, targs, err := resolve(root, alias, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pawon-shim:", err)
		os.Exit(1)
	}
	exit(execTarget(target, targs))
	return true
}

// resolve memetakan alias ke binary target + argumen. Versi PHP dipindai,
// bukan di-hardcode, supaya shim tidak perlu dibangun ulang saat pin naik.
func resolve(root, alias string, args []string) (string, []string, error) {
	phpHome := filepath.Join(root, "bin", "php")

	// php → seri tertinggi yang terpasang; php<seri> → seri itu persis.
	if alias == "php" || (strings.HasPrefix(alias, "php") && isSeriesDigits(alias[3:])) {
		series := ""
		if alias != "php" {
			series = insertDot(alias[3:]) // "81" → "8.1"
		} else {
			var err error
			if series, err = highestPHP(phpHome); err != nil {
				return "", nil, err
			}
		}
		exe := filepath.Join(phpHome, series, "php.exe")
		if _, err := os.Stat(exe); err != nil {
			return "", nil, fmt.Errorf("php %s belum terpasang (%s)", series, exe)
		}
		return exe, args, nil
	}

	switch alias {
	case "composer":
		series, err := highestPHP(phpHome)
		if err != nil {
			return "", nil, err
		}
		phar := filepath.Join(phpHome, "composer.phar")
		if _, err := os.Stat(phar); err != nil {
			return "", nil, fmt.Errorf("composer.phar belum terpasang (%s)", phar)
		}
		return filepath.Join(phpHome, series, "php.exe"),
			append([]string{phar}, args...), nil

	case "mysql":
		// Klien MariaDB. Argumen diteruskan apa adanya — mariadb.exe butuh
		// -p<password> menyatu, jadi tidak boleh di-quote.
		home, err := findUnder(filepath.Join(root, "bin", "mariadb"), filepath.Join("bin", "mariadb.exe"))
		if err != nil {
			return "", nil, err
		}
		return filepath.Join(home, "bin", "mariadb.exe"), args, nil

	case "nginx":
		home, err := findUnder(filepath.Join(root, "bin", "nginx"), "nginx.exe")
		if err != nil {
			return "", nil, err
		}
		// Prefix eksplisit, sama seperti panel: tanpa -p, nginx memakai cwd
		// untuk pid & temp files.
		return filepath.Join(home, "nginx.exe"),
			append([]string{"-p", filepath.ToSlash(home) + "/"}, args...), nil
	}
	return "", nil, fmt.Errorf("alias shim tidak dikenal: %s", alias)
}

// highestPHP: seri tertinggi di bin/php yang punya php.exe. Diurutkan
// numerik per komponen supaya 8.10 (kelak) benar di atas 8.9.
func highestPHP(phpHome string) (string, error) {
	entries, err := os.ReadDir(phpHome)
	if err != nil {
		return "", fmt.Errorf("baca %s: %w", phpHome, err)
	}
	best, bestNums := "", []int(nil)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(phpHome, e.Name(), "php.exe")); err != nil {
			continue
		}
		nums, ok := seriesNums(e.Name())
		if !ok {
			continue
		}
		if best == "" || lessSeries(bestNums, nums) {
			best, bestNums = e.Name(), nums
		}
	}
	if best == "" {
		return "", fmt.Errorf("tidak ada PHP terpasang di %s", phpHome)
	}
	return best, nil
}

// findUnder: subdirektori pertama (urut nama) yang memuat file rel.
func findUnder(root, rel string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("baca %s: %w", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), rel)); err == nil {
			return filepath.Join(root, e.Name()), nil
		}
	}
	return "", fmt.Errorf("tidak ada %s di %s", rel, root)
}

func isSeriesDigits(s string) bool {
	if s == "" || len(s) > 3 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// insertDot: "81" → "8.1", "101" → "10.1".
func insertDot(s string) string {
	if len(s) <= 2 {
		return s[:1] + "." + s[1:]
	}
	return s[:2] + "." + s[2:]
}

func seriesNums(s string) ([]int, bool) {
	var out []int
	for _, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

func lessSeries(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// execTarget menjalankan target dengan stdio mewarisi terminal pemanggil
// dan mengembalikan exit code-nya.
//
// Dir sengaja tidak diset: mengubah direktori kerja anak akan merusak skrip
// yang memakai path relatif (php artisan, require './x.php'). PHP menemukan
// php.ini & ext/ dari path executable-nya sendiri, jadi tidak perlu Dir.
func execTarget(target string, args []string) int {
	cmd := exec.Command(target, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errorsAs(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "pawon-shim:", err)
		return 1
	}
	return 0
}

func errorsAs(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

var exit = os.Exit
