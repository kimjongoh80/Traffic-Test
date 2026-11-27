// Package gui serves the Traffic-Test web interface: a local HTTP server that
// drives the measurement engine and streams live results to the browser.
package gui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kimjongoh80/Traffic-Test/internal/engine"
	"github.com/kimjongoh80/Traffic-Test/internal/report"
)

//go:embed web
var webFS embed.FS

// Options configure the GUI.
type Options struct {
	WebAddr     string // e.g. 127.0.0.1:8080
	ServerPort  int    // ttest server port to listen on
	StartServer bool   // start the test server immediately
	OpenBrowser bool   // show the GUI (app window, or browser as fallback)
	Browser     bool   // use the default browser instead of an app window
	ResultsDir  string
	Version     string
}

// App holds the GUI state.
type App struct {
	opt   Options
	hub   *hub
	store *store
	host  string

	srvMu   sync.Mutex
	srv     *engine.Server
	srvPort int
	srvErr  string

	testMu     sync.Mutex
	live       *liveTest
	cancel     context.CancelFunc
	serverLive map[string]*liveTest
}

type liveTest struct {
	ID        string            `json:"id"`
	Role      string            `json:"role"` // "client": started here; "server": started by the peer
	Target    string            `json:"target"`
	Params    engine.TestParams `json:"params"`
	Criteria  engine.Criteria   `json:"criteria"`
	StartedAt time.Time         `json:"started_at"`
	Intervals []engine.Interval `json:"intervals"`
}

// Run starts the GUI and blocks.
func Run(opt Options) error {
	if opt.ServerPort == 0 {
		opt.ServerPort = engine.DefaultPort
	}
	if opt.WebAddr == "" {
		opt.WebAddr = "127.0.0.1:8080"
	}
	// If Traffic-Test is already running, just bring its page up again.
	if url, ok := runningInstance(opt.WebAddr); ok {
		fmt.Println("Traffic-Test is already running:", url)
		if opt.OpenBrowser {
			openBrowser(url)
			time.Sleep(time.Second)
		}
		return nil
	}
	settings := loadSettings()
	dir := opt.ResultsDir
	if dir == "" {
		dir = settings.ResultsDir
	}
	st, err := openStore(dir)
	if err != nil && dir != opt.ResultsDir {
		// The saved folder is gone (unplugged drive etc.): fall back to the default.
		st, err = openStore(opt.ResultsDir)
	}
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	a := &App{opt: opt, hub: newHub(), store: st, host: host}

	ln, err := listenWithFallback(opt.WebAddr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	if strings.HasPrefix(ln.Addr().String(), "0.0.0.0:") || strings.HasPrefix(ln.Addr().String(), "[::]:") {
		url = "http://localhost:" + portOf(ln.Addr())
	}
	fmt.Printf("Traffic-Test GUI: %s\n", url)
	fmt.Printf("Results folder : %s\n", st.dir)

	if opt.StartServer {
		if err := a.startServer(opt.ServerPort); err != nil {
			fmt.Println("test server not started:", err)
		}
	}
	if !opt.OpenBrowser {
		return http.Serve(ln, a.routes())
	}
	errCh := make(chan error, 1)
	go func() { errCh <- http.Serve(ln, a.routes()) }()
	if !opt.Browser && openWindow(url) {
		// The app window was closed: stop everything.
		a.shutdown()
		return nil
	}
	go openBrowser(url)
	return <-errCh
}

func (a *App) shutdown() {
	a.testMu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.testMu.Unlock()
	a.srvMu.Lock()
	if a.srv != nil {
		a.srv.Close()
	}
	a.srvMu.Unlock()
}

func runningInstance(addr string) (string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, port)
	c := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := c.Get(url + "/api/status")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var st struct {
		Version  string `json:"version"`
		Hostname string `json:"hostname"`
	}
	if json.NewDecoder(resp.Body).Decode(&st) != nil || st.Hostname == "" {
		return "", false
	}
	return url, true
}

func portOf(a net.Addr) string {
	_, p, _ := net.SplitHostPort(a.String())
	return p
}

