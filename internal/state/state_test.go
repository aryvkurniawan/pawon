package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsZeroConfig(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(c.Sites) != 0 {
		t.Fatalf("want zero config, got %+v err=%v", c, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pawon.json")
	var c Config
	c.AddSite(Site{Subdomain: "app", ZoneID: "z1", Hostname: "app.example.com",
		Root: "C:/pawon/sites/app", Docroot: "C:/pawon/sites/app/public",
		Type: "laravel", PHP: "8.4"})
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p)
	if err != nil || len(c2.Sites) != 1 || c2.Sites[0].Hostname != "app.example.com" {
		t.Fatalf("roundtrip mismatch: %+v err=%v", c2, err)
	}
	if len(c2.Sites[0].ID) != 8 {
		t.Fatalf("site ID must be 8 chars, got %q", c2.Sites[0].ID)
	}
}

func TestSaveIsAtomicNoTempLeft(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pawon.json")
	var c Config
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestAddRemoveSite(t *testing.T) {
	var c Config
	c.AddSite(Site{Hostname: "a.test"})
	c.AddSite(Site{Hostname: "b.test"})
	s, ok := c.Site(c.Sites[0].ID)
	if !ok || s.Hostname != "a.test" {
		t.Fatal("Site lookup failed")
	}
	if !c.RemoveSite(c.Sites[0].ID) || len(c.Sites) != 1 {
		t.Fatal("RemoveSite failed")
	}
	if c.RemoveSite("zzzz") {
		t.Fatal("RemoveSite unknown id must be false")
	}
}
