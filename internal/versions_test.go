package versions

import (
	"strings"
	"testing"
)

// TestPHPSeriesCanonical: seri harus urut naik tanpa celah, karena urutan
// menentukan nomor port pool (9100 + 100×indeks).
func TestPHPSeriesCanonical(t *testing.T) {
	want := []string{"8.1", "8.2", "8.3", "8.4"}
	if len(PHPSeries) != len(want) {
		t.Fatalf("want %d seri, got %d", len(want), len(PHPSeries))
	}
	for i, w := range want {
		if PHPSeries[i].Series != w {
			t.Errorf("seri ke-%d: want %s, got %s", i, w, PHPSeries[i].Series)
		}
	}
}

// TestPortBaseFor: alokasi port kanonik 9100 + 100×indeks.
func TestPortBaseFor(t *testing.T) {
	cases := map[string]int{"8.1": 9100, "8.2": 9200, "8.3": 9300, "8.4": 9400}
	for series, want := range cases {
		got, ok := PortBaseFor(series)
		if !ok {
			t.Errorf("PortBaseFor(%q) tidak ditemukan", series)
			continue
		}
		if got != want {
			t.Errorf("PortBaseFor(%q): want %d, got %d", series, want, got)
		}
	}
	if _, ok := PortBaseFor("9.9"); ok {
		t.Error("seri tak dikenal harus false")
	}
}

// TestPortRangesDoNotOverlap: tiap pool memakai 4 port berurutan
// (PortBase..+3), jadi jarak antar seri harus > 4.
func TestPortRangesDoNotOverlap(t *testing.T) {
	for i := 1; i < len(PHPSeries); i++ {
		prev, _ := PortBaseFor(PHPSeries[i-1].Series)
		cur, _ := PortBaseFor(PHPSeries[i].Series)
		if cur-prev <= 4 {
			t.Errorf("pool %s dan %s tumpang tindih (%d..%d vs %d..%d)",
				PHPSeries[i-1].Series, PHPSeries[i].Series, prev, prev+3, cur, cur+3)
		}
	}
}

// TestPHPPinsURLs: URL harus menunjuk arsip ber-versi, bukan "latest".
// 8.1–8.3 sudah EOL sehingga URL latest-nya 404 — ini yang membuat multi-PHP
// gagal senyap kalau pin dikembalikan ke pola lama.
func TestPHPPinsURLs(t *testing.T) {
	pins := PHPPins()
	if len(pins) != len(PHPSeries) {
		t.Fatalf("want %d pin, got %d", len(PHPSeries), len(pins))
	}
	for _, p := range pins {
		if strings.Contains(p.URL, "-latest.zip") {
			t.Errorf("%s: pakai URL latest, seri EOL akan 404: %s", p.Name, p.URL)
		}
		if !strings.Contains(p.URL, "/archives/") {
			t.Errorf("%s: harus dari direktori arsip: %s", p.Name, p.URL)
		}
		if p.Kind != "zip" {
			t.Errorf("%s: kind harus zip, got %s", p.Name, p.Kind)
		}
		if !strings.HasPrefix(p.Dir, "bin/php/") {
			t.Errorf("%s: dir salah: %s", p.Name, p.Dir)
		}
	}
}

// TestPHPPinsMatchSeries: tiap pin memakai patch + toolset dari PHPSeries.
func TestPHPPinsMatchSeries(t *testing.T) {
	for _, p := range PHPPins() {
		for _, v := range PHPSeries {
			if p.Name != "php-"+v.Series {
				continue
			}
			if !strings.Contains(p.URL, "php-"+v.Patch+"-nts-Win32-"+v.Toolset+"-x64.zip") {
				t.Errorf("%s: URL tidak cocok patch/toolset: %s", p.Name, p.URL)
			}
			if !strings.HasSuffix(p.Dir, "/"+v.Series) {
				t.Errorf("%s: dir tidak cocok seri: %s", p.Name, p.Dir)
			}
		}
	}
}

// TestPinnedContainsAllPHPAndStack: Pinned harus memuat semua PHP + komponen lain.
func TestPinnedContainsAllPHPAndStack(t *testing.T) {
	names := map[string]bool{}
	for _, b := range Pinned {
		names[b.Name] = true
	}
	for _, v := range PHPSeries {
		if !names["php-"+v.Series] {
			t.Errorf("Pinned tidak memuat php-%s", v.Series)
		}
	}
	for _, n := range []string{"nginx", "mariadb", "cloudflared", "composer", "pma"} {
		if !names[n] {
			t.Errorf("Pinned tidak memuat %s", n)
		}
	}
}
