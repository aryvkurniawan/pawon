package svc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nginx-1.30.4"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "mariadb-11.8.9", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nginx-1.30.4", "nginx.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mariadb-11.8.9", "bin", "mariadbd.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	dir, err := FindDir(root, "nginx.exe")
	if err != nil || filepath.Base(dir) != "nginx-1.30.4" {
		t.Fatalf("FindDir nginx: %q, %v", dir, err)
	}
	dir, err = FindDir(root, filepath.Join("bin", "mariadbd.exe"))
	if err != nil || filepath.Base(dir) != "mariadb-11.8.9" {
		t.Fatalf("FindDir mariadb: %q, %v", dir, err)
	}
	if _, err := FindDir(root, "cloudflared.exe"); err == nil {
		t.Fatal("exe yang tidak ada harus error")
	}
}
