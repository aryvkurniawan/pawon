// Package dl mengunduh & mengekstrak binary stack (first-run, idempoten).
package dl

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	versions "pawon/internal"
)

// Bin alias agar konsumen bisa menulis dl.Bin langsung.
type Bin = versions.Bin

// Ensure memastikan semua bins ada di bawah root: skip kalau file/dir target
// sudah ada; retry 3×; log kegagalan ke stderr.
func Ensure(root string, bins []Bin) error {
	for _, b := range bins {
		target := filepath.Join(root, b.Dir)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := fetch(b, target); err != nil {
			return err
		}
	}
	return nil
}

func fetch(b Bin, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".part"
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = download(b.URL, tmp); err == nil {
			break
		}
		fmt.Fprintf(os.Stderr, "dl: %s: percobaan %d/3 gagal: %v\n", b.Name, attempt, err)
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("dl: %s: %w", b.Name, err)
	}
	switch b.Kind {
	case "zip":
		defer os.Remove(tmp)
		return extractZip(tmp, target)
	case "exe", "phar":
		return os.Rename(tmp, target)
	default:
		return fmt.Errorf("dl: %s: kind tidak dikenal %q", b.Name, b.Kind)
	}
}

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func extractZip(path, target string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	base := filepath.Clean(target) + string(os.PathSeparator)
	for _, f := range zr.File {
		// Join sudah Clean; tolak entri yang molor keluar target (zip slip).
		dest := filepath.Join(target, f.Name)
		if dest != filepath.Clean(target) && !strings.HasPrefix(dest, base) {
			return fmt.Errorf("dl: path traversal di zip: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
