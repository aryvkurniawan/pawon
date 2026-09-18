package db

import (
	"strings"
	"testing"
)

func TestStatementsSafe(t *testing.T) {
	st := Statements("appdb", "appuser", "p@ss'w")
	joined := strings.Join(st, "; ")
	if !strings.Contains(joined, "`appdb`") || !strings.Contains(joined, "'appuser'@'localhost'") {
		t.Fatalf("statements salah: %s", joined)
	}
	if strings.Contains(joined, "appdb`) VALUES") {
		t.Fatal("interpolasi merusak sintaks")
	}
}

// TestStatementsIdempotent — issue #3: `CREATE USER IF NOT EXISTS ...
// IDENTIFIED BY` tidak mengubah password kalau user sudah ada, jadi tanpa
// ALTER USER, pemanggilan kedua menyimpan password baru di state sementara
// MariaDB masih memakai yang lama.
func TestStatementsIdempotent(t *testing.T) {
	st := Statements("appdb", "appuser", "pass2")
	joined := strings.Join(st, "; ")
	if !strings.Contains(joined, "ALTER USER 'appuser'@'localhost' IDENTIFIED BY 'pass2'") {
		t.Fatalf("harus ada ALTER USER agar password di DB ikut berubah: %s", joined)
	}
	// ALTER harus setelah CREATE USER.
	iCreate := strings.Index(joined, "CREATE USER")
	iAlter := strings.Index(joined, "ALTER USER")
	if iCreate < 0 || iAlter < 0 || iAlter < iCreate {
		t.Fatalf("urutan CREATE/ALTER salah: %s", joined)
	}
	// Idempoten: dipanggil dua kali dengan password berbeda menghasilkan
	// statement yang sama-sama menetapkan password terakhir.
	st2 := Statements("appdb", "appuser", "pass3")
	if !strings.Contains(strings.Join(st2, "; "), "IDENTIFIED BY 'pass3'") {
		t.Fatal("password baru harus ikut ditetapkan")
	}
}

func TestGenRootPassword(t *testing.T) {
	pw := GenRootPassword()
	if len(pw) != 24 {
		t.Fatalf("want 24 chars, got %d: %q", len(pw), pw)
	}
	if pw == GenRootPassword() {
		t.Fatal("password harus acak")
	}
}
