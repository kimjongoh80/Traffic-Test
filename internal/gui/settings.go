package gui

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Settings are user preferences kept between runs, in the user's config
// folder (%AppData%\TrafficTest on Windows, ~/Library/Application Support/
// TrafficTest on macOS), so they survive updates of the program itself.
type Settings struct {
	Theme      string  `json:"theme"`       // system, light or dark
	UIScale    float64 `json:"ui_scale"`    // multiplier on top of the automatic size
	FontScale  float64 `json:"font_scale"`  // text size multiplier
	ResultsDir string  `json:"results_dir"` // empty = default folder
}

var settingsMu sync.Mutex

func settingsPath() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "TrafficTest", "settings.json")
}

func loadSettings() Settings {
	s := Settings{Theme: "system", UIScale: 1, FontScale: 1}
	if p := settingsPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	return s.clean()
}

func (s Settings) clean() Settings {
	switch s.Theme {
	case "light", "dark":
	default:
		s.Theme = "system"
	}
	if s.UIScale < 0.6 || s.UIScale > 1.6 {
		s.UIScale = 1
	}
	if s.FontScale < 0.7 || s.FontScale > 2 {
		s.FontScale = 1
	}
	return s
}

func saveSettings(s Settings) error {
	p := settingsPath()
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// updateSettings applies fn to the saved settings and writes them back.
func updateSettings(fn func(*Settings)) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := loadSettings()
	fn(&s)
	s = s.clean()
	return s, saveSettings(s)
}

// GET returns the settings; POST updates theme and/or UI scale.
func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Theme     *string  `json:"theme"`
			UIScale   *float64 `json:"ui_scale"`
			FontScale *float64 `json:"font_scale"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if _, err := updateSettings(func(s *Settings) {
			if req.Theme != nil {
				s.Theme = *req.Theme
			}
			if req.UIScale != nil {
				s.UIScale = *req.UIScale
			}
			if req.FontScale != nil {
				s.FontScale = *req.FontScale
			}
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
	}
	s := loadSettings()
	s.ResultsDir = a.store.Dir()
	writeJSON(w, 200, s)
}

// handleResultsDir switches the history folder. An empty dir means the
// default folder. Records already saved stay where they are.
func (a *App) handleResultsDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	dir := strings.TrimSpace(req.Dir)
	if dir != "" && !filepath.IsAbs(dir) {
		writeJSON(w, 400, map[string]string{"error": "전체 경로를 입력하세요 (예: D:\\TrafficTest\\results)"})
		return
	}
	if err := a.store.reopen(dir); err != nil {
		writeJSON(w, 400, map[string]string{"error": "이 폴더를 쓸 수 없습니다: " + err.Error()})
		return
	}
	if _, err := updateSettings(func(s *Settings) { s.ResultsDir = dir }); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	a.hub.publish("history", nil)
	writeJSON(w, 200, map[string]string{"results_dir": a.store.Dir()})
}

// handlePickDir shows the system folder picker and returns the chosen path.
func (a *App) handlePickDir(w http.ResponseWriter, r *http.Request) {
	dir, err := pickFolder(a.store.Dir())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"dir": dir}) // "" = cancelled
}

func (a *App) handleOpenDir(w http.ResponseWriter, r *http.Request) {
	dir := a.store.Dir()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	_ = cmd.Start()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func pickFolder(start string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.FolderBrowserDialog
$f.Description = 'Traffic-Test 테스트 이력을 저장할 폴더를 선택하세요'
$f.ShowNewFolderButton = $true
$f.SelectedPath = $env:TT_START
$o = New-Object System.Windows.Forms.Form -Property @{TopMost = $true; ShowInTaskbar = $false}
if ($f.ShowDialog($o) -eq 'OK') { [Console]::OutputEncoding = [Text.Encoding]::UTF8; [Console]::Out.Write($f.SelectedPath) }`
		cmd = exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps)
		hideConsole(cmd)
	case "darwin":
		cmd = exec.Command("osascript",
			"-e", `set p to POSIX file (system attribute "TT_START")`,
			"-e", `try`,
			"-e", `POSIX path of (choose folder with prompt "Traffic-Test 테스트 이력을 저장할 폴더를 선택하세요" default location p)`,
			"-e", `on error number -128`,
			"-e", `""`,
			"-e", `end try`)
	default:
		cmd = exec.Command("zenity", "--file-selection", "--directory", "--filename="+start+"/")
	}
	cmd.Env = append(os.Environ(), "TT_START="+start)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) == 0 {
			return "", nil // dialog cancelled
		}
		return "", err
	}
	p := strings.TrimSpace(string(out))
	if p != "" && runtime.GOOS == "darwin" && len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p, nil
}

// handleWindowTheme lets the page tell the native window which theme it shows.
func (a *App) handleWindowTheme(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Theme string `json:"theme"`
		Dark  bool   `json:"dark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if f := setWindowTheme; f != nil {
		f(req.Theme, req.Dark)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
