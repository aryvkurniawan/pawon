package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	Cloudflare  CloudflareCfg         `json:"cloudflare"`
	Sites       []Site                `json:"sites"`
	PHPVersions []PHPVersion          `json:"php_versions"`
	Services    map[string]ServiceCfg `json:"services"`
	DB          DBCfg                 `json:"db"`
}

type CloudflareCfg struct {
	APIToken    string `json:"api_token"`
	AccountID   string `json:"account_id"`
	TunnelID    string `json:"tunnel_id"`
	TunnelToken string `json:"tunnel_token"`
	Zones       []Zone `json:"zones"`
}

type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Site struct {
	ID        string    `json:"id"`
	Subdomain string    `json:"subdomain"`
	ZoneID    string    `json:"zone_id"`
	Hostname  string    `json:"hostname"`
	Root      string    `json:"root"`
	Docroot   string    `json:"docroot"`
	Type      string    `json:"type"` // "php"|"laravel"
	PHP       string    `json:"php"`  // "8.4"
	DB        *DBCreds  `json:"db,omitempty"`
	NginxConf string    `json:"nginx_conf"`
	DNSRecordID string  `json:"dns_record_id"`
	IngressOK bool      `json:"ingress_ok"`
	DNSOK     bool      `json:"dns_ok"`
	CreatedAt time.Time `json:"created_at"`
}

type DBCreds struct {
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type PHPVersion struct {
	Version  string `json:"version"`
	PortBase int    `json:"port_base"`
	Enabled  bool   `json:"enabled"`
}

type ServiceCfg struct {
	Enabled bool `json:"enabled"`
}

type DBCfg struct {
	RootPassword string `json:"root_password"`
}

var mu sync.Mutex // serialisasi Load/Save/Add/Remove seluruh proses

func Load(path string) (Config, error) {
	mu.Lock()
	defer mu.Unlock()
	var c Config
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func (c *Config) Save(path string) error {
	mu.Lock()
	defer mu.Unlock()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) AddSite(s Site) {
	mu.Lock()
	defer mu.Unlock()
	b := make([]byte, 4)
	rand.Read(b)
	s.ID = hex.EncodeToString(b)
	s.CreatedAt = time.Now().UTC()
	c.Sites = append(c.Sites, s)
}

func (c *Config) RemoveSite(id string) bool {
	mu.Lock()
	defer mu.Unlock()
	for i, s := range c.Sites {
		if s.ID == id {
			c.Sites = append(c.Sites[:i], c.Sites[i+1:]...)
			return true
		}
	}
	return false
}

func (c *Config) Site(id string) (Site, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, s := range c.Sites {
		if s.ID == id {
			return s, true
		}
	}
	return Site{}, false
}
