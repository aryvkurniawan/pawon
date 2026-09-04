package cf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Client struct {
	Token string
	Base  string
	HC    *http.Client
}

type Account struct{ ID, Name string }
type Zone struct{ ID, Name string }
type Tunnel struct {
	ID, Name, Status string
	Connections      int
}
type Ingress struct {
	Hostname string `json:"hostname,omitempty"`
	Service  string `json:"service"`
}
type IngressConfig struct {
	Config struct {
		Ingress []Ingress `json:"ingress"`
	} `json:"config"`
}

type cfErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	Success bool            `json:"success"`
	Errors  []cfErr         `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

func New(token string) *Client {
	return &Client{Token: token, Base: "https://api.cloudflare.com/client/v4", HC: http.DefaultClient}
}

func (c *Client) withBase(base string) *Client { c.Base = base; return c }

func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HC
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	if !env.Success {
		if len(env.Errors) == 0 {
			return fmt.Errorf("CF: response success=false tanpa detail error")
		}
		return fmt.Errorf("CF %d: %s", env.Errors[0].Code, env.Errors[0].Message)
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func (c *Client) Accounts() ([]Account, error) {
	var out []Account
	return out, c.do("GET", "/accounts", nil, &out)
}

func (c *Client) Zones() ([]Zone, error) {
	var out []Zone
	return out, c.do("GET", "/zones?per_page=50", nil, &out)
}

func (c *Client) CreateTunnel(accountID, name string) (Tunnel, error) {
	var t Tunnel
	body := map[string]string{"name": name, "config_src": "cloudflare"}
	return t, c.do("POST", "/accounts/"+accountID+"/cfd_tunnel", body, &t)
}

func (c *Client) TunnelToken(accountID, tunnelID string) (string, error) {
	var tok string
	return tok, c.do("POST", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/token", nil, &tok)
}

func (c *Client) TunnelStatus(accountID, tunnelID string) (Tunnel, error) {
	var t Tunnel
	return t, c.do("GET", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID, nil, &t)
}

func (c *Client) GetConfig(accountID, tunnelID string) (IngressConfig, error) {
	var cfg IngressConfig
	return cfg, c.do("GET", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configuration", nil, &cfg)
}

func (c *Client) PutConfig(accountID, tunnelID string, cfg IngressConfig) error {
	return c.do("PUT", "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configuration", cfg, nil)
}

func (c *Client) CreateCNAME(zoneID, sub, tunnelID string) (string, error) {
	var rec struct {
		ID string `json:"id"`
	}
	body := map[string]any{
		"type":    "CNAME",
		"name":    sub,
		"content": tunnelID + ".cfargotunnel.com",
		"proxied": true,
	}
	return rec.ID, c.do("POST", "/zones/"+zoneID+"/dns_records", body, &rec)
}

func (c *Client) DeleteDNS(zoneID, recordID string) error {
	return c.do("DELETE", "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil)
}

func (c *Client) FindDNS(zoneID, name string) (string, error) {
	var recs []struct {
		ID string `json:"id"`
	}
	path := "/zones/" + zoneID + "/dns_records?type=CNAME&name=" + url.QueryEscape(name)
	if err := c.do("GET", path, nil, &recs); err != nil {
		return "", err
	}
	if len(recs) == 0 {
		return "", fmt.Errorf("CF: DNS record CNAME %q tidak ditemukan", name)
	}
	return recs[0].ID, nil
}

// UpsertIngress replace rule by hostname (posisi lama dipertahankan saat update);
// rule baru disisipkan sebelum catch-all, catch-all http_status:404 SELALU terakhir.
func UpsertIngress(cfg IngressConfig, hostname, service string) IngressConfig {
	rule := Ingress{Hostname: hostname, Service: service}
	ing := cfg.Config.Ingress
	for i, r := range ing {
		if r.Hostname == hostname {
			ing[i] = rule
			return cfg
		}
	}
	pos := len(ing)
	if pos > 0 && ing[pos-1].Hostname == "" {
		pos-- // sisip sebelum catch-all
	}
	ing = append(ing, Ingress{})
	copy(ing[pos+1:], ing[pos:])
	ing[pos] = rule
	cfg.Config.Ingress = ing
	return cfg
}

func RemoveIngress(cfg IngressConfig, hostname string) IngressConfig {
	var out []Ingress
	for _, r := range cfg.Config.Ingress {
		if r.Hostname != hostname {
			out = append(out, r)
		}
	}
	cfg.Config.Ingress = out
	return cfg
}
