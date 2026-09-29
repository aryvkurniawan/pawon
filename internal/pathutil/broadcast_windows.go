package pathutil

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// broadcastEnvironmentChange menyiarkan WM_SETTINGCHANGE ke semua jendela
// top-level agar Explorer & terminal me-refresh environment setelah PATH
// diubah. Tanpa ini, PATH baru hanya terlihat di proses yang di-spawn setelah
// Explorer me-reload — dalam praktiknya: setelah log-off/login.
//
// x/sys/windows tidak mengekspor SendMessageTimeout, jadi dipanggil lewat
// LazyProc user32: fungsi ini opsional, kegagalan (mesin tanpa user32,
// sesi non-interaktif) hanya menghilangkan refresh, bukan penulisan PATH.
func broadcastEnvironmentChange() {
	const (
		wmSettingChange = 0x001A
		hwndBroadcast   = 0xFFFF
		smtoAbortIfHung = 0x0002
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	if err := proc.Find(); err != nil {
		return
	}
	// lParam menunjuk string "Environment" (UTF-16, NUL-terminated).
	s, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	// Parameter tak terpakai (0) perlu uintptr(0); Go tidak bisa mengambil
	// alamat konstanta, dan SendMessageTimeout butuh pointer valid atau NULL.
	proc.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(s)),
		smtoAbortIfHung,
		5000,
		0,
	)
}
