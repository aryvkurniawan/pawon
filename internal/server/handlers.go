package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"pawon/internal/cf"
	"pawon/internal/db"
	"pawon/internal/proc"
	"pawon/internal/sites"
	"pawon/internal/state"
	"strconv"
	"strings"
)

type handlers struct{ Deps }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, format string, a ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, a...)})
}

// status: ringkasan services supervisor + jumlah site + status tunnel.
type statusResp struct {
	Services []proc.Status `json:"services"`
	Sites    int           `json:"sites"`
	Tunnel   *cf.Tunnel    `json:"tunnel"`
	Err      string        `json:"err,omitempty"`
}

func (h handlers) status(w http.ResponseWriter, r *http.Request) {
	resp := statusResp{Services: []proc.Status{}, Tunnel: nil}
	if h.Sup != nil {
		resp.Services = h.Sup.Status()
	}
	resp.Sites = len(h.St.Sites)
	if h.Tunnel != nil && h.Tunnel.API != nil {
		t, err := h.Tunnel.Status()
		if err != nil {
			resp.Err = err.Error()
		} else {
			resp.Tunnel = &t
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// service mengembalikan handler POST /api/services/{name}/start|stop|restart.
func (h handlers) service(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if h.Sup == nil {
			writeErr(w, http.StatusInternalServerError, "supervisor tidak tersedia")
			return
		}
		var err error
		switch op {
		case "start":
			err = h.Sup.Start(name)
		case "stop":
			err = h.Sup.Stop(name)
		case "restart":
			_ = h.Sup.Stop(name) // tidak jalan / belum ada → langsung start
			err = h.Sup.Start(name)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "%s %s: %v", op, name, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (h handlers) sitesList(w http.ResponseWriter, r *http.Request) {
	if h.St.Sites == nil {
		writeJSON(w, http.StatusOK, []state.Site{})
		return
	}
	writeJSON(w, http.StatusOK, h.St.Sites)
}

func (h handlers) sitesAdd(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Subdomain string `json:"subdomain"`
		ZoneID    string `json:"zone_id"`
		Root      string `json:"root"`
		Type      string `json:"type"`
		PHP       string `json:"php"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "body: %v", err)
		return
	}
	if h.Sites == nil {
		writeErr(w, http.StatusInternalServerError, "sites manager tidak tersedia")
		return
	}
	s, err := h.Sites.Add(sites.AddParams(p))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h handlers) sitesDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.St.Site(id); !ok {
		writeErr(w, http.StatusNotFound, "site %q tidak ditemukan", id)
		return
	}
	if err := h.Sites.Remove(id, h.Sites.CF); err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h handlers) zones(w http.ResponseWriter, r *http.Request) {
	if h.St.Cloudflare.Zones == nil {
		writeJSON(w, http.StatusOK, []state.Zone{})
		return
	}
	writeJSON(w, http.StatusOK, h.St.Cloudflare.Zones)
}

func (h handlers) tunnelSetup(w http.ResponseWriter, r *http.Request) {
	var p struct {
		APIToken string `json:"api_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "body: %v", err)
		return
	}
	if h.Tunnel == nil {
		writeErr(w, http.StatusInternalServerError, "tunnel manager tidak tersedia")
		return
	}
	if err := h.Tunnel.Setup(p.APIToken); err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h handlers) tunnelStatus(w http.ResponseWriter, r *http.Request) {
	if h.Tunnel == nil || h.Tunnel.API == nil {
		writeErr(w, http.StatusNotFound, "tunnel belum di-setup")
		return
	}
	t, err := h.Tunnel.Status()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// composerWhitelist: subcommand composer yang boleh dieksekusi panel.
var composerWhitelist = map[string]bool{
	"create-project": true, "require": true, "install": true,
	"update": true, "dump-autoload": true,
}

// flushWriter memaksa chunked streaming tiap Write dari cmd.Stdout.
type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

func (h handlers) composer(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Args []string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "body: %v", err)
		return
	}
	// Whitelist dicek dulu (sebelum lookup site) agar body jahat selalu 400.
	if len(p.Args) == 0 || !composerWhitelist[p.Args[0]] {
		writeErr(w, http.StatusBadRequest, "subcommand composer di luar whitelist: %v", p.Args)
		return
	}
	s, ok := h.St.Site(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "site %q tidak ditemukan", r.PathValue("id"))
		return
	}
	phpExe := filepath.Join(h.StackRoot, "bin", "php", s.PHP, "php.exe")
	phar := filepath.Join(h.StackRoot, "bin", "php", "composer.phar")
	if _, err := os.Stat(phpExe); err != nil {
		writeErr(w, http.StatusInternalServerError, "php %s: %v", s.PHP, err)
		return
	}
	if _, err := os.Stat(phar); err != nil {
		writeErr(w, http.StatusInternalServerError, "composer.phar: %v", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	cmd := exec.Command(phpExe, append([]string{phar}, p.Args...)...)
	cmd.Dir = s.Root
	cmd.Stdout = flushWriter{w}
	cmd.Stderr = flushWriter{w} // progress composer di stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(w, "\ncomposer exit: %v\n", err)
	}
}

func envPath(site state.Site) string { return filepath.Join(site.Root, ".env") }

func (h handlers) envGet(w http.ResponseWriter, r *http.Request) {
	s, ok := h.St.Site(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "site %q tidak ditemukan", r.PathValue("id"))
		return
	}
	b, err := os.ReadFile(envPath(s))
	if os.IsNotExist(err) {
		writeErr(w, http.StatusNotFound, ".env belum ada untuk %s", s.Hostname)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(b)
}

func (h handlers) envPut(w http.ResponseWriter, r *http.Request) {
	s, ok := h.St.Site(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "site %q tidak ditemukan", r.PathValue("id"))
		return
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "body: %v", err)
		return
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := os.WriteFile(envPath(s), b, 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h handlers) dbCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		SiteID string `json:"site_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "body: %v", err)
		return
	}
	s, ok := h.St.Site(p.SiteID)
	if !ok {
		writeErr(w, http.StatusNotFound, "site %q tidak ditemukan", p.SiteID)
		return
	}
	if h.MariadbDSN == nil {
		writeErr(w, http.StatusInternalServerError, "mariadb dsn belum dikonfigurasi")
		return
	}
	name, user := s.Subdomain, s.Subdomain
	pass := db.GenRootPassword()
	if err := db.CreateDatabase(h.MariadbDSN(), name, user, pass); err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	s.DB = &state.DBCreds{Name: name, User: user, Password: pass}
	for i := range h.St.Sites {
		if h.St.Sites[i].ID == s.ID {
			h.St.Sites[i] = s
			break
		}
	}
	if err := h.St.Save(h.StatePath); err != nil {
		writeErr(w, http.StatusInternalServerError, "save: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// logs men-tail N baris terakhir. Nama: "pawon", "nginx-error", "php-8.4",
// "mariadb", "cloudflared", "nginx/<hostname>-access|-error" → file
// <LogsDir>/<name>.log; "laravel:<site-id>" → <site.Root>/storage/logs/laravel.log.
func (h handlers) logs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tail := 200
	if n, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && n > 0 {
		tail = n
	}
	var path string
	if siteID, ok := strings.CutPrefix(name, "laravel:"); ok {
		s, found := h.St.Site(siteID)
		if !found {
			writeErr(w, http.StatusNotFound, "site %q tidak ditemukan", siteID)
			return
		}
		path = filepath.Join(s.Root, "storage", "logs", "laravel.log")
	} else {
		if name == "" || strings.ContainsAny(name, ":\\") || strings.Contains(name, "..") {
			writeErr(w, http.StatusBadRequest, "nama log tidak valid: %q", name)
			return
		}
		if !strings.HasSuffix(name, ".log") {
			name += ".log"
		}
		path = filepath.Join(h.LogsDir, filepath.FromSlash(name))
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		writeErr(w, http.StatusNotFound, "log tidak ditemukan: %s", path)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "lines": lines})
}
