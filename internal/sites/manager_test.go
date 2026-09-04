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

type fakeCF struct{ ingress, cname int }

func (f *fakeCF) UpsertIngress(string, string) error { f.ingress++; return nil }
func (f *fakeCF) UpsertCNAME(string, string, string) (string, error) {
	f.cname++
	return "rec1", nil
}
func (f *fakeCF) DeleteIngress(string) error       { return nil }
func (f *fakeCF) DeleteCNAME(string, string) error { return nil }

func TestAddLaravelSite(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "stack")
	os.MkdirAll(filepath.Join(root, "sites/app"), 0o755)
	m := &Manager{St: &state.Config{Cloudflare: state.CloudflareCfg{
		Zones: []state.Zone{{ID: "z1", Name: "example.com"}},
	}}, StatePath: filepath.Join(dir, "pawon.json"),
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
	m := &Manager{St: &state.Config{Cloudflare: state.CloudflareCfg{
		Zones: []state.Zone{{ID: "z1", Name: "example.com"}},
	}}, StatePath: filepath.Join(dir, "pawon.json"),
		StackRoot: dir, Run: &fakeRunner{failValidate: true}}
	if _, err := m.Add(AddParams{Subdomain: "bad", ZoneID: "z1", Root: dir, Type: "php", PHP: "8.4"}); err == nil {
		t.Fatal("harus error kalau nginx -t gagal")
	}
	if len(m.St.Sites) != 0 {
		t.Fatal("state tidak boleh berubah kalau validate gagal")
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