func listenWithFallback(addr string) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var p int
	fmt.Sscanf(port, "%d", &p)
	var lastErr error
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(p+i)))
		if err == nil {
			return ln, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func (a *App) routes() http.Handler {
	_ = mime.AddExtensionType(".js", "text/javascript")
	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/events", a.handleEvents)
	mux.HandleFunc("/api/server/start", a.post(a.handleServerStart))
	mux.HandleFunc("/api/server/stop", a.post(a.handleServerStop))
	mux.HandleFunc("/api/test/start", a.post(a.handleTestStart))
	mux.HandleFunc("/api/test/stop", a.post(a.handleTestStop))
	mux.HandleFunc("/api/quit", a.post(a.handleQuit))
	mux.HandleFunc("/api/settings", a.handleSettings)
	mux.HandleFunc("/api/results-dir", a.post(a.handleResultsDir))
	mux.HandleFunc("/api/results-dir/pick", a.post(a.handlePickDir))
	mux.HandleFunc("/api/results-dir/open", a.post(a.handleOpenDir))
	mux.HandleFunc("/api/results", a.handleResults)
	mux.HandleFunc("/api/results/", a.handleResult)
	return mux
}

// post only accepts JSON POSTs, which also blocks simple cross-site requests.
func (a *App) post(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "POST application/json required", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// ---- status ----

type statusResp struct {
	Version    string    `json:"version"`
	Hostname   string    `json:"hostname"`
	OS         string    `json:"os"`
	Addresses  []string  `json:"addresses"`
	Server     srvStatus `json:"server"`
	Live       *liveTest `json:"live,omitempty"`
	ResultsDir string    `json:"results_dir"`
}

type srvStatus struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	Error   string `json:"error,omitempty"`
}

func localAddrs() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, ad := range addrs {
			ipn, ok := ad.(*net.IPNet)
			if !ok || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, fmt.Sprintf("%s (%s)", ipn.IP.String(), ifc.Name))
		}
	}
	return out
}

func (a *App) serverStatus() srvStatus {
	a.srvMu.Lock()
	defer a.srvMu.Unlock()
	port := a.srvPort
	if port == 0 {
		port = a.opt.ServerPort
	}
	return srvStatus{Running: a.srv != nil, Port: port, Error: a.srvErr}
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.testMu.Lock()
	var live *liveTest
	if a.live != nil {
		cp := *a.live
		cp.Intervals = append([]engine.Interval(nil), a.live.Intervals...)
		live = &cp
	}
	a.testMu.Unlock()
	writeJSON(w, 200, statusResp{
		Version: a.opt.Version, Hostname: a.host, OS: runtime.GOOS + "/" + runtime.GOARCH,
		Addresses: localAddrs(), Server: a.serverStatus(), Live: live, ResultsDir: a.store.Dir(),
	})
}

// ---- test server ----

type logWriter struct{ a *App }

func (l logWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	os.Stdout.Write(p)
	l.a.hub.publish("log", map[string]any{"time": time.Now(), "message": line})
	return len(p), nil
}

func (a *App) startServer(port int) error {
	a.srvMu.Lock()
	if a.srv != nil {
		a.srvMu.Unlock()
		return errors.New("server already running")
	}
	srv := &engine.Server{Port: port, Logger: log.New(logWriter{a}, "", 0),
		OnStart: a.serverTestStart, OnInterval: a.serverTestInterval, OnEnd: a.serverTestEnd}
	a.srv, a.srvPort, a.srvErr = srv, port, ""
	a.srvMu.Unlock()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		a.srvMu.Lock()
		a.srv = nil
		if err != nil {
			a.srvErr = err.Error()
		}
		a.srvMu.Unlock()
		a.hub.publish("server", a.serverStatus())
		if err == nil {
			err = errors.New("server stopped")
		}
		return err
	case <-time.After(200 * time.Millisecond):
	}
	go func() {
		err := <-errCh
		a.srvMu.Lock()
		if a.srv == srv {
			a.srv = nil
			if err != nil {
				a.srvErr = err.Error()
			}
		}
		a.srvMu.Unlock()
		a.hub.publish("server", a.serverStatus())
	}()
	a.hub.publish("server", a.serverStatus())
	return nil
}

