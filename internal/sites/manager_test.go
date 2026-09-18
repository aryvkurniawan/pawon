package sites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pawon/internal/state"
)

type fakeRunner struct{ failValidate bool }

func (f *fakeRunner) Validate() error {
	if f.failValidate {
		return errInvalid
	}
	return nil
}
func (f *fakeRunner) Reload() error { return nil }

var errInvalid = errorStr("nginx -t gagal")

type errorStr string

func (e errorStr) Error() string { return string(e) }

type fakeCF struct{ ingress, cname, delIngress int }

func (f *fakeCF) UpsertIngress(string, string) error { f.ingress++; return nil }
func (f *fakeCF) UpsertCNAME(string, string, string) (string, error) {
	f.cname++
	return "rec1", nil
}
func (f *fakeCF) DeleteIngress(string) error       { f.delIngress++; return nil }
func (f *fakeCF) DeleteCNAME(string, string) error { return nil }

// baseState: state dengan zona + versi PHP terpasang (validPHP menolak versi
// yang tidak ada, jadi test harus menyediakannya).
func baseState(tunnelID string) state.Config {
	return state.Config{
		Cloudflare: state.CloudflareCfg{
			Zones:    []state.Zone{{ID: "z1", Name: "example.com"}},
			TunnelID: tunnelID,
		},
		PHPVersions: []state.PHPVersion{
			{Version: "8.1", PortBase: 9100, Enabled: true},
			{Version: "8.4", PortBase: 9400, Enabled: true},
		},
	}
}

func TestAddLaravelSite(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "stack")
	os.MkdirAll(filepath.Join(root, "sites/app"), 0o755)
	st := baseState("tun1")
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: root, Run: &fakeRunner{}, CF: &fakeCF{}}
	s, err := m.Add(AddParams{Subdomain: "app", ZoneID: "z1", Root: filepath.Join(root, "sites/app"), Type: "laravel", PHP: "8.4"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Docroot != s.Root+string(os.PathSeparator)+"public" && !strings.HasSuffix(s.Docroot, "public") {
		t.Fatalf("docroot salah: %s", s.Docroot)
	}
	if s.NginxConf == "" || !strings.HasSuffix(s.NginxConf, "app.example.com.conf") {
		t.Fatalf("nginx_conf salah: %s", s.NginxConf)
	}
	// vhost file ada
	if _, err := os.Stat(s.NginxConf); err != nil {
		t.Fatal(err)
	}
}

func TestAddRollbackOnInvalidNginx(t *testing.T) {
	dir := t.TempDir()
	st := baseState("")
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: dir, Run: &fakeRunner{failValidate: true}}
	if _, err := m.Add(AddParams{Subdomain: "bad", ZoneID: "z1", Root: dir, Type: "php", PHP: "8.4"}); err == nil {
		t.Fatal("harus error kalau nginx -t gagal")
	}
	if len(m.St.Sites) != 0 {
		t.Fatal("state tidak boleh berubah kalau validate gagal")
	}
}

// TestAddRollbackKeepsNoVhost — issue #1: kegagalan SETELAH validate (di sini
// hosts) tidak boleh meninggalkan vhost, dan nginx tidak boleh di-reload.
func TestAddRollbackKeepsNoVhost(t *testing.T) {
	dir := t.TempDir()
	st := baseState("")
	runner := &countingRunner{}
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: dir, Run: runner,
		HostsPath: filepath.Join(dir, "hosts")}
	// hosts tidak bisa ditulis → Add gagal di langkah hosts.
	os.MkdirAll(filepath.Join(dir, "hosts"), 0o755)

	_, err := m.Add(AddParams{Subdomain: "app", ZoneID: "z1", Root: filepath.Join(dir, "app"), Type: "php", PHP: "8.4"})
	if err == nil {
		t.Fatal("harus gagal saat hosts tidak bisa ditulis")
	}
	sitesDir := filepath.Join(dir, "nginx", "conf", "sites.d")
	entries, _ := os.ReadDir(sitesDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".conf") {
			t.Fatalf("vhost tertinggal: %s", e.Name())
		}
	}
	if runner.reloads != 0 {
		t.Fatalf("nginx di-reload %d kali padahal add gagal — vhost hantu bisa hidup", runner.reloads)
	}
	if len(m.St.Sites) != 0 {
		t.Fatal("state tidak boleh berubah")
	}
}

type countingRunner struct{ reloads int }

