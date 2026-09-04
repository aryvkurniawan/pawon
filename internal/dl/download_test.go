package dl

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDownloadsAndExtractsZip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z := zip.NewWriter(w)
		f, _ := z.Create("tool/readme.txt")
		f.Write([]byte("hello"))
		z.Close()
	}))
	defer srv.Close()
	root := t.TempDir()
	b := Bin{Name: "tool", URL: srv.URL + "/tool.zip", Kind: "zip", Dir: "bin/tool"}
	if err := Ensure(root, []Bin{b}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin/tool/tool/readme.txt")); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	// idempoten: panggil lagi → tidak error, tidak download ulang
	if err := Ensure(root, []Bin{b}); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureExeDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("MZ fake exe"))
	}))
	defer srv.Close()
	root := t.TempDir()
	b := Bin{Name: "tool", URL: srv.URL + "/tool.exe", Kind: "exe", Dir: "bin/tool.exe"}
	if err := Ensure(root, []Bin{b}); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(filepath.Join(root, "bin/tool.exe"))
	if string(b2) != "MZ fake exe" {
		t.Fatal("content mismatch")
	}
}
