// Package tray: ikon baki sistem (notification area) untuk memantau panel.
//
// Kenapa paket ini tidak bisa dijalankan dari dalam service: sejak Vista,
// service terisolasi di Session 0 dan tidak punya desktop. Ikon yang dibuat
// dari Session 0 tidak pernah muncul di baki pengguna (yang ada di session
// 1+). Karena itu `pawon.exe tray` harus berjalan sebagai proses terpisah di
// session pengguna — didaftarkan ke Run key HKCU — dan memantau panel lewat
// HTTP, bukan lewat state internal.
package tray

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

// PanelURL: alamat panel. Variabel (bukan const) supaya test bisa menunjuk
// ke server palsu. Tray hanya memantau panel lewat HTTP — panel dijalankan
// service, tray berjalan terpisah di session pengguna.
var PanelURL = "http://127.0.0.1:7080"

// status: bentuk respons GET /api/status yang dipakai tray. Sengaja hanya
// field yang dibutuhkan — panel boleh menambah field lain tanpa merusak tray.
type status struct {
	Services []struct {
		Name    string `json:"Name"`
		Running bool   `json:"Running"`
		PID     int    `json:"PID"`
	} `json:"services"`
	Sites  int `json:"sites"`
	Tunnel *struct {
		Healthy bool `json:"healthy"`
	} `json:"tunnel"`
}

type snapshot struct {
	ok         bool
	total      int
	running    int
	sites      int
	tunnel     string
	names      []string
	runningSet map[string]bool
}

// fetch menanyai panel. error jaringan bukan kegagalan fatal: itu justru
// informasi yang ingin ditampilkan (panel mati → ikon merah).
func fetch(ctx context.Context) snapshot {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PanelURL+"/api/status", nil)
	if err != nil {
		return snapshot{}
	}
	// Guard Host panel menolak host selain 127.0.0.1/localhost:7080.
	req.Host = "127.0.0.1:7080"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return snapshot{}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return snapshot{}
	}
	var s status
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return snapshot{}
	}
	snap := snapshot{
		ok:         true,
		total:      len(s.Services),
		sites:      s.Sites,
		tunnel:     "nonaktif",
		names:      make([]string, 0, len(s.Services)),
		runningSet: make(map[string]bool, len(s.Services)),
	}
	for _, sv := range s.Services {
		if sv.Running {
			snap.running++
		}
		snap.runningSet[sv.Name] = sv.Running
		snap.names = append(snap.names, sv.Name)
	}
	if s.Tunnel != nil && s.Tunnel.Healthy {
		snap.tunnel = "aktif"
	}
	sort.Strings(snap.names)
	return snap
}

func (s snapshot) health() health {
	switch {
	case !s.ok || s.total == 0:
		return healthDown
	case s.running == s.total:
		return healthOK
	default:
		return healthWarn
	}
}

// tooltip: teks saat kursor di atas ikon. Windows memotong di 128 karakter
// untuk tooltip lama, jadi dijaga pendek.
func (s snapshot) tooltip() string {
	if !s.ok {
		return "Pawon — panel tidak merespons"
	}
	return fmt.Sprintf("Pawon — %d/%d service jalan, %d site, tunnel %s",
		s.running, s.total, s.sites, s.tunnel)
}

// --- Runtime ---

// rt & mu: wndProc adalah callback Win32 murni (tanpa closure), jadi ia
// hanya bisa membaca lewat variabel paket. Semua akses dijaga mutex karena
// poll berjalan di goroutine sendiri.
var (
	mu sync.Mutex
	rt *trayRuntime
)

type trayRuntime struct {
	icons     map[health]string
	autostart bool
	wnd       *windowHandle
	icon      uintptr
	snap      snapshot
}

// writeIcons menulis tiga berkas .ico ke direktori cache pengguna.
//
// Kenapa berkas di disk, bukan dari memori: LoadImage hanya menerima jalur
// berkas atau resource module. Menulis ke cache pengguna adalah kompromi
// paling sederhana tanpa menambah resource .syso ke build.
func writeIcons() (map[health]string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "pawon")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	out := make(map[health]string, 3)
	for h, name := range map[health]string{
		healthOK: "tray-ok.ico", healthWarn: "tray-warn.ico", healthDown: "tray-down.ico",
	} {
		b, err := iconBytes(h)
		if err != nil {
			return nil, err
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return nil, err
		}
		out[h] = p
	}
	return out, nil
}

// Run menjalankan ikon baki sampai pengguna memilih Keluar.
//
// LockOSThread wajib: window Win32 terikat ke thread yang membuatnya, dan
// loop pesan harus berjalan di thread itu. Tanpa ini goroutine bisa pindah
// thread di tengah jalan dan window berhenti menerima pesan.
func Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	icons, err := writeIcons()
	if err != nil {
		return err
	}
	t := &trayRuntime{icons: icons}
	mu.Lock()
	rt = t
	mu.Unlock()
	return t.loop()
}

func (t *trayRuntime) loop() error {
	w, err := newWindow()
	if err != nil {
		return err
	}
	t.wnd = w
	// Mulai merah sampai polling pertama selesai — lebih jujur daripada
	// menampilkan hijau untuk keadaan yang belum diketahui.
	if err := t.apply(healthDown); err != nil {
		return err
	}
	go t.poll()
	return messageLoop()
}

func (t *trayRuntime) poll() {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	t.refresh()
	for range tick.C {
		t.refresh()
	}
}

// refresh mengambil status baru dan memperbarui ikon bila perlu.
func (t *trayRuntime) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	next := fetch(ctx)

	mu.Lock()
	prev := t.snap.health()
	t.snap = next
	wnd, iconPath := t.wnd, t.icons[next.health()]
	mu.Unlock()

	// Hanya panggil Shell_NotifyIcon kalau ada yang berubah: mengubah ikon
	// tepat saat pengguna membuka menu bisa membuat menu tidak muncul.
	if next.health() == prev {
		return
	}
	_ = setIcon(wnd, iconPath, next.tooltip())
}

// apply menampilkan/memperbarui ikon dengan status tertentu.
func (t *trayRuntime) apply(h health) error {
	mu.Lock()
	wnd := t.wnd
	mu.Unlock()
	if err := setIcon(wnd, t.icons[h], t.snap.tooltip()); err != nil {
		return err
	}
	mu.Lock()
	t.icon = currentIconHandle()
	mu.Unlock()
	return nil
}

// triggerRefresh dipanggil setelah aksi service supaya perubahan langsung
// terlihat, tanpa menunggu tick berikutnya.
func triggerRefresh() {
	mu.Lock()
	t := rt
	mu.Unlock()
	if t != nil {
		t.refresh()
	}
}

func lastSnapshot() snapshot {
	mu.Lock()
	defer mu.Unlock()
	if rt == nil {
		return snapshot{}
	}
	return rt.snap
}
