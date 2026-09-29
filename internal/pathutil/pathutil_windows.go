// Package pathutil: shim CLI untuk binary stack + pendaftaran PATH user.
//
// Binary stack (php/mariadb/nginx) hidup di bin/ milik panel supaya tiap
// proyek bisa bawa versinya sendiri tanpa install global. Akibatnya `php`
// tidak ketemu dari cmd biasa. Paket ini menempatkan alias di bin/shim dan
// mendaftarkan folder itu ke PATH user, sehingga `php`, `composer`, `mysql`,
// `nginx` jalan dari terminal mana pun.
//
// Alias adalah salinan pawon.exe dengan nama alias (lihat internal/shim).
// Menyalin binary panel, bukan meng-embed shim terpisah: tidak ada binary
// kedua yang bisa usang terhadap layout bin/, tidak ada beberapa MB ekstra
// di repo, dan panel + shim selalu satu versi.
//
// Kenapa .exe, bukan .bat: memanggil .bat dari .bat lain tanpa `call`
// mentransfer kontrol secara permanen — batch pemanggil berhenti setelah
// shim selesai. Shim .exe tidak punya semantik itu.
package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	versions "pawon/internal"

	"golang.org/x/sys/windows/registry"
)

// ShimDir: lokasi alias shim, relatif terhadap stack root.
func ShimDir(root string) string { return filepath.Join(root, "bin", "shim") }

// Aliases: nama alias shim yang diekspos ke PATH, tanpa ekstensi.
//
// `php` menunjuk seri tertinggi yang terpasang (diputuskan shim saat
// dijalankan), dan php<seri> mengunci seri tertentu.
func Aliases() []string {
	out := []string{"php", "composer", "mysql", "nginx"}
	for _, v := range versions.PHPSeries {
		if v.Series == PHPDefault {
			continue
		}
		out = append(out, "php"+strings.ReplaceAll(v.Series, ".", ""))
	}
	return out
}

// Members: nama file yang dikenal di dir shim — dipakai caller sebagai
// daftar putih cleanup (file lain dihapus).
func Members() map[string]bool {
	m := map[string]bool{}
	for _, a := range Aliases() {
		m[a+".exe"] = true
	}
	return m
}

// Ensure menempatkan alias shim ke bin/shim. Idempoten; alias disalin ulang
// bila panel binary berubah (ukur beda) atau alias hilang.
//
// Sengaja TIDAK menyentuh PATH: perubahan PATH itu global & persisten, jadi
// dipisah ke RegisterPath yang dipanggil eksplisit panel/installer — dengan
// begitu Ensure bisa diuji tanpa mencemari environment mesin.
//
// Kegagalan dikembalikan sebagai daftar, bukan error tunggal: satu alias
// gagal tidak boleh menggagalkan alias lain.
func Ensure(root string) []error {
	dir := ShimDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return []error{err}
	}
	exe, err := os.Executable()
	if err != nil {
		return []error{err}
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return []error{err}
	}
	// Alias harus menunjuk binary panel di lokasi tetap. Bila panel dijalankan
	// dari salinan sementara (go run), alias akan usang setelah proses mati —
	// tidak apa-apa, boot berikutnya menyalin ulang.
	src, err := os.Stat(exe)
	if err != nil {
		return []error{err}
	}

	var errs []error
	for _, a := range Aliases() {
		dst := filepath.Join(dir, a+".exe")
		if fi, err := os.Stat(dst); err == nil && fi.Size() == src.Size() {
			continue // sudah ada & ukuran sama
		}
		b, err := os.ReadFile(exe)
		if err != nil {
			return append(errs, err)
		}
		if err := os.WriteFile(dst, b, 0o755); err != nil {
			errs = append(errs, err)
		}
	}

	errs = append(errs, cleanup(dir, Members())...)
	return errs
}

// RegisterPath menempatkan alias shim lalu mendaftarkan dir-nya ke PATH
// user. Ini gerbang satu-satunya yang menyentuh registry — panel & installer
// memanggilnya, test cukup memanggil Ensure.
func RegisterPath(root string) []error {
	errs := Ensure(root)
	if err := AddToUserPath(ShimDir(root)); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// cleanup menghapus file di dir yang tidak ada di keep. Dipakai untuk
// membersihkan alias versi PHP yang sudah dihapus dari PHPSeries.
func cleanup(dir string, keep map[string]bool) []error {
	if keep == nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, e := range entries {
		if e.IsDir() || keep[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// AddToUserPath menambah dir ke PATH user (HKCU\Environment) bila belum ada.
// Pencocokan case-insensitive + trailing separator dibuang: Windows
// memperlakukan PATH case-insensitive, dan ada dua cara menulis direktori
// yang sama (dengan/tanpa `\` akhir).
//
// Nilai dibaca sebagai EXPAND_SZ dan ditulis dengan tipe yang sama supaya
// variabel seperti %USERPROFILE% tidak meledak jadi string mati. Bila nilai
// telanjang bertipe SZ (umum di mesin baharu), tipe itu dipertahankan.
//
// Setelah menulis, WM_SETTINGCHANGE disiarkan agar Explorer me-refresh
// environment — tanpanya terminal yang sudah berjalan belum melihat PATH
// baru. Kegagalan broadcast tidak menggagalkan penulisan.
func AddToUserPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("buka HKCU\\Environment: %w", err)
	}
	defer k.Close()

	cur := ""
	expand := true
	if val, typ, err := k.GetStringValue("Path"); err == nil {
		cur = val
		expand = typ == registry.EXPAND_SZ
	} else if err != registry.ErrNotExist {
		return fmt.Errorf("baca PATH: %w", err)
	}

	next, already := appendPath(cur, dir)
	if already {
		return nil
	}
	if err := setValue(k, "Path", next, expand); err != nil {
		return fmt.Errorf("tulis PATH: %w", err)
	}
	broadcastEnvironmentChange()
	return nil
}

// appendPath menambah dir ke nilai PATH bila belum ada; already=true bila
// tidak perlu diubah. Dipisah dari AddToUserPath supaya aturan pencocokan
// (case-insensitive, trailing separator) teruji tanpa menyentuh registry.
//
// Nilai existing dikembalikan apa adanya saat sudah ada: merapikan entri
// milik pengguna lain (membuang trailing separator) bukan urusan kita.
func appendPath(cur, dir string) (next string, already bool) {
	want := filepath.Clean(dir)
	for _, p := range strings.Split(cur, string(os.PathListSeparator)) {
		if p == "" {
			continue
		}
		if strings.EqualFold(filepath.Clean(p), want) {
			return cur, true
		}
	}
	if cur == "" {
		return want, false
	}
	return strings.TrimRight(cur, string(os.PathListSeparator)) +
		string(os.PathListSeparator) + want, false
}

// setValue menulis PATH dengan tipe aslinya (EXPAND_SZ atau SZ).
func setValue(k registry.Key, name, value string, expand bool) error {
	if expand {
		return k.SetExpandStringValue(name, value)
	}
	return k.SetStringValue(name, value)
}

// PHPDefault adalah seri PHP yang dipakai alias `php`. Ditentukan dari
// PHPSeries (sumber tunggal kebenaran), bukan isi folder di disk, supaya
// alias tetap konsisten meski seri itu belum ter-download.
var PHPDefault = func() string {
	if n := len(versions.PHPSeries); n > 0 {
		return versions.PHPSeries[n-1].Series
	}
	return "8.4"
}()
