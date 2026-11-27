package gui

import "runtime"

// Native windows (WebView2 on Windows, WKWebView on macOS) must run on the
// process's main thread. Package init runs on the main goroutine, so locking
// here keeps the main goroutine on the main OS thread.
func init() { runtime.LockOSThread() }

const (
	windowTitle  = "Traffic-Test"
	windowWidth  = 1440
	windowHeight = 940
)
