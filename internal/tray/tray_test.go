package tray

import (
	"context"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// fakePanel: server palsu yang meniru GET /api/status panel.
func fakePanel(t *testing.T, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		// Panel menolak host asing; tray harus mengirim host yang diizinkan.
		if r.Host != "127.0.0.1:7080" {
			http.Error(w, "host ditolak", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old := PanelURL
	PanelURL = srv.URL
	t.Cleanup(func() { PanelURL = old })
}

const bodyOK = `{"services":[
	{"Name":"nginx","Running":true,"PID":1},
	{"Name":"mariadb","Running":true,"PID":2}],
	"sites":3,"tunnel":{"healthy":true}}`

func TestFetchOK(t *testing.T) {
	fakePanel(t, bodyOK)
	s := fetch(context.Background())
	if !s.ok {
		t.Fatal("ok = false, want true")
	}
	if s.total != 2 || s.running != 2 {
		t.Errorf("total=%d running=%d, want 2/2", s.total, s.running)
	}
	if s.sites != 3 {
		t.Errorf("sites = %d, want 3", s.sites)
	}
	if s.tunnel != "aktif" {
		t.Errorf("tunnel = %q, want aktif", s.tunnel)
	}
	// Nama diurutkan supaya urutan menu tidak berubah-ubah.
	if len(s.names) != 2 || s.names[0] != "mariadb" || s.names[1] != "nginx" {
		t.Errorf("names = %v, want [mariadb nginx]", s.names)
	}
	if !s.runningSet["nginx"] {
		t.Error("runningSet[nginx] = false")
	}
	if s.health() != healthOK {
		t.Errorf("health = %v, want ok", s.health())
	}
}

func TestFetchPartial(t *testing.T) {
	fakePanel(t, `{"services":[
		{"Name":"nginx","Running":true},
		{"Name":"mariadb","Running":false}],"sites":0}`)
	s := fetch(context.Background())
	if s.health() != healthWarn {
		t.Errorf("health = %v, want warn", s.health())
	}
	if s.running != 1 || s.total != 2 {
		t.Errorf("running/total = %d/%d, want 1/2", s.running, s.total)
	}
	if s.tunnel != "nonaktif" {
		t.Errorf("tunnel tanpa field = %q, want nonaktif", s.tunnel)
	}
}

// TestFetchDown: panel mati / port tertutup → health down, bukan panic.
func TestFetchDown(t *testing.T) {
	old := PanelURL
	PanelURL = "http://127.0.0.1:1" // port tertutup
	defer func() { PanelURL = old }()
	s := fetch(context.Background())
	if s.ok {
		t.Error("panel mati tapi ok = true")
	}
	if s.health() != healthDown {
		t.Errorf("health = %v, want down", s.health())
	}
}

// TestFetchBadJSON: respons rusak tidak boleh membuat tray crash.
func TestFetchBadJSON(t *testing.T) {
	fakePanel(t, `bukan json`)
	s := fetch(context.Background())
	if s.ok {
		t.Error("JSON rusak harus ok = false")
	}
	if s.health() != healthDown {
		t.Errorf("health = %v, want down", s.health())
	}
}

// TestFetchZeroServices: panel merespons tapi belum ada service → down.
// 0/0 tidak boleh dihitung sebagai "semua jalan".
func TestFetchZeroServices(t *testing.T) {
	fakePanel(t, `{"services":[],"sites":0}`)
	s := fetch(context.Background())
	if !s.ok {
		t.Fatal("panel merespons, ok harus true")
	}
	if s.health() != healthDown {
		t.Errorf("0 service → health %v, want down", s.health())
	}
}

// TestFetchHTTPError: status bukan 200 dianggap panel tidak sehat.
func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	old := PanelURL
	PanelURL = srv.URL
	defer func() { PanelURL = old }()
	s := fetch(context.Background())
	if s.ok {
		t.Error("HTTP 500 harus ok = false")
	}
	if s.health() != healthDown {
		t.Errorf("health = %v, want down", s.health())
	}
}

func TestTooltip(t *testing.T) {
	fakePanel(t, bodyOK)
	s := fetch(context.Background())
	want := "Pawon — 2/2 service jalan, 3 site, tunnel aktif"
	if got := s.tooltip(); got != want {
		t.Errorf("tooltip = %q, want %q", got, want)
	}
	down := snapshot{}
	if got := down.tooltip(); got != "Pawon — panel tidak merespons" {
		t.Errorf("tooltip mati = %q", got)
	}
}

// TestHostHeader: tray harus mengirim Host yang diizinkan guard panel —
// kalau tidak semua permintaan ditolak 403 dan ikon selalu merah.
func TestHostHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Host
		_, _ = w.Write([]byte(`{"services":[{"Name":"a","Running":true}]}`))
	}))
	defer srv.Close()
	old := PanelURL
	PanelURL = srv.URL
	defer func() { PanelURL = old }()
	fetch(context.Background())
	if got != "127.0.0.1:7080" {
		t.Errorf("Host = %q, want 127.0.0.1:7080", got)
	}
}

