package pathutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	versions "pawon/internal"
)

func TestAliases(t *testing.T) {
	got := strings.Join(Aliases(), ",")
	for _, want := range []string{"php", "composer", "mysql", "nginx"} {
		if !contains(got, want) {
			t.Errorf("Aliases tanpa %s: %s", want, got)
		}
	}
	// Tiap seri non-default punya alias sendiri; seri default tidak dobel.
	for _, v := range versions.PHPSeries {
		alias := "php" + strings.ReplaceAll(v.Series, ".", "")
		has := contains(got, alias)
		if v.Series == PHPDefault && has {
			t.Errorf("seri default %s punya alias dobel: %s", v.Series, got)
		}
		if v.Series != PHPDefault && !has {
			t.Errorf("seri %s tanpa alias: %s", v.Series, got)
		}
	}
}

func contains(s, sub string) bool {
	for _, p := range strings.Split(s, ",") {
		if p == sub {
			return true
		}
	}
	return false
}

// TestEnsureCopiesAliases: Ensure menyalin alias .exe ke bin/shim.
// Ensure menyalin binary panel (os.Executable()), jadi di test ini hasilnya
// berupa salinan binary test — yang diuji adalah jumlah & nama alias.
func TestEnsureCopiesAliases(t *testing.T) {
	root := t.TempDir()
	if errs := Ensure(root); len(errs) != 0 {
		t.Fatalf("Ensure: %v", errs)
	}
	dir := ShimDir(root)
	for _, a := range Aliases() {
		fi, err := os.Stat(filepath.Join(dir, a+".exe"))
		if err != nil {
			t.Errorf("alias %s.exe tidak ada: %v", a, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("alias %s.exe kosong", a)
		}
	}
}

// TestEnsureIdempotent: Ensure dua kali tidak menambah file & tidak menimpa
// (penting karena panel memanggilnya tiap boot).
func TestEnsureIdempotent(t *testing.T) {
	root := t.TempDir()
	if errs := Ensure(root); len(errs) != 0 {
		t.Fatalf("Ensure 1: %v", errs)
	}
	dir := ShimDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n1 := len(entries)
	if errs := Ensure(root); len(errs) != 0 {
		t.Fatalf("Ensure 2: %v", errs)
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != n1 {
		t.Errorf("jumlah file berubah: %d → %d", n1, len(entries))
	}
	if len(entries) != len(Aliases()) {
		t.Errorf("jumlah file = %d, want %d alias", len(entries), len(Aliases()))
	}
}

// TestCleanupRemovesStaleAlias: alias versi yang sudah tidak dikenal
// (php74.exe) dan file asing dibersihkan, alias yang dikenal dipelihara.
func TestCleanupRemovesStaleAlias(t *testing.T) {
	root := t.TempDir()
	dir := ShimDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"php.exe", "php74.exe", "bendaasing.exe", "php.bat"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if errs := Ensure(root); len(errs) != 0 {
		t.Fatalf("Ensure: %v", errs)
	}
	for _, n := range []string{"php74.exe", "bendaasing.exe", "php.bat"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s tidak dibersihkan", n)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "php.exe")); err != nil {
		t.Error("php.exe ikut terhapus")
	}
}

// TestCleanupNil: cleanup tanpa daftar putih = no-op (tidak menghapus apa pun).
func TestCleanupNil(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if errs := cleanup(dir, nil); len(errs) != 0 {
		t.Fatalf("cleanup nil harus no-op: %v", errs)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.exe")); err != nil {
		t.Error("cleanup nil menghapus file")
	}
}

func TestPHPDefault(t *testing.T) {
	last := versions.PHPSeries[len(versions.PHPSeries)-1].Series
	if PHPDefault != last {
		t.Errorf("PHPDefault = %s, want seri tertinggi %s", PHPDefault, last)
	}
}

// TestAppendPath menguji aturan pencocokan PATH tanpa menyentuh registry:
// entri yang sudah ada (termasuk varian case & trailing separator) tidak
// digandakan, dan entri baru ditambah di akhir.
func TestAppendPath(t *testing.T) {
	cases := []struct {
		name     string
		cur, dir string
		want     string
		already  bool
	}{
		{"PATH kosong", "", `C:\a`, `C:\a`, false},
		{"entri baru ditambah", `C:\a`, `C:\b`, `C:\a;C:\b`, false},
		{"sudah ada", `C:\a;C:\b`, `C:\b`, `C:\a;C:\b`, true},
		{"case berbeda", `C:\a;C:\Shim`, `c:\shim`, `C:\a;C:\Shim`, true},
		{"trailing separator di arg", `C:\a`, `C:\b\`, `C:\a;C:\b`, false},
		// Sudah ada dengan trailing separator → nilai dipertahankan apa
		// adanya; merapikan entri milik pengguna lain bukan urusan kita.
		{"trailing separator di PATH", `C:\a;C:\b\;`, `C:\b`, `C:\a;C:\b\;`, true},
		{"PATH berakhir separator", `C:\a;`, `C:\b`, `C:\a;C:\b`, false},
		{"entri kosong diabaikan", `C:\a;;C:\b`, `C:\b`, `C:\a;;C:\b`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, already := appendPath(c.cur, c.dir)
			if already != c.already {
				t.Errorf("already = %v, want %v", already, c.already)
			}
			if got != c.want {
				t.Errorf("appendPath(%q,%q) = %q, want %q", c.cur, c.dir, got, c.want)
			}
		})
	}
}
