package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	versions "pawon/internal"
	"pawon/internal/state"
)

// TestSeedCanonical: seed harus menghasilkan tepat satu pool per seri di
// PHPSeries dengan port kanoniknya — inilah yang membuat state tidak mungkin
// desync dari upstream nginx.
func TestSeedCanonical(t *testing.T) {
	st := &state.Config{}
	seed(st)
	if len(st.PHPVersions) != len(versions.PHPSeries) {
		t.Fatalf("want %d versi, got %d", len(versions.PHPSeries), len(st.PHPVersions))
	}
	for i, v := range versions.PHPSeries {
		got := st.PHPVersions[i]
		want, _ := versions.PortBaseFor(v.Series)
		if got.Version != v.Series || got.PortBase != want || !got.Enabled {
			t.Errorf("versi ke-%d: want %s/%d/true, got %s/%d/%v",
				i, v.Series, want, got.Version, got.PortBase, got.Enabled)
		}
	}
}

// TestSeedIdempotent: dijalankan berkali-kali hasilnya sama.
func TestSeedIdempotent(t *testing.T) {
	st := &state.Config{}
	seed(st)
	first := len(st.PHPVersions)
	seed(st)
	seed(st)
	if len(st.PHPVersions) != first {
		t.Fatalf("seed tidak idempoten: %d → %d", first, len(st.PHPVersions))
	}
}

// TestSeedMigratesLegacyPort: state lama dengan 8.4 di 9100 (skema lama)
// harus dirapikan ke port kanonik 9400, dan versi 8.1–8.3 ditambahkan.
func TestSeedMigratesLegacyPort(t *testing.T) {
	st := &state.Config{
		PHPVersions: []state.PHPVersion{{Version: "8.4", PortBase: 9100, Enabled: true}},
	}
	seed(st)
	got, ok := versions.PortBaseFor("8.4")
	if !ok {
		t.Fatal("8.4 harus ada di PHPSeries")
	}
	if st.PHPVersions[3].PortBase != got {
		t.Fatalf("port 8.4 tidak dimigrasi: want %d, got %d", got, st.PHPVersions[3].PortBase)
	}
	if len(st.PHPVersions) != 4 {
		t.Fatalf("want 4 versi, got %d", len(st.PHPVersions))
	}
}

// TestSeedRespectsDisabled: pilihan user (versi dimatikan) dipertahankan,
// supaya seed tidak menyalakan ulang pool yang sengaja dimatikan.
func TestSeedRespectsDisabled(t *testing.T) {
	st := &state.Config{
		PHPVersions: []state.PHPVersion{
			{Version: "8.1", PortBase: 9100, Enabled: false},
			{Version: "8.4", PortBase: 9400, Enabled: true},
		},
	}
	seed(st)
	for _, v := range st.PHPVersions {
		if v.Version == "8.1" && v.Enabled {
			t.Error("8.1 harus tetap nonaktif")
		}
		if v.Version == "8.4" && !v.Enabled {
			t.Error("8.4 harus tetap aktif")
		}
	}
}

// TestEnsurePhpIniSkipsBuiltinExt — PHP 8.1 membundel zip tanpa php_zip.dll;
// menulis "extension=zip" memunculkan warning tiap request.
func TestEnsurePhpIniSkipsBuiltinExt(t *testing.T) {
	home := t.TempDir()
	logs := t.TempDir()
	// ext/ hanya punya sebagian extension.
	os.MkdirAll(filepath.Join(home, "ext"), 0o755)
	for _, dll := range []string{"php_openssl.dll", "php_curl.dll"} {
		os.WriteFile(filepath.Join(home, "ext", dll), []byte("x"), 0o644)
	}
	if err := ensurePhpIni(home, logs, "8.1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, "php.ini"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "extension=openssl") || !strings.Contains(s, "extension=curl") {
		t.Errorf("extension yang ada DLL-nya harus dimuat:\n%s", s)
	}
	if strings.Contains(s, "\nextension=zip") {
		t.Errorf("zip tanpa DLL tidak boleh ditulis sebagai extension:\n%s", s)
	}
	if !strings.Contains(s, "zip: built-in") {
		t.Errorf("harus ada komentar penjelasan untuk zip:\n%s", s)
	}
	if !strings.Contains(s, "error_log") {
		t.Error("error_log harus diset")
	}
}

// TestEnsurePhpIniDoesNotOverwrite: php.ini yang sudah ada tidak ditimpa.
func TestEnsurePhpIniDoesNotOverwrite(t *testing.T) {
	home := t.TempDir()
	ini := filepath.Join(home, "php.ini")
	os.WriteFile(ini, []byte("; kustom user\n"), 0o644)
	if err := ensurePhpIni(home, t.TempDir(), "8.4"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ini)
	if string(b) != "; kustom user\n" {
		t.Fatalf("php.ini tertimpa: %q", b)
	}
}

// TestHasExtDLL: deteksi DLL dengan dan tanpa prefix php_.
func TestHasExtDLL(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "ext"), 0o755)
	os.WriteFile(filepath.Join(home, "ext", "php_mbstring.dll"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(home, "ext", "gd.dll"), []byte("x"), 0o644)
	if !hasExtDLL(home, "mbstring") {
		t.Error("php_mbstring.dll harus terdeteksi")
	}
	if !hasExtDLL(home, "gd") {
		t.Error("gd.dll harus terdeteksi")
	}
	if hasExtDLL(home, "zip") {
		t.Error("zip tidak ada, harus false")
	}
}

// TestPanelLogWrites: panel harus benar-benar menulis pawon.log (sebelumnya
// menu "Panel (pawon.log)" selalu 404).
func TestPanelLogWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "logs", "pawon.log")
	logf, closeLog := panelLog(p)
	logf("halo %s", "dunia")
	logf("baris kedua")
	closeLog()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "halo dunia") || !strings.Contains(s, "baris kedua") {
		t.Fatalf("isi log salah: %q", s)
	}
	if len(strings.Split(strings.TrimRight(s, "\n"), "\n")) != 2 {
		t.Fatalf("want 2 baris: %q", s)
	}
}

// TestPanelLogDisabledOnBadPath: gagal buka log tidak boleh mematikan panel.
func TestPanelLogDisabledOnBadPath(t *testing.T) {
	logf, closeLog := panelLog(filepath.Join(t.TempDir(), "tidak-ada", "\x00", "x.log"))
	defer closeLog()
	logf("tidak boleh panic") // tidak ada error, hanya no-op
}
