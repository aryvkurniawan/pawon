package shim

import (
	"os"
	"path/filepath"
	"testing"
)

// stack membuat layout bin/ tiruan: php/<seri>/php.exe, mariadb/<ver>/bin,
// nginx/<ver>/. Dipakai untuk menguji resolve tanpa download sungguhan.
func stack(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	phpDir := filepath.Join(root, "bin", "php")
	for _, s := range []string{"8.1", "8.2", "8.4"} {
		if err := os.MkdirAll(filepath.Join(phpDir, s), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(phpDir, s, "php.exe"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(phpDir, "composer.phar"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	maria := filepath.Join(root, "bin", "mariadb", "mariadb-11.8.9-winx64", "bin")
	nginx := filepath.Join(root, "bin", "nginx", "nginx-1.30.4")
	for _, d := range []string{maria, nginx} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(maria, "mariadb.exe"), filepath.Join(nginx, "nginx.exe")} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestKnown(t *testing.T) {
	yes := []string{"php.exe", "php81.exe", "php82.exe", "composer.exe", "mysql.exe", "nginx.exe", "PHP.EXE"}
	for _, n := range yes {
		if !Known(n) {
			t.Errorf("Known(%q) = false, want true", n)
		}
	}
	// Nama panel & nama acak tidak boleh dianggap alias.
	for _, n := range []string{"pawon.exe", "pawon", "code.exe", "phpfull.exe", "phpxy.exe", ""} {
		if Known(n) {
			t.Errorf("Known(%q) = true, want false", n)
		}
	}
}

// TestResolvePHP: `php` → seri tertinggi yang terpasang; php<seri> → seri itu.
func TestResolvePHP(t *testing.T) {
	root := stack(t)
	exe, args, err := resolve(root, "php", []string{"-v"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "bin", "php", "8.4", "php.exe")
	if exe != want {
		t.Errorf("php → %s, want %s", exe, want)
	}
	// Argumen diteruskan apa adanya.
	if len(args) != 1 || args[0] != "-v" {
		t.Errorf("args = %v, want [-v]", args)
	}

	exe, _, err = resolve(root, "php81", nil)
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(root, "bin", "php", "8.1", "php.exe")
	if exe != want {
		t.Errorf("php81 → %s, want %s", exe, want)
	}
}

// TestResolvePHPMissing: seri yang belum ter-download harus error jelas,
// bukan diam-diam menjalankan versi lain.
func TestResolvePHPMissing(t *testing.T) {
	root := stack(t)
	if _, _, err := resolve(root, "php83", nil); err == nil {
		t.Error("php83 (tidak terpasang) harus error")
	}
}

func TestResolveComposer(t *testing.T) {
	root := stack(t)
	exe, args, err := resolve(root, "composer", []string{"install"})
	if err != nil {
		t.Fatal(err)
	}
	wantExe := filepath.Join(root, "bin", "php", "8.4", "php.exe")
	if exe != wantExe {
		t.Errorf("composer exe = %s, want %s", exe, wantExe)
	}
	wantArgs := []string{filepath.Join(root, "bin", "php", "composer.phar"), "install"}
	if len(args) != 2 || args[0] != wantArgs[0] || args[1] != wantArgs[1] {
		t.Errorf("composer args = %v, want %v", args, wantArgs)
	}
}

func TestResolveMySQL(t *testing.T) {
	root := stack(t)
	exe, args, err := resolve(root, "mysql", []string{"-uroot"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "bin", "mariadb", "mariadb-11.8.9-winx64", "bin", "mariadb.exe")
	if exe != want {
		t.Errorf("mysql → %s, want %s", exe, want)
	}
	// -p<password> tidak boleh dipecah/di-quote.
	if len(args) != 1 || args[0] != "-uroot" {
		t.Errorf("args = %v, want [-uroot]", args)
	}
}

// TestResolveNginx: prefix eksplisit, tanpa -p nginx memakai cwd untuk pid
// & temp files.
func TestResolveNginx(t *testing.T) {
	root := stack(t)
	exe, args, err := resolve(root, "nginx", []string{"-s", "reload"})
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "bin", "nginx", "nginx-1.30.4")
	if exe != filepath.Join(home, "nginx.exe") {
		t.Errorf("nginx exe = %s", exe)
	}
	if len(args) != 4 || args[0] != "-p" || args[2] != "-s" || args[3] != "reload" {
		t.Errorf("nginx args = %v, want [-p <prefix> -s reload]", args)
	}
}

func TestResolveUnknown(t *testing.T) {
	root := stack(t)
	if _, _, err := resolve(root, "phpfull", nil); err == nil {
		t.Error("alias tidak dikenal harus error")
	}
}

// TestHighestPHP: urutan numerik per komponen — 8.10 harus di atas 8.9,
// yang salah kalau diurutkan sebagai string.
func TestHighestPHP(t *testing.T) {
	root := t.TempDir()
	phpDir := filepath.Join(root, "bin", "php")
	for _, s := range []string{"8.9", "8.10", "9.1"} {
		if err := os.MkdirAll(filepath.Join(phpDir, s), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(phpDir, s, "php.exe"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := highestPHP(phpDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "9.1" {
		t.Errorf("highestPHP = %s, want 9.1", got)
	}
}

// TestHighestPHPSkipsEmpty: folder tanpa php.exe diabaikan (mis. ekstraksi
// yang gagal di tengah).
func TestHighestPHPSkipsEmpty(t *testing.T) {
	root := t.TempDir()
	phpDir := filepath.Join(root, "bin", "php")
	if err := os.MkdirAll(filepath.Join(phpDir, "8.4"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(phpDir, "8.5"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(phpDir, "8.4", "php.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := highestPHP(phpDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "8.4" {
		t.Errorf("highestPHP = %s, want 8.4 (8.5 tanpa php.exe diabaikan)", got)
	}
}

// TestHighestPHPMissingDir: bin/php belum ada → error, bukan panic.
func TestHighestPHPMissingDir(t *testing.T) {
	if _, err := highestPHP(filepath.Join(t.TempDir(), "bin", "php")); err == nil {
		t.Error("bin/php tidak ada harus error")
	}
}

func TestInsertDot(t *testing.T) {
	cases := map[string]string{"81": "8.1", "82": "8.2", "101": "10.1", "90": "9.0"}
	for in, want := range cases {
		if got := insertDot(in); got != want {
			t.Errorf("insertDot(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRunNotAlias: Run mengembalikan false (dan tidak exit) bila nama bukan
// alias — panel lalu berjalan seperti biasa.
func TestRunNotAlias(t *testing.T) {
	if Run(filepath.Join(t.TempDir(), "pawon.exe"), nil) {
		t.Error("Run dengan nama panel harus false")
	}
}
