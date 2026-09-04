package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
