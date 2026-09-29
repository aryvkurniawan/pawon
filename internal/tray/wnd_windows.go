// Bagian Win32 dari package tray: window tersembunyi, menu klik-kanan, dan
// aksi yang dipicu menu itu.
//
// Shell_NotifyIcon butuh HWND untuk mengirim notifikasi klik, jadi tray
// harus punya window — dibuat message-only (HWND_MESSAGE) supaya tidak
// pernah terlihat dan tidak muncul di taskbar/alt-tab.
package tray

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// --- Konstanta Win32 ---

const (
	wmDestroy   = 0x0002
	wmNull      = 0x0000
	wmRButtonUp = 0x0205
	wmLDblClk   = 0x0203
	wmUser      = 0x0400

	// Pesan yang dikirim Shell ke window kita saat ikon diklik.
	wmTrayCallback = wmUser + 1

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	imageIcon      = 1
	lrLoadFromFile = 0x00000010

	// TaskbarCreated dikirim Explorer saat taskbar dibuat ulang
	// (crash/restart). Ikon baki ikut hilang, jadi harus dipasang ulang.
	// Nilainya diminta ke Windows saat runtime (RegisterWindowMessage) —
	// tidak boleh di-hardcode.
	taskbarCreatedName = "TaskbarCreated"

	hwndMessage = ^uintptr(2) // -3

	mfString    = 0x00000000
	mfPopup     = 0x00000010
	mfSeparator = 0x00000800
	mfDisabled  = 0x00000002

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	miiMState  = 0x00000001
	mfsChecked = 0x00000008

	swShow = 5
)

// ID menu. Item service dialokasikan dinamis dari idServiceBase dengan 3
// slot per service (mulai/berhenti/ulangi), karena jumlah service berubah
// tergantung konfigurasi PHP.
const (
	idOpen = 1000 + iota
	idAutostart
	idQuit
	idServiceBase
)

const svcSlots = 3

// --- Prosedur Win32 ---
//
// Dimuat lazy: tray harus tetap bisa memberi pesan error yang jelas bila
// dipanggil dari session tanpa shell (mis. lewat SSH), bukannya crash saat
// init package.

var (
	shell32  = windows.NewLazySystemDLL("Shell32.dll")
	user32   = windows.NewLazySystemDLL("User32.dll")
	kernel32 = windows.NewLazySystemDLL("Kernel32.dll")

	pShellNotify = shell32.NewProc("Shell_NotifyIconW")
	pShellExec   = shell32.NewProc("ShellExecuteW")

	pGetModule       = kernel32.NewProc("GetModuleHandleW")
	pRegisterClassEx = user32.NewProc("RegisterClassExW")
	pCreateWindowEx  = user32.NewProc("CreateWindowExW")
	pDefWindowProc   = user32.NewProc("DefWindowProcW")
	pDestroyWindow   = user32.NewProc("DestroyWindow")
	pGetMessage      = user32.NewProc("GetMessageW")
	pTranslateMsg    = user32.NewProc("TranslateMessage")
	pDispatchMsg     = user32.NewProc("DispatchMessageW")
	pPostQuit        = user32.NewProc("PostQuitMessage")
	pLoadImage       = user32.NewProc("LoadImageW")
	pDestroyIcon     = user32.NewProc("DestroyIcon")
	pCreatePopupMenu = user32.NewProc("CreatePopupMenu")
	pAppendMenu      = user32.NewProc("AppendMenuW")
	pSetMenuItem     = user32.NewProc("SetMenuItemInfoW")
	pTrackPopupMenu  = user32.NewProc("TrackPopupMenu")
	pGetCursorPos    = user32.NewProc("GetCursorPos")
	pSetForeground   = user32.NewProc("SetForegroundWindow")
	pPostMessage     = user32.NewProc("PostMessageW")
	pDestroyMenu     = user32.NewProc("DestroyMenu")
	pRegisterMessage = user32.NewProc("RegisterWindowMessageW")
)

// --- Struct Win32 ---

