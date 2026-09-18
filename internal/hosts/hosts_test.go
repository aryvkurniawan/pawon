package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddRemoveIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(p, []byte("127.0.0.1 localhost\n"), 0o644)
	if err := Add(p, "app.test"); err != nil {
		t.Fatal(err)
	}
	if err := Add(p, "app.test"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(string(b), "\n")
	if countContains(lines, "app.test") != 1 {
		t.Fatalf("want 1 baris app.test, got:\n%s", b)
	}
	if err := Remove(p, "app.test"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if strings.Contains(string(b), "app.test") {
		t.Fatalf("Remove gagal:\n%s", b)
	}
	if !strings.Contains(string(b), "localhost") {
		t.Fatal("baris lain ikut terhapus")
	}
}

func countContains(lines []string, sub string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// TestWriteAtomicRetriesRename — issue #2: rename bisa gagal sementara
// (race dengan filter driver/AV) lalu berhasil di percobaan berikutnya.
// Target dikunci eksklusif supaya rename gagal, lalu dilepas sementara retry
// masih berjalan.
func TestWriteAtomicRetriesRename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hosts")
	os.WriteFile(p, []byte("lama\n"), 0o644)

	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Lepas kunci setelah retry pertama menempuh jalur gagal (delay 50ms).
	go func() {
		time.Sleep(80 * time.Millisecond)
		f.Close()
	}()

	if err := writeAtomic(p, "baru\n"); err != nil {
		t.Fatalf("writeAtomic harus berhasil setelah retry: %v", err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "baru\n" {
		t.Fatalf("konten salah: %q", b)
	}
}

// TestWriteAtomicFallbackDirectWrite — kalau semua percobaan rename gagal,
// hasilnya tetap ditulis langsung ke target, bukan gagal total.
// Rename dibuat mustahil secara struktural: <target>.tmp adalah direktori,
// jadi os.WriteFile ke tmp selalu gagal.
func TestWriteAtomicFallbackDirectWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hosts")
	os.WriteFile(p, []byte("lama\n"), 0o644)
	if err := os.Mkdir(p+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(p, "baru\n"); err != nil {
		t.Fatalf("fallback harus menyelamatkan penulisan: %v", err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "baru\n" {
		t.Fatalf("konten salah: %q", b)
	}
}

// TestWriteAtomicCreatesNewFile — jalur normal: file belum ada.
func TestWriteAtomicCreatesNewFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	if err := writeAtomic(p, "127.0.0.1 a.test # pawon\n"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "a.test") {
		t.Fatalf("isi salah: %q", b)
	}
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Fatal("file .tmp harus dibersihkan")
	}
}
