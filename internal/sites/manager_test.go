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

func TestAddLocalOnlySkipsCloudflare(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "stack")
	os.MkdirAll(filepath.Join(root, "sites/app"), 0o755)
	st := baseState("tun1")
	cf := &fakeCF{}
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: root, Run: &fakeRunner{}, CF: cf}

	// Tunnel sudah di-setup, tapi site lokal-saja tidak boleh menyentuh CF.
	s, err := m.Add(AddParams{Subdomain: "app", Root: filepath.Join(root, "sites/app"), Type: "php", PHP: "8.4", LocalOnly: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !s.LocalOnly {
		t.Error("LocalOnly tidak tersimpan di state")
	}
	if s.IngressOK || s.DNSOK {
		t.Errorf("site lokal-saja ditandai ter-wire ke CF: ingress=%v dns=%v", s.IngressOK, s.DNSOK)
	}
	if cf.ingress != 0 || cf.cname != 0 {
		t.Errorf("CF tersentuh padahal lokal-saja: ingress=%d cname=%d", cf.ingress, cf.cname)
	}
	// Hostname lokal-saja adalah <sub>.test, dan vhost tidak boleh
	// menggandakannya jadi app.test.test.
	if s.Hostname != "app.test" {
		t.Errorf("hostname = %q, mau app.test", s.Hostname)
	}
	conf, err := os.ReadFile(s.NginxConf)
	if err != nil {
		t.Fatalf("baca vhost: %v", err)
	}
	if strings.Contains(string(conf), "app.test.test") {
		t.Error("server_name menggandakan .test")
	}
	if !strings.Contains(string(conf), "server_name app.test;") {
		t.Errorf("server_name tidak memuat app.test:\n%s", conf)
	}
}

func TestAddWithoutZoneRequiresLocalOnly(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "stack")
	os.MkdirAll(filepath.Join(root, "sites/app"), 0o755)
	st := baseState("")
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: root, Run: &fakeRunner{}, CF: &fakeCF{}}

	if _, err := m.Add(AddParams{Subdomain: "app", Root: filepath.Join(root, "sites/app"), Type: "php", PHP: "8.4"}); err == nil {
		t.Fatal("Add tanpa zone dan tanpa LocalOnly seharusnya gagal")
	}
}

func TestAddRemembersLastZone(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "stack")
	os.MkdirAll(filepath.Join(root, "sites/app"), 0o755)
	st := baseState("")
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: root, Run: &fakeRunner{}, CF: &fakeCF{}}

	if _, err := m.Add(AddParams{Subdomain: "app", ZoneID: "z1", Root: filepath.Join(root, "sites/app"), Type: "php", PHP: "8.4"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if st.LastZoneID != "z1" {
		t.Errorf("LastZoneID = %q, mau z1", st.LastZoneID)
	}
	// Harus ikut tersimpan, bukan cuma di memori.
	saved, err := state.Load(m.StatePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.LastZoneID != "z1" {
		t.Errorf("LastZoneID tidak tersimpan ke disk: %q", saved.LastZoneID)
	}
}

func TestScanFindsUnregisteredFolders(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "stack")
	sitesDir := filepath.Join(root, "sites")
	os.MkdirAll(filepath.Join(sitesDir, "app"), 0o755)
	os.WriteFile(filepath.Join(sitesDir, "app", "index.php"), []byte("<?php"), 0o644)
	os.MkdirAll(filepath.Join(sitesDir, "Blog Baru"), 0o755) // nama perlu disanitasi
	os.MkdirAll(filepath.Join(sitesDir, "larry"), 0o755)
	os.WriteFile(filepath.Join(sitesDir, "larry", "artisan"), []byte("#!/usr/bin/env php"), 0o644)
	os.WriteFile(filepath.Join(sitesDir, "catatan.txt"), []byte("bukan folder"), 0o644)

	st := baseState("")
	m := &Manager{St: &st, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: root, Run: &fakeRunner{}, SitesDir: sitesDir}

	list, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := map[string]Unregistered{}
	for _, u := range list {
		got[u.Name] = u
	}
	if len(got) != 3 {
		t.Fatalf("Scan mengembalikan %d folder (%v), mau 3 (file biasa harus diabaikan)", len(got), got)
	}
	if !got["app"].HasApp {
		t.Error("app punya index.php, HasApp harus true")
	}
	if got["Blog Baru"].Sub != "blog-baru" {
		t.Errorf("sanitasi 'Blog Baru' = %q, mau blog-baru", got["Blog Baru"].Sub)
	}
	if got["larry"].Type != "laravel" {
		t.Errorf("folder dengan artisan harus disarankan laravel, dapat %q", got["larry"].Type)
	}

	// Setelah didaftarkan, folder itu tidak boleh muncul lagi. Root ditulis
	// dengan bentuk Windows supaya perbandingan path teruji.
	st.AddSite(state.Site{Subdomain: "app", Root: filepath.FromSlash(filepath.ToSlash(filepath.Join(sitesDir, "app"))), Hostname: "app.test"})
	list2, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan kedua: %v", err)
	}
	for _, u := range list2 {
		if u.Name == "app" {
			t.Error("folder yang sudah terdaftar masih muncul di Scan")
		}
	}
}

func TestServerNames(t *testing.T) {
	cases := []struct {
		name string
		s    state.Site
		want []string
	}{
		{"publik", state.Site{Subdomain: "app", Hostname: "app.example.com"}, []string{"app.example.com", "app.test"}},
		{"lokal saja", state.Site{Subdomain: "app", Hostname: "app.test", LocalOnly: true}, []string{"app.test"}},
		// Penjaga berbasis hostname, bukan flag: site lama yang LocalOnly-nya
		// belum tersimpan tetap tidak boleh digandakan.
		{"hostname sudah .test", state.Site{Subdomain: "app", Hostname: "app.test"}, []string{"app.test"}},
	}
	for _, c := range cases {
		got := ServerNames(c.s)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: ServerNames = %v, mau %v", c.name, got, c.want)
		}
	}
}
