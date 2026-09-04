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

func TestGenRootPassword(t *testing.T) {
	pw := GenRootPassword()
	if len(pw) != 24 {
		t.Fatalf("want 24 chars, got %d: %q", len(pw), pw)
	}
	if pw == GenRootPassword() {
		t.Fatal("password harus acak")
	}
}
