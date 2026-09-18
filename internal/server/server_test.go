package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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
// PHPVersions diisi karena sites.Add sekarang menolak versi yang tidak
// terpasang (validPHP) — request test memakai "8.4".
func testDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st := &state.Config{
		PHPVersions: []state.PHPVersion{{Version: "8.4", PortBase: 9400, Enabled: true}},
	}
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

// req membuat request dengan Host yang diizinkan guard + Content-Type JSON
// untuk metode yang memutasi. httptest.NewRequest default Host "example.com"
// dan tanpa Content-Type, keduanya sekarang ditolak (lihat TestGuardHost...).
func req(method, target, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.Host = "127.0.0.1:7080"
	if method == "POST" || method == "PUT" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func TestStatusEndpoint(t *testing.T) {
	h := New(testDeps(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/api/status", ""))
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
	New(d).ServeHTTP(w, req("POST", "/api/sites", body))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "app.example.com") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestComposerWhitelist(t *testing.T) {
	d := testDeps(t)
	w := httptest.NewRecorder()
	New(d).ServeHTTP(w, req("POST", "/api/sites/xxx/composer",
		`{"args":["shell","rm -rf /"]}`))
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
	New(d).ServeHTTP(w, req("PUT", "/api/sites/"+id+"/env",
		"APP_KEY=base64:xyz\n"))
	if w.Code != 200 {
		t.Fatalf("put env: %d %s", w.Code, w.Body)
	}
	w2 := httptest.NewRecorder()
	New(d).ServeHTTP(w2, req("GET", "/api/sites/"+id+"/env", ""))
	if !strings.Contains(w2.Body.String(), "APP_KEY=base64:xyz") {
		t.Fatalf("get env: %s", w2.Body)
	}
}

// TestGuardRejectsForeignHost — issue #5: DNS rebinding. Panel tanpa
// autentikasi, jadi Host adalah satu-satunya pembeda antara dibuka sendiri
// dan dipanggil diam-diam oleh situs lain.
func TestGuardRejectsForeignHost(t *testing.T) {
	h := New(testDeps(t))
	for _, host := range []string{"evil.example", "attacker.com:7080", "127.0.0.1:9999", ""} {
		r := req("GET", "/api/sites", "")
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("Host %q harus 403, dapat %d", host, w.Code)
		}
	}
	// Host sah tetap lolos.
	for _, host := range []string{"127.0.0.1:7080", "localhost:7080"} {
		r := req("GET", "/api/sites", "")
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("Host %q harus 200, dapat %d", host, w.Code)
		}
	}
}

// TestGuardRequiresJSONBody — text/plain & form-urlencoded termasuk
// CORS-safelisted, jadi browser mengirimnya tanpa preflight.
func TestGuardRequiresJSONBody(t *testing.T) {
	h := New(testDeps(t))
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		r := req("POST", "/api/sites", `{"subdomain":"x"}`)
		r.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 415 {
			t.Errorf("Content-Type %q harus 415, dapat %d", ct, w.Code)
		}
	}
	// application/json dengan charset tetap diterima.
	r := req("POST", "/api/sites", `{"subdomain":"x","zone_id":"z","root":"C:/x","type":"php","php":"8.4"}`)
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == 415 {
		t.Fatal("application/json dengan charset harus diterima")
	}
	// GET tidak butuh Content-Type.
	r = req("GET", "/api/sites", "")
	r.Header.Del("Content-Type")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("GET harus 200, dapat %d", w.Code)
	}
}

// TestStaticEmbed: UI dari embed FS — / → index.html, /static/app.css,
// aset lain (/app.js), dan path /api/* tak dikenal → 404 JSON.
func TestStaticEmbed(t *testing.T) {
	d := testDeps(t)
	d.Web = fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<html>dashboard</html>")},
		"app.js":         &fstest.MapFile{Data: []byte("console.log(1)")},
		"static/app.css": &fstest.MapFile{Data: []byte("body{}")},
	}
	h := New(d)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/", ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "dashboard") {
		t.Fatalf("index: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/static/app.css", ""))
	if w.Code != 200 || w.Body.String() != "body{}" {
		t.Fatalf("css: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/app.js", ""))
	if w.Code != 200 {
		t.Fatalf("app.js: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/api/tidak-ada", ""))
	var v map[string]string
	if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v["error"] == "" {
		t.Fatalf("api 404 JSON: %d %s", w.Code, w.Body)
	}
	if _, ok := any(d.Web).(fs.FS); !ok {
		t.Fatal("Web harus fs.FS")
	}
}
