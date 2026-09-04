package hosts

import (
	"os"
	"strings"
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

func writeAtomic(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