func (c *countingRunner) Validate() error { return nil }
func (c *countingRunner) Reload() error   { c.reloads++; return nil }

// TestAddRejectsDuplicateHostname — issue #4.
func TestAddRejectsDuplicateHostname(t *testing.T) {
	dir := t.TempDir()
	st := baseState("")
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: dir, Run: &fakeRunner{}}
	p := AddParams{Subdomain: "app", ZoneID: "z1", Root: filepath.Join(dir, "first"), Type: "php", PHP: "8.4"}
	if _, err := m.Add(p); err != nil {
		t.Fatal(err)
	}
	p.Root = filepath.Join(dir, "second")
	if _, err := m.Add(p); err == nil {
		t.Fatal("hostname duplikat harus ditolak")
	}
	if len(m.St.Sites) != 1 {
		t.Fatalf("site harus tetap 1, dapat %d", len(m.St.Sites))
	}
}

// TestAddValidatesPHPAndRoot — issue #6.
func TestAddValidatesPHPAndRoot(t *testing.T) {
	dir := t.TempDir()
	st := baseState("")
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: dir, Run: &fakeRunner{}}
	cases := []struct {
		name string
		p    AddParams
	}{
		{"php tidak terpasang", AddParams{Subdomain: "a", ZoneID: "z1", Root: filepath.Join(dir, "a"), Type: "php", PHP: "9.9"}},
		{"php kosong", AddParams{Subdomain: "b", ZoneID: "z1", Root: filepath.Join(dir, "b"), Type: "php", PHP: ""}},
		{"root dengan kutip", AddParams{Subdomain: "c", ZoneID: "z1", Root: `C:/x"; } server { listen 9999; #`, Type: "php", PHP: "8.4"}},
		{"root dengan titik-koma", AddParams{Subdomain: "d", ZoneID: "z1", Root: "C:/x;y", Type: "php", PHP: "8.4"}},
		{"root relatif", AddParams{Subdomain: "e", ZoneID: "z1", Root: "sites/rel", Type: "php", PHP: "8.4"}},
		{"tipe tak dikenal", AddParams{Subdomain: "f", ZoneID: "z1", Root: filepath.Join(dir, "f"), Type: "nodejs", PHP: "8.4"}},
	}
	for _, c := range cases {
		if _, err := m.Add(c.p); err == nil {
			t.Errorf("%s: harus ditolak", c.name)
		}
	}
	if len(m.St.Sites) != 0 {
		t.Fatalf("tidak ada site yang boleh terbuat, dapat %d", len(m.St.Sites))
	}
}

// TestAddSkipsCFWhenTunnelNotSetup — adapter selalu terpasang, tapi tanpa
// TunnelID tidak boleh menembak CF.
func TestAddSkipsCFWhenTunnelNotSetup(t *testing.T) {
	dir := t.TempDir()
	st := baseState("") // tanpa TunnelID
	cf := &fakeCF{}
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: dir, Run: &fakeRunner{}, CF: cf}
	if _, err := m.Add(AddParams{Subdomain: "app", ZoneID: "z1", Root: filepath.Join(dir, "app"), Type: "php", PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if cf.ingress != 0 || cf.cname != 0 {
		t.Fatalf("CF tidak boleh dipanggil tanpa tunnel: ingress=%d cname=%d", cf.ingress, cf.cname)
	}
}

// TestAddWiresCFWhenTunnelSetup — dan harus dipanggil begitu tunnel ada.
func TestAddWiresCFWhenTunnelSetup(t *testing.T) {
	dir := t.TempDir()
	st := baseState("tun1")
	cf := &fakeCF{}
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: dir, Run: &fakeRunner{}, CF: cf}
	s, err := m.Add(AddParams{Subdomain: "app", ZoneID: "z1", Root: filepath.Join(dir, "app"), Type: "php", PHP: "8.4"})
	if err != nil {
		t.Fatal(err)
	}
	if cf.ingress != 1 || cf.cname != 1 {
		t.Fatalf("CF harus dipanggil sekali: ingress=%d cname=%d", cf.ingress, cf.cname)
	}
	if !s.IngressOK || !s.DNSOK {
		t.Fatal("site harus ditandai ter-wire")
	}
}

func TestDocrootLaravel(t *testing.T) {
	if got := Docroot("C:/s/app", "laravel"); got != "C:/s/app/public" {
		t.Fatal(got)
	}
	if got := Docroot("C:/s/app", "php"); got != "C:/s/app" {
		t.Fatal(got)
	}
}