// --- Ikon ---

func TestStatusImage(t *testing.T) {
	img := statusImage(healthOK)
	if img.Bounds().Dx() != iconSize || img.Bounds().Dy() != iconSize {
		t.Fatalf("ukuran = %v, want %dx%d", img.Bounds(), iconSize, iconSize)
	}
	// Tengah harus warna status, sudut harus transparan.
	if c := img.At(iconSize/2, iconSize/2).(color.RGBA); c != colors[healthOK] {
		t.Errorf("tengah = %v, want %v", c, colors[healthOK])
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Errorf("sudut alpha = %d, want 0", a)
	}
	// Tepi harus lebih gelap dari isi — kontras ini yang membuat ikon
	// terbaca di taskbar terang.
	if e := img.At(iconSize/2, 1).(color.RGBA); e == colors[healthOK] {
		t.Error("tepi sama dengan isi; ikon akan sulit terlihat")
	}
}

// TestStatusImageDistinct: tiap status harus menghasilkan warna berbeda —
// kalau tidak, ikon tidak bisa dipakai membedakan keadaan.
func TestStatusImageDistinct(t *testing.T) {
	seen := map[color.RGBA]health{}
	for _, h := range []health{healthOK, healthWarn, healthDown} {
		c := statusImage(h).At(iconSize/2, iconSize/2).(color.RGBA)
		if other, dup := seen[c]; dup {
			t.Errorf("status %d & %d menghasilkan warna sama: %v", other, h, c)
		}
		seen[c] = h
	}
}

func TestIcoBytes(t *testing.T) {
	b, err := iconBytes(healthOK)
	if err != nil {
		t.Fatal(err)
	}
	// Header ICO: reserved=0, type=1 (icon), count=1.
	if b[0] != 0 || b[1] != 0 || b[2] != 1 || b[3] != 0 || b[4] != 1 || b[5] != 0 {
		t.Errorf("header = %x, want 00 00 01 00 01 00", b[:6])
	}
	// Direnti: lebar & tinggi 32, 32bpp.
	if b[6] != iconSize || b[7] != iconSize {
		t.Errorf("ukuran direnti = %d x %d, want %d", b[6], b[7], iconSize)
	}
	if b[12] != 32 {
		t.Errorf("bpp = %d, want 32", b[12])
	}
	// Panjang: header 6 + direnti 16 + BITMAPINFOHEADER 40 + XOR + AND.
	want := 6 + 16 + 40 + iconSize*iconSize*4 + iconSize*iconSize/8
	if len(b) != want {
		t.Errorf("panjang = %d, want %d", len(b), want)
	}
}

// TestWriteIcons: ketiga berkas ikon harus ditulis & tidak kosong.
func TestWriteIcons(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	icons, err := writeIcons()
	if err != nil {
		t.Fatal(err)
	}
	if len(icons) != 3 {
		t.Fatalf("jumlah ikon = %d, want 3", len(icons))
	}
	for h, p := range icons {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("status %d: %v", h, err)
			continue
		}
		if len(b) == 0 {
			t.Errorf("status %d: berkas kosong", h)
		}
	}
}

// TestServiceAction: aksi service dikirim sebagai POST ke endpoint panel
// dengan Host & Content-Type yang diwajibkan guard.
func TestServiceAction(t *testing.T) {
	var method, path, host, ct string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, host, ct = r.Method, r.URL.Path, r.Host, r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	old := PanelURL
	PanelURL = srv.URL
	defer func() { PanelURL = old }()
	serviceAction("nginx", "restart")
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if path != "/api/services/nginx/restart" {
		t.Errorf("path = %s", path)
	}
	if host != "127.0.0.1:7080" {
		t.Errorf("host = %s", host)
	}
	if ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// Loop pesan Win32 & menu tidak diuji: keduanya butuh session interaktif
// yang tidak ada di CI. Yang diuji di sini logika murninya — status, warna,
// dan pembentukan permintaan.
var _ = image.NewRGBA
