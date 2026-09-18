package hosts

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const marker = "# pawon"

// Add menambah baris "127.0.0.1 <hostname> # pawon". Idempoten.
func Add(hostsPath, hostname string) error {
	b, err := os.ReadFile(hostsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for _, l := range lines {
		if strings.Contains(l, marker) && hasHost(l, hostname) {
			return nil
		}
	}
	entry := "127.0.0.1 " + hostname + " " + marker
	if n := len(lines); n > 0 && lines[n-1] == "" { // file berakhir \n → sisip sebelum baris kosong
		lines = append(lines[:n-1], entry, "")
	} else {
		lines = append(lines, entry)
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return writeAtomic(hostsPath, out)
}

// Remove menghapus baris ber-marker untuk hostname; tak ada → no-op.
func Remove(hostsPath, hostname string) error {
	b, err := os.ReadFile(hostsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(b), "\n")
	out := lines[:0]
	for _, l := range lines {
		if strings.Contains(l, marker) && hasHost(l, hostname) {
			continue
		}
		out = append(out, l)
	}
	return writeAtomic(hostsPath, strings.Join(out, "\n"))
}

// hasHost match hostname sebagai field utuh — "app.test" tidak kena "xapp.test".
func hasHost(line, hostname string) bool {
	for _, f := range strings.Fields(line) {
		if f == hostname {
			return true
		}
	}
	return false
}

// writeAtomic menulis lewat file sementara lalu rename, dengan retry.
//
// Kenapa retry: rename tepat setelah write bisa gagal "Access is denied"
// ketika panel jalan sebagai service (LocalSystem) — race dengan filter
// driver/AV yang masih memegang file sementara. Terukur di lapangan: konten
// identik kadang berhasil kadang tidak, dan jeda beberapa ratus ms cukup
// membuatnya berhasil. Retry dengan backoff menutup jendela itu.
//
// Kalau semua percobaan habis (mis. file terkunci proses lain), fallback
// terakhir adalah menulis langsung ke target — kehilangan sifat atomik, tapi
// hosts tetap ter-update, dan itu lebih berguna daripada gagal total.
func writeAtomic(path, content string) error {
	tmp := path + ".tmp"
	delays := []time.Duration{0, 50 * time.Millisecond, 150 * time.Millisecond, 400 * time.Millisecond, time.Second}
	var lastErr error
	for _, d := range delays {
		if d > 0 {
			time.Sleep(d)
		}
		if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
			lastErr = err
			continue
		}
		if err := os.Rename(tmp, path); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	os.Remove(tmp)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		// Laporkan error rename asli — itu penyebab utamanya, dan lebih
		// informatif daripada error fallback yang cuma akibat turunannya.
		if lastErr != nil {
			return fmt.Errorf("%w (fallback tulis langsung juga gagal: %v)", lastErr, err)
		}
		return err
	}
	return nil
}
