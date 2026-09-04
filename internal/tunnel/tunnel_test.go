package tunnel

import (
	"path/filepath"
	"strings"
	"testing"

	"pawon/internal/cf"
	"pawon/internal/state"
)

type fakeAPI struct {
	cf.Client // compile guard; semua method dioverride
	calls     []string
	ingress   cf.IngressConfig
}

func (f *fakeAPI) GetConfig(string, string) (cf.IngressConfig, error) {
	f.calls = append(f.calls, "getConfig")
	return f.ingress, nil
}

func (f *fakeAPI) PutConfig(_, _ string, cfg cf.IngressConfig) error {
	f.calls = append(f.calls, "putConfig")
	f.ingress = cfg
	return nil
}

func (f *fakeAPI) CreateCNAME(_, sub, _ string) (string, error) {
	f.calls = append(f.calls, "cname:"+sub)
	return "rec-" + sub, nil
}

func (f *fakeAPI) FindDNS(string, string) (string, error) { return "", errNotFound }

var errNotFound = errStr("not found")

type errStr string

func (e errStr) Error() string { return string(e) }

func TestWireSiteIngressAndCNAME(t *testing.T) {
	dir := t.TempDir()
	api := &fakeAPI{}
	m := &Manager{St: &state.Config{}, StatePath: filepath.Join(dir, "pawon.json"), API: api}
	s := state.Site{Hostname: "app.example.com", ZoneID: "z1", Subdomain: "app"}
	if err := m.WireSite(&s); err != nil {
		t.Fatal(err)
	}
	if !s.IngressOK || !s.DNSOK || s.DNSRecordID != "rec-app" {
		t.Fatalf("site flags salah: %+v", s)
	}
	joined := strings.Join(api.calls, ",")
	if !strings.Contains(joined, "putConfig") || !strings.Contains(joined, "cname:app") {
		t.Fatalf("calls: %s", joined)
	}
	last := api.ingress.Config.Ingress[len(api.ingress.Config.Ingress)-1]
	if last.Service != "http_status:404" {
		t.Fatalf("catch-all harus tetap terakhir: %+v", last)
	}
}
