// Package tunnel manages the Cloudflare Tunnel: one-time setup from an API
// token, the cloudflared connector via the supervisor, and per-site wiring
// (ingress rule + proxied CNAME) with the panel state as source of truth.
package tunnel

import (
	"errors"
	"strings"
	"sync"

	"pawon/internal/cf"
	"pawon/internal/proc"
	"pawon/internal/state"
)

const (
	tunnelName     = "pawon"
	ingressService = "http://localhost:80"
)

// API is the Cloudflare surface the manager needs; *cf.Client implements it.
type API interface {
	Accounts() ([]cf.Account, error)
	Zones() ([]cf.Zone, error)
	CreateTunnel(accountID, name string) (cf.Tunnel, error)
	TunnelToken(accountID, tunnelID string) (string, error)
	TunnelStatus(accountID, tunnelID string) (cf.Tunnel, error)
	GetConfig(accountID, tunnelID string) (cf.IngressConfig, error)
	PutConfig(accountID, tunnelID string, cfg cf.IngressConfig) error
	CreateCNAME(zoneID, sub, tunnelID string) (string, error)
	DeleteDNS(zoneID, recordID string) error
	FindDNS(zoneID, name string) (string, error)
}

var _ API = (*cf.Client)(nil)

// Manager wires sites into the remotely-managed tunnel and owns the
// cloudflared connector process.
type Manager struct {
	St             *state.Config
	StatePath      string
	API            API
	Sup            *proc.Supervisor
	CloudflaredExe string
	mu             sync.Mutex // serialisasi mutasi ingress (GET-modify-PUT)
}

// Setup exchanges an API token for a dedicated tunnel: resolve the first
// account, sync zones, create tunnel "pawon" (config_src=cloudflare), fetch
// its connector token, write the initial catch-all ingress, persist state,
// and start the connector.
func (m *Manager) Setup(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	accts, err := m.API.Accounts()
	if err != nil {
		return err
	}
	if len(accts) == 0 {
		return errors.New("CF: token ini tidak punya account")
	}
	acc := accts[0].ID
	zones, err := m.API.Zones()
	if err != nil {
		return err
	}
	t, err := m.API.CreateTunnel(acc, tunnelName)
	if err != nil {
		return err
	}
	tok, err := m.API.TunnelToken(acc, t.ID)
	if err != nil {
		return err
	}
	cfg, err := m.API.GetConfig(acc, t.ID)
	if err != nil {
		return err
	}
	if err := m.API.PutConfig(acc, t.ID, ensureCatchAll(cfg)); err != nil {
		return err
	}
	m.St.Cloudflare.APIToken = token
	m.St.Cloudflare.AccountID = acc
	m.St.Cloudflare.TunnelID = t.ID
	m.St.Cloudflare.TunnelToken = tok
	zs := make([]state.Zone, len(zones))
	for i, z := range zones {
		zs[i] = state.Zone{ID: z.ID, Name: z.Name}
	}
	m.St.Cloudflare.Zones = zs
	if err := m.St.Save(m.StatePath); err != nil {
		return err
	}
	return m.spawn()
}

// Start launches the connector if a tunnel is already configured in state;
// otherwise it is a no-op.
func (m *Manager) Start() error {
	if m.St.Cloudflare.TunnelID == "" {
		return nil
	}
	return m.spawn()
}

// Status reports the tunnel's live status from the CF API.
func (m *Manager) Status() (cf.Tunnel, error) {
	c := m.St.Cloudflare
	return m.API.TunnelStatus(c.AccountID, c.TunnelID)
}

// WireSite points the site's hostname at nginx via an ingress rule plus a
// proxied CNAME and persists the wiring flags on the site.
func (m *Manager) WireSite(s *state.Site) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.St.Cloudflare
	cfg, err := m.API.GetConfig(c.AccountID, c.TunnelID)
	if err != nil {
		return err
	}
	cfg = ensureCatchAll(cf.UpsertIngress(cfg, s.Hostname, ingressService))
	if err := m.API.PutConfig(c.AccountID, c.TunnelID, cfg); err != nil {
		return err
	}
	s.IngressOK = true
	rec, err := m.API.FindDNS(s.ZoneID, s.Hostname)
	if isNotFound(err) {
		rec, err = m.API.CreateCNAME(s.ZoneID, s.Subdomain, c.TunnelID)
	}
	if err != nil {
		return err
	}
	s.DNSRecordID = rec
	s.DNSOK = true
	return m.St.Save(m.StatePath)
}

// UnwireSite removes the site's DNS record and ingress rule; missing records
// are not an error.
func (m *Manager) UnwireSite(s state.Site) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.St.Cloudflare
	rec := s.DNSRecordID
	if rec == "" {
		rec, _ = m.API.FindDNS(s.ZoneID, s.Hostname)
	}
	if rec != "" {
		if err := m.API.DeleteDNS(s.ZoneID, rec); err != nil && !isNotFound(err) {
			return err
		}
	}
	cfg, err := m.API.GetConfig(c.AccountID, c.TunnelID)
	if err != nil {
		return err
	}
	if err := m.API.PutConfig(c.AccountID, c.TunnelID, cf.RemoveIngress(cfg, s.Hostname)); err != nil {
		return err
	}
	return m.St.Save(m.StatePath)
}

// spawn defines and starts the cloudflared connector; nil Sup or empty exe
// (unit tests) skips spawning.
func (m *Manager) spawn() error {
	if m.Sup == nil || m.CloudflaredExe == "" {
		return nil
	}
	m.Sup.Set(proc.Spec{
		Name: "cloudflared",
		Exe:  m.CloudflaredExe,
		Args: []string{"tunnel", "run", "--token", m.St.Cloudflare.TunnelToken},
	})
	return m.Sup.Start("cloudflared")
}

// ensureCatchAll appends the 404 catch-all when missing so unknown hosts
// never fall through; UpsertIngress only preserves an existing one.
func ensureCatchAll(cfg cf.IngressConfig) cf.IngressConfig {
	ing := cfg.Config.Ingress
	if len(ing) > 0 && ing[len(ing)-1].Hostname == "" && ing[len(ing)-1].Service == "http_status:404" {
		return cfg
	}
	cfg.Config.Ingress = append(ing, cf.Ingress{Service: "http_status:404"})
	return cfg
}

// isNotFound matches "not found" errors from the CF client and fakes.
// ponytail: string match karena cfErr tidak diekspor; ganti ke errors.As saat cf mengekspos tipe error-nya.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "tidak ditemukan")
}
