package gui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kimjongoh80/Traffic-Test/internal/engine"
)

// Record is one saved test: the engine result plus criteria and verdict.
type Record struct {
	ID        string          `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	Target    string          `json:"target"`
	Stopped   bool            `json:"stopped,omitempty"`
	Result    *engine.Result  `json:"result"`
	Criteria  engine.Criteria `json:"criteria"`
	Verdict   engine.Verdict  `json:"verdict"`
}

// RecordInfo is the short form used by the history list.
type RecordInfo struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	Target      string    `json:"target"`
	Protocol    string    `json:"protocol"`
	Reverse     bool      `json:"reverse"`
	Streams     int       `json:"streams"`
	DurationSec float64   `json:"duration_sec"`
	BitsPerSec  float64   `json:"bits_per_sec"`
	LossPct     float64   `json:"loss_pct"`
	RTTMs       float64   `json:"rtt_ms"`
	Evaluated   bool      `json:"evaluated"`
	Pass        bool      `json:"pass"`
	Stopped     bool      `json:"stopped,omitempty"`
}

func (r *Record) info() RecordInfo {
	ri := RecordInfo{ID: r.ID, CreatedAt: r.CreatedAt, Target: r.Target, Stopped: r.Stopped,
		Evaluated: r.Verdict.Evaluated, Pass: r.Verdict.Pass}
	if r.Result != nil {
		p, s := r.Result.Params, r.Result.Summary
		ri.Protocol, ri.Reverse, ri.Streams = p.Protocol, p.Reverse, p.Streams
		ri.DurationSec, ri.BitsPerSec, ri.LossPct = s.DurationSec, s.BitsPerSec, s.LossPct
		if s.Latency != nil {
			ri.RTTMs = s.Latency.AvgMs
		}
	}
	return ri
}

var idRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

func newID() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

type store struct {
	dir   string
	mu    sync.Mutex
	infos map[string]RecordInfo
}

func defaultResultsDir() string {
	exe, exeErr := os.Executable()
	// Never write inside a macOS .app bundle; use ~/Documents instead.
	inBundle := exeErr == nil && strings.Contains(exe, ".app/Contents/MacOS")
	if inBundle {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Documents", "TrafficTest", "results")
		}
	}
	if exe, err := exe, exeErr; err == nil {
		d := filepath.Join(filepath.Dir(exe), "results")
		if os.MkdirAll(d, 0o755) == nil {
			if f, err := os.CreateTemp(d, ".w"); err == nil {
				f.Close()
				os.Remove(f.Name())
				return d
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "TrafficTest", "results")
	}
	return filepath.Join(os.TempDir(), "ttest-results")
}

func openStore(dir string) (*store, error) {
	if dir == "" {
		dir = defaultResultsDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &store{dir: dir, infos: map[string]RecordInfo{}}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r Record
		if json.Unmarshal(b, &r) == nil && idRe.MatchString(r.ID) {
			s.infos[r.ID] = r.info()
		}
	}
	return s, nil
}

func (s *store) path(id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", errors.New("invalid id")
	}
	return filepath.Join(s.dir, "ttest-"+id+".json"), nil
}

func (s *store) save(r *Record) error {
	p, err := s.path(r.ID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return err
	}
	s.mu.Lock()
	s.infos[r.ID] = r.info()
	s.mu.Unlock()
	return nil
}

func (s *store) get(id string) (*Record, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, errors.New("not found")
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *store) remove(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.infos, id)
	s.mu.Unlock()
	return os.Remove(p)
}

func (s *store) list() []RecordInfo {
	s.mu.Lock()
	out := make([]RecordInfo, 0, len(s.infos))
	for _, ri := range s.infos {
		out = append(out, ri)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
