package proc

import (
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func itoa(i int) string { return strconv.Itoa(i) }

func TestRestartDelayBackoff(t *testing.T) {
	want := []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := restartDelay(i); got != w {
			t.Fatalf("attempt %d: want %v got %v", i, w, got)
		}
	}
}

func TestStartStopRealChild(t *testing.T) {
	s := New()
	defer s.StopAll()
	s.Set(Spec{Name: "sleeper", Exe: "cmd", Args: []string{"/c", "ping -n 60 127.0.0.1 >nul"}})
	if err := s.Start("sleeper"); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st[0].Running || st[0].PID == 0 {
		t.Fatalf("not running: %+v", st[0])
	}
	if _, err := exec.Command("tasklist", "/FI", "PID eq "+itoa(st[0].PID)).Output(); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop("sleeper"); err != nil {
		t.Fatal(err)
	}
	if s.Status()[0].Running {
		t.Fatal("still running after Stop")
	}
}

func TestStartRefusesWhenPortBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	s := New()
	defer s.StopAll()
	s.Set(Spec{Name: "x", Exe: "cmd", Args: []string{"/c", "exit"}, Port: port})
	if err := s.Start("x"); err == nil || !strings.Contains(err.Error(), "sudah dijawab") {
		t.Fatalf("want port-busy error, got %v", err)
	}
	if s.Status()[0].Running {
		t.Fatal("must not spawn when port busy")
	}
}

func TestStopNotRunningIsSentinel(t *testing.T) {
	s := New()
	defer s.StopAll()
	s.Set(Spec{Name: "x", Exe: "cmd", Args: []string{"/c", "exit"}})
	err := s.Stop("x")
	if err == nil || !errors.Is(err, ErrNotRunning) {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
}
