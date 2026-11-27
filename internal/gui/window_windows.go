//go:build windows

package gui

import (
	"os"
	"path/filepath"

	webview2 "github.com/jchv/go-webview2"
)

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
	w.Navigate(url)
	w.Run()
	return true
}
