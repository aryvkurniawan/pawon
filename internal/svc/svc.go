// Package svc: mode Windows service (SCM) panel + helper resolusi path
// binary stack. Zip nginx/mariadb/pma di-extract verbatim sehingga berisi
// top-dir versi (bin/nginx/nginx-1.30.4/...); versi dicari dinamis, tidak
// di-hardcode.
package svc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	winsvc "golang.org/x/sys/windows/svc"
)

// Name adalah nama Windows service yang didaftarkan.
const Name = "Pawon"

// IsWindowsService melaporkan apakah proses ini dijalankan oleh SCM.
func IsWindowsService() bool {
	in, err := winsvc.IsWindowsService()
	return err == nil && in
}

// RunService menjalankan panel di bawah SCM. start dipanggil di goroutine
// sendiri; channel stop di-close saat SCM meminta stop/shutdown.
func RunService(start func(stop chan struct{})) error {
	return winsvc.Run(Name, runner{start: start})
}

type runner struct{ start func(stop chan struct{}) }

func (r runner) Execute(_ []string, req <-chan winsvc.ChangeRequest, status chan<- winsvc.Status) (bool, uint32) {
	status <- winsvc.Status{State: winsvc.StartPending}
	stop := make(chan struct{})
	go r.start(stop)
	status <- winsvc.Status{State: winsvc.Running, Accepts: winsvc.AcceptStop | winsvc.AcceptShutdown}
	for c := range req {
		switch c.Cmd {
		case winsvc.Interrogate:
			status <- c.CurrentStatus
		case winsvc.Stop, winsvc.Shutdown:
			status <- winsvc.Status{State: winsvc.StopPending}
			close(stop)
			return false, 0
		}
	}
	return false, 0
}

// Install mendaftarkan service (auto-start) via sc.exe: binPath = exe ini.
func Install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}
	return sc("create", Name, "binPath=", `"`+exe+`"`, "start=", "auto")
}

// Remove menghapus service.
func Remove() error {
	return sc("delete", Name)
}

func sc(args ...string) error {
	out, err := exec.Command("sc", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc %s: %w: %s", args[0], err, out)
	}
	return nil
}

// FindDir mengembalikan subdirektori pertama root (urut nama) yang memuat
// file rel, mis. FindDir(root+"/bin/nginx", "nginx.exe") → .../nginx-1.30.4.
func FindDir(root, rel string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), rel)); err == nil {
			return filepath.Join(root, e.Name()), nil
		}
	}
	return "", fmt.Errorf("svc: tidak ada subdirektori di %s yang memuat %s", root, rel)
}
