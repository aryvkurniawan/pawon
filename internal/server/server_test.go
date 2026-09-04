package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pawon/internal/proc"
	"pawon/internal/sites"
	"pawon/internal/state"
)

// fakeRunner mengganti nginx.Validate/Reload (seperti fakeRunner Task 7).
type fakeRunner struct{}

func (fakeRunner) Validate() error { return nil }
func (fakeRunner) Reload() error   { return nil }

// testDeps: state kosong + fake Runner + Supervisor tanpa service.
// Tunnel nil (fakeCF opsional, belum ada tunnel di unit test).
func testDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st := &state.Config{}
	sp := filepath.Join(dir, "pawon.json")
	return Deps{
		St:        st,
		StatePath: sp,
		StackRoot: dir,
		Sites:     &sites.Manager{St: st, StatePath: sp, StackRoot: dir, Run: &fakeRunner{}},
		Tunnel:    nil,
		Sup:       proc.New(),
		LogsDir:   filepath.Join(dir, "pawon-data", "logs"),
	}
}

func TestStatusEndpoint(t *testing.T) {
	h := New(testDeps(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/status", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	if _, ok := v["services"]; !ok {
		t.Fatalf("status tanpa services: %s", w.Body)
	}
}

func TestAddSiteEndpoint(t *testing.T) {
	d := testDeps(t) // Sites pakai fakeRunner seperti Task 7
	body := `{"subdomain":"app","zone_id":"z1","root":"C:/tmp/app","type":"laravel","php":"8.4"}`
	w := httptest.NewRecorder()
	d.St.Cloudflare.Zones = []state.Zone{{ID: "z1", Name: "example.com"}}
	// Penyesuaian: testDeps mengembalikan Deps (bukan pointer) sesuai plan
	// ("testDeps(t) Deps"), jadi New(d) — bukan New(*d).
	New(d).ServeHTTP(w, httptest.NewRequest("POST", "/api/sites", strings.NewReader(body)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "app.example.com") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestComposerWhitelist(t *testing.T) {
	d := testDeps(t)
	w := httptest.NewRecorder()
	New(d).ServeHTTP(w, httptest.NewRequest("POST", "/api/sites/xxx/composer",
		strings.NewReader(`{"args":["shell","rm -rf /"]}`)))
	if w.Code != 400 {
		t.Fatalf("subcommand di luar whitelist harus 400: %d", w.Code)
	}
}

func TestEnvGetPut(t *testing.T) {
	d := testDeps(t)
	// Penyesuaian: state.AddSite selalu generate ID baru (mengabaikan ID yang
	// diberikan), jadi ambil ID hasil AddSite untuk URL.
	d.St.AddSite(state.Site{Root: t.TempDir(), Type: "laravel"})
	id := d.St.Sites[len(d.St.Sites)-1].ID
	w := httptest.NewRecorder()
	New(d).ServeHTTP(w, httptest.NewRequest("PUT", "/api/sites/"+id+"/env",
		strings.NewReader("APP_KEY=base64:xyz\n")))
	if w.Code != 200 {
		t.Fatalf("put env: %d %s", w.Code, w.Body)
	}
	w2 := httptest.NewRecorder()
	New(d).ServeHTTP(w2, httptest.NewRequest("GET", "/api/sites/"+id+"/env", nil))
	if !strings.Contains(w2.Body.String(), "APP_KEY=base64:xyz") {
		t.Fatalf("get env: %s", w2.Body)
	}
}
