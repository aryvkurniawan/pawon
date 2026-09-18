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
	// SitesDir: folder yang dipindai Scan() untuk menemukan folder belum
	// terdaftar. Kosong → <StackRoot>/sites.
	SitesDir string
}

type AddParams struct {
	Subdomain, ZoneID, Root, Type, PHP string // Type: php|laravel; Root boleh folder existing
	// LocalOnly: lewati ingress tunnel + CNAME sepenuhnya. Cukup vhost + hosts
	// sehingga site hanya hidup di <sub>.test. Berguna untuk eksperimen cepat;
	// menghindari menyentuh domain publik hanya untuk mencoba sesuatu.
	LocalOnly bool
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
	// Zone opsional untuk site lokal-saja: tanpa zone, hostname publik tidak
	// ada gunanya, jadi site hidup di <sub>.test saja. Kalau zone diisi
	// (walau lokal-saja), hostname publik tetap dicatat dan ikut masuk
	// server_name nginx — supaya mempublikasikannya nanti cukup dengan
	// menyambung DNS, tanpa daftar ulang.
	var zoneName string
	if p.ZoneID != "" {
		z, ok := zoneByID(m.St, p.ZoneID)
		if !ok {
			return state.Site{}, fmt.Errorf("zone %q tidak ditemukan", p.ZoneID)
		}
		zoneName = z.Name
	} else if !p.LocalOnly {
		return state.Site{}, fmt.Errorf("zone wajib dipilih (atau centang \"lokal saja\")")
	}
	// Tolak hostname duplikat SEBELUM menyentuh disk: nama file vhost adalah
	// <hostname>.conf, jadi dua site berhostname sama akan saling menimpa dan
	// menghapus vhost salah satunya saat salah satu di-remove.
	hostname := p.Subdomain + ".test"
	if zoneName != "" {
		hostname = p.Subdomain + "." + zoneName
	}
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
		LocalOnly: p.LocalOnly,
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
	if cf := m.cfFor(); cf != nil && !p.LocalOnly {
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
		if cf := m.cfFor(); cf != nil && !p.LocalOnly {
			cf.DeleteIngress(s.Hostname)
		}
		rollback()
		return state.Site{}, err
	}
	m.St.AddSite(s)
	if p.ZoneID != "" {
		m.St.LastZoneID = p.ZoneID
	}
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

// ServerNames mengembalikan daftar server_name nginx untuk sebuah site.
//
// Dipakai bersama oleh sites.Manager (saat add/remove) dan writeNginxConf di
// main.go (saat boot). Sebelumnya keduanya menulis daftar ini sendiri-sendiri
// dan sempat berbeda: versi boot tidak tahu soal site lokal-saja, sehingga
// hostname-nya digandakan jadi <sub>.test.test.
//
// Site lokal-saja hostname-nya sudah <sub>.test, jadi tidak perlu ditambah lagi.
func ServerNames(s state.Site) []string {
	if strings.EqualFold(s.Hostname, s.Subdomain+".test") {
		return []string{s.Hostname}
	}
	return []string{s.Hostname, s.Subdomain + ".test"}
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
		ServerNames: ServerNames(s),
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

// Unregistered adalah satu folder di SitesDir yang belum terdaftar sebagai site.
type Unregistered struct {
	Name   string `json:"name"`    // nama folder, dipakai sebagai usulan subdomain
	Root   string `json:"root"`    // path absolut siap kirim ke POST /api/sites
	Sub    string `json:"sub"`     // usulan subdomain (sudah disanitasi)
	Type   string `json:"type"`    // usulan tipe: laravel kalau ada artisan, selain itu php
	HasApp bool   `json:"has_app"` // ada index.php/index.html langsung di root
}

// Scan memindai SitesDir dan mengembalikan folder yang belum terdaftar sebagai
// site. Read-only: tidak menyentuh disk, tidak memanggil Cloudflare.
//
// Ini sengaja BUKAN auto-register. Folder yang muncul di sini belum dilayani
// nginx dan belum ada di hosts, jadi menaruh folder (termasuk yang berisi
// .env atau dump DB) tidak pernah dengan sendirinya menerbitkannya ke domain
// publik. Pendaftaran tetap satu klik sadar oleh pengguna.
func (m *Manager) Scan() ([]Unregistered, error) {
	dir := m.SitesDir
	if dir == "" {
		dir = filepath.Join(m.StackRoot, "sites")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Unregistered{}, nil
		}
		return nil, err
	}
	known := make(map[string]bool, len(m.St.Sites))
	for _, s := range m.St.Sites {
		// Bandingkan dalam bentuk slash + huruf kecil: state menyimpan path
		// yang ditulis pengguna ("C:/Pawon/sites/app"), sedangkan ReadDir
		// mengembalikan bentuk OS ("C:\Pawon\sites\app").
		known[pathKey(s.Root)] = true
	}
	out := []Unregistered{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		root := filepath.Join(dir, e.Name())
		if known[pathKey(root)] {
			continue
		}
		sub := sanitizeSub(e.Name())
		if sub == "" {
			continue // nama folder tidak bisa jadi subdomain yang valid
		}
		out = append(out, Unregistered{
			Name:   e.Name(),
			Root:   filepath.ToSlash(root),
			Sub:    sub,
			Type:   suggestType(root),
			HasApp: hasEntrypoint(root),
		})
	}
	return out, nil
}

// pathKey menormalkan path untuk perbandingan: absolut, slash, huruf kecil.
// Windows case-insensitive, jadi "C:/Pawon/sites/App" dan ".../app" itu sama.
func pathKey(p string) string {
	abs, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		abs = p
	}
	return strings.ToLower(filepath.ToSlash(abs))
}

// sanitizeSub menyaring nama folder jadi subdomain yang lolos subRe. Nama
// folder sering memakai huruf besar, spasi, atau garis bawah — semuanya tidak
// valid sebagai label DNS. Mengembalikan "" kalau tidak ada sisa yang berguna.
func sanitizeSub(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			// Spasi, "_", "." dan simbol lain jadi satu "-" tunggal.
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	// Label DNS maksimal 63 karakter dan tidak boleh diakhiri "-".
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	if !subRe.MatchString(s) {
		return ""
	}
	return s
}

// suggestType: folder dengan artisan dianggap Laravel, karena docroot-nya
// harus <root>/public. Deteksi ini cuma usulan — pengguna tetap bisa ubah.
func suggestType(root string) string {
	if fi, err := os.Stat(filepath.Join(root, "artisan")); err == nil && !fi.IsDir() {
		return "laravel"
	}
	return "php"
}

// hasEntrypoint: apakah folder punya index.php/index.html langsung di root.
// Dipakai UI untuk menandai folder yang isinya belum siap dilayani.
func hasEntrypoint(root string) bool {
	for _, f := range []string{"index.php", "index.html"} {
		if fi, err := os.Stat(filepath.Join(root, f)); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}
