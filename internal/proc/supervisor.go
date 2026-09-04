// Package proc supervises child processes: define (Set), spawn (Start),
// stop (Stop/StopAll), status (Status), with automatic restart using a
// backoff delay. On Windows every child is assigned to a Job Object with
// KILL_ON_JOB_CLOSE, so children die with the panel.
package proc

import (
	"fmt"
	"os/exec"
	"sort"
	"sync"
	"time"
)

// Spec defines a supervised service.
type Spec struct {
	Name string // "nginx", "php-8.4", "mariadb", "cloudflared"
	Exe  string
	Args []string
	Env  []string // format "K=V"
	Dir  string
}

// Status is a point-in-time snapshot of one service.
type Status struct {
	Name     string
	Running  bool
	PID      int
	Restarts int
	LastErr  string
}

type entry struct {
	spec     Spec
	cmd      *exec.Cmd
	started  time.Time
	running  bool
	stopping bool
	restarts int
	lastErr  string
	done     chan struct{} // closed once cmd.Wait returned
}

// Supervisor keeps service definitions and their live children.
type Supervisor struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// New returns an empty Supervisor.
func New() *Supervisor {
	return &Supervisor{entries: make(map[string]*entry)}
}

// Set defines or replaces the definition of a service.
func (s *Supervisor) Set(spec Spec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[spec.Name]
	if e == nil {
		e = &entry{}
		s.entries[spec.Name] = e
	}
	e.spec = spec
}

// Start spawns the child of a defined service.
func (s *Supervisor) Start(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[name]
	if !ok {
		return fmt.Errorf("proc: unknown service %q", name)
	}
	if e.running {
		return fmt.Errorf("proc: %q already running", name)
	}
	cmd := exec.Command(e.spec.Exe, e.spec.Args...)
	cmd.Env = e.spec.Env
	cmd.Dir = e.spec.Dir
	if err := cmd.Start(); err != nil {
		e.lastErr = err.Error()
		return err
	}
	assignToJob(cmd.Process)
	e.cmd = cmd
	e.started = time.Now()
	e.running = true
	e.stopping = false
	e.done = make(chan struct{})
	go s.supervise(e, cmd, e.done)
	return nil
}

// supervise waits for cmd; on unexpected death it schedules one restart via
// restartDelay. Children alive longer than 60s reset the backoff counter.
func (s *Supervisor) supervise(e *entry, cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	s.mu.Lock()
	e.running = false
	if err != nil {
		e.lastErr = err.Error()
	} else {
		e.lastErr = ""
	}
	if time.Since(e.started) > 60*time.Second {
		e.restarts = 0
	}
	restart := e.cmd == cmd && !e.stopping
	var delay time.Duration
	if restart {
		delay = restartDelay(e.restarts)
		e.restarts++
	}
	name := e.spec.Name
	s.mu.Unlock()
	close(done)

	if !restart {
		return
	}
	time.Sleep(delay)
	s.mu.Lock()
	abort := e.stopping || e.cmd != cmd // stopped, or superseded by a manual Start
	s.mu.Unlock()
	if !abort {
		_ = s.Start(name)
	}
}

// Stop kills the child of a service and waits for it to exit.
func (s *Supervisor) Stop(name string) error {
	s.mu.Lock()
	e, ok := s.entries[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("proc: unknown service %q", name)
	}
	if !e.running {
		e.stopping = true // cancel any pending restart
		s.mu.Unlock()
		return fmt.Errorf("proc: %q not running", name)
	}
	e.stopping = true
	done := e.done
	cmd := e.cmd
	s.mu.Unlock()
	cmd.Process.Kill()
	<-done
	return nil
}

// StopAll stops every running child.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	names := make([]string, 0, len(s.entries))
	for name, e := range s.entries {
		if e.running {
			names = append(names, name)
		}
	}
	s.mu.Unlock()
	for _, name := range names {
		_ = s.Stop(name)
	}
}

// Status returns a snapshot of all services sorted by name.
func (s *Supervisor) Status() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.entries))
	for name, e := range s.entries {
		st := Status{Name: name, Running: e.running, Restarts: e.restarts, LastErr: e.lastErr}
		if e.cmd != nil && e.cmd.Process != nil {
			st.PID = e.cmd.Process.Pid
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// restartDelay returns the delay before restart attempt n (0-based):
// three quick 2s retries, then 30s.
func restartDelay(attempt int) time.Duration {
	if attempt < 3 {
		return 2 * time.Second
	}
	return 30 * time.Second
}
