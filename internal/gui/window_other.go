//go:build !windows && !(darwin && cgo)

package gui

// openWindow is not available on this platform/build; the GUI falls back to
// the default browser.
func openWindow(url string) bool { return false }