// notifyIconData: NOTIFYICONDATAW yang dipakai Shell_NotifyIcon.
type notifyIconData struct {
	Size                       uint32
	Wnd                        windows.Handle
	ID, Flags, CallbackMessage uint32
	Icon                       windows.Handle
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Timeout, Version           uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GuidItem                   windows.GUID
	BalloonIcon                windows.Handle
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type point struct{ X, Y int32 }

// menuItemInfo: MENUITEMINFOW, dipakai untuk menandai item tercentang.
type menuItemInfo struct {
	Size, Mask, Type, State     uint32
	ID                          uint32
	SubMenu, Checked, Unchecked windows.Handle
	ItemData                    uintptr
	TypeData                    *uint16
	Cch                         uint32
	BMPItem                     windows.Handle
}

// --- Window ---

// wmTaskbar diisi saat window dibuat: nilai pesan global TaskbarCreated
// ditentukan runtime oleh RegisterWindowMessage, tidak bisa di-hardcode.
var wmTaskbar uint32

type windowHandle struct{ hwnd windows.Handle }

func newWindow() (*windowHandle, error) {
	if id, _, _ := pRegisterMessage.Call(uintptr(unsafe.Pointer(
		windows.StringToUTF16Ptr(taskbarCreatedName)))); id != 0 {
		wmTaskbar = uint32(id)
	}
	inst, _, _ := pGetModule.Call(0)
	name := windows.StringToUTF16Ptr("PawonTrayWindow")
	wc := wndClassEx{
		WndProc:   windows.NewCallback(wndProc),
		Instance:  windows.Handle(inst),
		ClassName: name,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if res, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); res == 0 {
		// Sudah terdaftar dari proses tray lain yang masih hidup — bukan
		// error fatal, lanjut membuat window.
		if err != windows.ERROR_CLASS_ALREADY_EXISTS {
			return nil, fmt.Errorf("RegisterClassExW: %w", err)
		}
	}
	hwnd, _, err := pCreateWindowEx.Call(
		0, uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Pawon"))),
		0, 0, 0, 0, 0, hwndMessage, 0, inst, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("CreateWindowExW: %w", err)
	}
	return &windowHandle{hwnd: windows.Handle(hwnd)}, nil
}

// messageLoop memompa pesan window sampai PostQuitMessage dipanggil.
func messageLoop() error {
	var m msg
	for {
		ret, _, err := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) == -1 {
			return fmt.Errorf("GetMessageW: %w", err)
		}
		if ret == 0 { // WM_QUIT
			return nil
		}
		_, _, _ = pTranslateMsg.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = pDispatchMsg.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// wndProc menangani klik ikon. Callback Win32 murni — tanpa closure — jadi
// state dibaca lewat variabel paket yang dijaga mutex (lihat rt/mu).
func wndProc(hWnd, message, wParam, lParam uintptr) uintptr {
	// Explorer restart → ikon hilang dari baki; pasang ulang.
	if wmTaskbar != 0 && message == uintptr(wmTaskbar) {
		restoreIcon()
		return 0
	}
	if message == uintptr(wmTrayCallback) {
		switch lParam {
		case wmLDblClk:
			openPanel()
		case wmRButtonUp:
			showMenu(windows.Handle(hWnd))
			_, _, _ = pPostMessage.Call(hWnd, wmNull, 0, 0)
			return 0
		}
	}
	if message == wmDestroy {
		_, _, _ = pPostQuit.Call(0)
		return 0
	}
	ret, _, _ := pDefWindowProc.Call(hWnd, message, wParam, lParam)
	return ret
}

// --- Ikon ---

// lastIcon menyimpan handle ikon terakhir supaya bisa dihancurkan saat
// diganti — menimpa handle tanpa DestroyIcon akan membocorkan GDI object.
var lastIcon windows.Handle

func currentIconHandle() uintptr { return uintptr(lastIcon) }

// setIcon memuat .ico lalu menambah/memperbarui ikon baki.
func setIcon(w *windowHandle, path, tip string) error {
	h, _, err := pLoadImage.Call(0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(path))),
		imageIcon, 0, 0, lrLoadFromFile)
	if h == 0 {
		return fmt.Errorf("LoadImageW %s: %w", path, err)
	}
	if lastIcon != 0 {
		_, _, _ = pDestroyIcon.Call(uintptr(lastIcon))
	}
	lastIcon = windows.Handle(h)

	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	if w != nil {
		nid.Wnd = w.hwnd
	}
	nid.ID = 1
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = wmTrayCallback
	nid.Icon = lastIcon
	copy(nid.Tip[:], windows.StringToUTF16(tip))

	res, _, err := pNotify.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if res == 0 {
		// Sudah ada dari proses tray sebelumnya → cukup ubah.
		if res2, _, _ := pNotify.Call(nimModify, uintptr(unsafe.Pointer(&nid))); res2 == 0 {
			return fmt.Errorf("Shell_NotifyIconW: %w", err)
		}
	}
	return nil
}

// removeIcon menghapus ikon dari baki; dipanggil saat Keluar.
func removeIcon(w *windowHandle) {
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	if w != nil {
		nid.Wnd = w.hwnd
	}
	nid.ID = 1
	_, _, _ = pNotify.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	if lastIcon != 0 {
		_, _, _ = pDestroyIcon.Call(uintptr(lastIcon))
		lastIcon = 0
	}
}

// restoreIcon memasang ulang ikon setelah taskbar dibuat ulang (Explorer
// crash/restart menghapus semua ikon baki).
func restoreIcon() {
	mu.Lock()
	t := rt
	mu.Unlock()
	if t == nil {
		return
	}
	_ = t.apply(t.snap.health())
}

// pNotify di tray_windows.go dipakai bersama; alias supaya nama konsisten.
var pNotify = pShellNotify

// --- Menu ---

