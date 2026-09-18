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
	if p.Type != "php" && p.Type != "laravel" {
		return state.Site{}, fmt.Errorf("tipe tidak dikenal: %q (pakai php|laravel)", p.Type)
	}
	if err := validPHP(m.St, p.PHP); err != nil {
		return state.Site{}, err
	}
	if err := validRoot(p.Root); err != nil {
		return state.Site{}, err
	}
	zone, ok := zoneByID(m.St, p.ZoneID)
	if !ok {
		return state.Site{}, fmt.Errorf("zone %q tidak ditemukan", p.ZoneID)
	}
	// Tolak hostname duplikat SEBELUM menyentuh disk: nama file vhost adalah
	// <hostname>.conf, jadi dua site berhostname sama akan saling menimpa dan
	// menghapus vhost salah satunya saat salah satu di-remove.
	hostname := p.Subdomain + "." + zone.Name
	if _, exists := m.St.SiteByHostname(hostname); exists {
		return state.Site{}, fmt.Errorf("site %s sudah ada", hostname)
	}
	root := filepath.ToSlash(p.Root)
	s := state.Site{
		Subdomain: p.Subdomain,
		ZoneID:    p.ZoneID,
		Hostname:  hostname,
		Root:      root,
		Docroot:   Docroot(root, p.Type),
		Type:      p.Type,
		PHP:       p.PHP,
	}
	if err := os.MkdirAll(s.Docroot, 0o755); err != nil {
		return state.Site{}, err
	}

	// Urutan: vhost → validate → hosts → CF → reload → save.
	//
	// Reload sengaja DITUNDA sampai semua langkah non-nginx selesai. Vhost yang
	// belum di-reload tidak dilayani nginx, jadi kalau langkah berikutnya gagal
	// kita cukup menghapus filenya — tidak ada hostname "hantu" yang hidup tanpa
	// terdaftar di state. Rollback dilakukan berurutan terbalik.
	conf, err := m.writeVhost(s)
	if err != nil {
		return state.Site{}, err
	}
	s.NginxConf = conf
	rollback := func() {
		os.Remove(conf)
		if m.HostsPath != "" {
			hosts.Remove(m.HostsPath, s.Subdomain+".test")
		}
	}
	if err := m.Run.Validate(); err != nil {
		rollback()
		return state.Site{}, err
	}
	if m.HostsPath != "" {
		if err := hosts.Add(m.HostsPath, s.Subdomain+".test"); err != nil {
			rollback()
			return state.Site{}, fmt.Errorf("hosts: %w", err)
		}
	}
	if cf := m.cfFor(); cf != nil {
		if err := cf.UpsertIngress(s.Hostname, ingressService); err != nil {
			rollback()
			return state.Site{}, err
		}
		rec, err := cf.UpsertCNAME(s.ZoneID, s.Subdomain, m.St.Cloudflare.TunnelID)
		if err != nil {
			cf.DeleteIngress(s.Hostname)
			rollback()
			return state.Site{}, err
		}
		s.DNSRecordID, s.IngressOK, s.DNSOK = rec, true, true
	}
	if err := m.Run.Reload(); err != nil {
		if cf := m.cfFor(); cf != nil {
			cf.DeleteIngress(s.Hostname)
		}
		rollback()
		return state.Site{}, err
	}
	m.St.AddSite(s)
	return m.St.Sites[len(m.St.Sites)-1], m.St.Save(m.StatePath)
}

// validPHP: versi harus ada di daftar versi terpasang dan aktif. Tanpa ini,
// nilai sembarang dari request masuk ke nama upstream nginx dan config-nya
// ditolak saat `nginx -t` — pesan errornya membingungkan dan vhost sudah
// terlanjur ditulis.
func validPHP(st *state.Config, version string) error {
	for _, v := range st.PHPVersions {
		if v.Version == version {
			if !v.Enabled {
				return fmt.Errorf("PHP %s tidak aktif", version)
			}
			return nil
		}
	}
	return fmt.Errorf("PHP %q tidak terpasang", version)
}

// validRoot: tolak karakter yang bisa memutus quoting di config nginx.
// Nilai ini diinterpolasi ke dalam `root "..."`, jadi kutip ganda, newline,
// dan titik-koma bisa menyuntik direktif baru.
func validRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("folder root wajib diisi")
	}
	if strings.ContainsAny(root, "\"';{}\n\r\t") {
		return fmt.Errorf("folder root mengandung karakter terlarang: %q", root)
	}
	if !filepath.IsAbs(filepath.FromSlash(root)) {
		return fmt.Errorf("folder root harus path absolut: %q", root)
	}
	return nil
}

// cfFor: adapter CF hanya dipakai kalau tunnel sudah benar-benar di-setup.
// Adapter selalu terpasang (supaya setup dari UI langsung berlaku tanpa
// restart panel), jadi gerbangnya pindah ke sini — tanpa TunnelID,
// UpsertIngress/UpsertCNAME akan menembak CF dengan kredensial kosong.
func (m *Manager) cfFor() Cloudflare {
	if m.CF == nil || m.St.Cloudflare.TunnelID == "" {
		return nil
	}
	return m.CF
}

// CFFor mengekspos cfFor ke handler (Remove menerima Cloudflare sebagai
// parameter agar tetap bisa diuji dengan mock).
func (m *Manager) CFFor() Cloudflare { return m.cfFor() }

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
