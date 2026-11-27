//go:build darwin && cgo

package gui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// WKWebView only handles copy/paste shortcuts when the app has an Edit menu.
static void ttSetupMenu(void) {
	NSMenu *bar = [[NSMenu alloc] init];
	NSMenuItem *appItem = [[NSMenuItem alloc] init];
	[bar addItem:appItem];
	NSMenu *appMenu = [[NSMenu alloc] init];
	[appMenu addItemWithTitle:@"Hide Traffic-Test" action:@selector(hide:) keyEquivalent:@"h"];
	[appMenu addItem:[NSMenuItem separatorItem]];
	[appMenu addItemWithTitle:@"Quit Traffic-Test" action:@selector(terminate:) keyEquivalent:@"q"];
	[appItem setSubmenu:appMenu];

	NSMenuItem *editItem = [[NSMenuItem alloc] init];
	[bar addItem:editItem];
	NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
	[edit addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
	[edit addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"Z"];
	[edit addItem:[NSMenuItem separatorItem]];
	[edit addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
	[edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
	[edit addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
	[edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
	[editItem setSubmenu:edit];
	[NSApp setMainMenu:bar];
}

// ttSetAppearance: 0 follows the system, 1 light, 2 dark. It colors the
// title bar and the rest of the native window chrome.
static void ttSetAppearance(int mode) {
	NSAppearance *a = nil;
	if (mode == 1) a = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
	if (mode == 2) a = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
	[NSApp setAppearance:a];
}
*/
import "C"

import webview "github.com/webview/webview_go"

// openWindow shows the GUI in a native macOS window (WKWebView). It blocks
// until the window is closed.
func openWindow(url string) bool {
	w := webview.New(false)
	if w == nil {
		return false
	}
	defer w.Destroy()
	C.ttSetupMenu()
	C.ttSetAppearance(appearanceMode(loadSettings().Theme))
	setWindowTheme = func(theme string, dark bool) {
		w.Dispatch(func() { C.ttSetAppearance(appearanceMode(theme)) })
	}
	defer func() { setWindowTheme = nil }()
	w.SetTitle(windowTitle)
	w.SetSize(windowWidth, windowHeight, webview.HintNone)
	w.Navigate(url)
	w.Run()
	return true
}

func appearanceMode(theme string) C.int {
	switch theme {
	case "light":
		return 1
	case "dark":
		return 2
	}
	return 0
}
