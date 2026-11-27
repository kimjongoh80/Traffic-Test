//go:build windows

package gui

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows/registry"
)

var (
	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	user32                    = syscall.NewLazyDLL("user32.dll")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
)

// setTitleBarDark switches the native title bar between dark and light.
func setTitleBarDark(hwnd uintptr, dark bool) {
	if hwnd == 0 || procDwmSetWindowAttribute.Find() != nil {
		return
	}
	var v int32
	if dark {
		v = 1
	}
	// DWMWA_USE_IMMERSIVE_DARK_MODE is 20 on Windows 10 20H1+ and 11, 19 before.
	if r, _, _ := procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4); r != 0 {
		procDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&v)), 4)
	}
	// Repaint the frame now rather than on the next activation.
	const swpFlags = 0x0001 | 0x0002 | 0x0004 | 0x0010 | 0x0020 // NOSIZE|NOMOVE|NOZORDER|NOACTIVATE|FRAMECHANGED
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpFlags)
}

// systemDark reports whether Windows apps are set to dark mode.
func systemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// openWindow shows the GUI in a native window backed by Microsoft Edge
// WebView2 (built into Windows 10/11). It blocks until the window is closed
// and returns false when WebView2 is not available.
func openWindow(url string) bool {
	dataPath := ""
	if dir, err := os.UserCacheDir(); err == nil { // %LOCALAPPDATA%
		dataPath = filepath.Join(dir, "TrafficTest", "WebView2")
		_ = os.MkdirAll(dataPath, 0o755)
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  windowWidth,
			Height: windowHeight,
			IconId: 1, // icon resource #1 embedded by go-winres
			Center: true,
		},
	})
	if w == nil {
		return false
	}
	defer w.Destroy()
	hwnd := uintptr(w.Window())
	st := loadSettings()
	setTitleBarDark(hwnd, st.Theme == "dark" || (st.Theme == "system" && systemDark()))
	setWindowTheme = func(theme string, dark bool) {
		w.Dispatch(func() { setTitleBarDark(hwnd, dark) })
	}
	defer func() { setWindowTheme = nil }()
	w.Navigate(url)
	w.Run()
	return true
}
