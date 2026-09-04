package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	_ "github.com/go-sql-driver/mysql"
)

// InitDatadir menjalankan mariadb-install-db.exe --datadir=...
func InitDatadir(mariadbDir, datadir string) error {
	bin := filepath.Join(mariadbDir, "bin")
	cmd := exec.Command(filepath.Join(bin, "mariadb-install-db.exe"), "--datadir="+filepath.ToSlash(datadir))
	cmd.Dir = bin
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mariadb-install-db: %w: %s", err, out)
	}
	return nil
}

// GenRootPassword: 24 char hex dari crypto/rand (96 bit entropi).
func GenRootPassword() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Statements pure: CREATE DATABASE/USER/GRANT; quote `'` di-escape.
func Statements(name, user, pass string) []string {
	return []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", name),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'", esc(user), esc(pass)),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'", name, esc(user)),
	}
}

func esc(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

// CreateDatabase: satu koneksi, eksekusi Statements berurutan.
func CreateDatabase(dsn, name, user, pass string) error {
	h, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer h.Close()
	for _, q := range Statements(name, user, pass) {
		if _, err := h.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	return nil
}

// SetRootPassword: menunggu mariadbd siap (retry 30×1s) lalu ALTER USER root —
// datadir fresh dibuat tanpa password; DSN panel butuh password ini.
func SetRootPassword(mariadbDir, pw string) error {
	bin := filepath.Join(mariadbDir, "bin")
	exe := filepath.Join(bin, "mariadb.exe")
	var lastErr error
	for range 30 {
		cmd := exec.Command(exe, "-u", "root", "-e",
			fmt.Sprintf("ALTER USER 'root'@'localhost' IDENTIFIED BY '%s'; FLUSH PRIVILEGES;", esc(pw)))
		cmd.Dir = bin
		if out, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("set root password: %w: %s", err, out)
		}
		time.Sleep(time.Second)
	}
	return lastErr
}
