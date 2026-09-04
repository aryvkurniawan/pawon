// Package sites mengorkestrasi add/remove site: validasi, docroot, vhost
// nginx, reload, hosts, wiring Cloudflare, dan state.
package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pawon/internal/hosts"
	"pawon/internal/nginx"
	"pawon/internal/php"
	"pawon/internal/state"
)

const ingressService = "http://localhost:80" // sama dengan spec §tunnel

var subRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Runner injeksi untuk test; real: nginx.Validate/Reload via exec.
type Runner interface {
	Validate() error
	Reload() error
}

// Cloudflare subset yang dipakai manager; real impl: adapter cf.Client
// (GetConfig+UpsertIngress+PutConfig, FindDNS+CreateCNAME, DeleteDNS+RemoveIngress)
// di wiring main.go. tunnel.Manager tidak didelegasi langsung supaya manager
// tetap testable tanpa tunnel (lihat notes report).
type Cloudflare interface {
	UpsertIngress(hostname, service string) error
	UpsertCNAME(zoneID, sub, tunnelID string) (recordID string, err error)
	DeleteIngress(hostname string) error
	DeleteCNAME(zoneID, recordID string) error
}

type Manager struct {
	St        *state.Config
	StatePath string
	StackRoot string
	Run       Runner
	CF        Cloudflare // boleh nil → site lokal saja (tunnel belum setup)
	HostsPath string     // kosong → skip update hosts (unit test)
}

type AddParams struct {
	Subdomain, ZoneID, Root, Type, PHP string // Type: php|laravel; Root boleh folder existing
}

func (m *Manager) Add(p AddParams) (state.Site, error) {
	if !subRe.MatchString(p.Subdomain) {
		return state.Site{}, fmt.Errorf("subdomain tidak valid: %q", p.Subdomain)
	}
	zone, ok := zoneByID(m.St, p.ZoneID)
	if !ok {
		return state.Site{}, fmt.Errorf("zone %q tidak ditemukan", p.ZoneID)
	}
	root := filepath.ToSlash(p.Root)
	s := state.Site{
		Subdomain: p.Subdomain,
		ZoneID:    p.ZoneID,
		Hostname:  p.Subdomain + "." + zone.Name,
		Root:      root,
		Docroot:   Docroot(root, p.Type),
		Type:      p.Type,
		PHP:       p.PHP,
	}
	if err := os.MkdirAll(s.Docroot, 0o755); err != nil {
		return state.Site{}, err
	}
	conf, err := m.writeVhost(s)
	if err != nil {
		return state.Site{}, err
	}
	s.NginxConf = conf
	if err := m.Run.Validate(); err != nil {
		os.Remove(conf)
		return state.Site{}, err
	}
	if err := m.Run.Reload(); err != nil {
		os.Remove(conf)
		return state.Site{}, err
	}
	if m.HostsPath != "" {
		if err := hosts.Add(m.HostsPath, s.Subdomain+".test"); err != nil {
			return state.Site{}, err
		}
	}
	if m.CF != nil {
		if err := m.CF.UpsertIngress(s.Hostname, ingressService); err != nil {
			return state.Site{}, err
		}
		rec, err := m.CF.UpsertCNAME(s.ZoneID, s.Subdomain, m.St.Cloudflare.TunnelID)
		if err != nil {
			return state.Site{}, err
		}
		s.DNSRecordID, s.IngressOK, s.DNSOK = rec, true, true
	}
	m.St.AddSite(s)
	return m.St.Sites[len(m.St.Sites)-1], m.St.Save(m.StatePath)
}

// Remove kebalikan Add: CF delete → hosts remove → vhost hapus + reload →
// state remove + save. cf boleh nil (site lokal).
func (m *Manager) Remove(id string, cf Cloudflare) error {
	s, ok := m.St.Site(id)
	if !ok {
		return fmt.Errorf("site %q tidak ditemukan", id)
	}
	if cf != nil {
		if s.DNSRecordID != "" {
			if err := cf.DeleteCNAME(s.ZoneID, s.DNSRecordID); err != nil {
				return err
			}
		}
		if err := cf.DeleteIngress(s.Hostname); err != nil {
			return err
		}
	}
	if m.HostsPath != "" {
		if err := hosts.Remove(m.HostsPath, s.Subdomain+".test"); err != nil {
			return err
		}
	}
	if s.NginxConf != "" {
		if err := os.Remove(s.NginxConf); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if m.Run != nil {
		if err := m.Run.Reload(); err != nil {
			return err
		}
	}
	m.St.RemoveSite(id)
	return m.St.Save(m.StatePath)
}

// Docroot mengembalikan root siap-nginx (path "/"): laravel → <root>/public.
func Docroot(root, typ string) string {
	root = strings.TrimRight(filepath.ToSlash(root), "/")
	if typ == "laravel" {
		return root + "/public"
	}
	return root
}

func (m *Manager) writeVhost(s state.Site) (string, error) {
	sitesDir := filepath.Join(m.StackRoot, "nginx", "conf", "sites.d")
	logsDir := filepath.Join(m.StackRoot, "pawon-data", "logs", "nginx")
	if err := os.MkdirAll(sitesDir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return "", err
	}
	v := nginx.Vhost{
		ServerNames: []string{s.Hostname, s.Subdomain + ".test"},
		Docroot:     s.Docroot,
		Pool:        php.PoolName(s.PHP),
		AccessLog:   filepath.ToSlash(filepath.Join(logsDir, s.Hostname+"-access.log")),
		ErrorLog:    filepath.ToSlash(filepath.Join(logsDir, s.Hostname+"-error.log")),
	}
	conf := filepath.Join(sitesDir, s.Hostname+".conf")
	return conf, os.WriteFile(conf, []byte(nginx.RenderVhost(v)), 0o644)
}

func zoneByID(c *state.Config, id string) (state.Zone, bool) {
	for _, z := range c.Cloudflare.Zones {
		if z.ID == id {
			return z, true
		}
	}
	return state.Zone{}, false
}
