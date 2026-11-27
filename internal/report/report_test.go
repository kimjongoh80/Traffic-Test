package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kimjongoh80/Traffic-Test/internal/engine"
)

func sample() *engine.Result {
	r := &engine.Result{Params: engine.TestParams{Protocol: "udp", Streams: 2, DurationSec: 2, IntervalSec: 1, Length: 1460},
		Server: "10.0.0.2:5210", ServerHost: "pc-b", ClientHost: "pc-a", StartedAt: time.Now()}
	for i := 0; i < 2; i++ {
		s0 := engine.StreamInterval{Stream: 0, Bytes: 1000, BitsPerSec: 8000, Packets: 10, Lost: 1}
		s1 := engine.StreamInterval{Stream: 1, Bytes: 1000, BitsPerSec: 8000, Packets: 10}
		r.Intervals = append(r.Intervals, engine.Interval{Index: i, Start: float64(i), End: float64(i + 1),
			Streams: []engine.StreamInterval{s0, s1},
			Sum:     engine.StreamInterval{Stream: -1, Bytes: 2000, BitsPerSec: 16000, Packets: 20, Lost: 1, LossPct: 4.76},
			Latency: &engine.LatencyStats{Sent: 10, Received: 10, AvgMs: 1, MinMs: 0.5, MaxMs: 2, JitterMs: 0.1}})
	}
	r.Summary = engine.Summary{DurationSec: 2, Bytes: 4000, BitsPerSec: 16000, PacketsSent: 42, PacketsReceived: 40, Lost: 2,
		Streams: []engine.StreamInterval{{Stream: 0, Bytes: 2000}, {Stream: 1, Bytes: 2000}}}
	return r
}

func TestHTML(t *testing.T) {
	r := sample()
	c := engine.Criteria{MinBitsPerSec: 10000}
	var b bytes.Buffer
	if err := HTML(&b, Input{ID: "x", Result: r, Criteria: c, Verdict: engine.Evaluate(r, c), Version: "test"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"트래픽 스트레스 테스트 보고서", "PASS", "pc-a (Client) → pc-b (Server)", "<svg", "구간별 결과"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestCSV(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, sample()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if !strings.HasPrefix(lines[0], "\xef\xbb\xbf") {
		t.Error("missing UTF-8 BOM")
	}
	if len(lines) != 1+2*3 { // header + 2 intervals x (2 streams + SUM)
		t.Errorf("got %d lines", len(lines))
	}
}
