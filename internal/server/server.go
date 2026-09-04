// Package server: HTTP API panel + slot UI statis. Semua endpoint JSON;
// error selalu {"error": "..."} dengan status 4xx/5xx.
package server

import (
	"io/fs"
	"net/http"
	"strings"

	"pawon/internal/cf"
	"pawon/internal/proc"
	"pawon/internal/sites"
	"pawon/internal/state"
	"pawon/internal/tunnel"
)

// Deps: kebutuhan runtime server; di-wiring main.go (Task 12).
type Deps struct {
	St         *state.Config
	StatePath  string
	StackRoot  string
	Sites      *sites.Manager
	Tunnel     *tunnel.Manager
	Sup        *proc.Supervisor
	MariadbDSN func() string // root dsn untuk create db
	LogsDir    string
	Web        fs.FS // UI embed (web/, di-set main.go); nil → 404
}

// New membangun mux API (Go 1.22 pattern) + slot static UI.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	h := handlers{d}
	mux.HandleFunc("GET /api/status", h.status)
	mux.HandleFunc("POST /api/services/{name}/start", h.service("start"))
	mux.HandleFunc("POST /api/services/{name}/stop", h.service("stop"))
	mux.HandleFunc("POST /api/services/{name}/restart", h.service("restart"))
	mux.HandleFunc("GET /api/sites", h.sitesList)
	mux.HandleFunc("POST /api/sites", h.sitesAdd)
	mux.HandleFunc("DELETE /api/sites/{id}", h.sitesDelete)
	mux.HandleFunc("GET /api/zones", h.zones)
	mux.HandleFunc("POST /api/tunnel/setup", h.tunnelSetup)
	mux.HandleFunc("GET /api/tunnel/status", h.tunnelStatus)
	mux.HandleFunc("POST /api/sites/{id}/composer", h.composer)
	mux.HandleFunc("GET /api/sites/{id}/env", h.envGet)
	mux.HandleFunc("PUT /api/sites/{id}/env", h.envPut)
	mux.HandleFunc("POST /api/dbs", h.dbCreate)
	mux.HandleFunc("GET /api/logs/{name...}", h.logs)
	mux.Handle("/", static(d.Web))
	return mux
}

// static melayani UI dari embed.FS. web/ ada di root repo sedangkan package
// ini di internal/server, jadi //go:embed wajib di main.go (embed hanya bisa
// membaca file di bawah dir package-nya) dan FS-nya di-inject lewat Deps.
// "/" → index.html, /<name>.html & aset (/app.js, /static/*) via FileServer;
// path /api/* yang tidak cocok pattern mux → 404 JSON konsisten dengan API.
func static(web fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "api" || strings.HasPrefix(p, "api/") {
			writeErr(w, http.StatusNotFound, "not found: %s", r.URL.Path)
			return
		}
		if web == nil {
			http.NotFound(w, r)
			return
		}
		http.FileServer(http.FS(web)).ServeHTTP(w, r)
	})
}

// CFAdapter mengimplementasikan sites.Cloudflare di atas tunnel.API
// (*cf.Client): ingress = GET config → upsert → PUT config, CNAME =
// FindDNS (upsert) / CreateCNAME, delete = RemoveIngress / DeleteDNS.
// Dipakai main.go (Task 12) saat tunnel sudah di-setup.
type CFAdapter struct {
	API tunnel.API
	St  *state.Config
}

var _ sites.Cloudflare = (*CFAdapter)(nil)

// NewCFAdapter membungkus *cf.Client + state sebagai sites.Cloudflare.
func NewCFAdapter(api tunnel.API, st *state.Config) *CFAdapter {
	return &CFAdapter{API: api, St: st}
}

func (a *CFAdapter) UpsertIngress(hostname, service string) error {
	c := a.St.Cloudflare
	cfg, err := a.API.GetConfig(c.AccountID, c.TunnelID)
	if err != nil {
		return err
	}
	return a.API.PutConfig(c.AccountID, c.TunnelID, cf.UpsertIngress(cfg, hostname, service))
}

func (a *CFAdapter) UpsertCNAME(zoneID, sub, tunnelID string) (string, error) {
	zone := ""
	for _, z := range a.St.Cloudflare.Zones {
		if z.ID == zoneID {
			zone = z.Name
			break
		}
	}
	if id, err := a.API.FindDNS(zoneID, sub+"."+zone); err == nil {
		return id, nil
	} else if !isNotFound(err) {
		return "", err
	}
	return a.API.CreateCNAME(zoneID, sub, tunnelID)
}

func (a *CFAdapter) DeleteIngress(hostname string) error {
	c := a.St.Cloudflare
	cfg, err := a.API.GetConfig(c.AccountID, c.TunnelID)
	if err != nil {
		return err
	}
	return a.API.PutConfig(c.AccountID, c.TunnelID, cf.RemoveIngress(cfg, hostname))
}

func (a *CFAdapter) DeleteCNAME(zoneID, recordID string) error {
	return a.API.DeleteDNS(zoneID, recordID)
}

// isNotFound: sama dengan tunnel.isNotFound (cfErr tidak diekspor).
// ponytail: string match; ganti ke errors.As saat cf mengekspos tipe error-nya.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "tidak ditemukan")
}