// showMenu membangun menu popup dari snapshot terakhir lalu menjalankan
// perintah yang dipilih. Menu dibangun ulang tiap kali dibuka karena jumlah
// & status service berubah-ubah.
func showMenu(hWnd windows.Handle) {
	snap := lastSnapshot()
	m, _, _ := pCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer func() { _, _, _ = pDestroyMenu.Call(m) }()

	append := func(menu uintptr, flags uintptr, id uintptr, text string) {
		_, _, _ = pAppendMenu.Call(menu, flags, id,
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))))
	}
	// Satu submenu per service: Mulai / Berhenti / Ulangi. Aksi yang tidak
	// berlaku dinonaktifkan supaya tidak ada klik yang sia-sia.
	for i, name := range snap.names {
		sub, _, _ := pCreatePopupMenu.Call()
		if sub == 0 {
			continue
		}
		running := snap.runningSet[name]
		base := uintptr(idServiceBase + i*svcSlots)
		flags := uintptr(mfString)
		if running {
			flags |= mfDisabled
		}
		append(sub, flags, base+0, "Mulai")
		flags = mfString
		if !running {
			flags |= mfDisabled
		}
		append(sub, flags, base+1, "Berhenti")
		append(sub, mfString, base+2, "Ulangi")

		tanda := "  ✗"
		if running {
			tanda = "  ✓"
		}
		append(m, mfPopup|mfString, sub, name+tanda)
	}
	append(m, mfSeparator, 0, "")
	append(m, mfString, idOpen, "Buka Panel")
	flags := uintptr(mfString)
	if autostartEnabled() {
		flags |= mfsChecked
	}
	append(m, flags, idAutostart, "Mulai saat login")
	append(m, mfSeparator, 0, "")
	append(m, mfString, idQuit, "Keluar")

	// Menu butuh window foreground — tanpanya menu tidak mau menutup saat
	// diklik di luar.
	_, _, _ = pSetForeground.Call(uintptr(hWnd))
	var pt point
	_, _, _ = pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := pTrackPopupMenu.Call(m,
		tpmLeftAlign|tpmBottomAlign|tpmRightButton|tpmReturnCmd,
		uintptr(int32(pt.X)), uintptr(int32(pt.Y)), 0, uintptr(hWnd), 0)
	if cmd == 0 {
		return // dibatalkan
	}
	handleCommand(uint32(cmd), snap)
}

// handleCommand menjalankan aksi menu. Aksi service dikirim ke API panel —
// tray tidak pernah mengelola proses sendiri, panel tetap sumber kebenaran.
func handleCommand(cmd uint32, snap snapshot) {
	switch {
	case cmd == idOpen:
		openPanel()
	case cmd == idAutostart:
		_ = toggleAutostart()
	case cmd == idQuit:
		mu.Lock()
		w := rt.wnd
		mu.Unlock()
		removeIcon(w)
		_, _, _ = pPostQuit.Call(0)
	case cmd >= idServiceBase:
		i := int(cmd-idServiceBase) / svcSlots
		op := []string{"start", "stop", "restart"}[int(cmd-idServiceBase)%svcSlots]
		if i < len(snap.names) {
			go serviceAction(snap.names[i], op) // jangan blokir loop pesan
		}
	}
}

// serviceAction memanggil API panel. Guard panel mewajibkan Host dan
// Content-Type, jadi keduanya dikirim eksplisit.
func serviceAction(name, op string) {
	url := fmt.Sprintf("%s/api/services/%s/%s", PanelURL, name, op)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return
	}
	req.Host = "127.0.0.1:7080"
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = res.Body.Close()
	}
	triggerRefresh() // jangan tunggu tick berikutnya
}

// openPanel membuka UI di browser bawaan.
func openPanel() {
	verb := windows.StringToUTF16Ptr("open")
	target := windows.StringToUTF16Ptr(PanelURL + "/")
	_, _, _ = pShellExec.Call(0, uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)), 0, 0, swShow)
}

// --- Autostart (Run key HKCU) ---
//
// Tray hidup di session pengguna, jadi autostart memakai Run key HKCU —
// bukan service, yang justru tidak bisa menampilkan ikon.

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Pawon")
	return err == nil && v != ""
}

// EnableAutostart mendaftarkan tray ke Run key. Dipanggil `service install`
// supaya ikon muncul otomatis setelah login berikutnya.
func EnableAutostart() error {
	if autostartEnabled() {
		return nil
	}
	return setAutostart(true)
}

func toggleAutostart() error { return setAutostart(!autostartEnabled()) }

func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		// Nilai bisa saja tidak ada (dihapus dari luar) — anggap sudah selesai.
		if err := k.DeleteValue("Pawon"); err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Mode tray, bukan panel: panel sudah dijalankan service.
	return k.SetStringValue("Pawon", `"`+exe+`" tray`)
}

// Start menjalankan tray di proses terpisah (detached) — dipakai installer
// supaya ikon langsung muncul tanpa menunggu login berikutnya.
func Start() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "tray")
	if err := cmd.Start(); err != nil {
		return err
	}
	// Lepas dari proses ini: tray harus bertahan setelah installer keluar.
	go func() { _ = cmd.Wait() }()
	return nil
}