func (a *App) handleServerStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port int `json:"port"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Port <= 0 || req.Port > 65000 {
		req.Port = a.opt.ServerPort
	}
	if err := a.startServer(req.Port); err != nil {
		writeErr(w, 409, err)
		return
	}
	writeJSON(w, 200, a.serverStatus())
}

func (a *App) handleServerStop(w http.ResponseWriter, r *http.Request) {
	a.srvMu.Lock()
	srv := a.srv
	a.srv = nil
	a.srvMu.Unlock()
	if srv != nil {
		srv.Close()
	}
	a.hub.publish("server", a.serverStatus())
	writeJSON(w, 200, a.serverStatus())
}

// ---- tests ----

type startReq struct {
	Host     string            `json:"host"`
	Port     int               `json:"port"`
	Params   engine.TestParams `json:"params"`
	Criteria engine.Criteria   `json:"criteria"`
}

func (a *App) handleTestStart(w http.ResponseWriter, r *http.Request) {
	var req startReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	req.Host = strings.TrimSpace(req.Host)
	if req.Host == "" {
		writeErr(w, 400, errors.New("서버 주소를 입력하세요"))
		return
	}
	if req.Port == 0 {
		req.Port = engine.DefaultPort
	}
	if err := req.Params.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.testMu.Lock()
	if a.live != nil {
		a.testMu.Unlock()
		writeErr(w, 409, errors.New("이미 테스트가 진행 중입니다"))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	req.Params.Criteria = &req.Criteria
	lt := &liveTest{
		ID: newID(), Role: "client", Target: net.JoinHostPort(req.Host, fmt.Sprint(req.Port)),
		Params: req.Params, Criteria: req.Criteria, StartedAt: time.Now(),
	}
	a.live, a.cancel = lt, cancel
	a.testMu.Unlock()

	a.hub.publish("test_start", lt)
	go a.runTest(ctx, lt, req)
	writeJSON(w, 200, lt)
}

func (a *App) runTest(ctx context.Context, lt *liveTest, req startReq) {
	res, err := engine.RunClient(ctx, req.Host, req.Port, req.Params, func(iv engine.Interval) {
		a.testMu.Lock()
		lt.Intervals = append(lt.Intervals, iv)
		a.testMu.Unlock()
		a.hub.publish("interval", map[string]any{"id": lt.ID, "interval": iv})
	})
	a.testMu.Lock()
	a.live, a.cancel = nil, nil
	a.testMu.Unlock()
	if err != nil {
		a.hub.publish("test_error", map[string]any{"id": lt.ID, "error": err.Error()})
		return
	}
	rec := &Record{
		ID: lt.ID, Role: "client", CreatedAt: lt.StartedAt, Target: lt.Target,
		Result: res, Criteria: req.Criteria, Verdict: engine.Evaluate(res, req.Criteria),
	}
	if ctx.Err() != nil {
		rec.Stopped = true
	}
	if err := a.store.save(rec); err != nil {
		log.Printf("saving result: %v", err)
	}
	a.hub.publish("test_done", rec)
}

// ---- tests started by the peer on this PC's server ----

func (a *App) serverTestStart(ti engine.TestInfo) {
	crit := engine.Criteria{}
	if ti.Params.Criteria != nil {
		crit = *ti.Params.Criteria
	}
	target := ti.Remote
	if ti.ClientHost != "" {
		target = ti.ClientHost + " (" + ti.Remote + ")"
	}
	lt := &liveTest{ID: newID(), Role: "server", Target: target, Params: ti.Params, Criteria: crit, StartedAt: ti.StartedAt}
	a.testMu.Lock()
	if a.live == nil {
		a.live = lt
	}
	a.serverLive = map[string]*liveTest{ti.ID: lt}
	a.testMu.Unlock()
	a.hub.publish("test_start", lt)
}

func (a *App) serverLiveFor(engineID string) *liveTest {
	a.testMu.Lock()
	defer a.testMu.Unlock()
	return a.serverLive[engineID]
}

func (a *App) serverTestInterval(engineID string, iv engine.Interval) {
	lt := a.serverLiveFor(engineID)
	if lt == nil {
		return
	}
	a.testMu.Lock()
	lt.Intervals = append(lt.Intervals, iv)
	a.testMu.Unlock()
	a.hub.publish("interval", map[string]any{"id": lt.ID, "interval": iv})
}

func (a *App) serverTestEnd(engineID string, res *engine.Result, err error) {
	lt := a.serverLiveFor(engineID)
	a.testMu.Lock()
	if a.live == lt {
		a.live = nil
	}
	delete(a.serverLive, engineID)
	a.testMu.Unlock()
	if lt == nil {
		return
	}
	if err != nil || res == nil {
		msg := "테스트가 비정상 종료되었습니다"
		if err != nil {
			msg = err.Error()
		}
		a.hub.publish("test_error", map[string]any{"id": lt.ID, "error": msg})
		return
	}
	rec := &Record{ID: lt.ID, Role: "server", CreatedAt: lt.StartedAt, Target: lt.Target,
		Result: res, Criteria: lt.Criteria, Verdict: engine.Evaluate(res, lt.Criteria)}
	if res.Summary.DurationSec+0.5 < res.Params.DurationSec {
		rec.Stopped = true
	}
	if err := a.store.save(rec); err != nil {
		log.Printf("saving result: %v", err)
	}
	a.hub.publish("test_done", rec)
}

func (a *App) handleTestStop(w http.ResponseWriter, r *http.Request) {
	a.testMu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.testMu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleQuit stops the whole program (there is no window to close when it
// was started from a double-click on macOS).
func (a *App) handleQuit(w http.ResponseWriter, r *http.Request) {
	a.testMu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.testMu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.shutdown()
		fmt.Println("Traffic-Test stopped from the web page.")
		os.Exit(0)
	}()
}

// ---- results ----

func (a *App) handleResults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.store.list())
}

func (a *App) handleResult(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/results/")
	if base, kind, ok := strings.Cut(id, "/"); ok {
		a.handleExport(w, r, base, kind)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rec, err := a.store.get(id)
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ttest-%s.json"`, id))
		}
		writeJSON(w, 200, rec)
	case http.MethodDelete:
		if err := a.store.remove(id); err != nil {
			writeErr(w, 404, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (a *App) reportInput(rec *Record) report.Input {
	return report.Input{ID: rec.ID, Target: rec.Target, Stopped: rec.Stopped, Result: rec.Result,
		Criteria: rec.Criteria, Verdict: rec.Verdict, Version: a.opt.Version}
}

// handleExport serves /api/results/{id}/report (HTML, print to PDF) and
// /api/results/{id}/csv. Add ?download=1 to save as a file.
func (a *App) handleExport(w http.ResponseWriter, r *http.Request, id, kind string) {
	rec, err := a.store.get(id)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	dl := r.URL.Query().Get("download") == "1"
	switch kind {
	case "report":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if dl {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ttest-report-%s.html"`, id))
		}
		if err := report.HTML(w, a.reportInput(rec)); err != nil {
			http.Error(w, err.Error(), 500)
		}
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ttest-%s.csv"`, id))
		if err := report.CSV(w, rec.Result); err != nil {
			http.Error(w, err.Error(), 500)
		}
	default:
		http.NotFound(w, r)
	}
}

// ---- server-sent events ----

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := a.hub.subscribe()
	defer a.hub.unsubscribe(ch)
	_, _ = io.WriteString(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := w.Write(msg); err != nil {
				return
			}
			fl.Flush()
		case <-ping.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newHub() *hub { return &hub{subs: map[chan []byte]struct{}{}} }

func (h *hub) subscribe() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *hub) publish(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte("event: " + event + "\ndata: " + string(b) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // slow client: drop
		}
	}
}
