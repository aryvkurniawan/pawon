package php

import (
	"strings"
	"testing"

	"pawon/internal/state"
)

func TestPoolNameStripsDot(t *testing.T) {
	if PoolName("8.4") != "php_84_pool" {
		t.Fatal(PoolName("8.4"))
	}
}

func TestInstancesFourWorkersPorts(t *testing.T) {
	specs := Instances(state.PHPVersion{Version: "8.4", PortBase: 9100, Enabled: true})
	if len(specs) != 4 {
		t.Fatalf("want 4, got %d", len(specs))
	}
	if !strings.Contains(specs[0].Args[1], ":9100") || !strings.Contains(specs[3].Args[1], ":9103") {
		t.Fatalf("ports wrong: %+v", specs)
	}
	for _, s := range specs {
		if s.Name != "php-8.4" {
			t.Fatalf("name wrong: %q", s.Name)
		}
	}
}
