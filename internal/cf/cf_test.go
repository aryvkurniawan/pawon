package cf

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const zonesBody = `{"success":true,"result":[{"id":"z1","name":"example.com"}]}`

func TestZones(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Fatal("token header salah")
		}
		w.Write([]byte(zonesBody))
	}))
	defer srv.Close()
	zs, err := New("tok").withBase(srv.URL).Zones()
	if err != nil || len(zs) != 1 || zs[0].Name != "example.com" {
		t.Fatalf("%+v %v", zs, err)
	}
}

func TestUpsertIngressKeepsCatchAllLast(t *testing.T) {
	cfg := IngressConfig{}
	cfg.Config.Ingress = []Ingress{{Service: "http_status:404"}}
	cfg = UpsertIngress(cfg, "a.test", "http://localhost:80")
	cfg = UpsertIngress(cfg, "b.test", "http://localhost:80")
	cfg = UpsertIngress(cfg, "a.test", "http://localhost:80") // update, bukan duplikat
	n := len(cfg.Config.Ingress)
	if n != 3 {
		t.Fatalf("want 3 entries, got %d", n)
	}
	last := cfg.Config.Ingress[n-1]
	if last.Hostname != "" || last.Service != "http_status:404" {
		t.Fatalf("catch-all harus terakhir: %+v", last)
	}
	if cfg.Config.Ingress[0].Hostname != "a.test" {
		t.Fatalf("upsert harus replace posisi lama: %+v", cfg.Config.Ingress)
	}
}

func TestAPIErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
	}))
	defer srv.Close()
	_, err := New("tok").withBase(srv.URL).Zones()
	if err == nil || !strings.Contains(err.Error(), "Authentication error") {
		t.Fatalf("error CF harus muncul: %v", err)
	}
}
